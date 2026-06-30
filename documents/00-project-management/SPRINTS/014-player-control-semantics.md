# R09 – Player control semantics

**Status:** R09 split into R09a, R09b (vote-to-skip), and R09c+ (volume/prev/room auto-queue/full device integration — still deferred). R09a was implemented on `dev` and accepted by the Product Owner (2026-06-30) (see *Implementation summary (R09a)*, *Closure*). R09b was implemented on `dev` and accepted by the Product Owner (2026-06-30) (see *Implementation summary (R09b)*, *Closure*). The remaining slices (volume, prev, room auto-queue, full media-player device integration) are deferred to future sprints.

**Sprint name:** Player control semantics

## Goal

Define and implement the semantics for controlling playback within a room.  Users must be able to play, pause, skip and adjust playback attributes (e.g. volume).  Control commands should respect the current `PlayerLease` holder while still allowing democratic actions such as vote‑to‑skip.  The sprint aims to provide clear rules, APIs and WebSocket events so that clients can build consistent user interfaces.

## Current behaviour (pre‑sprint baseline)

At present, there are no explicit APIs or event types for controlling playback.  The player automatically progresses through tracks as they are provided, and there is no way to pause or skip ahead.  Because there is no lease concept implemented yet (this depends on R06), there is also no authoritative controller.  As a result, clients have no reliable method to alter playback state.

## Desired behaviour (post‑sprint)

* **Control commands**: Implement REST and/or WebSocket commands for the following actions:
  * **Play/Pause** – toggle playback state.  Only the active lease holder may directly issue this command.
  * **Skip** – move to the next track in the queue.  Users who are not the lease holder may request a skip, but it will only occur once a configurable number or percentage of room members vote in favour (vote‑to‑skip).  The lease holder can skip unilaterally.
  * **Previous** – optional: return to the previous track if available.  This may be restricted to the lease holder.
  * **Volume change** – adjust playback volume.  Only the lease holder may set the volume directly.  Other users may set local client volume but not global volume.
* **Vote‑to‑skip mechanism**: Implement a mechanism to tally skip votes from non‑lease members.  When the threshold is reached (e.g. more than 50 % of connected members), automatically send a skip command on behalf of the room.  Reset the vote count after each skip or if the track changes by other means.
* **Authorisation**: Tie direct control commands to the `PlayerLease` concept introduced in R06.  If no lease exists, treat the first control command as an implicit claim (subject to R06 rules) or reject the command.
* **Events**: Emit WebSocket events to all clients when playback state changes (play → pause, track skipped, volume changed).  Include relevant metadata (e.g. track index, new state).
* **Error handling**: Return clear error messages when unauthorised users attempt control actions, or when actions cannot be performed (e.g. no track to skip).

## Required context

* The `PlayerLease` mechanism defined in sprint R06.
* The persistent queue and vote tracking from sprint R07.
* The session token auth layer from R05.
* Understanding of the underlying player implementation to know how to trigger play, pause, skip and volume changes.

## Requirements

1. **API endpoints and WebSocket commands.**  Design HTTP endpoints or WebSocket messages for play, pause, skip, previous and volume adjustments.  Document each command’s parameters and response shape.
2. **Vote‑to‑skip logic.**  Implement a service to record skip votes from members.  The service should track votes per track and reset on track change.  Parameterise the threshold either by absolute count or percentage of active members.
3. **Lease enforcement.**  Check that the caller holds the current `PlayerLease` before executing direct commands (play, pause, previous, volume).  For skip, check for either lease or sufficient votes.
4. **State broadcast.**  Upon any control action, broadcast a `player_state_changed` event via WebSocket to all room participants.  Ensure that clients can update their UI accordingly.
5. **Tests.**  Add unit tests for vote tallying and command routing.  Write integration tests simulating multiple clients voting to skip and verifying that the skip occurs when the threshold is reached.
6. **Documentation.**  Update API documentation with control semantics, including error cases and authorisation requirements.

## Out of scope

* Implementing a GUI or mobile UI for control – only server‑side semantics and events are covered here.
* Playback device integration (e.g. controlling Spotify or YouTube players) – assume that an internal player service exists and can respond to commands.
* Persisting playback history or analytics.

## Implementation guidance

* Choose one channel (REST or WebSocket) as the primary control path.  WebSocket commands may provide lower latency, but REST endpoints offer simplicity for clients.  You can support both by translating HTTP requests into internal commands emitted on the WebSocket bus.
* Represent the vote‑to‑skip threshold in configuration (e.g. `SKIP_VOTE_THRESHOLD_PERCENT=50`).  For small rooms, consider a minimum number of votes to avoid one person skipping in a two‑person room.
* Store skip votes in a transient in‑memory data structure keyed by `(room_id, track_id)`.  No long‑term persistence is required; votes reset when the track changes or when the player is paused for an extended period.
* Use idempotent command handlers: if two identical play commands arrive back‑to‑back, ensure that the state remains consistent.

## Execution note

This stub serves as a high‑level plan for the **player control semantics** sprint.  When the sprint is taken up, refine the requirements, consult with stakeholders on skip vote thresholds and authorisation rules, and update this document with the final implementation details.  Upon completion, mark the sprint as **closed** in this document and in `ROOM_EPIC_SPRINT_SEQUENCE.md`.
## Implementation summary (R09a)

R09a is the first narrow slice of R09: lease-aware **direct** playback
control (status / sync / skip / ended) for the active lease holder
only. Vote-to-skip, volume, prev, auto-queue, and full device
integration are deferred to R09b+ slices and intentionally NOT in
this implementation. R09a follows the same narrow-slice pattern as
R07a/R07b/R07c/R07d: additive per-room REST mutations + matching
per-room WebSocket deltas + minimal frontend control, with no changes
to global queue behavior, global `/ws` contract, auth, Docker, or
deployment.

**What landed:**
- `room.PlaybackLeaseAuthorizer` interface in
  `internal/usecase/room/interactor.go`, implemented by
  `PlayerLeaseInteractor.RequireActiveLeaseHolder(ctx, slug, actorUserID)`
  — a read-only counterpart of `Heartbeat` that does NOT renew the
  lease (a sync call should not indefinitely extend the lease).
- `internal/domain/entity/queue.go`: additive helpers
  `(*Queue).SetStatus`, `(*Queue).SetElapsed`, `(*Queue).AdvanceToNext`
  plus sentinels `ErrNoCurrentSong`, `ErrInvalidStatus`,
  `ErrInvalidElapsed`. `AdvanceToNext` deliberately checks
  no-next-song BEFORE mutating so a skip on the last song does not
  partially mutate state (the global `queue.Next` mutates Status then
  returns ErrNoNextSong; R09a avoids that footgun).
- `internal/usecase/roomqueue.Interactor`: four new methods
  (`SetPlaybackStatus`, `SyncPlaybackElapsed`, `SkipPlayback`,
  `PlaybackEnded`) plus a `leaseAuthorizer` field wired via
  `SetLeaseAuthorizer`. The `Broadcaster` interface gains three new
  methods (`BroadcastRoomPlaybackStatusChanged`,
  `BroadcastRoomPlaybackElapsedSync`,
  `BroadcastRoomPlaybackSongAdvanced`).
- `internal/delivery/ws/events.go`: three new constants
  (`EventRoomPlaybackStatusChanged`, `EventRoomPlaybackElapsedSync`,
  `EventRoomPlaybackSongAdvanced`) and three matching payload structs.
  The 16-event global `/ws` inventory is unchanged.
- `internal/delivery/ws/room_hub.go`: three new
  `BroadcastRoomPlayback*` methods on `*RoomWSHub` that follow the
  exact dispatch pattern as the R07b/R07d broadcasters. The hub loop
  remains the sole owner of per-room seq allocation (`dispatch` does
  NOT allocate seq); the R07b interleaving invariant
  (initial-sync seq < delta seq) is preserved automatically.
- `internal/delivery/http/room_queue_handlers.go`: four new
  handlers (`HandleSetRoomPlaybackStatus`, `HandleSyncRoomPlayback`,
  `HandleSkipRoomPlayback`, `HandleRoomSongEnded`) with the documented
  status mapping (204 on success; 400/401/403/404/409/410 on
  validation / auth / archived / lease-gone). `writeRoomQueueError`
  extended with the R09a sentinels.
- `cmd/server/main.go`: registers four routes
  (`POST /api/rooms/{slug}/playback/{status,sync,skip,ended}`) behind
  `roomAuth`, and wires `roomQueueInteractor.SetLeaseAuthorizer(playerLeaseInteractor)`.
- `frontend/src/services/api.js`: four new methods
  (`api.setRoomPlaybackStatus`, `api.syncRoomPlayback`,
  `api.skipRoomPlayback`, `api.roomSongEnded`).
- `frontend/src/store/index.js`: three new isolated mutators on
  `globalStore.roomQueues[slug]` (`applyRoomPlaybackStatusChanged`,
  `applyRoomPlaybackElapsedSync`, `applyRoomPlaybackSongAdvanced`).
  Each prefers `payload.state` when present; the fallback path stamps
  the relevant fields without touching global queueState, currentUser,
  voteSessions, or autoQueueConfig.
- `frontend/src/views/RoomView.vue`: handles the three new room WS
  event types in `applyMessage`; adds a minimal Play/Pause/Skip/Ended
  control panel with per-status toast mapping (400/401/403/404/409/410)
  mirroring the R07d prioritize handler. UI gates are convenience
  only; the backend is authoritative.

**Request shapes:**
- `POST /api/rooms/{slug}/playback/status` body `{"status":"playing"}`
  or `{"status":"paused"}`. `idle` / empty / unknown status → 400.
- `POST /api/rooms/{slug}/playback/sync` body
  `{"elapsed": <non-negative int>}`. Missing `elapsed` (nil pointer)
  → 400; negative → 400.
- `POST /api/rooms/{slug}/playback/skip` empty body. No next song →
  400, queue is NOT partially mutated.
- `POST /api/rooms/{slug}/playback/ended` empty body. If next song
  exists, advance + `room_playback_song_advanced reason="ended"`. If
  no next song, persist paused end-of-queue state and broadcast
  `room_playback_status_changed`.

**Authorisation (use-case layer):**
- Active room (else 409 archived / 404 not-found)
- Active membership (else 403)
- Current active player-lease holder (else 404 missing lease / 403
  non-holder / 410 lease past grace / 409 archived room)

The use-case layer rejects invalid requests / lease failures BEFORE
the broadcaster fires; no broadcast on failed validation or
authorisation.

**Verification:**
- `go test -count=1 ./internal/usecase/roomqueue ./internal/usecase/room ./internal/delivery/http ./internal/delivery/ws ./cmd/server` — PASS
- `go test -race -count=1 ./internal/usecase/roomqueue ./internal/usecase/room ./internal/delivery/http ./internal/delivery/ws` — PASS
- `go build ./cmd/... ./internal/...` — PASS
- `go vet ./cmd/... ./internal/...` — PASS
- `git diff --check` — PASS

**Closure:** R09a was implemented on `dev` and accepted by the Product Owner (2026-06-30).
The implementation follows the R07d narrow-slice contract and explicitly defers
vote-to-skip, volume, prev, room auto-queue, and full media-player
device integration to subsequent R09b+ slices. Global
`/api/queue/...` is untouched, no priority balance spending, no
relational queue rows, no mutation of the global `queueState` /
`currentUser` / `voteSessions` / `autoQueueConfig` from room events,
and per-room seq allocation remains hub-loop owned (`dispatch()` does
not allocate seq).

## Implementation summary (R09b)

R09b is the **backend + WebSocket** slice for room-scoped vote-to-skip (no frontend UI; matches R09a's scope). It adds one HTTP mutation and three additive per-room WebSocket events without changing global queue behavior, global vote thresholds, priority balances, or auto-queue.

### HTTP

- `POST /api/rooms/{slug}/vote/skip` — behind `makeRoomActor` (bearer token). Empty body (or `{}`); server resolves `actor_user_id` from `actorFromCtx`. Any active room member (host / admin / guest) may cast one vote for the current song. Wire shape (e.g. body-supplied `user_id`) is ignored.

### Vote session state machine

- In-memory map keyed `skip:{roomSlug}:{songID}` lives in `internal/usecase/roomvote.Interactor`.
- First vote creates a session; subsequent votes within the 30s window add the user to `VotedBy` (`map[int]bool`).
- Threshold captured at session creation via `(*RoomWSHub).UniqueConnectedUserIDs(slug)` (de-duplicates by `userID`, so multi-conn same user = one vote weight) using `max(2, n/2 + 1)` strict majority with 2-voter minimum.
- When `vote_count >= threshold`: session is deleted and the queue advances to the next song via `(*roomqueue.Interactor).SkipVote(ctx, slug, expectedSongID)` — a narrow, **lease-bypassing** method that runs under the existing roomqueue mutex, validates `queue.Songs[queue.CurrentIndex].ID == expectedSongID` before any mutation, then calls `entity.Queue.AdvanceToNext` (which itself refuses to mutate when no next song exists).
- When a 30s session expires without passing, it is deleted and broadcast as `room_vote_resolved outcome="expired"`. Any subsequent vote on the same (room, song) starts a fresh session.
- Sessions are never persisted to Postgres.

### Per-room WebSocket events

- `room_vote_updated` — broadcast on every successful vote cast. Payload: `{room_slug, session, actor_user_id, state}`.
- `room_vote_resolved` — broadcast when a session is deleted. Payload: `{room_slug, session_id, outcome ("passed"|"expired"), state}`. `outcome="passed"` is followed by the existing `room_playback_song_advanced` event with `reason="skip"`.
- The global `/ws` 16-event inventory is unchanged; these events ride `GET /ws/rooms/{slug}` only.

**Payload sanitisation:** The session embedded in the `room_vote_updated` payload is serialised through a `RoomVoteSessionDTO` that omits `voted_by`. The internal `map[int]bool` of voter IDs stays in-memory only and is NEVER broadcast on the wire; clients receive only the aggregate counters (e.g. `vote_count`, `threshold`, session metadata). `voted_by` is never sent to clients.

### Resolution invariants

- `SkipVote` checks `expectedSongID == queue.Songs[queue.CurrentIndex].ID` BEFORE mutating. On mismatch (stale session — song already advanced by another path) the interactor returns `ErrStaleSkipVote` and the queue is NOT mutated, no `room_playback_song_advanced` is broadcast.
- `entity.Queue.AdvanceToNext` is unchanged; it still refuses to mutate when no next song exists.
- On resolved-but-no-next-song, `SkipVote` returns `ErrNoNextSong` and broadcasts `room_vote_resolved outcome="passed"` with the unchanged queue state — the existing lease-only skip path already handles no-next-song; the vote path simply surfaces it cleanly.

### Out of scope (explicit non-goals)

- No global `/api/vote/skip` change (R05 contract preserved).
- No priority-balance spend.
- No auto-queue trigger.
- No Docker/CORS/auth/session design changes.
- No frontend UI; the existing global VoteSkip UI is untouched.

**Closure:** R09b was implemented on `dev` and accepted by the Product Owner (2026-06-30). The implementation follows the R09a narrow-slice contract: one additive room-scoped mutation (`POST /api/rooms/{slug}/vote/skip`), two additive per-room WebSocket deltas (`room_vote_updated`, `room_vote_resolved`), and a server-owned `expiryAdapter` ticker that fans out `room_vote_resolved outcome="expired"` on `ExpireSessions` — the use case stays free of any `delivery/ws` import. The closure pass tightened five narrow invariants on `dev`: (1) the wiring order in `cmd/server/main.go` now constructs `roomvote.Interactor` after `*ws.RoomWSHub` is assigned, removing the typed-nil-resolver panic risk; (2) the threshold rule is strict-majority `max(2, n/2 + 1)` (the prior `max(2, n/2)` produced a tie, rejected by the Product Owner); (3) the WS payload is sanitised through `RoomVoteSessionDTO` so `voted_by` is never broadcast; (4) `entity.ErrNoCurrentSong` (empty queue) maps to `400 Bad Request` rather than `404`; (5) the `.gitignore` rule `server` was tightened to `/server` so the build binary stays ignored without swallowing `cmd/server/expiry_adapter_test.go`. Global `/api/queue/...` is untouched, no priority balance spending, no relational queue rows, no mutation of the global `queueState` / `currentUser` / `voteSessions` / `autoQueueConfig` from room events, no auto-queue trigger, and per-room seq allocation remains hub-loop owned (`dispatch()` does not allocate seq).

## Implementation summary (R09c)

R09c is the **first narrow slice** of the R09c+ bucket (volume, prev, room auto-queue, full device integration). It adds **one backend-only HTTP command + one additive per-room WebSocket event + a narrow frontend control surface** for room-scoped volume. Vote-to-sskip, previous, room auto-queue, and full media-player device integration are intentionally NOT in this implementation. Volume is command-only: there is no `entity.Queue.Volume` field, no `room_queue_state` schema change, and the command does not call `queueRepo.Save`. R09c follows the R07d/R09a/R09b narrow-slice pattern: additive per-room REST mutation + matching per-room WebSocket delta + minimal frontend control, with no changes to global queue behavior, global `/ws` contract, auth, Docker, or deployment.

**What landed:**
- `internal/usecase/roomqueue/interactor.go`: new sentinel `ErrInvalidDirection`; new method `(*Interactor).ChangePlaybackVolume(ctx, slug, actorUserID, direction)` that resolves the active room + enforces lease-holder via `requirePlaybackLease` + returns `ErrInvalidDirection` for any direction not equal to `"up"` or `"down"`. The method does NOT load or save queue state and does NOT require a current song. The `Broadcaster` interface gains `BroadcastRoomPlaybackVolumeChanged(roomSlug, direction string)`.
- `internal/delivery/http/room_queue_handlers.go`: new handler `HandleChangeRoomPlaybackVolume`; new request body type `roomQueuePlaybackVolumeReq`; `writeRoomQueueError` extended with the `ErrInvalidDirection` → 400 mapping.
- `internal/delivery/ws/events.go`: new constant `EventRoomPlaybackVolumeChanged = "room_playback_volume_changed"`; new payload struct `RoomPlaybackVolumeChangedData { RoomSlug, Direction }`. The 16-event global `/ws` inventory is unchanged.
- `internal/delivery/ws/room_hub.go`: new method `(*RoomWSHub).BroadcastRoomPlaybackVolumeChanged` following the existing dispatch pattern (no seq allocation in dispatch; hub loop stamps seq on dequeue).
- `cmd/server/main.go`: registers `POST /api/rooms/{slug}/playback/volume` behind `roomAuth`.
- `frontend/src/services/api.js`: new method `api.changeRoomPlaybackVolume(slug, direction)`.
- `frontend/src/views/RoomView.vue`: small `Vol +` / `Vol −` row in the existing playback panel, gated on `canMutate` (UI convenience only; backend is authoritative). Adds a `room_playback_volume_changed` WS listener that surfaces a low-priority info toast — no store mutation, since volume is not persisted.

**Request shape:** `POST /api/rooms/{slug}/playback/volume` body `{"direction":"up"}` or `{"direction":"down"}`.

**Authorisation (use-case layer):**
- Active room (else 409 archived / 404 not-found)
- Active player-lease holder (else 404 missing lease / 403 non-holder / 410 lease past grace / 409 archived room)

**No persistence:** there is no `entity.Queue.Volume` field, no `room_queue_state` schema change, and the command does not call `queueRepo.Save`. The `changePlaybackVolume` method returns immediately after validation + lease check; the broadcaster delivers the event to subscribed room clients.

**Verification:** (filled in by the implementer before committing this summary)
- `go test -count=1 ./internal/usecase/roomqueue ./internal/delivery/http ./internal/delivery/ws ./cmd/server` — PASS
- `go test -race -count=1 ./internal/usecase/roomqueue ./internal/delivery/ws` — PASS
- `go vet ./cmd/... ./internal/...` — PASS
- `git diff --check` — PASS
- `cd frontend && npm run test:unit -- --run` — PASS
- `cd frontend && npm run build` — clean

**Closure:** R09c is implemented on `dev` (2026-06-30); full Product Owner acceptance is pending. The implementation follows the R07d/R09a/R09b narrow-slice contract: one additive room-scoped command (`POST /api/rooms/{slug}/playback/volume`), one additive per-room WebSocket delta (`room_playback_volume_changed`), and a minimal frontend Vol± control surface gated on the existing lease-holder UI affordance. Volume is intentionally NOT persisted; the global `/api/queue/volume` contract, global `/ws` 16-event inventory, voting, priority balances, auto-queue, Docker, CORS, and auth/session design are all untouched.
