# Community Voting Feature — Implementation Workflow

## Overview

Add community voting so any Guest or Admin can start a vote to skip the
current song or prioritize a queued song. The Host is excluded from
voting because they already have direct controls. Vote sessions are
in-memory, time-limited (30 s), and require a strict majority of
connected users to pass.

Work through the tasks **in the order listed**. Each task states exactly
which file to create or modify and what the result must look like. Do not
skip ahead — later tasks depend on earlier ones compiling cleanly.

---

## Task 1 — Domain entity: `internal/domain/entity/vote.go` (CREATE)

Create this file from scratch. It must contain:

### 1a. Sentinel errors

```go
var (
    ErrAlreadyVoted       = errors.New("user has already voted in this session")
    ErrVoteSessionExpired = errors.New("vote session has expired")
    ErrVoteOnCurrentSong  = errors.New("cannot start a priority vote on the currently playing song")
    ErrSongNotFound       = errors.New("song not found at the given index")
)
```

### 1b. `VoteType` constants

```go
type VoteType string

const (
    VoteTypeSkip       VoteType = "skip"
    VoteTypePrioritize VoteType = "prioritize"
)
```

### 1c. `VoteSession` struct

Fields:
- `ID string` — unique key, format `"<voteType>:<songID>"`
- `Type VoteType`
- `SongID string`
- `SongTitle string`
- `SongIndex int` — position snapshot when session was created
- `VotedBy map[int]bool` — userID → true
- `Threshold int` — votes needed to pass
- `CreatedAt time.Time`
- `ExpiresAt time.Time`

All fields exported with `json` tags.

### 1d. `NewVoteSession` constructor

```go
func NewVoteSession(voteType VoteType, song Song, songIndex, threshold int, expiry time.Duration) *VoteSession
```

- Sets `ID` as `fmt.Sprintf("%s:%s", voteType, song.ID)`
- Initialises `VotedBy` as an empty map
- Sets `CreatedAt = time.Now()`, `ExpiresAt = now.Add(expiry)`

### 1e. Methods on `*VoteSession`

```
Cast(userID int) error
    — returns ErrVoteSessionExpired if IsExpired()
    — returns ErrAlreadyVoted if VotedBy[userID] is true
    — otherwise sets VotedBy[userID] = true

HasVoted(userID int) bool

VoteCount() int
    — returns len(VotedBy)

IsPassed() bool
    — returns VoteCount() >= Threshold

IsExpired() bool
    — returns time.Now().After(ExpiresAt)

RemainingSeconds() int
    — returns max(0, int(time.Until(ExpiresAt).Seconds()))
```

### 1f. `MajorityThreshold` free function

```go
func MajorityThreshold(connectedUsers int) int
```

- Returns `connectedUsers/2 + 1`
- Minimum return value is `2` (a single user must never self-pass a vote)

---

## Task 2 — Domain entity: patch `internal/domain/entity/activity.go` (MODIFY)

Add three new `ActivityType` constants to the existing `const` block:

```go
ActivityVoteCast    ActivityType = "vote_cast"    // a user cast a vote
ActivityVotePassed  ActivityType = "vote_passed"  // threshold reached
ActivityVoteExpired ActivityType = "vote_expired" // session timed out
```

---

## Task 3 — Usecase: `internal/usecase/vote/interactor.go` (CREATE)

Create the package `vote`. It must import only
`internal/domain/entity` and `internal/domain/repository`.
Do **not** import other usecase packages.

### 3a. `VoteOutcome` struct

```go
type VoteOutcome struct {
    Session *entity.VoteSession
    Passed  bool
}
```

### 3b. `Interactor` struct

```go
type Interactor struct {
    queueRepo repository.QueueRepository
    userRepo  repository.UserRepository
    mu        sync.Mutex
    sessions  map[string]*entity.VoteSession   // key = VoteSession.ID
    expiry    time.Duration
}
```

### 3c. `NewInteractor`

```go
func NewInteractor(
    queueRepo repository.QueueRepository,
    userRepo  repository.UserRepository,
    expiry    time.Duration,
) *Interactor
```

Default `expiry` to `30 * time.Second` when passed as `0`.

### 3d. `ConnectedCount() int` method on Hub (needed later — leave a TODO comment)

The interactor needs the connected user count at call time.
Receive it as a plain `int` parameter on each public method so the
interactor stays decoupled from the Hub.

### 3e. `CastSkipVote`

```go
func (i *Interactor) CastSkipVote(ctx context.Context, userID, connectedUsers int) (*VoteOutcome, error)
```

Steps (all under `i.mu.Lock()`):

1. `i.queueRepo.Load(ctx)` → fail fast on error
2. `queue.CurrentSong()` → fail fast; returns `ErrQueueEmpty` if nothing is playing
3. Build `sessionID = "skip:<currentSong.ID>"`
4. Call `i.castVote(...)` (internal, see 3g)
5. If `outcome.Passed`:
   - Call `queue.Next()` on the already-loaded queue
   - Save with `i.queueRepo.Save(ctx, queue)`
   - Log `ActivitySongSkipped` activity
6. Return `outcome`

Note: `queue.Next()` returns `ErrNoNextSong` when the current song is
the last one. In that case the skip should still "pass" but leave the
queue in its end state — do **not** propagate this as an HTTP error.
Set `queue.Status = StatusIdle` instead and save.

### 3f. `CastPriorityVote`

```go
func (i *Interactor) CastPriorityVote(ctx context.Context, userID, songIndex, connectedUsers int) (*VoteOutcome, error)
```

Steps (all under `i.mu.Lock()`):

1. `i.queueRepo.Load(ctx)` → fail fast
2. Validate `songIndex`: must be in range and **not** equal to `queue.CurrentIndex` (→ `ErrVoteOnCurrentSong`)
3. `targetSong = queue.Songs[songIndex]`
4. Build `sessionID = "prioritize:<targetSong.ID>"`
5. Call `i.castVote(...)`
6. If `outcome.Passed`:
   - Re-load the queue (queue may have changed during the vote window)
   - Call `i.resolveSongIndex(freshQueue, outcome.Session.SongID, outcome.Session.SongIndex)` to find current index
   - If the song is no longer in the queue (resolve returns -1) → return `outcome` with no action
   - Call `freshQueue.Prioritize(resolvedIndex)` — this is the same method used by the token system; vote-driven priority lands in the same slot (right after current song, after any already-prioritized songs)
   - Save with `i.queueRepo.Save(ctx, freshQueue)`
   - Log `ActivityPlayback` activity: `"vote prioritized \"<title>\""`
7. Return `outcome`

### 3g. `castVote` (private)

```go
func (i *Interactor) castVote(
    ctx         context.Context,
    sessionID   string,
    voteType    entity.VoteType,
    song        entity.Song,
    songIndex   int,
    userID      int,
    connectedUsers int,
) (*VoteOutcome, error)
```

Must be called with `i.mu` already held.

Steps:
1. Call `i.evictExpired(ctx)` first (lazy cleanup)
2. Look up `i.sessions[sessionID]`; if not found, create with `entity.NewVoteSession(...)` using `entity.MajorityThreshold(connectedUsers)` as threshold
3. Call `session.Cast(userID)` — propagate errors directly (`ErrAlreadyVoted`, `ErrVoteSessionExpired`)
4. Fetch display name for activity log: `i.displayName(ctx, userID)` (fall back to `"Unknown"`)
5. Log `ActivityVoteCast`: `"voted to <type> \"<title>\" (<count>/<threshold>)"`
6. Check `session.IsPassed()`:
   - If true: `delete(i.sessions, sessionID)`, log `ActivityVotePassed`
7. Return `&VoteOutcome{Session: session, Passed: passed}`

### 3h. `GetActiveSessions`

```go
func (i *Interactor) GetActiveSessions() []*entity.VoteSession
```

Lock, evict expired, return a slice of all remaining sessions.
Used to send active votes to clients on initial WS connect.

### 3i. `ExpireOldSessions`

```go
func (i *Interactor) ExpireOldSessions(ctx context.Context)
```

Lock, call `i.evictExpired(ctx)`.
Called on a ticker from the WS hub every 5 seconds.

### 3j. Private helpers

```
evictExpired(ctx context.Context)
    Must be called with i.mu held.
    For each expired session: log ActivityVoteExpired, delete from map.

resolveSongIndex(queue *entity.Queue, songID string, snapshotIndex int) int
    Fast path: if Songs[snapshotIndex].ID == songID, return snapshotIndex.
    Slow path: linear scan by ID.
    Return -1 if not found.

displayName(ctx context.Context, userID int) string
    Call i.userRepo.GetUserByID; return DisplayName or "Unknown" on error.
```

---

## Task 4 — Usecase tests: `internal/usecase/vote/interactor_test.go` (CREATE)

Write table-driven tests covering the following cases.
Follow the same mock pattern used in `internal/usecase/queue/interactor_test.go`
(local `mockQueueRepo` and `mockUserRepo` structs that implement the repository interfaces).

Required test cases:

**CastSkipVote**
- First vote creates a session, `Passed == false`
- Second vote joins the existing session (vote count increments)
- When threshold is reached, `Passed == true` and queue advances
- Duplicate vote from same user returns `ErrAlreadyVoted`
- Empty queue returns an error
- Passed session is removed from active sessions

**CastPriorityVote**
- First vote creates a session, `Passed == false`
- When threshold is reached, `Passed == true` and target song is at index `currentIndex+1`
- `songIndex == currentIndex` returns `ErrVoteOnCurrentSong`
- Out-of-range index returns `ErrSongNotFound`
- Duplicate vote returns `ErrAlreadyVoted`
- Skip and priority sessions for different songs coexist independently

**ExpireOldSessions**
- Sessions created with a negative expiry are removed on next call
- Live sessions are kept
- Expired sessions log an `ActivityVoteExpired` entry

**Activity logging**
- `CastSkipVote` always logs `ActivityVoteCast`
- A passing vote additionally logs `ActivityVotePassed`

---

## Task 5 — WebSocket events: patch `internal/delivery/ws/events.go` (MODIFY)

### 5a. Add two new event type constants

```go
EventVoteUpdated = "vote_updated"   // a vote was cast; send session state
EventVoteResolved = "vote_resolved" // a vote passed or expired; session closed
```

### 5b. Add two new data structs

```go
// VoteUpdatedData is broadcast after every vote cast so clients can
// show live vote counts without polling.
type VoteUpdatedData struct {
    Session  *entity.VoteSession `json:"session"`
    Activity entity.Activity     `json:"activity"`
}

// VoteResolvedData is broadcast when a session passes or expires.
type VoteResolvedData struct {
    SessionID string          `json:"session_id"`
    Outcome   string          `json:"outcome"`   // "passed" | "expired"
    Activity  entity.Activity `json:"activity"`
}
```

---

## Task 6 — WebSocket hub: patch `internal/delivery/ws/hub.go` (MODIFY)

### 6a. Add `ConnectedCount() int` method

```go
func (h *Hub) ConnectedCount() int {
    h.mu.Lock()
    defer h.mu.Unlock()
    return len(h.clients)
}
```

### 6b. Add vote expiry ticker to `Run()`

Add a `time.NewTicker(5 * time.Second)` case inside the `Run()` select loop.
The ticker needs a reference to the `vote.Interactor` — add it as a field
on `Hub` called `voteInteractor` with type `VoteExpiryRunner` (see below).

### 6c. Define `VoteExpiryRunner` interface in the ws package

```go
// VoteExpiryRunner is satisfied by vote.Interactor.
// Defined here to avoid an import cycle.
type VoteExpiryRunner interface {
    ExpireOldSessions(ctx context.Context)
}
```

### 6d. Add `SetVoteInteractor` method

```go
func (h *Hub) SetVoteInteractor(v VoteExpiryRunner) {
    h.voteInteractor = v
}
```

Call `h.SetVoteInteractor(voteInteractor)` in `main.go` after both are
constructed (see Task 9).

### 6e. Updated `Run()` ticker case

```go
case <-ticker.C:
    if h.voteInteractor != nil {
        h.voteInteractor.ExpireOldSessions(context.Background())
    }
```

### 6f. Patch `RegisterHandler` to include active vote sessions in full sync

After `h.SendFullSync(conn, state)`, also send any active vote sessions:

```go
if h.voteInteractor != nil {
    // send active sessions as individual vote_updated events so the
    // newly connected client sees any in-progress votes
}
```

This requires passing the `voteInteractor` as a concrete `*vote.Interactor`
(or adding `GetActiveSessions() []*entity.VoteSession` to the interface).
Extend `VoteExpiryRunner` to include this method:

```go
type VoteExpiryRunner interface {
    ExpireOldSessions(ctx context.Context)
    GetActiveSessions() []*entity.VoteSession
}
```

---

## Task 7 — HTTP handlers: patch `internal/delivery/http/handlers.go` (MODIFY)

### 7a. Add `vote *vote.Interactor` field to `Handlers`

```go
type Handlers struct {
    queue    *queue.Interactor
    auth     *auth.Interactor
    activity *activity.Interactor
    priority *priority.Interactor
    vote     *vote.Interactor          // NEW
    hub      *ws.Hub
}
```

Update `NewHandlers` signature to accept `*vote.Interactor`.

### 7b. Role guard helper (add as private function in the file)

```go
func canVote(role entity.Role) bool {
    return role == entity.RoleGuest || role == entity.RoleAdmin
}
```

### 7c. `HandleVoteSkip`

```
POST /api/vote/skip
Body: { "user_id": int, "user_role": string }
```

Steps:
1. Decode body; reject non-Guest/Admin roles with `403`
2. `connectedUsers := h.hub.ConnectedCount()`
3. `outcome, err := h.vote.CastSkipVote(ctx, req.UserID, connectedUsers)`
4. Map errors:
   - `ErrAlreadyVoted` → `409 Conflict`
   - `ErrQueueEmpty` → `422 Unprocessable Entity`
   - anything else → `500`
5. Broadcast `EventVoteUpdated` with the session state
6. If `outcome.Passed`:
   - Load fresh queue state with `h.queue.GetState(ctx)`
   - Broadcast `EventSongSkipped` (same shape as the direct-skip handler)
   - Broadcast `EventVoteResolved` with `Outcome: "passed"`
7. Return `204 No Content`

### 7d. `HandleVotePriority`

```
POST /api/vote/prioritize
Body: { "user_id": int, "user_role": string, "song_index": int }
```

Steps:
1. Decode body; reject non-Guest/Admin roles with `403`
2. `connectedUsers := h.hub.ConnectedCount()`
3. `outcome, err := h.vote.CastPriorityVote(ctx, req.UserID, req.SongIndex, connectedUsers)`
4. Map errors:
   - `ErrAlreadyVoted` → `409`
   - `ErrVoteOnCurrentSong` → `422`
   - `ErrSongNotFound` → `422`
   - anything else → `500`
5. Broadcast `EventVoteUpdated`
6. If `outcome.Passed`:
   - Load fresh state
   - Broadcast `EventSongPrioritized` (same shape as `HandlePrioritizeSong` but with `UserID: 0` to signal it was vote-driven)
   - Broadcast `EventVoteResolved` with `Outcome: "passed"`
7. Return `204 No Content`

---

## Task 8 — Route registration: patch `cmd/server/main.go` (MODIFY)

### 8a. Import the new usecase

```go
usecaseVote "local-music-queue/internal/usecase/vote"
```

### 8b. Construct the interactor

After the `priorityInteractor` line:

```go
voteInteractor := usecaseVote.NewInteractor(repo, userRepo, 0) // 0 = default 30s expiry
```

### 8c. Wire into Hub and Handlers

```go
hub.SetVoteInteractor(voteInteractor)
handlers := delivery.NewHandlers(qInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub)
```

### 8d. Register routes

```go
mux.HandleFunc("POST /api/vote/skip",       handlers.HandleVoteSkip)
mux.HandleFunc("POST /api/vote/prioritize", handlers.HandleVotePriority)
```

---

## Task 9 — Frontend API client: patch `frontend/src/services/api.js` (MODIFY)

Add two new methods to the `api` object:

```js
async castSkipVote(userID, userRole) {
  return this.request('/vote/skip', {
    method: 'POST',
    body: { user_id: userID, user_role: userRole }
  })
},

async castPriorityVote(userID, userRole, songIndex) {
  return this.request('/vote/prioritize', {
    method: 'POST',
    body: { user_id: userID, user_role: userRole, song_index: songIndex }
  })
},
```

---

## Task 10 — Frontend store: patch `frontend/src/store/index.js` (MODIFY)

### 10a. Add `voteSessionss` to the reactive state

```js
voteSessionss: {}   // key: session.id → session object
```

Note: `voteSessionss` (the store key) reflects that the backend
`VoteSession.ID` format is `"<type>:<songID>"` — it is a map, not an array,
so lookups are O(1).

### 10b. Add store methods

```js
upsertVoteSession(session) {
  this.voteSessions[session.id] = session
},

removeVoteSession(sessionID) {
  delete this.voteSessions[sessionID]
},

// Convenience: return skip session for a song ID, or null
skipSessionFor(songID) {
  return this.voteSessions[`skip:${songID}`] || null
},

// Convenience: return priority session for a song ID, or null
prioritySessionFor(songID) {
  return this.voteSessions[`prioritize:${songID}`] || null
},
```

Fix the typo: rename `voteSessionss` → `voteSessions` (double-s was a note above, use single `s`).

---

## Task 11 — Frontend WebSocket handler: patch `frontend/src/services/websocket.js` (MODIFY)

Add two new cases to the `handleMessage` switch:

```js
case 'vote_updated':
  globalStore.upsertVoteSession(message.data.session)
  globalStore.addActivity(message.data.activity)
  break

case 'vote_resolved':
  globalStore.removeVoteSession(message.data.session_id)
  globalStore.addActivity(message.data.activity)
  break
```

---

## Task 12 — Frontend component: `frontend/src/components/dashboard/VoteButton.vue` (CREATE)

A single reusable component used in both `NowPlaying.vue` and `QueueList.vue`.

### Props

```js
props: {
  voteType: String,       // 'skip' | 'prioritize'
  songID:   String,       // used to look up active session
  songIndex: Number,      // only used when voteType === 'prioritize'
  disabled: Boolean       // true when user is Host
}
```

### Computed

```js
session()    // globalStore.skipSessionFor(songID) or prioritySessionFor(songID)
hasVoted()   // session && session.voted_by[currentUser.id] === true
label()      // dynamic: "Vote to Skip", "Skip? 2/3", etc.
```

### Template structure (simplified)

```html
<button
  :disabled="disabled || hasVoted"
  @click="handleVote"
  class="vote-btn"
  :class="{ 'vote-btn--active': !!session, 'vote-btn--voted': hasVoted }"
>
  <span v-if="!session">
    {{ voteType === 'skip' ? 'Vote to Skip' : 'Vote to Prioritize' }}
  </span>
  <span v-else>
    {{ voteType === 'skip' ? 'Skip' : 'Bump' }}?
    {{ session.vote_count }}/{{ session.threshold }}
    <small>({{ session.remaining_seconds }}s)</small>
  </span>
</button>
```

Note: `vote_count`, `threshold`, `remaining_seconds` are the JSON-serialised
fields from `VoteSession` — verify the exact field names match the Go struct
`json` tags when wiring.

### `handleVote` method

```js
async handleVote() {
  const user = globalStore.currentUser
  try {
    if (this.voteType === 'skip') {
      await api.castSkipVote(user.id, user.role)
    } else {
      await api.castPriorityVote(user.id, user.role, this.songIndex)
    }
  } catch (err) {
    if (err.message.includes('409')) {
      // already voted — UI should have prevented this but handle gracefully
    } else {
      console.error('Vote failed:', err)
    }
  }
}
```

### Countdown timer

Add a `setInterval` in `onMounted` that decrements a local `countdown`
reactive value every second when `session` is active. Clear it in
`onUnmounted` and whenever `session` becomes null (watch the computed).
This avoids relying on the backend to push remaining-seconds updates
on every tick.

---

## Task 13 — Wire `VoteButton` into existing components (MODIFY)

### `frontend/src/components/dashboard/NowPlaying.vue`

- Import `VoteButton`
- Add `<VoteButton>` below the song title area:

```html
<VoteButton
  voteType="skip"
  :songID="currentSong.id"
  :disabled="currentUser.role === 'host'"
/>
```

Only render the button when `currentSong` exists.

### `frontend/src/components/dashboard/QueueList.vue`

- Import `VoteButton`
- In the `v-for` song row template, add a `VoteButton` per song:

```html
<VoteButton
  voteType="prioritize"
  :songID="song.id"
  :songIndex="queueIndexFor(song)"
  :disabled="currentUser.role === 'host'"
/>
```

`queueIndexFor(song)` must return the **full songs array index** (not the
"Up Next" slice index) because the backend endpoint expects the absolute
index in `queue.Songs`. Compute it as `store.queueState.current_index + 1 + upNextIndex`.

---

## Task 14 — Verify everything compiles and tests pass

```bash
# Backend
go build ./...
go test ./internal/domain/entity/... ./internal/usecase/vote/... -v

# Frontend
cd frontend
npm run build
npm run test:unit
```

Fix any type errors or import issues before considering the feature complete.

---

## Constraints and rules to follow throughout

1. **Do not modify `entity/queue.go` or `entity/song.go`** — the existing `Prioritize()` method is reused as-is. Vote-driven priority goes through the same code path as token priority.

2. **Vote sessions are never persisted** — they live only in the `Interactor.sessions` map. A server restart wipes them. This is intentional.

3. **The `vote.Interactor` must not import other usecase packages** — use repository interfaces only to stay within Clean Architecture boundaries.

4. **Threshold is computed once at session creation** using the connected-user count at that moment. Subsequent votes reuse the threshold even if users join or leave mid-vote. This avoids a moving-goalpost problem.

5. **The Host's direct skip and prioritize controls are untouched.** Do not add any role checks to the existing `HandleSkipSong` or `HandlePrioritizeSong` handlers.

6. **Error mapping in handlers must be explicit** — use `errors.Is()`, not string matching.

7. **All new Go code must have passing unit tests before moving to the next task.**
