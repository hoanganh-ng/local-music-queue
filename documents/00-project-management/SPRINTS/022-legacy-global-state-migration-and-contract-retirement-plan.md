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

R14b is a backend-only sprint that ships **the mechanism** as a new dedicated CLI and verifies it against a representative PostgreSQL snapshot. R14b does NOT retire the global runtime in production; it lands the CLI, the marker contract, the source-to-target copy, the dry-run, the verification, and the rollback hook. R14b's verification gate MUST pass before R14c can run.

- R14b ships `cmd/room-cutover` with subcommands `plan` (read-only snapshot + dry-run), `up` (apply), `verify` (re-assert hash record), and `abort` (release the advisory lock without committing).
- R14b ships the marker table `room_cutover_marker` (single-row, `id = 1`) as described above.
- R14b ships the binary build flag `--room-cutover-authoritative` (default `false`; in R14b this is `false` in every deployment). When `false`, the legacy global routes continue to serve from the legacy repositories **unchanged**; the cutover migrator reads from the legacy tables and writes to the room tables, but the server still serves the global routes for concurrent testing. This is the verification state.
- R14b verification gate (must pass before R14c can run):
  - `cmd/room-cutover plan` against a fresh PostgreSQL snapshot returns a report with no PII (numeric ids + sha256 + counts only).
  - `cmd/room-cutover up --dry-run` against the same snapshot completes with no writes and a successful SHA256 record.
  - `cmd/room-cutover up` (real run) against the same snapshot completes the source-to-target copy inside one transaction, then commits the marker, then returns 0.
  - Re-running `cmd/room-cutover up` immediately after returns `already cut over; no-op` (exit 0), driven by the marker.
  - Hash drift on either source or target is detected and rejected (no `--force` / `--reset`).
  - PII redaction verified (no email, no OAuth token, no display name, no session token in the report).
  - The marker preserves the R03 `migration_marker` row untouched.
  - `go test ./...` clean.

#### Phase B — coordinated production cutover (executed by R14c)

R14c owns the **coordinated production execution**: a planned maintenance window, a redeploy with the **binary build flag `--room-cutover-authoritative=true`**, and the run of `cmd/room-cutover up` against the live PostgreSQL. R14c is the ONLY sprint where the production binary is changed.

- Before the window: a `pg_dump` is taken and retained ≥30 days (mirrors ADR 002 §9). The R14b binary is already deployed and the R14b verification gate has passed.
- During the window:
  - The application is shut down (or restarted with a maintenance-mode flag that disables ALL mutating endpoints — global AND room — while keeping reads disabled too in this state).
  - `cmd/room-cutover up` runs against the live database. It pins a connection, takes the advisory lock, opens a single transaction, copies the source rows to the target rows, commits, writes the marker.
  - The application restarts with `--room-cutover-authoritative=true`. In this build:
    - The legacy global `/api/queue/...` / `/api/vote/skip` / `/api/autoqueue/...` routes and the legacy `/ws` endpoint are **unregistered**, NOT just "must not be hit". The room routes are the only registered set.
    - The legacy repositories are still in the binary for read-only rollback support, but they are NOT wired into HTTP handlers or the WS hub.
    - On the cutover boundary, the application restart is the only window where the global routes are unreachable — this is the documented maintenance window.
  - The legacy global repositories write paths are also disabled by `--room-cutover-authoritative=true` to prevent any tool / script / cron that bypassed HTTP from mutating the legacy tables during the rollback window. The `--safe-write` mode is OFF in this build for legacy tables.
- Frontend deployment order is captured in § Deployment compatibility matrix; the cutover binary is paired with the post-R05b + post-R14d frontend at production cutover time.

#### Phase C — frontend cutover (R14d)

- `RoomView` becomes the only supported view; `DashboardView` is removed or redirects to a Welcome / room entry surface.
- `globalStore.queueState`, `globalStore.voteSessions`, and `globalStore.autoQueueConfig` are cleared.
- `globalStore.currentUser` is preserved (RoomView still reads it).
- `globalStore.roomQueues[slug]` per-slug slices are preserved (RoomView reads them).
- The global `WebSocketClient` is closed and not reconnected.
- **Frontend entry UX is a prerequisite**, not part of R14d: the unbuilt SPA client methods (`api.createRoom`, `api.listRooms`, `api.getRoom`, `api.joinRoom`, the invite endpoints, `api.claimPlayerLease`, `api.heartbeatPlayerLease`, `api.releasePlayerLease`, `api.getPlayerLease`) MUST exist before R14c can run. This is the **R05b — Room entry / player-lease UI** prerequisite sprint (see § Future sprint sequence). R05b is the slice that ships these frontend methods and exposes them as actual UI (room create form, room list, room join, invite redeem, lease claim UI). R14d assumes these exist.

#### Phase D — schema cleanup (R14e)

- Drop the legacy `queue_state`, `activities`, `auto_queue_config`, and `play_history` tables.
- Reduce `0001_initial.up.sql` to only the account-scoped tables (`users`, `user_sessions`, `priority_transactions`); preserve the historical migration files on disk so the v8 era remains auditable.
- Schema version bumps from 8 to 9 (only if R14e accepts and destructive cleanup completes).
- Phase D is destructive cleanup only and runs **after** a verified rollback window AND after no runtime path references the legacy tables.

### WebSocket cutover

- `/ws` is **unregistered** in the `--room-cutover-authoritative=true` build (R14c). It returns HTTP 404 from the mux because the handler is not registered. (404 is the natural outcome of unregistering — R14a does NOT claim a `410 Gone` for `/ws` because there is no path to retire; the upgrade is refused.)
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- `seq_num` continuity: per-room sequence numbers restart from the per-room hub's own counter (which is independent of the global hub's counter); this is the existing R07b behaviour and is not changed by R14a.
- `/ws` MUST NOT silently join an arbitrary room under any circumstance.
- Per-room event families and initial sync ownership remain per-room.
- The first per-room `room_queue_sync` after migration lands carries the migrated room's queue state under the migrated room's `seq_num` allocation.

### Frontend transition & recovery

- Where users land after upgrade: when no active room is selected, redirect to a room entry surface (or Welcome). Users with the migrated room bookmarked land directly on the migrated room's `RoomView`. The room entry surface REQUIRES the R05b SPA client methods and routes.
- The migrated room is presented to its initial host and to other existing accounts via the existing room-frontend entry points; no automatic join.
- Global queue / store / WebSocket state is cleared (per Phase C); per-slug `roomQueues[slug]` slices preserved; `currentUser` preserved.
- Stale bookmarks, stale local storage, archived-room view, and unregistered-route responses surface as clear UI states; the frontend MUST NOT attempt to fall back to the unregistered endpoints.
- Frontend cutover is paired with the binary cutover at production time per § Deployment compatibility matrix.

### Deployment compatibility matrix

The cutover involves two build flags (`--room-cutover-authoritative` per-server, R05b frontend bundle version per-client) and two cutover states (pre-cutover vs post-cutover binary). Valid combinations only:

| Server binary | Frontend SPA | Legacy global routes | Per-room routes | Result |
| --- | --- | --- | --- | --- |
| `--room-cutover-authoritative=false` (pre-R14c) | pre-R05b (no entry UI; DashboardView only) | Registered; serve legacy repos | Registered | Pre-cutover baseline. Documented. |
| `--room-cutover-authoritative=false` (post-R14b; pre-R14c) | post-R05b (entry UI exists; user can create / list / join rooms / claim lease) | Registered; serve legacy repos | Registered | The R14b verification state. Users can use both surfaces. |
| `--room-cutover-authoritative=true` (post-R14c) | post-R14c + post-R05b + post-R14d (DashboardView removed / redirected) | **Unregistered** (HTTP 404 from mux); `/api/vote/prioritize` may still be live if R09h has not yet landed and is excluded from the unregistration set | Registered; sole authoritative path | Production cutover state. Documented. |
| `--room-cutover-authoritative=true` (post-R14c) | post-R05b only (DashboardView still present) | **Unregistered** | Registered | Frontend shows DashboardView, but the legacy routes it calls return 404. This is a **BROKEN** combination — the compatibility matrix forbids it. Either pair `--room-cutover-authoritative=true` with the R14d frontend, or keep the pre-R14c binary. |
| R03 binary (legacy) | any | Registered (legacy repos) | Registered | Pre-cutover baseline. Documented as the rollback entry point if Phase B fails: restart the R03 binary, the `room_cutover_marker` remains in place, but the legacy tables are untouched and the room tables are the new (empty / partial) copies. Forward recovery may be needed depending on what mutations landed. |
| `--room-cutover-authoritative=false` (R14c mid-window; binary restarted before `cmd/room-cutover up`) | any | Registered; serves legacy repos | Registered | Mid-window rollback state. Documented. The `pg_dump` from the start of the window is the source of truth. |

**Client-continuity claim:** the only combinations that guarantee client continuity are the documented ones. Specifically: the `post-R05b + pre-R14c` combination is safe to deploy before the cutover window; the `post-R14c + post-R14d` combination is the production cutover state. **There is NO combination in which a frontend relying on legacy global routes is connected to a server that has unregistered those routes.** This is the client-continuity guarantee.

### Deployment, backup, and rollback gates

- Required PostgreSQL `pg_dump` BEFORE R14c cutover; retained ≥30 days (mirrors ADR 002 §9).
- Application downtime / maintenance window is required during Phase B.
- `cmd/room-cutover plan` (read-only) and `cmd/room-cutover up --dry-run` MUST be run before any commit.
- Target verification (`verifyWithinTx` + `cmd/room-cutover verify` post-commit) MUST pass before R14c accepts writes on the room-authoritative version.
- Migration succeeds but application startup fails: the documented rollback entry point is to restart the pre-R14c binary (which still serves legacy routes). The room tables are then partially populated from the R14b run; forward recovery against the `pg_dump` is the safe path.
- **Explicit warning:** rollback after new room writes may require forward recovery or data reconciliation; the compatibility matrix is the deployment-time safeguard.

## Future sprint sequence

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R05b — Room entry / player-lease UI** | Frontend only (no new HTTP routes; uses the existing R04 / R06 / R11a routes). Adds SPA client methods `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / invite + `api.claimPlayerLease` / `api.heartbeatPlayerLease` / `api.releasePlayerLease` / `api.getPlayerLease`; exposes them as actual room create / list / join / invite-redeem / lease-claim UI. **BLOCKING PREREQUISITE for R14c** — without it, retiring `/api/queue` / `/ws` etc. strands users on a global dashboard that has no way to enter a room. | The existing backend routes are already registered (per `cmd/server/main.go:413–486`); R05b only wires the SPA. |
| **R09h — Room vote-to-prioritize parity** | Backend room-vote-prioritize parity. Adds `POST /api/rooms/{slug}/vote/prioritize`. **BLOCKING PREREQUISITE for R14c** to retire `/api/vote/prioritize` only. | Reuses existing `room_queue_song_prioritized` event. No new WS event constant. |
| **R14b — room-cutover mechanism** | Backend-only. New dedicated CLI `cmd/room-cutover` with `plan` / `up` / `verify` / `abort` subcommands. New marker table `room_cutover_marker`. Source-to-target copy (legacy → room). `--room-cutover-authoritative=false` build flag (default `false`). Pre-commit + post-commit verification. **NO global route removal. NO frontend changes.** R14b's binary ships the mechanism and verifies it; R14b's gate MUST pass before R14c can run. | Does NOT reuse R03's CLI or `migration_marker`. R03's marker is preserved untouched. |
| **R14c — coordinated production cutover** | Backend production cutover (Phase B). Maintenance window + restart with `--room-cutover-authoritative=true` binary + run `cmd/room-cutover up` against the live DB. Unregisters the legacy global `/api/queue/...` / `/api/vote/skip` / `/api/autoqueue/...` routes and the global `/ws` endpoint. Pairs with the R14d + R05b frontend in the deployment compatibility matrix. | `/api/vote/prioritize` is excluded from the unregistration set until R09h is accepted; the cutover binary may retire the OTHER legacy routes without R09h. |
| **R14d — frontend global-path removal** | Frontend only. Removes / redirects `DashboardView`; clears `globalStore.queueState` / `voteSessions` / `autoQueueConfig`; closes global `WebSocketClient`; preserves `currentUser` + `roomQueues[slug]`. | Assumes R05b has landed. |
| **R14e — schema cleanup** | Backend only. Drops legacy `queue_state`, `activities`, `auto_queue_config`, `play_history`; reduces `0001_initial.up.sql` to account-scoped tables only; bumps schema v8 → v9 (only if R14e accepts). | Runs after the verified rollback window AND after no runtime path references the legacy tables. Historical migration files remain on disk. |

Sprint names may be renamed if evidence requires it, but R05b / R09h / R14b / R14c / R14d / R14e MUST NOT be combined; migration, destructive cleanup, frontend rewrite, frontend entry UX, and vote-prioritize parity must remain independently reviewable.

## Validation, errors, and test inventory expectations

R14a documents the expected test inventory; tests are NOT in R14a scope.

### Validation rules

- Migrated room slug: server-side regex / length rules mirror the existing room-slug validation in `internal/domain/repository/room_repository.go`.
- Migrated room display name: explicitly decide whether it must equal the slug or may differ under the current room model. **R14a recommends allowing them to differ**, mirroring the existing room model.
- `--host-user-id`: positive integer; resolved by PK lookup against `users`.
- Idempotency: re-run = no-op exit 0; hash drift = reject.
- Archived-room / missing-host failures map to explicit sentinels.

### Errors and the route-unregistered responses

- After R14c, unregistered legacy global REST routes and `/ws` return HTTP 404 from the mux because the handlers are not registered.
- The proposed envelope body for documentation purposes (NOT carried on the wire — the mux 404 is the actual response):

```json
{
  "error": "gone",
  "code": "global_contract_retired",
  "documentation": "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"
}
```

- Clients discover the migrated room through the standard `GET /api/rooms` listing endpoint, which is the documented successor-discovery surface.
- The HTTP `Link: </api/rooms>; rel="successor-version"` header MAY be added if the Product Owner wants explicit discovery.
- The first R14a draft's mistake was carrying a concrete `successor: "/api/rooms/{slug}/..."` value in the envelope; R14a drops that field. The successor room is discovered, not pre-supplied.

### Migration report fields

- Operator-supplied numeric `users.id` for the host.
- Numeric row counts per table.
- SHA256 hashes per migrated table.
- Sequence names resynced.
- Durations per phase.
- `legacy_id_offset` for `room_play_history`.
- `binary_build_sha` for the marker.
- No email, OAuth tokens, display names, session tokens, credentials, cookies, or `Authorization` headers.

### Future test matrix (NOT in R14a scope)

- R05b: frontend tests for `api.createRoom` / `api.listRooms` / `api.getRoom` / invite / lease methods; UI tests for the entry surfaces.
- R09h: `room_vote_prioritize` interactor tests (session create, vote, threshold, expiry); HTTP handler tests (auth, body shape, success, 400 / 403 / 404 / 409 mapping); race-sensitive tests; WebSocket event emission tests (`room_vote_updated`, `room_vote_resolved`, `room_queue_song_prioritized`).
- R14b: focused migration tests — `room_activities` lossless copy; `room_queue_state` JSON round-trip + SHA256; sequence resync; idempotent re-run (no-op); hash-drift rejection; offline CLI smoke; advisory-lock conflict; transaction rollback on mid-flight failure; report PII redaction; dry-run output; legacy-id-offset calculation correctness.
- R14b repository tests: `RoomActivityRepository.AddActivity` / `GetActivities`.
- R14c: HTTP 404 tests for unregistered routes; `/ws` upgrade rejection; per-room WebSocket event inventory unchanged; success-path routing.
- R14d: frontend tests for `globalStore` clearing on cutover; `currentUser` preservation; `roomQueues[slug]` preservation; global `WebSocketClient` close; toast / redirect for unregistered responses; `DashboardView` removal.
- R14e: focused cleanup tests; legacy-table-drop verifies no runtime path references them; schema version bumped to 9.

## Schema-version plan

The schema version stays **v8** throughout R14a, R14b, R14c, R14d, and during the rollback window after R14c. The v9 bump lands in **R14e** and only if R14e accepts destructive cleanup. If the rollback window must be extended or a forward-recovery path is needed, v9 is delayed.

**No historical migration files are removed by R14a.** All of `0001_initial.up.sql` .. `0008_room_chat_messages.up.sql` remain on disk; a future `0009_room_activities.up.sql` (added in R14b) is additive. R14e may drop legacy tables and reduce the `0001_initial.up.sql` content to only the account-scoped tables (`users`, `user_sessions`, `priority_transactions`) so the historical migration files still describe what they originally created.

## ADR reconciliation

R14a supersedes parts of ADR 001 and the R06-fold attribution in ADR 002 §11/§13 via the new ADR 003 (`documents/00-project-management/ADRS/003-legacy-global-state-migration-and-contract-retirement.md`). ADR 003 is **Proposed** (R14a contract; pending Architect review and Product Owner acceptance). ADR 001 body §3–§18 is unchanged byte-for-byte except for one metadata supersession note near the header. ADR 002 body is unchanged (the §11/§13 R06-fold attribution is reconciled inside ADR 003 rather than edited in ADR 002). ADR 001 §3 Decision 2 (no permanent `main` room), §4 (lifecycle), §5 (lease model), §7 (invite model), §16 (deferred work), and the "single-instance only" posture remain authoritative.

## PO acceptance blockers

These are explicit blockers that the Product Owner must sign off on before R14c can run (and before R14e can land):

1. **Live room-activity read parity.** R14a designs the `room_activities` table and the repository contract; **live read parity (a `RoomView` activity panel that replaces the global dashboard's activity surface) is NOT in this sprint and is NOT required for R14c or R14d**. If the Product Owner wants read parity in the room frontend before accepting R14c, it must be added as a separate frontend task and its scope must be settled in a focused sprint before R14d (or R14c depending on ordering). Confirming acceptance of R14c without read parity may strand users who used the global activity surface.
2. **Slug-collision resolution.** If the migrated room slug already exists in `rooms` at cutover time with a different identity, the migrator fails loudly. The recovery path is operator-driven (rename the existing room and re-run, or pick a different slug). No automatic rename or proxy.
3. **Route-unregistered response posture.** After R14c, unregistered legacy routes return HTTP 404 from the mux. The optional envelope body is `{error, code: "global_contract_retired", documentation}` (no concrete `successor` value). Clients discover the migrated room via `GET /api/rooms`. The HTTP `Link: </api/rooms>; rel="successor-version"` header is optional.
4. **Rollback posture.** Rollback after the binary restart with `--room-cutover-authoritative=true` is **NOT a transparent rollback** — it requires either restarting the pre-R14c binary (which still serves legacy routes; the room data is partially populated) or a forward-recovery path against the `pg_dump`. The compatibility matrix in § Deployment compatibility matrix is the deployment-time safeguard. The Product Owner must accept this posture.
5. **R09h landing prerequisite for `/api/vote/prioritize` retirement.** Until R09h is accepted, R14c keeps `/api/vote/prioritize` registered. The cutover binary may retire the OTHER legacy routes but explicitly EXCLUDES `/api/vote/prioritize` from the unregistration set.
6. **R05b landing prerequisite for user-facing cutover.** Until R05b is accepted, no user can create / list / join rooms from the SPA. R14c is therefore blocked.

## Items already settled (informational)

- ID-collision handling for `room_play_history` (see § room_play_history id-collision handling).
- Account-table posture: no recopy (see § Source-to-target mapping).
- Schema version stays v8 in R14a; v9 only if R14e lands.
- Historical migration files preserved on disk.
- The R03 `cmd/migrate-data` + `migration_marker` is preserved untouched and is the SQLite-to-PostgreSQL migrator; it is NOT reused for the room cutover. R14b adds a SEPARATE `cmd/room-cutover` + `room_cutover_marker`.
- Per-room `room_play_history` 50-row cap is enforced in `roomautoqueue`, matching R09f.
- The `entity.Queue` JSON conversion contract uses the default `encoding/json` tagged-field round-trip; no custom `MarshalJSON` / `UnmarshalJSON` methods are assumed.
- The queue-prioritize / vote-prioritize distinction: global `/api/queue/prioritize` has a room equivalent (`/api/rooms/{slug}/queue/prioritize`, R07d) and retires in R14c without a parity sprint; only `/api/vote/prioritize` needs R09h.
- WebSocket: `/ws` is unregistered in the cutover build, returning HTTP 404 from the mux. No `410 Gone` is claimed for `/ws`.
- Per-room sequence numbers restart from the per-room hub's own counter.
- The cutover CLI is offline; the application is shut down during Phase B.
- A hidden default-room `/ws` is forbidden.

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
