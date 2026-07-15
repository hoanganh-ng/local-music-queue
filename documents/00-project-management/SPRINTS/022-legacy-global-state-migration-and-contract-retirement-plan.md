# R14a — Legacy Global State Migration and Contract Retirement Plan

**Status:** Proposed (planning/design — documentation only). No Go runtime code, no Vue runtime code, no migrations, no routes, no WebSocket constants, and no config changes are introduced by this sprint. R14a narrows and precedes the legacy R14 stub (`019-global-contract-cleanup-and-documentation.md`); it shapes the implementation-ready contract for R14b → R14c → R14d → R14e and records R05b (frontend entry UI) and R09h (vote-prioritize parity) as blocking prerequisites. R14a supersedes parts of ADR 001 via the new ADR 003 (Proposed, pending Architect review and Product Owner acceptance). R13 (auth/session/authorization hardening), R10f+ (deferred room lifecycle hardening), R11b+ (deferred R11 chat slice), and R12 (search and room discovery) remain out of scope.

**Sprint name:** Legacy global state migration and contract retirement plan (planning/design only)

**Parent epic:** Issue #17 (legacy global contract cleanup). This sprint addresses the same constraint set that #17 records; #20 is the authoritative spec.

> **R14a review-revision summary (2026-07-15).** This sprint doc was revised after the Architect's review of the first R14a draft. The corrections visible throughout this document: (1) the cutover is split — R14b builds + verifies the offline mechanism (with a new `cmd/room-cutover` CLI and a new `room_cutover_marker` contract), R14c owns the coordinated production execution with a binary that has *disabled* (unregistered) global write paths; (2) the room-cutover mechanism does NOT reuse R03's `cmd/migrate-data` + `migration_marker` — those are preserved untouched; (3) `users` / `user_sessions` / `priority_transactions` are explicitly NOT recopied at cutover (they stay account-scoped); (4) the missing room entry / player-lease frontend UX (R05b) is added as a blocking prerequisite for R14c; (5) the `room_play_history` id-collision handling is settled via a legacy-id offset recorded in the marker; (6) live room-activity read parity is explicitly surfaced as a PO acceptance blocker (not in R14a scope); (7) the schema version stays v8 throughout R14a–R14d and the rollback window; v9 lands ONLY if R14e accepts (destructive cleanup completes); historical migration files are preserved on disk; (8) the `410 Gone` envelope drops the concrete migrated-room `successor` value; (9) the JSON conversion contract is rewritten to the real semantic shape (`encoding/json` tagged-field round-trip on `entity.Queue`, no `MarshalJSON` / `UnmarshalJSON` claim); (10) the queue-prioritize / vote-prioritize routes are separated (only the latter needs R09h); (11) contradictory active-sprint statements are removed by the `active.md` revision; (12) the open-items list is split into PO acceptance blockers vs settled items; (13) rollback / client-continuity claims are replaced by a deployment compatibility matrix.

## Goal

Produce an implementation-ready contract for converting the legacy global state into exactly one Product-Owner-named room and for retiring the parallel global REST/WS contracts. The sprint removes ambiguity before any destructive migration or compatibility removal is implemented. It separates data conversion, frontend entry UX prerequisites, room cutover coordination, frontend cutover, final contract removal, and vote-prioritize parity into independently reviewable future sprints. Authentication/authorization hardening remains a separate R13 concern.

## Non-goals

- No Go runtime code, no Vue runtime code, no PostgreSQL migration in this sprint.
- No HTTP route additions or removals; no WebSocket event additions or removals; no payload changes.
- No schema version bump in R14a (stays v8; v9 is R14e and only if it accepts).
- No Docker, Nginx, TLS, environment, dependency, or deployment changes.
- No automatic destructive startup migration (mirrors the offline posture of R03's `cmd/migrate-data` but uses a NEW dedicated CLI; see § Compatibility & endpoint retirement phases).
- No R13 redesign; no R10f+ retention/audit/revocation/archived-room cleanup; no R11b+ chat slice; no R12 search.
- No multi-instance / cross-process safety claims (single-instance only).
- No new WebSocket event constant for vote-to-prioritize (reuses the existing `room_queue_song_prioritized` event in R09h).
- No reuse of the R03 `cmd/migrate-data` CLI or the R03 `migration_marker` table for the room cutover.
- No creation of a permanent `main`, `room 0`, hidden default room, writable global fallback, or synthesized default-room `/ws`.

## Out of scope (explicit)

- Runtime Go/Vue/SQL/Docker changes.
- Executing or implementing the migration.
- Dropping global tables or code.
- R13 session/authentication/authorization redesign.
- R10f+ retention, audit-read, per-room token revocation, archived-room cleanup.
- R11b+ chat enhancements.
- R12 search and public room discovery.
- Relational per-song queue storage.
- Cross-process/multi-instance coordination.
- Live room-activity read parity (a `RoomView` activity panel) — see § PO acceptance blockers.
- R05b room entry / player-lease UI work.
- R09h vote-to-prioritize parity work.
- R14b / R14c / R14d / R14e work.
- New public API versioning unless required solely to express the retirement contract.

## Current behavior (pre-R14a baseline)

The verified `dev` baseline after accepted R11a has two parallel product models.

### Legacy global persistence (still live)

- `queue_state` (singleton, `id = 1`, TEXT JSON blob). Migration `internal/infrastructure/persistence/migrations/postgres/0001_initial.up.sql:7-11`. Repository `QueueRepository` (`internal/domain/repository/queue_repository.go:9-14`). Implementation `*PostgresRepository` (`internal/infrastructure/persistence/postgres_repository.go:20`, `Save :38`, `Load :58`). Wired in `cmd/server/main.go:621` → `queueRepo :142` → `usecaseQueue.NewInteractor(queueRepo, ytService) :181`.
- `activities` (append-only). Same migration, lines `:13-19`. Same `QueueRepository` interface (`AddActivity`, `GetActivities` at `queue_repository.go:12-13`). Implementation `AddActivity :77`, `GetActivities :94` in `postgres_repository.go`. Wired via `actInteractor := usecaseActivity.NewInteractor(queueRepo)` at `main.go:183`. Live consumers: `usecase/vote/interactor.go` (AddActivity ×5), `usecase/priority/interactor.go:81`, `usecase/queue/interactor.go` (AddActivity ×many, `GetActivities :300`), `delivery/http/handlers.go:92`.
- `auto_queue_config` (singleton, `id = 1`, with seed row). Migration `0001_initial.up.sql:52-60` (seed `:58-60`). Repository `domain.AutoQueueRepository` (`internal/domain/auto_queue.go:30-37`). Implementation `*PostgresAutoQueueRepository` (`internal/infrastructure/persistence/postgres_auto_queue_repo.go:21`, constructor `:27`, `GetConfig :32`, `SaveConfig :53`). Wired `pgAutoQueue := persistence.NewPostgresAutoQueueRepository(db)` at `main.go:623` → `autoQueueRepo :142` → `usecaseAutoQueue.NewInteractor(autoQueueRepo, queueRepo, ytRelatedFetcher) :189`.
- `play_history` (50-row cap, Go-enforced). Migration `0001_initial.up.sql:62-70` (index `:69`). Same `AutoQueueRepository` (`AppendHistory :34`, `GetRecentHistory :36`). Same impl (`AppendHistory :69`, `GetRecentHistory :103`; `PlayHistoryCap :11`).

All four sources are **live** — none are dead. They are read/written by non-test code paths wired through `main.go`.

### Room persistence (target side)

| Concern | Table | Migration | Repo interface | Repo impl | main.go wiring |
| --- | --- | --- | --- | --- | --- |
| Rooms/members/invites | `rooms`, `room_members`, `room_invites` | `0004_rooms.up.sql:8/19/34` (+ one-host index `:31`) | `repository.RoomRepository` `room_repository.go:21-110` | `postgres_room_repository.go:22` | `:146`, `:190` |
| Room queue | `room_queue_state` (JSONB) | `0006_room_queue_state.up.sql:11-15` | `repository.RoomQueueRepository` `room_queue_repository.go:22-32` | `postgres_room_queue_repository.go:27` | `:147`, `:191` |
| Room auto-queue config | `room_auto_queue_config` | `0007_room_auto_queue.up.sql:15-20` | `domain.RoomAutoQueueRepository` `room_auto_queue.go:40-57` | `postgres_room_auto_queue_repository.go:31` | `:299`, `:300` |
| Room play history | `room_play_history` | `0007_room_auto_queue.up.sql:22-33` (same migration; no separate file) | SAME `RoomAutoQueueRepository` | SAME impl | same |
| Player leases | `player_leases` | `0005_player_leases.up.sql:8-16` (one-active index `:19`) | `repository.PlayerLeaseRepository` `player_lease_repository.go:18-45` | `postgres_player_lease_repository.go:40` | `:202` |
| Room chat | `room_chat_messages` | `0008_room_chat_messages.up.sql:9-15` | `repository.RoomChatMessageRepository` `room_chat_message_repository.go:15-25` | `postgres_room_chat_message_repository.go:26` | `:199`, `:200` |

**Critical:** `room_activities` **does NOT exist**. `grep -rn "room_activities\|RoomActivit"` returns zero matches across `*.go`, `*.sql`, `*.js`. There is no room-scoped activity table, migration, entity, or repository. The global `activities` table has no room target today; R14a designs one, and live read parity is documented as a PO acceptance blocker.

Type mismatch: `queue_state.data` is **TEXT**; `room_queue_state.data` is **JSONB**. Both serialize the same `*entity.Queue` aggregate. `entity.Queue` uses default `encoding/json` tagged-field marshalling — there are NO custom `MarshalJSON` / `UnmarshalJSON` methods on the type (a claim the first R14a draft incorrectly made; see review-revision note above). The conversion contract uses the default `encoding/json` round-trip on tagged fields.

Migrations embedded via `//go:embed migrations/postgres/*.sql` (`migrations_postgres.go:14-15`), run at startup by `RunEmbeddedMigrationsUp` (`migrator.go:23`, called `main.go:169`).

### REST routes (`cmd/server/main.go`)

**Global routes registered under `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...`, plus auth and a few utilities** — handlers in `internal/delivery/http/handlers.go` (registered at `main.go:354–409`) and `internal/delivery/http/autoqueue_handler.go`:

| Route | main.go | Room equivalent (or status) |
| --- | --- | --- |
| `POST /api/auth/google` | `:389` | n/a (auth — R13) |
| `POST /api/auth` (Deprecated) | `:390` | n/a (auth — R13) |
| `GET /api/queue` | `:391` | `GET /api/rooms/{slug}/queue` |
| `POST /api/queue/add` | `:392` | `POST /api/rooms/{slug}/queue/add` |
| `POST /api/queue/skip` | `:393` | `POST /api/rooms/{slug}/playback/skip` |
| `POST /api/queue/status` | `:394` | `POST /api/rooms/{slug}/playback/status` |
| `POST /api/queue/sync` | `:395` | `POST /api/rooms/{slug}/playback/sync` |
| `POST /api/queue/ended` | `:396` | `POST /api/rooms/{slug}/playback/ended` |
| `POST /api/queue/prev` | `:397` | `POST /api/rooms/{slug}/playback/prev` |
| `POST /api/queue/remove` | `:398` | `POST /api/rooms/{slug}/queue/remove` |
| `POST /api/queue/clear` | `:399` | `POST /api/rooms/{slug}/queue/clear` |
| `POST /api/queue/volume` | `:400` | `POST /api/rooms/{slug}/playback/volume` |
| `POST /api/queue/prioritize` | `:401` | `POST /api/rooms/{slug}/queue/prioritize` (R07d) — **queue mutation; retires in R14c with no parity sprint** |
| `GET /api/user/priority-balance` | `:402` | OUT — account-scoped per ADR 001 §3 Decision 9 |
| `GET /api/youtube/search` | `:403` | OUT — global utility |
| `POST /api/vote/skip` | `:404` | `POST /api/rooms/{slug}/vote/skip` — **vote mutation; retires in R14c with no parity sprint** |
| `POST /api/vote/prioritize` | `:405` | `POST /api/rooms/{slug}/vote/prioritize` — **vote-driven queue mutation; NOT YET BUILT; BLOCKED on R09h before R14c may retire it** |
| `POST /api/autoqueue/toggle` | `:408` | `POST /api/rooms/{slug}/autoqueue/toggle` |
| `GET /api/autoqueue/status` | `:409` | `GET /api/rooms/{slug}/autoqueue/status` |

**Room routes under `/api/rooms/{slug}/...`** — `room_handlers.go` (`:356`), `room_queue_handlers.go` (`:201`), `room_vote_handlers.go` (`:338`), `room_auto_queue_handlers.go` (`:562`), `room_chat_handlers.go` (`:357`):

- Rooms/members/invites (`:413–474`): `POST /api/rooms`, `GET /api/rooms`, `GET .../{slug}`, `GET .../members`, `POST .../members/{userId}/promote`, `.../demote`, `POST .../invites`, `GET .../invites`, `DELETE .../invites/{inviteId}`, `POST /api/invites/{token}/redeem`.
- Player lease (`:477–486`): `.../player/claim`, `.../heartbeat`, `.../release`, `GET .../player/lease`.
- Room queue (`:489–501`): `GET .../queue`, `.../queue/add`, `.../queue/remove`, `.../queue/clear`, `.../queue/prioritize`.
- Room playback (`:526–548`): `.../playback/status`, `.../playback/sync`, `.../playback/skip`, `.../playback/ended`, `.../playback/volume`, `.../playback/prev`.
- Room vote (`:551`): `.../vote/skip`.
- Room auto-queue (`:563, :566`): `GET .../autoqueue/status`, `POST .../autoqueue/toggle`.
- Room chat (`:580, :583`): `GET .../chat/messages`, `POST .../chat/messages`.

The `roomAuth` middleware (`makeRoomActor(authInteractor)` at `main.go:413`, def `:633`) wraps every room route.

**Room entry / lease frontend methods are MISSING from the SPA.** The backend routes (`POST /api/rooms`, `GET /api/rooms`, `GET /api/rooms/{slug}`, `POST /api/invites/{token}/redeem`, the player-lease quartet) exist on the server, but there are NO `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / `api.redeemInvite` / `api.claimPlayerLease` / `api.heartbeatPlayerLease` / `api.releasePlayerLease` / `api.getPlayerLease` client methods in `frontend/src/services/api.js`. Rooms are entered only by direct URL (`/rooms/:slug`). This is a **blocking prerequisite for R14c** and is captured as the R05b sprint in § Future sprint sequence.

### WebSocket endpoints

**Global `/ws`** — `mux.HandleFunc("/ws", hub.RegisterHandler)` at `main.go:588`. Hub built by `ws.NewHub(qInteractor.GetState)` at `:232`; `go hub.Run()` at `:235`. State read is **GLOBAL** — `qInteractor.GetState` over `queue_state`. `(*Hub).RegisterHandler` lives at `internal/delivery/ws/hub.go:244`. The 16-event inventory lives at `internal/delivery/ws/events.go:11-35`: `full_sync`, `user_joined`, `song_added`, `song_skipped`, `status_changed`, `elapsed_sync`, `song_previous`, `song_removed`, `queue_cleared`, `volume_changed`, `song_prioritized`, `priority_balance_updated`, `vote_updated`, `vote_resolved`, `auto_queue_added`, `auto_queue_config_changed` (+ additive `error` `:30` and `room_archived` `:35` which also rides global `/ws` via the R06 sweeper `hubArchiveBroadcaster` at `main.go:688-699`).

**Room `/ws/rooms/{slug}`** — `mux.HandleFunc("/ws/rooms/{slug}", roomWSHub.RegisterHandler)` at `main.go:591`. Built by `ws.NewRoomWSHub(...)` at `:242-258` with three resolvers (room-by-slug → `pgRoom.GetRoomBySlug`; member → `pgRoom.GetMember`; queue state → `roomQueueInteractor.GetStateByRoomID`). `go roomWSHub.Run()` at `:261`; `SetSessionResolver(authInteractor)` at `:260`. Hub at `internal/delivery/ws/room_hub.go` (constructor `:168`, `dispatch :362`, broadcasters `:374-528`, `nextSeq :209`). State read is **PER-ROOM**. Per-room events (`events.go:39-98`): `room_queue_sync`, `room_queue_song_added`, `room_queue_song_removed`, `room_queue_cleared`, `room_queue_song_prioritized`, `room_playback_status_changed`, `room_playback_elapsed_sync`, `room_playback_song_advanced`, `room_vote_updated`, `room_vote_resolved`, `room_playback_volume_changed`, `room_playback_song_previous`, `room_auto_queue_added`, `room_auto_queue_config_changed`, `room_member_removed`, `room_members_changed`, `room_chat_message_created`, plus per-room `room_archived` (R10b/R06).

### Frontend entry points

- `frontend/src/router/index.js`: `/` → `DashboardView` (global, `:6-11`); `/auth` → `AuthView` (`:12-16`); `/rooms/:slug` → `RoomView` (`:17-25`); guard `:33-51`. **No `Welcome` / `CreateRoom` / `JoinRoom` / `Invite` / `PlayerLease` routes** — these surfaces are unbuilt; rooms are entered only by direct URL.
- `frontend/src/store/index.js`: single `globalStore` (`:33`). Global slices: `currentUser`, `queueState{...}`, `voteSessions`, `autoQueueConfig{enabled, strategy}`, `connectionStatus`. Isolated per-room slices under `globalStore.roomQueues[slug]` (`:241+`, R07c) with mutators `applyRoomSongAdded :343`, `applyRoomPlaybackStatusChanged :428`, `applyRoomAutoQueueAdded :527`, `applyRoomAutoQueueConfigChanged :549` (explicitly MUST NOT touch global `autoQueueConfig`), `markRoomArchived` / `markRoomRemovedAsCurrentUser` / `applyRoomMembersChanged :557+`, chat mutators `:631 / :642`; `MaxRoomChatMessages = 100` `:11`.
- `frontend/src/services/api.js`: global methods still present (`getQueue :68`, `addSong :74`, `skipSong :85`, `setStatus :92`, `syncPlayback :99`, `songEnded :106`, `prevSong :112`, `removeSong :119`, `clearQueue :126`, `changeVolume :133`, `searchYouTube :141`, `prioritizeSong :148`, `getPriorityBalance :155`, `castSkipVote :162`, `castPriorityVote :169`, `getAutoQueueStatus :177`, `setAutoQueueEnabled :183`, `login` / `loginWithGoogle :53 / :60`). Room methods `:196-368`. **Gap:** no `createRoom` / `listRooms` / `getRoom` / invite / player-lease client methods — those backend routes are not called by the SPA. This is the gap that R05b closes.
- WS clients: global `frontend/src/services/websocket.js` (`wsClient`, `connect :99` builds `+ '/ws' :107`, `handleMessage :159` per-event cases `:171-298`), consumed by `DashboardView`. Room `frontend/src/services/room-websocket.js` (`createRoomWsClient :11`, builds `.../ws/rooms/{slug}` `:12`, seq-gap recovery, `send()` no-op `:40`, passes `CloseEvent` to `onClose`), consumed by `RoomView`.
- `DashboardView.vue`: GLOBAL only. Imports `globalStore` / `api` / `wsClient` `:110-112`. Reads `globalStore.queueState.*` `:72-99`. `wsClient.connect()` `:221` / `disconnect()` `:265`. This is the legacy global surface that R14d removes / redirects.
- `RoomView.vue`: ROOM only but reuses `globalStore` (`:268-270`). Reads `globalStore.roomQueues[slug]` `:283-286` and `globalStore.currentUser` `:281`. Routes per-room WS events through room mutators `:390-510`. REST recovery `api.getRoomQueue` / `getRoomChatMessages` `:541 / :585`. **Boundary:** a full global-store teardown in R14d must preserve `currentUser` (RoomView depends on it) AND the per-slug `roomQueues[slug]` slices.

## Desired behavior (post-R14a, sketched)

R14a does not implement any of this. R14a designs the contract.

### Source-to-target mapping (real conversion contract)

| Source (global) | Target (room) | Real conversion contract |
| --- | --- | --- |
| `queue_state` (TEXT JSON, singleton `id = 1`) | `room_queue_state` (JSONB, one row per `rooms.id`) | Read legacy TEXT into a `[]byte`. Decode through `encoding/json` into the tagged-field `entity.Queue` struct. Validate all invariants (`Songs`, `CurrentIndex`, `Status`, `Elapsed`, history fields, first-song / current-song behaviour). Re-encode through `encoding/json` to canonical JSON bytes. Compute SHA256 over the canonical bytes for the marker record. Write to `room_queue_state` with PK = `rooms.id`. **No `MarshalJSON` / `UnmarshalJSON` custom methods on `entity.Queue` are assumed; the contract is the default `encoding/json` tagged-field round-trip.** |
| `activities` | new `room_activities` (R14b builds) | Lossless copy of every row with preserved `id`, `timestamp`, `type`, `user`, `description`, count, and ordering; `room_id` set to the migrated room's id. After insert, `setval(room_activities_id_seq, MAX(id), is_called=true)` resyncs the sequence. |
| `auto_queue_config` (singleton `id = 1`) | `room_auto_queue_config` (already exists, migration 0007) | Keyed by `room_id`; migrator writes the migrated room's row with the legacy singleton's values. The 50-row cap lives in `room_play_history`, not in `auto_queue_config`. |
| `play_history` (legacy 50-row cap, Go-enforced) | `room_play_history` (already exists, migration 0007) | Id-collision handling — see § room_play_history id-collision handling below. Per-room 50-row cap is enforced in `roomautoqueue` after cutover (matching R09f). |

After the copy, source rows are retained as migration evidence (read-only for the rollback window) and dropped in R14e. The room targets are the sole writable source of truth after cutover.

`users`, `user_sessions`, and `priority_transactions` are **account-scoped per ADR 001 §3 Decision 9** and are NOT recopied at cutover. The migrator explicitly does NOT touch these tables. The legacy `users` table is the same table the room runtime reads; there is no separate "migrated user" identity surface. There is no remap, no re-issuance, no password rotation, and no identity rewriting. Idempotency is read-only against `users` (the host lookup only); writes go to `rooms` and `room_members` (new rows). The `users.legacy_id` column introduced in R03 is preserved untouched.

### room_play_history id-collision handling

`room_play_history` is one table shared across all rooms with a single global BIGSERIAL. The legacy `play_history` table also used a single global BIGSERIAL. Copying the legacy rows with preserved `id` values into `room_play_history` can collide with rows that any room's auto-queue already appended since the R09f migration.

The cutover migrator MUST handle this by **partitioning the id space**:

- Before the data copy, the migrator runs `SELECT MAX(id), COUNT(*) FROM room_play_history` and computes `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)`.
- Each legacy row is inserted with `id + :legacy_id_offset` (e.g., `INSERT INTO room_play_history (id, room_id, played_at, video_id, title) SELECT id + :legacy_id_offset, :new_room_id, played_at, video_id, title FROM play_history`). The migrator records the offset in the `room_cutover_marker` record so a re-run / rollback window can refer to the same shift.
- After the insert, `setval(room_play_history_id_seq, MAX(id), is_called=true)` resyncs the sequence so the next `BIGSERIAL` lands at `MAX(id) + 1`.
- The shifted ids are an implementation detail of the cutover; the per-room cap (50 rows) is enforced in `roomautoqueue` regardless of absolute id values.
- Re-run / idempotency: the `room_cutover_marker` carries SHA256 hashes of both the source (`play_history`) and the target post-copy (`room_play_history` filtered by `room_id = :new_room_id`) so a repeated `cmd/room-cutover up` is a no-op when the hashes match.

### Migration identity contract

**Activities target** — new `room_activities` table + dedicated repo (NOT coupled to `QueueRepository`):

```text
room_activities
  id          BIGSERIAL PRIMARY KEY
  room_id     BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE
  timestamp   TIMESTAMPTZ NOT NULL
  type        TEXT NOT NULL
  user        TEXT NOT NULL
  description TEXT NOT NULL

  INDEX (room_id, timestamp DESC, id DESC)
```

Migration preserves every legacy `activities` row's `id`, `timestamp`, `type`, `user`, `description`, count, and ordering. After the preserved-id insert, the migrator resynchronises the `room_activities_id_seq` via `setval(seq, MAX(id), is_called=true)`.

No `user_id` requirement during migration. Legacy rows store display text and may represent users that cannot be resolved reliably or system-generated actions. A nullable actor id is a future-feature concern, not a migration concern.

Repository contract (do NOT couple to `QueueRepository`):

```go
AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error
GetActivities(ctx context.Context, roomID int64, limit int) ([]entity.Activity, error)
```

After cutover, `room_activities` is the sole writable source of truth. The legacy `activities` table is read-only for the rollback window and dropped in R14e.

**Host bootstrap** — operator flag `--host-user-id=<positive integer>` (the canonical `users.id` BIGINT, NOT a UUID).

Validation:

- The value is a positive integer.
- Exactly one `users` row exists with that ID. Because it is a primary key, "ambiguous" is not a meaningful outcome.
- The host is inserted into the migrated room as its sole host **inside the same migration transaction** as room creation and data conversion.
- Failure to resolve the ID aborts the migration before any target state is committed.

The migrator does NOT infer the host from: the first user to join, `users.role`, `HOST_EMAILS` / `ADMIN_EMAILS`, an existing player lease, or frontend / request-supplied identity.

**Override of issue #20 §2:** the schema permits one user to be a member of multiple rooms (uniqueness is `(room_id, user_id)` only); the one-host invariant is **per room**, not per user globally. The migrator does NOT reject a host merely because they belong to another active room.

**Idempotent retry with a different `--host-user-id` MUST fail on the room/slug conflict**, not silently change the room's host.

Email may be documented as an operator-side discovery aid (used manually to look up the numeric ID) but is NOT the migration command's canonical identity input.

**Conflict behaviour**:

- Migrated room slug already exists in `rooms` with a different identity → fail loudly; do NOT mutate the existing room.
- Migrated room slug already exists with identical identity AND `room_cutover_marker` proves the same completed cutover → return `already cut over; no-op`, exit 0.
- Migrated room slug already exists with identical identity but `room_cutover_marker` shows a different completed cutover → fail on hash drift; do NOT overwrite.
- `--host-user-id` not resolvable → fail before any commit.

**Migration report** — PII posture:

- MAY contain: the operator-supplied numeric `users.id` for the host; numeric row counts; sha256 hashes; sequence names; table names; durations.
- MUST NOT contain: email addresses; OAuth tokens / ID token claims; any `display_name` / `user` text from activities rows; session tokens; any credential, cookie, or `Authorization` header value.
- DSN redaction uses `config.RedactDSN` (existing R03 helper).

### Atomicity and idempotency (DEDICATED mechanism, not R03 reuse)

The R14b room-cutover mechanism is built fresh, drawing on R03 patterns where appropriate, but **does NOT reuse R03's CLI, marker, or transactional scaffolding**:

- `pg_try_advisory_lock(987654321)` on a pinned connection (same key, same idiom as R03). Deferred unlock on success or error. The lock is released in `cmd/room-cutover abort` and on process exit.
- Single transaction per `cmd/room-cutover up`: `conn.BeginTx` → `tx.Commit`. `tx.Rollback` on any error.
- **NEW** marker table `room_cutover_marker` (single-row, `id = 1`) — distinct from R03's `migration_marker` table. The R03 marker is preserved untouched.
- Marker record fields: `room_cutover_id` (UUID generated by the CLI), `target_room_slug`, `target_room_id` (resolved after the room row is inserted), `host_user_id`, per-source SHA256 hashes (`queue_state.data`, `activities.*`, `auto_queue_config.*`, `play_history.*`), per-target post-copy SHA256 hashes (`room_queue_state.data` for the migrated room, `room_activities.*` filtered by `room_id`, `room_auto_queue_config.*` filtered by `room_id`, `room_play_history.*` filtered by `room_id`), `legacy_id_offset` for `room_play_history`, `cutover_started_at`, `cutover_committed_at`, `binary_build_sha`.
- Re-run on identical hash record = `already cut over; no-op`, exit 0. **Driven by the `room_cutover_marker` SHA256 record, NOT by R03's `migration_marker`.**
- Hash drift on any source or target = rejected with an explicit sentinel. No `--force` / `--reset`.
- Two-phase verification:
  - **Pre-commit (within tx)**: source row counts, target row counts, `queue_state.data` byte length parity (legacy TEXT to canonical JSONB), per-target SHA256 after the copy.
  - **Post-commit (after tx)**: `cmd/room-cutover verify` subcommand re-reads the sources (which are now retained as migration evidence) and the targets, computes the SHA256s again, asserts they match the marker.
- `cmd/room-cutover plan` subcommand (read-only): snapshot the source hashes, plan the target writes, write a JSON/text report WITHOUT committing.
- `--dry-run` mode on `cmd/room-cutover up`: same as `plan` but the connection is wired through the live DB; no writes commit.
- `config.RedactDSN` in the report.
- Sequence resync via `setval(seq, MAX(id), is_called=true)` for each migrated sequence (`room_activities_id_seq`, `room_play_history_id_seq` (post-shift)). R03's `setval` calls for `user_sessions` / `priority_transactions` / `activities` / `play_history` are unchanged and re-run only as part of a future R03 re-cutover (out of R14 scope).
- The room-cutover CLI is an offline CLI; it does NOT run inside the server process; it does NOT mutate either model concurrently with runtime writers (the application is shut down during Phase B).

### Compatibility & endpoint retirement phases A→D (chosen path)

R14a chooses ONE recommended sequence. The four phases MUST be implemented as separate sprints so each can be reviewed independently. Per the Architect review, the cutover is split between R14b (builds + verifies the mechanism) and R14c (owns the coordinated production execution with a binary that has disabled global write paths).

#### Phase A — offline conversion (built + verified by R14b)

R14b is a backend-only sprint that ships **the mechanism** as a new dedicated CLI and verifies it against a representative PostgreSQL snapshot. R14b does NOT retire the global runtime in production; it lands the CLI, the marker contract, the source-to-target copy, the dry-run, the verification, and the rollback hook. R14b's verification gate MUST pass before R14d (frontend cutover) can run.

- R14b ships `cmd/room-cutover` with subcommands:
  - `plan` (read-only snapshot + dry-run)
  - `up` (apply)
  - `verify` (re-assert hash record against the marker)
  - **No `abort` subcommand.** PostgreSQL advisory locks belong to the connection that acquired them; another process cannot release them. Rollback and lock release happen by ending the held connection — `tx.Rollback` on any error returns the connection to the pool on process exit, releasing the lock with it. If the operator wants to abandon a held cutover mid-flight, they stop the `cmd/room-cutover up` process (`SIGTERM` / Ctrl-C); the connection closes, the lock releases, the in-flight transaction rolls back. The CLI MAY print a textual "press Ctrl-C to abandon" hint before the copy phase begins.
- R14b ships the marker table `room_cutover_marker` (single-row, `id = 1`) as described in § Atomicity and idempotency.
- R14b ships the binary build flag `--room-cutover-authoritative` (default `false`; in R14b this is `false` in every deployment). When `false`, the legacy global routes continue to serve from the legacy repositories **unchanged**; the cutover migrator reads from the legacy tables and writes to the room tables, but the server still serves the global routes for concurrent testing. This is the verification state.
- R14b verification gate (must pass before R14d can run):
  - `cmd/room-cutover plan` against a fresh PostgreSQL snapshot returns a report with no PII (numeric ids + sha256 + counts only).
  - `cmd/room-cutover up --dry-run` against the same snapshot completes with no writes and a successful SHA256 record.
  - `cmd/room-cutover up` (real run) against the same snapshot creates the room, inserts the host membership, copies the source rows into the target rows, and inserts the marker **all in one transaction**; then commits and returns 0. (See § Atomicity and idempotency for the in-transaction marker contract.)
  - Re-running `cmd/room-cutover up` immediately after returns `already cut over; no-op` (exit 0), driven by the marker.
  - Hash drift on either source or target is detected and rejected (no `--force` / `--reset`).
  - PII redaction verified (no email, no OAuth token, no display name, no session token in the report).
  - The marker preserves the R03 `migration_marker` row untouched.
  - Schema version after the R14b migration is **9** (verified by the runner's reported version, not by a separate config file).
  - `go test ./...` clean.

#### Phase B — coordinated production cutover (executed by R14c)

R14c owns the **coordinated production execution**: a planned maintenance window, a redeploy with the **binary build flag `--room-cutover-authoritative=true`**, and the run of `cmd/room-cutover up` against the live PostgreSQL. R14c is the ONLY sprint where the production binary is changed.

- Before the window: a `pg_dump` is taken and retained ≥30 days (mirrors ADR 002 §9). The R14b binary is already deployed and the R14b verification gate has passed. The post-R14d frontend bundle has been deployed and is in production. R05b (room entry UI), R09h (vote-prioritize parity), and R09i (room-activity runtime parity) are all accepted. The three blocking prerequisites close together; R14c cannot start until all of them are accepted.
- During the window:
  - The application is shut down (or restarted with a maintenance-mode flag that disables ALL mutating endpoints — global AND room — while keeping reads disabled too in this state).
  - `cmd/room-cutover up` runs against the live database. It pins a connection, takes the advisory lock, opens a single transaction, creates the room, inserts the host membership, copies the source rows into the target rows, inserts the marker **in the same transaction**, and commits. See § Atomicity and idempotency.
  - The application restarts with `--room-cutover-authoritative=true`. In this build:
    - The legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes and the legacy `/ws` endpoint are **registered with repository-free tombstone handlers** that return `410 Gone` with the documented envelope (see § Errors and the route-retired responses) — they are NOT unregistered. The room routes are still the only data path; the tombstone handlers carry no repository wiring and no WS hub mutation logic. The mux returns `410` for the listed legacy routes; the global `/ws` upgrade is **rejected during the HTTP phase** with `410 Gone` (the WS handler is replaced by a 410-returning HTTP handler at upgrade time, so no WebSocket connection succeeds). This is the approved `410 Gone` retirement posture.
    - On the cutover boundary, the application restart is the only window where the listed legacy routes transition from serving real responses to `410 Gone` — this is the documented maintenance window.
  - The legacy global repositories write paths are also disabled by `--room-cutover-authoritative=true` to prevent any tool / script / cron that bypassed HTTP from mutating the legacy tables during the rollback window. The `--safe-write` mode is OFF in this build for legacy tables.
- Frontend deployment order is captured in § Deployment compatibility matrix; the cutover binary is paired with the post-R05b + post-R14d frontend at production cutover time.

**R14c is atomic across the listed legacy routes.** The cutover binary retires `/api/queue/...`, `/api/vote/skip`, `/api/autoqueue/...`, `/api/vote/prioritize`, the global `/ws`, all in one step. There is NO partial cutover where some legacy routes are retired while others (notably `/api/vote/prioritize`) remain live; a partial cutover would let surviving legacy mutations continue writing legacy global queue state after the room state is declared authoritative. Because R09h is a blocking prerequisite for the entire R14c cutover (not just `/api/vote/prioritize`), `/api/vote/prioritize` is guaranteed retired by Phase B.

#### Phase C — frontend cutover (R14d) — runs BEFORE R14c

R14d executes BEFORE R14c per § Future sprint sequence. R14d deploys a frontend bundle that pairs with the still-`authoritative=false` server. The legacy global routes are still live during R14d deployment; the post-R14d frontend simply does not call them.

- `RoomView` becomes the only supported view; `DashboardView` is removed or redirects to a Welcome / room entry surface.
- `globalStore.queueState`, `globalStore.voteSessions`, and `globalStore.autoQueueConfig` are cleared.
- `globalStore.currentUser` is preserved (RoomView still reads it).
- `globalStore.roomQueues[slug]` per-slug slices are preserved (RoomView still reads them).
- The global `WebSocketClient` is closed and not reconnected.
- **Frontend entry UX is a prerequisite**, not part of R14d: the unbuilt SPA client methods (`api.createRoom`, `api.listRooms`, `api.getRoom`, `api.joinRoom`, the invite endpoints, `api.claimPlayerLease`, `api.heartbeatPlayerLease`, `api.releasePlayerLease`, `api.getPlayerLease`) MUST exist before R14c can run. This is the **R05b — Room entry / player-lease UI** prerequisite sprint (see § Future sprint sequence). R05b is the slice that ships these frontend methods and exposes them as actual UI (room create form, room list, room join, invite redeem, lease claim UI). R14d assumes these exist.

#### Phase D — schema cleanup (R14e)

- Land `0010_drop_legacy_global_tables.up.sql` as a SEPARATE forward migration (do NOT modify `0001_initial.up.sql`). The forward migration drops the legacy `queue_state`, `activities`, `auto_queue_config`, and `play_history` tables. **DO NOT touch `room_play_history` rows** — `room_play_history` is the per-room play-history table and is NOT legacy; the migration drops only the **global** legacy tables. **The historical `0001_initial.up.sql` file remains on disk byte-for-byte and is NEVER rewritten.**
- Schema version bumps from 9 to 10 (only if R14e accepts and destructive cleanup completes).
- Phase D is destructive cleanup only and runs **after** a verified rollback window AND after no runtime path references the legacy tables.

### WebSocket cutover

- `/ws` is **retired with `410 Gone` in the `--room-cutover-authoritative=true` build (R14c).** The global WS upgrade handler is replaced by an HTTP handler at the mux entry that returns `410 Gone` with the documented envelope (see § Errors and the route-retired responses). The HTTP-phase rejection prevents WebSocket connections from being established against the retired endpoint. R14a explicitly does NOT use `404` for the retired `/ws`; `410 Gone` is the approved retirement posture.
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- `seq_num` continuity: per-room sequence numbers restart from the per-room hub's own counter (which is independent of the global hub's counter); this is the existing R07b behaviour and is not changed by R14a.
- `/ws` MUST NOT silently join an arbitrary room under any circumstance.
- Per-room event families and initial sync ownership remain per-room.
- The first per-room `room_queue_sync` after migration lands carries the migrated room's queue state under the migrated room's `seq_num` allocation.

### Frontend transition & recovery

- Where users land after upgrade: when no active room is selected, redirect to a room entry surface (or Welcome). Users with the migrated room bookmarked land directly on the migrated room's `RoomView`. The room entry surface REQUIRES the R05b SPA client methods and routes.
- The migrated room is presented to its initial host and to other existing accounts via the existing room-frontend entry points; no automatic join.
- Global queue / store / WebSocket state is cleared (per Phase C); per-slug `roomQueues[slug]` slices preserved; `currentUser` preserved.
- Stale bookmarks, stale local storage, archived-room view, and `410 Gone` responses surface as clear UI states; the frontend MUST NOT attempt to fall back to the retired endpoints.
- Frontend cutover is paired with the binary cutover at production time per § Deployment compatibility matrix.

### Deployment compatibility matrix

The cutover involves two build flags (`--room-cutover-authoritative` per-server, frontend bundle version per-client — pre-R05b / post-R05b / post-R14d) and three server cutover states (pre-R14b / post-R14b / post-R14c). Valid combinations only:

| Server binary | Frontend SPA | Legacy global routes | Per-room routes | Result |
| --- | --- | --- | --- | --- |
| Pre-R14b binary (`--room-cutover-authoritative=false` default) | pre-R05b (no entry UI) | Registered; serve legacy repos | Registered | Pre-cutover baseline. Documented. |
| Post-R14b binary (`--room-cutover-authoritative=false`) | pre-R05b OR post-R05b (entry UI exists) | Registered; serve legacy repos | Registered | The R14b verification state. Users can use both surfaces. |
| Post-R14c binary (`--room-cutover-authoritative=true`) | post-R14c + post-R05b + post-R14d (DashboardView removed / redirected) | **Tombstone handlers; `410 Gone` with the documented envelope** (see § Errors and the route-retired responses); the global `/ws` upgrade is rejected with `410` in the HTTP phase | Registered; sole authoritative path | Production cutover state. Documented. This is the ONLY valid post-R14c combination. |
| Post-R14c binary | post-R05b only (DashboardView still present) | `410 Gone` tombstone | Registered | **BROKEN** — frontend shows DashboardView, but every legacy route it calls returns `410`. The compatibility matrix forbids this combination. |
| Post-R14c binary | pre-R05b (no entry UI) | `410 Gone` tombstone | Registered | **BROKEN** — frontend cannot enter rooms. The compatibility matrix forbids this combination. |
| Mid-window rollback (immediately pre-cutover production release, server binary running with `--room-cutover-authoritative=false` because the cutover flag rollout was reverted before `cmd/room-cutover up`) | post-R14c + post-R05b + post-R14d | Registered; serves legacy repos | Registered | Mid-window rollback. **The rollback target is the immediately pre-cutover production release**, not an "R03 binary" — the R03 release is the SQLite-to-PostgreSQL migration release and is unrelated to the room cutover. The room tables in the target release are populated by the prior `cmd/room-cutover up` run; mutations since that point would need forward recovery against the `pg_dump` taken at the start of the window. |
| Pre-R14c binary | pre-R05b / post-R05b | Registered | Registered | Pre-cutover state. Rollback target when only the room-cutover migration has been run. |

The rollback target for any failure during Phase B is the **immediately pre-cutover production release** (the same server binary deployed at the time the maintenance window opened, with `--room-cutover-authoritative=false`). The legacy global repositories in the target release resume serving real responses. The `room_cutover_marker` row and the migrated room data persist; further room writes (post-rollback) would create drift the next cutover must reconcile. Forward recovery against the `pg_dump` taken at window start is the safe path for any data that mutated after the cutover.

**Client-continuity claim:** the only combinations that guarantee client continuity are the documented ones. Specifically:
- The `post-R05b + post-R14d` frontend paired with the `--room-cutover-authoritative=false` (post-R14b / pre-R14c) server is safe to deploy before the cutover window — both global and room surfaces are live.
- The `post-R14c + post-R14d` frontend paired with the `--room-cutover-authoritative=true` (post-R14c) server is the production cutover state — legacy surfaces return `410` and clients see the documented surface.
- A frontend relying on legacy global routes paired with a post-R14c server will receive `410` from the legacy surfaces. The compatibility matrix forbids this combination.
This is the client-continuity guarantee.

### Deployment, backup, and rollback gates

- Required PostgreSQL `pg_dump` BEFORE R14c cutover; retained ≥30 days (mirrors ADR 002 §9). The `pg_dump` is the source of truth for the immediately pre-cutover production state.
- Application downtime / maintenance window is required during Phase B.
- `cmd/room-cutover plan` (read-only) and `cmd/room-cutover up --dry-run` MUST be run before any commit.
- Target verification (`verifyWithinTx` + `cmd/room-cutover verify` post-commit) MUST pass before R14c accepts writes on the room-authoritative version.
- Migration succeeds but application startup fails: the documented rollback entry point is to restart the **immediately pre-cutover production release** of the binary (the same build deployed at window open, with `--room-cutover-authoritative=false`); the legacy repositories resume serving real responses. The room tables in the target release are populated by the prior `cmd/room-cutover up` run; mutations since that point would need forward recovery against the `pg_dump`.
- The rollback target is **not** an "R03 binary". The R03 release is the SQLite-to-PostgreSQL migration release; it is unrelated to the room cutover and is not a valid rollback target for R14c.
- **Explicit warning:** rollback after new room writes may require forward recovery or data reconciliation; the compatibility matrix is the deployment-time safeguard.

## Future sprint sequence

Sprint execution order is fixed: **`R05b → R09h → R14b → R14d → R14c → R14e`** (with R14e only after the verified rollback window). Reordering is NOT permitted within this sprint's scope.

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R05b — Room entry / player-lease UI** | Frontend only (no new HTTP routes; uses the existing R04 / R06 / R11a routes). Adds SPA client methods `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / invite + `api.claimPlayerLease` / `api.heartbeatPlayerLease` / `api.releasePlayerLease` / `api.getPlayerLease`; exposes them as actual room create / list / join / invite-redeem / lease-claim UI. **BLOCKING PREREQUISITE for R14c** — without it, retiring `/api/queue` / `/ws` etc. strands users on a global dashboard that has no way to enter a room. | The existing backend routes are already registered (per `cmd/server/main.go:413–486`); R05b only wires the SPA. |
| **R09h — Room vote-to-prioritize parity** | Backend room-vote-prioritize parity. Adds `POST /api/rooms/{slug}/vote/prioritize`. **BLOCKING PREREQUISITE for the ENTIRE R14c cutover** — R14c cannot run while `/api/vote/prioritize` is still live (a partial cutover would let that global mutation continue writing legacy global queue state after room state is declared authoritative). | Reuses existing `room_queue_song_prioritized` event. No new WS event constant. |
| **R09i — Room activity runtime parity** *(new)* | Backend-only slice that wires existing room mutations to append rows to the new `room_activities` table. Replaces the global dashboard's silent activity append (today the global `activities` table is fed from vote / priority / queue mutations in `usecase/vote/interactor.go`, `usecase/priority/interactor.go:81`, `usecase/queue/interactor.go`; the room runtime has no equivalent). After R09i, the room frontend shows live activity through the existing `RoomActivityRepository.AddActivity` path; the per-room mutators call it. **BLOCKING PREREQUISITE for R14c** unless the Product Owner explicitly approves retiring live activity logging entirely. See § PO acceptance blockers #1. | The `room_activities` table itself is built in R14b; R09i wires the call sites and the read surface. |
| **R14b — room-cutover mechanism** | Backend-only. New dedicated CLI `cmd/room-cutover` with `plan` / `up` / `verify` subcommands (NO `abort` — see § CLI flags). New marker table `room_cutover_marker`. Source-to-target copy (legacy → room). NEW migration `0009_room_activities.up.sql` (additive; schema v8 → v9). Build flag `--room-cutover-authoritative=false` (default). Pre-commit + post-commit verification. **NO global route retirement. NO frontend changes.** R14b's binary ships the mechanism and verifies it; R14b's gate MUST pass before R14d can run. | Does NOT reuse R03's CLI or `migration_marker`. R03's marker is preserved untouched. |
| **R14d — frontend global-path removal** | Frontend-only. Removes / redirects `DashboardView`; clears `globalStore.queueState` / `voteSessions` / `autoQueueConfig`; closes global `WebSocketClient`; preserves `currentUser` + `roomQueues[slug]`; deploys the post-R14d SPA bundle alongside the (still `authoritative=false`) server. | Assumes R05b + R09h + R14b have landed. **R14d ships BEFORE R14c** so that the post-R14c binary has a compatible frontend bundle to pair with. |
| **R14c — coordinated production cutover** | Backend production cutover (Phase B). Maintenance window + restart with `--room-cutover-authoritative=true` binary + run `cmd/room-cutover up` against the live DB. The cutover binary retires ALL listed legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes and the global `/ws` endpoint in one atomic step — `/api/vote/prioritize` is INCLUDED in the retirement set because R09h has landed. The room runtime is the only authoritative path. Pairs with the post-R14d + R05b frontend in the deployment compatibility matrix. | Requires R05b + R09h + R09i + R14b + R14d all accepted. Blocks until each prerequisite gate has passed. |
| **R14e — schema cleanup** | Backend only. Lands `0010_drop_legacy_global_tables.up.sql` (a SEPARATE forward migration that drops legacy `queue_state`, `activities`, `auto_queue_config`, `play_history`); bumps schema v9 → v10. Historical migration files `0001`..`0009` remain on disk byte-for-byte; `0001_initial.up.sql` is NEVER rewritten. | Runs after the verified rollback window AND after no runtime path references the legacy tables. |

Sprint names may be renamed if evidence requires it, but R05b / R09h / R14b / R14c / R14d / R14e MUST NOT be combined; migration, destructive cleanup, frontend rewrite, frontend entry UX, and vote-prioritize parity must remain independently reviewable.

## Validation, errors, and test inventory expectations

R14a documents the expected test inventory; tests are NOT in R14a scope.

### Validation rules

- Migrated room slug: server-side regex / length rules mirror the existing room-slug validation in `internal/domain/repository/room_repository.go`.
- Migrated room display name: explicitly decide whether it must equal the slug or may differ under the current room model. **R14a recommends allowing them to differ**, mirroring the existing room model.
- `--host-user-id`: positive integer; resolved by PK lookup against `users`.
- Idempotency: re-run = no-op exit 0; hash drift = reject.
- Archived-room / missing-host failures map to explicit sentinels.

### Errors and the route-retired responses

- After R14c, the legacy global REST routes and `/ws` return `410 Gone` via repository-free tombstone handlers wired at the mux entry. **`410 Gone` is the approved retirement posture** (not `404`). The handlers carry no repository wiring and no WS hub mutation logic.
- The route-retired envelope (the actual wire response):

```json
{
  "error": "gone",
  "code": "global_contract_retired",
  "documentation": "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"
}
```

- Clients discover the migrated room through the standard `GET /api/rooms` listing endpoint, which is the documented successor-discovery surface.
- The HTTP `Link: </api/rooms>; rel="successor-version"` header IS added in the tombstone handler response so clients can navigate to the discovery surface without out-of-band knowledge.
- The global `/ws` upgrade is rejected with `410 Gone` during the HTTP phase of the request (the upgrade handler returns the HTTP `410` before the WebSocket handshake completes) — this is the tombstone-handler behaviour for `/ws`, not a separate `404`-from-unregister path.
- The earlier R14a draft's mistake was describing retirement as route unregistration returning `404`; R14a explicitly reverts to `410 Gone` with repository-free tombstone handlers. The retirement is a stable, machine-readable surface.
- No concrete `successor: "/api/rooms/{slug}/..."` field appears in the envelope. The successor room is discovered through `GET /api/rooms`, not pre-supplied.

### Migration CLI flags (R14b)

- `--room-slug=<slug>` (required): the slug of the migrated room. Server-side regex / length rules mirror the existing room-slug validation in `internal/domain/repository/room_repository.go`.
- `--room-name=<name>` (required): the displayed room name. The room model permits the name to differ from the slug; R14a recommends allowing them to differ, mirroring the existing room model. If a future migration rules this out, the validation becomes an explicit equality check.
- `--host-user-id=<positive integer>` (required): the canonical PostgreSQL `users.id` (BIGINT) of the initial host. PK lookup; no inference from joiners / roles / emails / lease / frontend.
- `--dry-run` (optional; default false): same as the body of `up` but no writes commit; produces the same report as `plan`.
- The CLI MUST reject unknown / extra positional arguments and unknown flags. Idempotent re-run requires only the three flags above (and `verify` uses no flags).
- The CLI MUST NOT accept `--force` / `--reset` / `--allow-hash-drift`. Hash drift fails the migration.
- The CLI MUST NOT expose a subcommand to release another process's advisory lock (PostgreSQL session locks belong to the connection that acquired them). See § Atomicity and idempotency for release semantics.

### Migration report fields

- Operator-supplied numeric `users.id` for the host.
- Numeric row counts per table.
- SHA256 hashes per migrated table.
- Sequence names resynced.
- Durations per phase.
- `legacy_id_offset` (history-id offset formula): `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)` — the maximum existing `room_play_history.id` at the start of the migration; the copy shifts every legacy `play_history.id` by this offset and inserts it into `room_play_history` with `id + legacy_id_offset`. The migrator records the offset in the marker record so a re-run or rollback-window reference uses the same shift. (This is the simpler rule the Architect review called out; the sprint document previously had a more elaborate formula — the simpler `MAX(id)` rule is the agreed contract.)
- `binary_build_sha` for the marker.
- No email, OAuth tokens, display names, session tokens, credentials, cookies, or `Authorization` headers.

### Future test matrix (NOT in R14a scope)

- R05b: frontend tests for `api.createRoom` / `api.listRooms` / `api.getRoom` / invite / lease methods; UI tests for the entry surfaces.
- R09h: `room_vote_prioritize` interactor tests (session create, vote, threshold, expiry); HTTP handler tests (auth, body shape, success, 400 / 403 / 404 / 409 mapping); race-sensitive tests; WebSocket event emission tests (`room_vote_updated`, `room_vote_resolved`, `room_queue_song_prioritized`).
- R09i: room-activity runtime parity tests — verify that the room mutators (queue / playback / vote / auto-queue) call `RoomActivityRepository.AddActivity` with the documented shapes; verify that the read surface exposes `room_activities` rows to the RoomView panel; race-sensitive tests.
- R14b: focused migration tests — `room_activities` lossless copy; `room_queue_state` JSON round-trip + SHA256; sequence resync; idempotent re-run (no-op); hash-drift rejection; offline CLI smoke; advisory-lock conflict; transaction rollback on mid-flight failure; report PII redaction; dry-run output; legacy-id-offset calculation correctness; marker-inserted-in-same-transaction-as-room-and-host-and-copy test (kill the cutover mid-flight, restart the DB, verify NO partial state); CLI flag validation (`--room-slug` / `--room-name` / `--host-user-id`).
- R14b repository tests: `RoomActivityRepository.AddActivity` / `GetActivities`.
- R14c: HTTP `410 Gone` envelope tests; `Link` header tests; `/ws` HTTP-phase rejection returning `410`; per-room WebSocket event inventory unchanged; success-path routing; atomic retirement of all listed legacy routes in one step (test that the cutover binary enables `410` for ALL listed legacy routes simultaneously, NOT a partial cutover).
- R14d: frontend tests for `globalStore` clearing on cutover; `currentUser` preservation; `roomQueues[slug]` preservation; global `WebSocketClient` close; toast / redirect for `410` responses; `DashboardView` removal.
- R14e: focused cleanup tests; legacy-table-drop verifies no runtime path references them; schema version bumped to 10; `room_play_history` is NOT touched by the `0010` migration.

## Schema-version plan

The migration runner (`internal/infrastructure/persistence/migrations/postgres/...`) reports the applied numbered migration as the schema version; adding a new migration necessarily bumps the version. **No sprint can both add a migration and keep the version unchanged.**

- **Schema at start of R14a: v8** (last applied: `0008_room_chat_messages.up.sql`; current schema-version report = 8).
- **R14b lands `0009_room_activities.up.sql`** — a NEW additive migration that creates the `room_activities` table + index. The migration runner bumps the schema version. **Schema after R14b: v9.** The schema version reported at the end of the R14b verification gate MUST be 9.
- **R14c is a runtime + binary-flag cutover; it does NOT add a migration.** Schema during R14c and during the rollback window: v9.
- **R14e lands `0010_drop_legacy_global_tables.up.sql`** which drops the legacy `queue_state`, `activities`, `auto_queue_config`, and `play_history` tables. Bumps to **v10**. Only if R14e accepts destructive cleanup. If the rollback window must be extended or forward recovery is required, v10 is delayed.

**Historical migration files are never rewritten.** `0001_initial.up.sql` .. `0008_room_chat_messages.up.sql` are preserved on disk byte-for-byte; R14a adds new files only (no edits to existing ones). R14e does NOT modify `0001_initial.up.sql` to remove rows — it instead lands `0010_drop_legacy_global_tables.up.sql` as a SEPARATE forward migration that drops the legacy tables. The historical files remain auditable. The `0001` file's contents continue to describe what `0001` originally created; the v10 state is the cumulative result of running `0001`..`0010` in order.

## ADR reconciliation

R14a supersedes parts of ADR 001 and the R06-fold attribution in ADR 002 §11/§13 via the new ADR 003 (`documents/00-project-management/ADRS/003-legacy-global-state-migration-and-contract-retirement.md`). ADR 003 is **Proposed** (R14a contract; pending Architect review and Product Owner acceptance). ADR 001 body §3–§18 is unchanged byte-for-byte except for one metadata supersession note near the header. ADR 002 body is unchanged (the §11/§13 R06-fold attribution is reconciled inside ADR 003 rather than edited in ADR 002). ADR 001 §3 Decision 2 (no permanent `main` room), §4 (lifecycle), §5 (lease model), §7 (invite model), §16 (deferred work), and the "single-instance only" posture remain authoritative.

## PO acceptance blockers

These are explicit blockers that the Product Owner must sign off on before R14c can run (and before R14e can land):

1. **Room-activity runtime parity (R09i).** R14a designs the `room_activities` table and the repository contract; R09i (a separate future backend-only sprint) wires the room runtime to append rows to `room_activities` on the same events that the global runtime today appends to `activities` (vote / priority / queue mutations in `usecase/vote/interactor.go`, `usecase/priority/interactor.go:81`, `usecase/queue/interactor.go`). **R09i is a BLOCKING PREREQUISITE for R14c** unless the Product Owner explicitly approves retiring live activity logging entirely (separate sign-off recorded on Issue #20). The frontend activity panel may be a later follow-up; the runtime write path is the slice R09i ships. The R09i gate prevents the global write path (which feeds `activities`) from running concurrently with the room runtime (which would feed `room_activities`) — the cutover would otherwise lose activity write continuity for the room.
2. **Live room-activity read parity (frontend panel).** May be deferred as a separate frontend task; not in scope for R09i or R14a. If the Product Owner wants read parity in the room frontend before accepting R14c, it must be added as a separate frontend task and its scope settled in a focused sprint before R14d.
3. **R09h landing prerequisite for the entire R14c cutover.** Until R09h is accepted, R14c cannot run at all — R14c is atomic across the listed legacy routes. A partial cutover where `/api/vote/prioritize` is live while other legacy routes are retired would let the surviving legacy mutation continue writing legacy global queue state after room state is declared authoritative. R14c therefore blocks on R09h, not just `/api/vote/prioritize`.
4. **R05b landing prerequisite for user-facing cutover.** Until R05b is accepted, no user can create / list / join rooms from the SPA. R14c is blocked.
5. **Route-retirement response posture.** After R14c, the listed legacy routes return `410 Gone` via repository-free tombstone handlers (NOT `404` from unregistration). The envelope is `{error, code: "global_contract_retired", documentation}` plus the HTTP `Link: </api/rooms>; rel="successor-version"` header. Clients discover the migrated room via `GET /api/rooms`.
6. **Rollback posture.** Rollback after the binary restart with `--room-cutover-authoritative=true` is **NOT a transparent rollback** — it requires restarting the **immediately pre-cutover production release** (the same server binary deployed at window open, with `--room-cutover-authoritative=false`). The room tables in the target release are populated by the prior `cmd/room-cutover up` run; mutations since that point would need forward recovery against the `pg_dump`. The compatibility matrix in § Deployment compatibility matrix is the deployment-time safeguard. **The rollback target is NOT an "R03 binary".** The R03 release is the unrelated SQLite-to-PostgreSQL migration.
7. **Slug-collision resolution.** If the migrated room slug already exists in `rooms` at cutover time with a different identity, the migrator fails loudly. The recovery path is operator-driven (rename the existing room and re-run, or pick a different slug). No automatic rename or proxy.
8. **Schema-version plan approval.** The Product Owner must approve the schema-version plan in § Schema-version plan (v8 → v9 in R14b; v9 → v10 in R14e; historical migrations preserved on disk).

## Items already settled (informational)

- ID-collision handling for `room_play_history` (see § room_play_history id-collision handling) — `legacy_id_offset = MAX(room_play_history.id)` is the agreed simple rule (see § Migration CLI flags).
- Account-table posture: no recopy (see § Source-to-target mapping).
- Schema version plan: v8 → v9 in R14b (with the new `0009_room_activities.up.sql` migration); v9 → v10 in R14e (with the new `0010_drop_legacy_global_tables.up.sql` migration). Historical migrations preserved on disk; `0001_initial.up.sql` is NEVER rewritten.
- The R03 `cmd/migrate-data` + `migration_marker` is preserved untouched and is the SQLite-to-PostgreSQL migrator; it is NOT reused for the room cutover. R14b adds a SEPARATE `cmd/room-cutover` + `room_cutover_marker`.
- Per-room `room_play_history` 50-row cap is enforced in `roomautoqueue`, matching R09f. **The R14e `0010_drop_legacy_global_tables.up.sql` migration drops the legacy global `play_history` table only — `room_play_history` is NOT a legacy table and is NOT touched.**
- The `entity.Queue` JSON conversion contract uses the default `encoding/json` tagged-field round-trip; no custom `MarshalJSON` / `UnmarshalJSON` methods are assumed.
- The queue-prioritize / vote-prioritize distinction: global `/api/queue/prioritize` has a room equivalent (`/api/rooms/{slug}/queue/prioritize`, R07d) and retires in R14c without a parity sprint; only `/api/vote/prioritize` needs R09h, and only R09h (not just `/api/vote/prioritize`) is a blocking prerequisite for the entire R14c cutover.
- Marker is inserted in the same transaction as the room, the host membership, and the copied state. There is NO post-commit write of the marker; a mid-flight crash leaves NO partial state.
- `cmd/room-cutover` has no `abort` subcommand. PostgreSQL session locks belong to the connection that acquired them; another process cannot release them. Lock release happens through connection close (process exit, `SIGTERM`, `Ctrl-C`).
- Retirement posture: `410 Gone` via repository-free tombstone handlers (NOT `404` from route unregistration). The global `/ws` upgrade is rejected with `410` in the HTTP phase.
- Per-room sequence numbers restart from the per-room hub's own counter.
- The cutover CLI is offline; the application is shut down during Phase B.
- A hidden default-room `/ws` is forbidden.
- The cutover binary retires ALL listed legacy global routes in one atomic step — no partial cutover.
- Sprint execution order is fixed: `R05b → R09h → R14b → R14d → R14c → R14e`.
- R09i (room-activity runtime parity) is a blocking prerequisite for R14c unless the Product Owner explicitly waives it.

## Issue #17 requirement mapping

Every Issue #17 (Epic) requirement maps to either an R14a decision or an explicit defer.

| #17 Requirement | R14a status |
| --- | --- |
| Migrate legacy global state into one PO-named room | R14a § Source-to-target mapping; R14b implements. |
| Atomic offline migration (no startup magic) | R14a § Atomicity and idempotency; new dedicated `cmd/room-cutover`. |
| Idempotent migration; no `--force` / `--reset`; hash drift rejected | R14a § Atomicity and idempotency. |
| No permanent `main`, `room 0`, hidden default room | R14a § Non-goals; ADR 003 §3.4. |
| Single source of truth after cutover | R14a § Source-to-target mapping; legacy tables read-only then dropped. |
| Old global routes transition via explicit route retirement (no hidden default-room proxy) | R14a § Compatibility & endpoint retirement phases; ADR 003 §3.1. |
| `/ws` cutover with no synthesized default-room fallback | R14a § WebSocket cutover; ADR 003 §3.4. |
| Frontend cutover with state-reset, navigation, recovery | R14a § Frontend transition & recovery. |
| R13 authorization hardening remains separate | R14a § Out of scope. |
| Final implementation plan split into small future sprints with migration gates, verification, rollback | R14a § Future sprint sequence + § Deployment compatibility matrix. |
| Route-unregistered envelope with documentation pointer | R14a § Errors and the route-unregistered responses. |
| Migrated room identity contract (slug, name, host, conflict) | R14a § Migration identity contract. |
| Source rows deleted, archived, or retained as evidence | R14a § Source-to-target mapping: retained as migration evidence, read-only for rollback window, dropped in R14e. |
| Activities handled explicitly (not silently dropped) | R14a § Activities target: new `room_activities` table + dedicated repo. Read parity surfaced as PO blocker. |
| `/api/vote/prioritize` parity before retirement | R14a § Future sprint sequence: blocked on R09h. |
| Frontend rollback behaviour if newer frontend with older backend | R14a § Deployment compatibility matrix. |
| R14a documentation-only | R14a § Non-goals (explicit). |

## Verification evidence expectations

R14a Builder evidence MUST include:

- `git diff --check` — clean.
- `git diff --name-only` contains only documents under `documents/00-project-management/`. No `*.go`, `*.vue`, `*.sql`, `Dockerfile*`, `nginx/*`, `*.env*`, `package*.json`, `package-lock.json`, `go.mod`, `go.sum`, anything under `cmd/`, `internal/`, `frontend/src/`, `docker/`, `certs/`, `letsencrypt-*`, `extension/`, `.gitignore`, or `.gitattributes`.
- File references throughout the R14a documents that prove the current route and table inventory (this document's § Current behavior cites every file the inventory requires).
- A mapping table for every Issue #17 requirement to an R14a decision or explicit defer (see § Issue #17 requirement mapping).
- Confirmation that no runtime files changed.
- Confirmation that R13, R10f+, R11b+, and R12 were not silently pulled into scope.
- Confirmation that the legacy global tables were NOT dropped, archived, or mutated in R14a.
- Confirmation that the R03 CLI / `migration_marker` is NOT reused for the room cutover.
- Confirmation that ADR 003 does not mark itself Accepted.
- Confirmation that R09h, R05b, and R14b–R14e are NOT marked active in any tracker.

No runtime command may be claimed as passing unless actually run. Backend / frontend test suites are optional for a docs-only diff; if a touched documentation check or link checker exists, run the focused check.

## Risks and review focus

Risk level: **High**. This sprint is documentation-only, but it shapes future data-loss and breaking-contract work.

Review must focus on:

- whether every live global source has a lossless target;
- whether activities are handled explicitly (R14a designs a new `room_activities` table; R14b builds it; live read parity is surfaced as a PO blocker);
- whether the host bootstrap is deterministic and server / operator controlled;
- whether retry, backup, rollback, and source cleanup timing are safe;
- whether global route retirement can accidentally select or mutate the wrong room (the deployment compatibility matrix is the safeguard);
- whether frontend and backend can be deployed in a compatible order (the compatibility matrix);
- whether security hardening is honestly deferred rather than implied complete (R13 is separate);
- whether the future slices are small enough to review independently (R05b / R09h / R14b / R14c / R14d / R14e are NOT combined);
- whether R03's `cmd/migrate-data` + `migration_marker` are NOT reused for the room cutover;
- whether the JSON conversion contract uses the default `encoding/json` tagged-field round-trip (no custom `MarshalJSON`/`UnmarshalJSON` claim);
- whether the queue-prioritize / vote-prioritize routes are correctly separated (only `/api/vote/prioritize` needs R09h).

## Builder reasoning effort

**High.** This is a cross-layer contract and data-migration planning sprint with data-loss, compatibility, and deployment risk. The output must be precise enough that later Builders do not invent behaviour while implementing R05b / R09h / R14b–R14e.

## Cross-references

- ADR 003 — Legacy global-state migration and contract retirement: [`../ADRS/003-legacy-global-state-migration-and-contract-retirement.md`](../ADRS/003-legacy-global-state-migration-and-contract-retirement.md).
- ADR 001 — Room architecture and contracts (metadata-only supersession note): [`../ADRS/001-room-architecture-and-contracts.md`](../ADRS/001-room-architecture-and-contracts.md).
- ADR 002 — PostgreSQL migration design (R06-fold attribution reconciled via ADR 003): [`../ADRS/002-postgresql-migration-design.md`](../ADRS/002-postgresql-migration-design.md).
- Room epic sequence: [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md).
- R03 migrator patterns (preserved untouched; NOT reused by R14b): [`009-sqlite-to-postgresql-data-migration.md`](./009-sqlite-to-postgresql-data-migration.md).
- R05d room-queue prioritize runtime (R07d): [`012-room-scoped-playback-queue.md`](./012-room-scoped-playback-queue.md) (R07d section).
- R09b room-vote runtime (mirrors R09h session shape): [`014-player-control-semantics.md`](./014-player-control-semantics.md) (R09b Implementation summary).
- R09e / R09f / R09g room auto-queue contract + runtime: [`021-room-auto-queue-contract-design.md`](./021-room-auto-queue-contract-design.md).
- R10a / R10b / R10c / R10d room deletion: [`015-room-deletion-and-membership-removal.md`](./015-room-deletion-and-membership-removal.md).
- R11a room chat: [`016-room-chat-feature.md`](./016-room-chat-feature.md).
- Issue #17 (epic): the GitHub issue referenced from the sprint directive.
- Issue #20 (R14a spec): the GitHub issue this document implements.
