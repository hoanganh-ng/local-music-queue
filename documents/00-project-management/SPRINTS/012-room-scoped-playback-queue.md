# R07 – Room‑scoped playback queue

**Status:** R07a closed 2026-06-29 (accepted by the Product Owner); R07b closed 2026-06-29 (accepted by the Product Owner). R07b (Room Queue WebSocket Sync and Deltas, this slice) added per-room WebSocket sync/delta broadcasts on top of R07a. See *Implementation summary (R07a)* and *Implementation summary (R07b)* below. Remaining R07 scope (relational queue rows, room-scoped reorder/vote endpoints, cross-process safety, migration of the legacy global `queue_state` into a room) remains split and deferred to subsequent slices.

**Sprint name:** Room‑scoped playback queue

## Goal

Persist the playback queue as a first‑class, room‑scoped entity and expose operations to manage it.  The queue should support adding tracks, removing tracks, reordering, and tracking up‑votes/down‑votes.  Changes must be persisted to PostgreSQL and broadcast to all connected clients via WebSocket events.  Concurrency must be handled gracefully so that users can collaborate without race conditions.

## Current behaviour (pre‑sprint baseline)

The current implementation does not maintain a durable queue.  Tracks are passed ad‑hoc from client to player, and there is no persistent ordering or metadata.  When a page reloads, the queue is lost, and only the currently playing track is known.  There is no API to inspect or modify the queue, and queue state is not broadcast to spectators.  This prevents co‑operative management of playback and makes it impossible to implement skip voting or other queue‑related features.

## Desired behaviour (post‑sprint)

* Introduce a `room_playback_queue` table or equivalent persistence layer with fields such as `id`, `room_id`, `track_uri` (or provider‑specific identifiers), `added_by_user_id`, `position`, and a vote tally (up‑votes / down‑votes).
* Expose REST endpoints for:
  * **Add track** – add a new track to the end of the queue.  Requires an active session token.  Validate that the track can be fetched/played by the room’s source (e.g. YouTube, Spotify) and record who added it.
  * **Remove track** – remove a track at a given queue position or by ID.  Allow only the user who added it or the current lease holder (host) to remove.  Reject if the track is currently playing.
  * **Reorder tracks** – move a track to a new position.  Only the lease holder may reorder the queue.  Validate that positions remain contiguous.
  * **Vote on track** – up‑vote or down‑vote a track.  Maintain a per‑user vote record to prevent duplicate votes and allow changing a vote from up to down.
* Emit WebSocket events whenever the queue changes.  Include the full queue state in a `queue_sync` message so clients can update their UIs.
* On room creation and on new client connections, send the current queue state alongside `player_state` and other metadata.
* Ensure concurrent modifications do not corrupt the queue.  Use optimistic locking or transactions at the repository layer to handle concurrent add/remove/reorder operations.  Resolve conflicts by retrying or returning a conflict error to the client.
* Add unit and integration tests covering all endpoints, including concurrent modifications and permission checks.

## Required context

* Review `ROOM_EPIC_SPRINT_SEQUENCE.md` for the overall plan and preceding sprints.
* The baseline domain entities and database setup, as defined in ADR 001 and ADR 002.
* The session token authentication layer from sprint R05 (session token auth) and the planned `PlayerLease` semantics from sprint R06.  Some operations may require an active lease (host) whereas others (add, vote) only require a valid session.
* Any existing queue or playback logic in the codebase.

## Requirements

1. **Database schema.**  Add a migration introducing `room_playback_queue` with appropriate fields and indexes.  Include a composite unique constraint on `(room_id, position)` to prevent duplicate positions.
2. **Domain model.**  Introduce a `QueueItem` domain type with fields matching the schema.  Update the service layer to encapsulate queue operations with proper business rules.
3. **API endpoints.**  Implement controller functions for add, remove, reorder, vote and read operations.  Apply middleware for session token resolution and permission checks.
4. **WebSocket integration.**  Extend the hub to broadcast queue updates to all clients in the room.  Define new event types such as `queue_updated` and `queue_sync`.
5. **Concurrency handling.**  Use database transactions or application‑level locks to ensure queue operations are atomic.  Return an HTTP `409 Conflict` on version mismatch and let clients retry.
6. **Validation and error handling.**  Validate track URIs, membership status, lease status and input positions.  Provide clear error messages for unauthorised or invalid requests.
7. **Testing.**  Write unit tests for the queue repository, service and controller.  Add integration tests demonstrating concurrent modifications and correct WebSocket messages.
8. **Documentation.**  Update API documentation and developer guides to cover the new endpoints and queue semantics.

## Out of scope

* Implementing actual audio playback or integration with streaming providers.  The queue stores URIs but does not fetch or play tracks.
* UI or client‑side changes beyond necessary WebSocket event handling.  Front‑end integration may be handled in a subsequent sprint.
* Search functionality for tracks or automatic playlist generation – that will be addressed in later sprints.
* Persisting vote history beyond tally counts (e.g. for analytics).

## Implementation guidance

* Keep queue logic within a dedicated service.  Do not embed database calls in HTTP controllers or WebSocket handlers.
* Use database constraints (unique on `(room_id, position)`) to enforce ordering invariants.  When reordering, update positions within a transaction to avoid gaps.
* Consider storing votes in a separate table keyed by `(queue_item_id, user_id)` to prevent duplicate votes and enable up/down switching.  Aggregate vote counts on read.
* Use optimistic locking (e.g. `row_version` column) if available, or check the highest position value before inserting a new item to avoid race conditions.
* Minimise coupling to the `PlayerLease` concept – queue operations should be valid regardless of whether a lease is currently held, except where explicitly restricted to the host (reorder, remove others’ tracks).

## Execution note

R07a and R07b have landed on `dev` and both are closed (Product Owner
accepted; see the per-slice implementation summaries below). A
post-acceptance review pass on R07b surfaced one remaining
sequence-ordering defect: a broadcast dispatched after the client was
inserted into the per-room hub's client map but before the initial sync
was stamped with a seq could carry a LOWER seq than the initial sync.
The fix centralises per-room seq allocation inside the `RoomWSHub.Run`
loop (the hub loop is the single owner of per-room seq allocation for
both the initial sync and room broadcasts); the new interleaving test
`TestRoomHub_InitialSyncOrderedBeforeInterleavedBroadcast` fails on
the pre-fix code and passes after. No changes to the global `/ws`
contract or global queue behaviour. The remaining R07 scope listed
under *Deferred to the rest of R07* is intentionally split into
subsequent slices; this document remains the authoritative entry point
for tracking that work and should be updated as each deferred slice
ships.

## Implementation summary (R07a)

R07a is the narrow first slice of R07. It establishes the per-room
queue persistence and the four room-scoped REST endpoints without
changing the existing global queue behavior, without introducing a
compatibility shim, and without emitting any new WebSocket events
(the global hub is unchanged in this slice).

**What landed:**
- Migration `0006_room_queue_state.up.sql` adds a single-row-per-room
  JSONB table keyed by `rooms.id`.
- `repository.RoomQueueRepository` + `PostgresRoomQueueRepository`:
  `Load(roomID)` returns `ErrRoomQueueNotFound` for empty rooms;
  `Save(roomID, queue)` upserts the JSONB document. Mirrors the
  existing global `queue_state` shape.
- `usecase/roomqueue.Interactor`: `GetState`, `AddSong`, `RemoveSong`,
  `ClearQueue` under a per-interactor mutex; reuses `entity.Queue`
  invariants (Add / Remove / Clear / ContainsSong). Actor identity
  flows from the bearer token only; `AddedBy` / `AddedByID` are set
  by the handler from the resolved user.
- HTTP routes behind `roomAuth` (bearer token):
  - `GET    /api/rooms/{slug}/queue`
  - `POST   /api/rooms/{slug}/queue/add`
  - `POST   /api/rooms/{slug}/queue/remove`
  - `POST   /api/rooms/{slug}/queue/clear`
- Permission model:
  - read: any active member
  - add: any active member
  - remove: host/admin may remove any song; guest may remove only
    their own upcoming song (`CurrentIndex < index` AND
    `AddedByID == actorUserID`)
  - clear: host/admin only
- No WebSocket events emitted from these routes. The global hub is
  unchanged. Per-room WS deltas are deferred to R08.
- The global `/api/queue/...` routes are untouched. There is no
  compatibility shim in this slice.
- Schema version after migration lands: **6**.

**Deferred to the rest of R07 (or later):**
- Per-room WebSocket events, per-room sequence numbers, and
  `/ws/rooms/{slug}` (R08).
- Per-song relational rows in place of the JSONB blob.
- Reorder endpoint and vote endpoint (room-scoped).
- Migration of the existing global `queue_state` into a room and
  the global compatibility shim / `410 Gone` cleanup (R14).
- Cross-process safety.

**Verification:**
- `go test -count=1 ./internal/domain/entity ./internal/usecase/queue ./internal/usecase/room ./internal/delivery/http ./internal/infrastructure/persistence ./cmd/server`
- `go test -race -count=1 ./internal/usecase/queue ./internal/usecase/room ./internal/delivery/http`
- `go build ./cmd/... ./internal/...`
- `go vet ./cmd/... ./internal/...`
- `git diff --check`

## Implementation summary (R07b)

R07b lands the per-room WebSocket sync and delta events on top of R07a.
R07b was closed 2026-06-29 and accepted by the Product Owner. A
post-acceptance review pass surfaced one remaining
sequence-ordering defect (see *Post-acceptance fix pass* below), which
is fixed on `dev` and reflected in the Verification section.

**What landed:**
- Four additive WebSocket events scoped to the per-room endpoint:
  - `room_queue_sync` (initial on connect)
  - `room_queue_song_added`
  - `room_queue_song_removed`
  - `room_queue_cleared`
- New route `GET /ws/rooms/{slug}?session_token=<opaque>` behind the
  existing A01 `ALLOWED_ORIGINS` policy (same `policy.AllowWebSocket`
  gate as the global `/ws` endpoint).
- Authentication: resolved session token only (R05). The legacy
  `?user_id=...` hint is ignored for identity (A01).
- Authorization: active room membership required. Non-members receive
  `403 Forbidden`; archived rooms return `409 Conflict`; missing or
  invalid session tokens return `401 Unauthorized`.
- Initial sync runs inside the hub loop, so the per-room seq
  allocation is linearised with broadcast dispatch. The hub loop is
  the single owner of per-room seq allocation for both the initial
  sync and room broadcasts (see *Post-acceptance fix pass*).
- Per-room sequence numbers, per-room client maps, and a per-room
  broadcaster dispatcher with goroutine fan-out (mirrors the global
  hub's room_archived ticker deadlock fix in R06).
- `usecase/roomqueue.Broadcaster` seam keeps the use case independent
  of `delivery/ws`. The room queue handlers are the only call sites
  for the broadcaster; the interactor itself does not broadcast.
- Per-room ping keepalive (R07b follow-up). Without a ping, idle
  per-room clients fall off after 60s.

**Post-acceptance fix pass (2026-06-29):** Review found that
`dispatch()` was calling `nextSeq()` synchronously outside the hub
loop, racing with `sendInitialSync()` (which calls `nextSeq()` inside
the loop). A broadcast dispatched after the new client was inserted
into `h.clients` but before the initial sync was stamped could carry a
LOWER seq than the initial sync, violating the sequenced-delta
contract. The fix moves per-room seq allocation into the `Run` loop
for both the initial sync and broadcasts; `dispatch()` no longer
allocates a seq, it just enqueues `(roomSlug, msgType, data)`. The
internal `roomBroadcast` envelope now carries that pair instead of a
pre-sequenced `BroadcastMessage`. A new test
`TestRoomHub_InitialSyncOrderedBeforeInterleavedBroadcast` fails on
the pre-fix code and passes after the fix. Ping keepalive and
register/sync ordering are intact; no changes to the global `/ws`
contract or global queue behaviour.

**Renumbering note:** The original R08 placeholder ("Public read-only
API and CORS") referenced in `ROOM_EPIC_SPRINT_SEQUENCE.md` is
deferred and renumberable. R07b is the narrow second slice of R07
(per-room WS), not the original R08 scope. The R08 row in the
sequence table remains "Planned" pending renumber.

**Single-process caveat:** All sequencing is single-process / in-memory.
No cross-process broadcast ordering is provided. Clients in a
horizontally-scaled deployment would need a separate solution.

**Out of scope (carried forward to subsequent slices):**
- Relational queue rows (per-song) — JSONB blob remains the storage shape.
- Reorder endpoint and vote endpoint (room-scoped).
- Migration of the legacy global `queue_state` into a room and the
  global compatibility shim / `410 Gone` cleanup.
- Cross-process broadcast safety.
- Public unauthenticated room API or broad CORS redesign beyond
  reusing the existing A01 origin policy.

**Verification:**
- `go test -count=1 ./internal/usecase/roomqueue ./internal/delivery/http ./internal/delivery/ws ./cmd/server`
- `go test -race -count=1 ./internal/usecase/roomqueue ./internal/delivery/http ./internal/delivery/ws`
- `go build ./cmd/... ./internal/...`
- `go vet ./cmd/... ./internal/...`
- `git diff --check`

## Implementation summary (R07c)

R07c is the frontend narrow wiring slice for R07a/R07b. It does not
change any backend contract or handler; it consumes the per-room REST
endpoints and the per-room WebSocket event inventory as already
shipped and accepted.

**What landed:**
- Four thin API methods in `frontend/src/services/api.js` that reuse
  the existing bearer-token path: `getRoomQueue`, `addRoomSong`,
  `removeRoomSong`, `clearRoomQueue`. None of them send
  client-supplied identity fields (`added_by`, `added_by_id`,
  `requested_by`, `user_id`, `user_role`).
- A dedicated `frontend/src/services/room-websocket.js` module that
  opens `GET /ws/rooms/{slug}?session_token=<opaque>`, tracks the
  per-room sequence number, dispatches only the four documented room
  event types (`room_queue_sync`, `room_queue_song_added`,
  `room_queue_song_removed`, `room_queue_cleared`), and surfaces a
  seq-gap via an `onGap` callback. The client **never** sends any
  frame to the room hub; frames are ignored by the backend per
  R07b. The raw room WS URL and the raw token are never logged.
- An isolated `globalStore.roomQueues` slice (slug-keyed) in
  `frontend/src/store/index.js` with dedicated mutators
  (`setRoomQueueState`, `applyRoomSongAdded`,
  `applyRoomSongRemoved`, `applyRoomQueueCleared`,
  `clearRoomQueueState`, plus connection/seq/error/recovery
  bookkeeping). Room events **never** mutate the global
  `queueState`, `currentUser`, `voteSessions`, or `autoQueueConfig`.
- An authenticated `/rooms/:slug` route registered in
  `frontend/src/router/index.js` (existing `requiresAuth` guard).
- A minimal `frontend/src/views/RoomView.vue` that:
  - Performs a REST seed fetch on mount, then opens the room WS.
  - Renders the room queue, an add-by-URL control, a per-row
    remove button, and a clear control.
  - Recovers from a sequence gap by REST-refetching
    `GET /api/rooms/{slug}/queue` and applying the authoritative
    state.
  - Surfaces backend 401/403/404/409 responses as clear per-status
    messages through the toast channel.
  - Disconnects the room WS and clears per-slug state on unmount.
  - Omits playback, vote, priority, auto-queue, invite, member,
    and player-lease UI per the R07c scope.

**Out of scope (unchanged from R07a/R07b):**
- Backend routes, event payloads, or hub internals.
- Room-scoped playback controls.
- Room-scoped voting/priority.
- Auto-queue room scoping.
- Relational queue rows.
- Global compatibility shim or 410 cleanup.
- Public unauthenticated room API.
- Full room discovery/invite/member/Welcome UX.
- Cross-process safety.

**Verification (reported when implementation finishes):**
- `cd frontend && npm run test:unit -- --run` — PASS
- `cd frontend && npm run build` — PASS
- `git diff --check` — PASS
