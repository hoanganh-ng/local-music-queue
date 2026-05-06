# Auto-Queue (Radio Mode) — Implementation Workflow

> **Claude Code instructions**: Work through each phase in order. Complete all checklist items
> and pass all tests in a phase before moving to the next. Never skip a phase.
> Run `go test ./...` after every file change.

---

## Context & Goal

Add a **Radio Mode** to `local-music-queue`: when the queue drops to exactly 1 song,
the system automatically fetches 1 related YouTube song (via yt-dlp radio mix) and appends it.
If no human adds songs, auto-queue keeps firing — endless radio.

**Trigger condition**: `len(queue) == 1 AND autoqueue.enabled == true`  
**Song source**: yt-dlp YouTube radio mix of the last queued song → pick 1 candidate,
skip songs already in queue or in recent play history.  
**Fallback**: random pick from `play_history` if yt-dlp fails.  
**Actor**: a virtual system user `system:autoqueue` (no token cost, no OAuth).

---

## Repo Map (read before starting)

```
internal/
  domain/          ← entities + repository interfaces (no external deps)
  usecase/         ← business logic, orchestrates domain + infra interfaces
  infrastructure/  ← SQLite repos, yt-dlp wrapper, external calls
  delivery/        ← HTTP handlers, WebSocket hub
frontend/src/      ← Vue 3 components
```

> **Rule**: domain imports nothing. Usecase imports domain only.
> Infrastructure and delivery import usecase + domain.

---

## Phase 0 — Exploration (read-only, no changes)

**Goal**: understand existing patterns before writing anything.

- [ ] Read `internal/domain/` — list all entities and repository interfaces.
      Note how `Song` is defined, what fields it has, what `AddedBy` looks like.
- [ ] Read `internal/usecase/queue_usecase.go` — find the methods that
      dequeue or advance songs (skip, natural end). Note their exact signatures.
- [ ] Read `internal/infrastructure/` — find the yt-dlp wrapper. Note how
      commands are invoked and how results are parsed.
- [ ] Read `internal/delivery/websocket.go` (or equivalent) — find how events
      are broadcast. Note the event envelope struct.
- [ ] Read `internal/delivery/` HTTP handlers — find one POST endpoint as a
      pattern to follow for the new toggle endpoint.
- [ ] Check the SQLite migration files. Note the naming convention and where
      they live.

**Done when**: you can answer these without re-reading:
1. What struct represents a song in the queue?
2. Which usecase method runs after a song is removed?
3. How does the WS hub broadcast a typed event?

---

## Phase 1 — Database Schema

**Files to create/modify**:
- `internal/infrastructure/migrations/XXXX_auto_queue.sql` *(new)*

### 1.1 Write migration

```sql
-- auto_queue_config: single-row config table (upsert on id=1)
CREATE TABLE IF NOT EXISTS auto_queue_config (
    id       INTEGER PRIMARY KEY CHECK (id = 1),
    enabled  INTEGER NOT NULL DEFAULT 0,
    strategy TEXT    NOT NULL DEFAULT 'related'
);
INSERT OR IGNORE INTO auto_queue_config (id, enabled, strategy) VALUES (1, 0, 'related');

-- play_history: tracks last N played video IDs for dedup + fallback
CREATE TABLE IF NOT EXISTS play_history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id   TEXT     NOT NULL,
    title      TEXT     NOT NULL,
    played_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_play_history_played_at ON play_history(played_at DESC);
```

### 1.2 Enforce history cap (trigger or cleanup)

Add an `AFTER INSERT` trigger on `play_history` that deletes rows older than the
50 most recent:

```sql
CREATE TRIGGER IF NOT EXISTS trg_play_history_cap
AFTER INSERT ON play_history
BEGIN
    DELETE FROM play_history
    WHERE id NOT IN (
        SELECT id FROM play_history ORDER BY played_at DESC LIMIT 50
    );
END;
```

### 1.3 Tests

- [ ] Create `internal/infrastructure/migrations/migration_test.go`
- [ ] Test: apply migration on a fresh in-memory SQLite → assert both tables exist.
- [ ] Test: insert 51 rows into `play_history` → assert only 50 remain after trigger.

**Done when**: `go test ./internal/infrastructure/migrations/...` passes.

---

## Phase 2 — Domain Layer

**Files to create**:
- `internal/domain/auto_queue.go` *(new)*

### 2.1 Entities

```go
package domain

// AutoQueueConfig holds the persisted settings for radio mode.
type AutoQueueConfig struct {
    Enabled  bool
    Strategy AutoQueueStrategy
}

type AutoQueueStrategy string

const (
    StrategyRelated       AutoQueueStrategy = "related"
    StrategyHistoryRandom AutoQueueStrategy = "history_random"
)

// PlayHistoryEntry is a record of a song that has been played.
type PlayHistoryEntry struct {
    VideoID  string
    Title    string
    PlayedAt time.Time
}
```

### 2.2 Repository interfaces

```go
// AutoQueueRepository persists config and play history.
type AutoQueueRepository interface {
    GetConfig(ctx context.Context) (*AutoQueueConfig, error)
    SaveConfig(ctx context.Context, cfg AutoQueueConfig) error

    AppendHistory(ctx context.Context, entry PlayHistoryEntry) error
    // GetRecentHistory returns up to `limit` entries, newest first.
    GetRecentHistory(ctx context.Context, limit int) ([]PlayHistoryEntry, error)
}

// RelatedSongFetcher fetches one candidate song related to a given video.
// exclude is a list of videoIDs already in queue or recently played.
type RelatedSongFetcher interface {
    FetchRelated(ctx context.Context, videoID string, exclude []string) (*Song, error)
}
```

> **Note**: `RelatedSongFetcher` lives in domain as an interface so usecase can
> depend on it without importing infrastructure.

### 2.3 Extend `Song` (only if `AddedBy` doesn't already exist)

If `Song` has no `AddedBy` field, add:

```go
const SystemUserID = "system:autoqueue"
```

If `Song` already has `AddedBy string`, just add the constant above.

### 2.4 Tests

- [ ] Create `internal/domain/auto_queue_test.go`
- [ ] Test: `AutoQueueConfig` default values are correct.
- [ ] Test: `StrategyRelated` and `StrategyHistoryRandom` constants match
      the SQL default string `"related"`.

**Done when**: `go test ./internal/domain/...` passes.

---

## Phase 3 — Infrastructure Layer

**Files to create**:
- `internal/infrastructure/auto_queue_repo.go` *(new)*
- `internal/infrastructure/yt_related_fetcher.go` *(new)*
- `internal/infrastructure/auto_queue_repo_test.go` *(new)*
- `internal/infrastructure/yt_related_fetcher_test.go` *(new)*

### 3.1 `AutoQueueRepository` implementation

Implement `domain.AutoQueueRepository` on SQLite.

Key details:
- `GetConfig`: `SELECT enabled, strategy FROM auto_queue_config WHERE id=1`
- `SaveConfig`: `INSERT OR REPLACE INTO auto_queue_config ...`
- `AppendHistory`: `INSERT INTO play_history (video_id, title) VALUES (?, ?)`
  — trigger handles cap automatically.
- `GetRecentHistory`: `SELECT video_id, title, played_at FROM play_history ORDER BY played_at DESC LIMIT ?`

### 3.2 `RelatedSongFetcher` implementation

```go
type YtDlpRelatedFetcher struct {
    // MaxCandidates is how many radio-mix results to fetch (default 10)
    MaxCandidates int
}

// FetchRelated uses yt-dlp's YouTube radio mix to get candidates.
// Filters out IDs in exclude, returns the first non-excluded one (random order).
func (f *YtDlpRelatedFetcher) FetchRelated(
    ctx context.Context, videoID string, exclude []string,
) (*domain.Song, error)
```

yt-dlp command to run:

```bash
yt-dlp \
  --flat-playlist \
  --dump-single-json \
  --playlist-end 10 \
  "https://www.youtube.com/watch?v={videoID}&list=RD{videoID}"
```

Parse the JSON output. Shuffle the `entries` slice. Return the first entry whose
`id` is not in `exclude`. Populate a `domain.Song` with:
- `VideoID` = entry `id`
- `Title` = entry `title`
- `AddedBy` = `domain.SystemUserID`
- `AddedAt` = `time.Now()`

**Error handling**:
- Command exits non-zero → return `ErrFetchFailed` (define this sentinel).
- All candidates excluded → return `ErrAllCandidatesExcluded`.
- Caller (`AutoQueueUsecase`) handles these by falling back to history.

### 3.3 Tests

**`auto_queue_repo_test.go`** — use in-memory SQLite:
- [ ] `GetConfig` on fresh DB returns default `{Enabled: false, Strategy: "related"}`.
- [ ] `SaveConfig(enabled=true)` → `GetConfig` returns `enabled=true`.
- [ ] `AppendHistory` adds a row; `GetRecentHistory(1)` returns it.
- [ ] Inserting 51 history entries → `GetRecentHistory(100)` returns exactly 50.

**`yt_related_fetcher_test.go`** — mock the exec.Command call:
- [ ] Mock returns valid JSON with 3 entries, none excluded → returns first entry as `*Song`.
- [ ] Mock returns valid JSON with all entries in `exclude` → returns `ErrAllCandidatesExcluded`.
- [ ] Mock exits non-zero → returns `ErrFetchFailed`.
- [ ] `AddedBy` on returned song equals `domain.SystemUserID`.

> **How to mock exec.Command**: use the standard Go test-helper pattern —
> inject a `commandRunner func(name string, args ...string) *exec.Cmd` field
> on `YtDlpRelatedFetcher`, default to `exec.Command`, override in tests.

**Done when**: `go test ./internal/infrastructure/...` passes.

---

## Phase 4 — Usecase Layer

**Files to create**:
- `internal/usecase/auto_queue_usecase.go` *(new)*
- `internal/usecase/auto_queue_usecase_test.go` *(new)*

### 4.1 Struct

```go
type AutoQueueUsecase struct {
    autoQueueRepo domain.AutoQueueRepository
    queueRepo     domain.QueueRepository
    fetcher       domain.RelatedSongFetcher
    activityUC    ActivityUsecase        // reuse existing
    mu            sync.Mutex
    triggering    bool                   // debounce flag
}

func NewAutoQueueUsecase(
    autoQueueRepo domain.AutoQueueRepository,
    queueRepo     domain.QueueRepository,
    fetcher       domain.RelatedSongFetcher,
    activityUC    ActivityUsecase,
) *AutoQueueUsecase
```

### 4.2 Core method: `CheckAndTrigger`

```
func (uc *AutoQueueUsecase) CheckAndTrigger(ctx context.Context) error
```

Algorithm:

```
1. Lock mu. If triggering==true, return nil (already in flight).
2. Set triggering=true. Defer: set triggering=false, unlock mu.
3. cfg = autoQueueRepo.GetConfig()
4. If !cfg.Enabled → return nil.
5. queue = queueRepo.GetQueue()
6. If len(queue) != 1 → return nil.
7. lastSong = queue[0]
8. recentIDs = autoQueueRepo.GetRecentHistory(20) → extract video IDs
9. inQueueIDs = extract video IDs from queue
10. exclude = union(recentIDs, inQueueIDs)
11. song, err = fetcher.FetchRelated(ctx, lastSong.VideoID, exclude)
12. If err == ErrFetchFailed || err == ErrAllCandidatesExcluded:
        song = fallbackFromHistory(ctx)  ← see 4.3
13. If song == nil → log warning, return nil (fail silently).
14. queueRepo.AddSong(ctx, song)
15. autoQueueRepo.AppendHistory(ctx, PlayHistoryEntry{...lastSong})
16. activityUC.Log(ctx, ActivityAutoQueueAdded{Song: song, SourceSong: lastSong})
17. return nil
```

### 4.3 Fallback: `fallbackFromHistory`

```
func (uc *AutoQueueUsecase) fallbackFromHistory(ctx context.Context) *domain.Song
```

- Get `GetRecentHistory(50)`.
- Filter out any ID already in current queue.
- Shuffle, return first match as a `domain.Song`.
- Return nil if history is empty or all filtered.

### 4.4 Hook into existing `QueueUsecase`

Find the method(s) that run after a song is removed (skip, advance). Add at the
end of each:

```go
go func() {
    if err := uc.autoQueueUC.CheckAndTrigger(context.Background()); err != nil {
        log.Printf("auto-queue: %v", err)
    }
}()
```

Run in a goroutine so it doesn't block the skip/advance response.

### 4.5 Config methods

```go
func (uc *AutoQueueUsecase) GetConfig(ctx context.Context) (*domain.AutoQueueConfig, error)
func (uc *AutoQueueUsecase) SetEnabled(ctx context.Context, enabled bool) error
```

`SetEnabled` reads config, flips `Enabled`, writes back.

### 4.6 Tests

Use mock implementations of all dependencies (write minimal mocks inline in the
test file, not a mocking library).

- [ ] `CheckAndTrigger` when `enabled=false` → `queueRepo.AddSong` never called.
- [ ] `CheckAndTrigger` when `enabled=true`, `len(queue)==3` → `AddSong` never called.
- [ ] `CheckAndTrigger` when `enabled=true`, `len(queue)==1`, fetcher returns song
      → `AddSong` called once with correct song, `AppendHistory` called once.
- [ ] `CheckAndTrigger` when fetcher returns `ErrFetchFailed` and history has entries
      → `AddSong` called once with a song from history.
- [ ] `CheckAndTrigger` when fetcher returns `ErrFetchFailed` and history is empty
      → `AddSong` never called, no error returned (silent fail).
- [ ] `CheckAndTrigger` called concurrently 5 times → `AddSong` called at most once
      (debounce/mutex test). Use `sync.WaitGroup`.
- [ ] `SetEnabled(true)` → `GetConfig` returns `Enabled=true`.
- [ ] Added song has `AddedBy == domain.SystemUserID`.

**Done when**: `go test ./internal/usecase/...` passes.

---

## Phase 5 — Delivery Layer

**Files to create/modify**:
- `internal/delivery/auto_queue_handler.go` *(new)*
- `internal/delivery/websocket.go` *(modify — add new event type)*
- `internal/delivery/router.go` (or equivalent) *(modify — register new routes)*

### 5.1 REST endpoints

```
POST /api/autoqueue/toggle
  Body:    { "enabled": true | false }
  Auth:    Host or Admin only
  Success: 200 { "enabled": true }
  Error:   403 if Guest role

GET /api/autoqueue/status
  Auth:    any authenticated user
  Success: 200 { "enabled": bool, "strategy": string }
```

Follow the exact same handler pattern used by existing endpoints.

### 5.2 WebSocket event

Define a new event type constant (follow existing naming convention):

```go
const EventAutoQueueAdded = "auto_queue_added"
```

Payload:

```json
{
  "type": "auto_queue_added",
  "song": { ...existing Song fields... },
  "source_song_title": "Title of the song that triggered it"
}
```

Broadcast this event from `AutoQueueUsecase.CheckAndTrigger` step 16, after
`activityUC.Log`. Inject the WS hub into `AutoQueueUsecase` if not already
accessible, or emit via the activity log system if it already fans out to WS.

### 5.3 Tests

- [ ] `POST /api/autoqueue/toggle` with Host token → 200.
- [ ] `POST /api/autoqueue/toggle` with Guest token → 403.
- [ ] `GET /api/autoqueue/status` returns correct `enabled` value after toggle.
- [ ] WS event `auto_queue_added` is received by a connected client after
      `CheckAndTrigger` runs successfully. *(Use the existing WS test client
      pattern if one exists.)*

**Done when**: `go test ./internal/delivery/...` passes.

---

## Phase 6 — Wire Everything (Dependency Injection)

**Files to modify**:
- `main.go` or wherever the DI container / app bootstrap lives.

- [ ] Instantiate `SQLiteAutoQueueRepository` with the existing DB handle.
- [ ] Instantiate `YtDlpRelatedFetcher{MaxCandidates: 10}`.
- [ ] Instantiate `AutoQueueUsecase` with all deps.
- [ ] Inject `autoQueueUC` into the existing `QueueUsecase`.
- [ ] Register the two new HTTP routes.
- [ ] Run `go build ./...` — zero errors.

**Done when**: `go build ./...` succeeds and `go test ./...` still passes.

---

## Phase 7 — Frontend

**Files to create/modify** (all under `frontend/src/`):

### 7.1 API client additions

In the existing API module (e.g. `api.ts` or `services/queue.ts`):

```typescript
export const getAutoQueueStatus  = () => api.get<AutoQueueStatus>('/autoqueue/status')
export const setAutoQueueEnabled = (enabled: boolean) =>
  api.post<AutoQueueStatus>('/autoqueue/toggle', { enabled })
```

Add type:

```typescript
export interface AutoQueueStatus {
  enabled: boolean
  strategy: 'related' | 'history_random'
}
```

### 7.2 Radio Mode toggle (Host/Admin only)

In the queue controls component:

- Add a toggle button: **"📻 Radio Mode"**.
- Disabled + tooltip "Host/Admin only" when user is Guest.
- On click: call `setAutoQueueEnabled(!current)`, update local state optimistically.
- Show a subtle active indicator (e.g. animated pulse dot) when enabled.

### 7.3 Queue item badge

In the song/queue-item component:
- If `song.addedBy === 'system:autoqueue'`, show a small badge: `⚡ auto`.
- Style it differently from user-added songs (muted color, smaller font).

### 7.4 Activity feed entry

In the activity feed component:
- Handle new WS event type `auto_queue_added`.
- Display: `🤖 Auto-Queue added "{song.title}" (from "{sourceSongTitle}")`.
- Use the same timestamp + formatting as existing activity entries.

### 7.5 Frontend tests (Vitest / Vue Test Utils)

- [ ] Toggle button renders for Host, is disabled for Guest.
- [ ] Clicking toggle calls `setAutoQueueEnabled` with correct arg.
- [ ] Song with `addedBy: 'system:autoqueue'` shows `⚡ auto` badge.
- [ ] Song with a real user `addedBy` does NOT show the badge.
- [ ] `auto_queue_added` WS event appends correct entry to activity feed.

**Done when**: `npm run test` passes.

---

## Phase 8 — Integration & Manual QA

### 8.1 Integration test (Go)

Create `integration/auto_queue_integration_test.go`:

- [ ] Full stack test with real SQLite (temp file), real `QueueUsecase`, mocked
      `YtDlpRelatedFetcher` (return a fixed song).
- [ ] Scenario A — Auto-queue fires:
  1. Enable auto-queue via `AutoQueueUsecase.SetEnabled(true)`.
  2. Add 2 songs to queue.
  3. Simulate skip of song 1 (calls `QueueUsecase.Skip()`).
  4. Queue now has 1 song → assert auto-queue song was added (queue len == 2).
  5. Simulate skip again → assert another auto-queue song added (queue len == 2 again).
- [ ] Scenario B — Auto-queue off:
  1. `SetEnabled(false)`.
  2. Add 2 songs, skip one.
  3. Queue len == 1 → assert no auto-add (queue len still 1).
- [ ] Scenario C — Fallback path:
  1. `SetEnabled(true)`.
  2. Fetcher always returns `ErrFetchFailed`.
  3. Seed `play_history` with 3 entries.
  4. Skip to 1-song queue → assert a fallback song was added from history.

### 8.2 Manual QA checklist

- [ ] Start the server. Enable Radio Mode via the toggle.
- [ ] Add 3 songs. Let them play (or skip) down to 1.
      Assert: a new song appears automatically.
- [ ] Keep skipping. Assert: songs keep appearing with `⚡ auto` badge.
- [ ] Activity feed shows `🤖 Auto-Queue added ...` entries.
- [ ] Disable Radio Mode. Skip to 1 song. Assert: no new song added.
- [ ] Re-enable. Assert: resumes on next skip.
- [ ] Two clients connected. Client B sees `auto_queue_added` WS event.
- [ ] Guest user cannot toggle Radio Mode (button disabled / 403 on API).

---

## Phase 9 — Cleanup & Docs

- [ ] Add `## Radio Mode (Auto-Queue)` section to `README.md` or `documents/`.
      Explain: what it is, how to enable, which role can toggle it.
- [ ] Check all new exported symbols have Go doc comments.
- [ ] Run `go vet ./...` — zero warnings.
- [ ] Run `golangci-lint run` if the project uses it.
- [ ] Delete any TODO / debug log statements added during development.
- [ ] Final `go test ./...` — all green.

---

## File Checklist (new files only)

```
internal/infrastructure/migrations/XXXX_auto_queue.sql
internal/domain/auto_queue.go
internal/domain/auto_queue_test.go
internal/infrastructure/auto_queue_repo.go
internal/infrastructure/auto_queue_repo_test.go
internal/infrastructure/yt_related_fetcher.go
internal/infrastructure/yt_related_fetcher_test.go
internal/usecase/auto_queue_usecase.go
internal/usecase/auto_queue_usecase_test.go
internal/delivery/auto_queue_handler.go
integration/auto_queue_integration_test.go
frontend/src/composables/useAutoQueue.ts  (or equivalent)
```

Modified files:
```
internal/domain/song.go                   (add SystemUserID const)
internal/usecase/queue_usecase.go         (hook CheckAndTrigger after skip/advance)
internal/delivery/websocket.go            (add EventAutoQueueAdded)
internal/delivery/router.go               (register 2 new routes)
main.go                                   (DI wiring)
frontend/src/components/QueueControls.*   (radio toggle button)
frontend/src/components/QueueItem.*       (auto badge)
frontend/src/components/ActivityFeed.*    (auto-queue event)
frontend/src/services/api.ts              (new API calls)
```

---

## Definition of Done

The feature is complete when:

1. `go test ./...` — all tests pass, no skipped tests.
2. `npm run test` — all frontend tests pass.
3. Integration test Scenarios A, B, C all pass.
4. Manual QA checklist fully checked.
5. `go build ./...` and `go vet ./...` — zero errors/warnings.
6. README updated.
