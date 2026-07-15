# R14a — Legacy Global State Migration and Contract Retirement Plan

**Status:** Proposed (planning/design — documentation only). No Go runtime code, no Vue runtime code, no migrations, no routes, no WebSocket constants, and no config changes are introduced by this sprint. R14a narrows and precedes the legacy R14 stub (`019-global-contract-cleanup-and-documentation.md`); it shapes the implementation-ready contract for R14b, R14c, R14d, R14e and records R09h as a blocking prerequisite for `/api/vote/prioritize` retirement. R14a supersedes parts of ADR 001 via the new ADR 003 (Proposed, pending Architect review and Product Owner acceptance). R13 (auth/session/authorization hardening), R10f+ (deferred room lifecycle hardening), R11b+ (deferred R11 chat slice), and R12 (search and room discovery) remain out of scope.

**Sprint name:** Legacy global state migration and contract retirement plan (planning/design only)

**Parent epic:** Issue #17 (legacy global contract cleanup). This sprint addresses the same constraint set that #17 records; #20 is the authoritative spec.

## Goal

Produce an implementation-ready contract for converting the legacy global state into exactly one Product-Owner-named room and for retiring the parallel global REST/WS contracts in controlled later implementation slices. The sprint removes ambiguity before any destructive migration or compatibility removal is implemented. It separates data conversion, compatibility behavior, frontend cutover, and final contract removal into independently reviewable future sprints. Authentication/authorization hardening remains a separate R13 concern.

## Non-goals

- No Go runtime code, no Vue runtime code, no PostgreSQL migration in this sprint.
- No HTTP route additions or removals; no WebSocket event additions or removals; no payload changes.
- No schema version bump in R14a (stays v8; v9 is R14e).
- No Docker, Nginx, TLS, environment, dependency, or deployment changes.
- No automatic destructive startup migration (mirrors the R03 operator-driven posture).
- No R13 redesign; no R10f+ retention/audit/revocation/archived-room cleanup; no R11b+ chat slice; no R12 search.
- No multi-instance / cross-process safety claims (single-instance only).
- No new WebSocket event constant for vote-to-prioritize (reuses the existing `room_queue_song_prioritized` event).

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
- New public API versioning unless required solely to express the retirement contract.
- Creating a permanent `main`, `room 0`, hidden default room, or writable global fallback.

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

**Critical:** `room_activities` **does NOT exist**. `grep -rn "room_activities\|RoomActivit"` returns zero matches across `*.go`, `*.sql`, `*.js`. There is no room-scoped activity table, migration, entity, or repository. The global `activities` table has no room target today; R14a designs one (see Desired behavior § Source-to-target mapping).

Type mismatch: `queue_state.data` is **TEXT**; `room_queue_state.data` is **JSONB**. Both serialize the same `*entity.Queue` aggregate; the migrator converts via `(*entity.Queue).MarshalJSON` / `UnmarshalJSON` round-trip + SHA256 over canonical bytes.

Migrations embedded via `//go:embed migrations/postgres/*.sql` (`migrations_postgres.go:14-15`), run at startup by `RunEmbeddedMigrationsUp` (`migrator.go:23`, called `main.go:169`).

### REST routes (`cmd/server/main.go`)

**Global routes registered under `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...`, plus auth and a few utilities** — handlers in `internal/delivery/http/handlers.go` (registered at `main.go:354–409`) and `internal/delivery/http/autoqueue_handler.go`:

| Route | main.go | Room equivalent |
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
| `POST /api/queue/prioritize` | `:401` | `POST /api/rooms/{slug}/queue/prioritize` |
| `GET /api/user/priority-balance` | `:402` | OUT — account-scoped per ADR 001 §3 Decision 9 |
| `GET /api/youtube/search` | `:403` | OUT — global utility |
| `POST /api/vote/skip` | `:404` | `POST /api/rooms/{slug}/vote/skip` |
| `POST /api/vote/prioritize` | `:405` | **`/api/rooms/{slug}/vote/prioritize` (planned — R09h, BLOCKING for R14c retirement)** |
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

### WebSocket endpoints

**Global `/ws`** — `mux.HandleFunc("/ws", hub.RegisterHandler)` at `main.go:588`. Hub built by `ws.NewHub(qInteractor.GetState)` at `:232`; `go hub.Run()` at `:235`. State read is **GLOBAL** — `qInteractor.GetState` over `queue_state`. `(*Hub).RegisterHandler` lives at `internal/delivery/ws/hub.go:244`. The 16-event inventory lives at `internal/delivery/ws/events.go:11-35`: `full_sync`, `user_joined`, `song_added`, `song_skipped`, `status_changed`, `elapsed_sync`, `song_previous`, `song_removed`, `queue_cleared`, `volume_changed`, `song_prioritized`, `priority_balance_updated`, `vote_updated`, `vote_resolved`, `auto_queue_added`, `auto_queue_config_changed` (+ additive `error` `:30` and `room_archived` `:35` which also rides global `/ws` via the R06 sweeper `hubArchiveBroadcaster` at `main.go:688-699`).

**Room `/ws/rooms/{slug}`** — `mux.HandleFunc("/ws/rooms/{slug}", roomWSHub.RegisterHandler)` at `main.go:591`. Built by `ws.NewRoomWSHub(...)` at `:242-258` with three resolvers (room-by-slug → `pgRoom.GetRoomBySlug`; member → `pgRoom.GetMember`; queue state → `roomQueueInteractor.GetStateByRoomID`). `go roomWSHub.Run()` at `:261`; `SetSessionResolver(authInteractor)` at `:260`. Hub at `internal/delivery/ws/room_hub.go` (constructor `:168`, `dispatch :362`, broadcasters `:374-528`, `nextSeq :209`). State read is **PER-ROOM**. Per-room events (`events.go:39-98`): `room_queue_sync`, `room_queue_song_added`, `room_queue_song_removed`, `room_queue_cleared`, `room_queue_song_prioritized`, `room_playback_status_changed`, `room_playback_elapsed_sync`, `room_playback_song_advanced`, `room_vote_updated`, `room_vote_resolved`, `room_playback_volume_changed`, `room_playback_song_previous`, `room_auto_queue_added`, `room_auto_queue_config_changed`, `room_member_removed`, `room_members_changed`, `room_chat_message_created`, plus per-room `room_archived` (R10b/R06).

### Frontend entry points

- `frontend/src/router/index.js`: `/` → `DashboardView` (global, `:6-11`); `/auth` → `AuthView` (`:12-16`); `/rooms/:slug` → `RoomView` (`:17-25`); guard `:33-51`. **No `Welcome` / `CreateRoom` / `JoinRoom` / `Invite` routes** — ADR 001 §3 Decision 10 views were never built; rooms entered by direct URL only.
- `frontend/src/store/index.js`: single `globalStore` (`:33`). Global slices: `currentUser`, `queueState{...}`, `voteSessions`, `autoQueueConfig{enabled, strategy}`, `connectionStatus`. Isolated per-room slices under `globalStore.roomQueues[slug]` (`:241+`, R07c) with mutators `applyRoomSongAdded :343`, `applyRoomPlaybackStatusChanged :428`, `applyRoomAutoQueueAdded :527`, `applyRoomAutoQueueConfigChanged :549` (explicitly MUST NOT touch global `autoQueueConfig`), `markRoomArchived` / `markRoomRemovedAsCurrentUser` / `applyRoomMembersChanged :557+`, chat mutators `:631 / :642`; `MaxRoomChatMessages = 100` `:11`.
- `frontend/src/services/api.js`: global methods still present (`getQueue :68`, `addSong :74`, `skipSong :85`, `setStatus :92`, `syncPlayback :99`, `songEnded :106`, `prevSong :112`, `removeSong :119`, `clearQueue :126`, `changeVolume :133`, `searchYouTube :141`, `prioritizeSong :148`, `getPriorityBalance :155`, `castSkipVote :162`, `castPriorityVote :169`, `getAutoQueueStatus :177`, `setAutoQueueEnabled :183`, `login` / `loginWithGoogle :53 / :60`). Room methods `:196-368`. **Gap:** no `createRoom` / `listRooms` / `getRoom` / invite / player-lease client methods — those backend routes are not called by the SPA (room create/join/invite/lease UX unbuilt).
- WS clients: global `frontend/src/services/websocket.js` (`wsClient`, `connect :99` builds `+ '/ws' :107`, `handleMessage :159` per-event cases `:171-298`), consumed by `DashboardView`. Room `frontend/src/services/room-websocket.js` (`createRoomWsClient :11`, builds `.../ws/rooms/{slug}` `:12`, seq-gap recovery, `send()` no-op `:40`, passes `CloseEvent` to `onClose`), consumed by `RoomView`.
- `DashboardView.vue`: GLOBAL only. Imports `globalStore` / `api` / `wsClient` `:110-112`. Reads `globalStore.queueState.*` `:72-99`. `wsClient.connect()` `:221` / `disconnect()` `:265`. This is the legacy global surface to retire in R14 Phase C.
- `RoomView.vue`: ROOM only but reuses `globalStore` (`:268-270`). Reads `globalStore.roomQueues[slug]` `:283-286` and `globalStore.currentUser` `:281`. Routes per-room WS events through room mutators `:390-510`. REST recovery `api.getRoomQueue` / `getRoomChatMessages` `:541 / :585`. **Boundary:** a full global-store teardown in R14d must preserve `currentUser` (RoomView depends on it) AND the per-slug `roomQueues[slug]` slices.

## Desired behavior (post-R14a, sketched)

R14a does not implement any of this. R14a designs the contract.

### Source-to-target mapping

| Source (global) | Target (room) | Conversion notes |
| --- | --- | --- |
| `queue_state` (TEXT JSON, singleton `id = 1`) | `room_queue_state` (JSONB, one row per `rooms.id`) | Marshal/Unmarshal round-trip via `(*entity.Queue).MarshalJSON` / `UnmarshalJSON`; SHA256 over canonical bytes; full invariant preservation (`Songs`, `CurrentIndex`, `Status`, `Elapsed`, history fields, first-song / current-song behaviour); `room_queue_state` row PK = `rooms.id`; migrator writes one row per room (the migrated room, in R14b's case one row). |
| `activities` | new `room_activities` (R14b builds) | Lossless copy of every row with preserved `id`, `timestamp`, `type`, `user`, `description`, count, and ordering; `room_id` set to the migrated room's id; sequence resync via `setval(seq, MAX(id), is_called=true)`. |
| `auto_queue_config` (singleton `id = 1`) | `room_auto_queue_config` (already exists, migration 0007) | Keyed by `room_id`; migrator writes the migrated room's row with the legacy singleton's values. |
| `play_history` (50-row cap, Go-enforced) | `room_play_history` (already exists, migration 0007) | Lossless copy of every row with preserved `id`, `played_at`, `video_id`, `title`; per-room 50-row cap preserved (Go-enforced, mirrors `PlayHistoryCap`); sequence resync. |

After the copy, source rows are retained as migration evidence (read-only for the rollback window) and dropped in R14e. The room targets are the sole writable source of truth after cutover.

`users`, `user_sessions`, and `priority_transactions` remain account-scoped (per ADR 001 §3 Decision 9) and are NOT migrated into a room.

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
- Migrated room slug already exists with identical identity AND `migration_marker` proves the same completed migration → return `already migrated; no-op`, exit 0.
- Migrated room slug already exists with identical identity but `migration_marker` shows a different completed migration → fail on hash drift; do NOT overwrite.
- `--host-user-id` not resolvable → fail before any commit.

**Migration report** — PII posture:

- MAY contain: the operator-supplied numeric `users.id` for the host; numeric row counts; sha256 hashes; sequence names; table names; durations.
- MUST NOT contain: email addresses; OAuth tokens / ID token claims; any `display_name` / `user` text from activities rows; session tokens; any credential, cookie, or `Authorization` header value.
- DSN redaction uses `config.RedactDSN` (existing R03 helper).

### Atomicity and idempotency (R03 reuse)

The future R14b migrator reuses the R03 patterns verbatim, NOT as a startup-magic transform:

- `pg_try_advisory_lock(987654321)` on a pinned connection (`migrator.go:442`); error if held; deferred unlock at `:449`.
- Single transaction: `conn.BeginTx :452` → `tx.Commit :533`; `tx.Rollback() :459` on any error.
- `migration_marker` table (R03, migration 0003) extended with per-table SHA256 hashes for the new `room_activities` and per-room `room_queue_state` target.
- Re-run on identical source = `already migrated; no-op`, exit 0.
- Hash drift on target = rejected (no `--force` / `--reset`).
- `verifyWithinTx :1125` (pre-commit COUNT/MIN/MAX + `queue_state` byte length parity) and `verifyAfterCommit :1189` (post-commit sanity).
- `Options.DryRun :249` for a report-only run; writes suppressed at `:424`.
- `config.RedactDSN` in the report (existing helper).
- Sequence resync via `setval(seq, MAX(id), is_called=true)` for each migrated sequence.
- Order: `users` → `user_sessions` → `priority_transactions` → `queue_state` → `activities` → `auto_queue_config` → `play_history` → `room_activities` (the new table).
- The migrator is an offline CLI; it does NOT run inside the server process; it does NOT mutate either model concurrently with runtime writers; concurrent server writes during conversion are prevented by the maintenance/offline gate.

Cite `cmd/migrate-data/main.go` as the template.

### Compatibility & endpoint retirement phases A→D (chosen path)

R14a chooses ONE recommended sequence. The four phases MUST be implemented as separate sprints so each can be reviewed independently.

#### Phase A — migration / cutover (offline, R14b)

- Operator runs `migrate-data up` while the server is stopped.
- Global runtime is **disabled** during the migration; the server is not serving traffic.
- On success the operator restarts the server with the room state authoritative. There is NO simultaneous global+room write path; there is NO proxy during the cutover; there is NO startup magic.
- Phase A is the ONLY slice that creates or resolves the migrated room. The room is named by the operator-supplied slug; it is never called "main", "default", "implicit", or "room 0".

#### Phase B — `410 Gone` on the legacy global REST and `/ws` (R14c)

R14c lands the retirement behaviour. The routes that MUST return `410 Gone` in Phase B:

- `GET /api/queue`, `POST /api/queue/{add,skip,status,sync,ended,prev,remove,clear,volume,prioritize}`;
- `POST /api/vote/skip`;
- `POST /api/autoqueue/toggle`, `GET /api/autoqueue/status`;
- the global `/ws` endpoint.

`410 Gone` response shape:

```json
{
  "error": "gone",
  "code": "global_contract_retired",
  "successor": "/api/rooms/{slug}/...",
  "documentation": "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"
}
```

Plus an HTTP `Link: </api/rooms/{slug}/...>; rel="successor-version"` header. The `successor` value is operator-supplied at deploy time and points at the migrated room.

Routes OUT of the retirement set:

- `POST /api/auth/google`, `POST /api/auth` (Deprecated) — auth (R13 scope).
- `GET /api/user/priority-balance` — account-scoped per ADR 001 §3 Decision 9.
- `GET /api/youtube/search` — global utility.

**`POST /api/vote/prioritize` `410` is BLOCKED on R09h — Room vote-to-prioritize parity** landing and Product Owner acceptance. R09h shape:

- `POST /api/rooms/{slug}/vote/prioritize` body `{"song_index": <integer>}`; identity from the bearer token only (never from the body).
- No priority-balance debit; no priority transaction.
- In-memory vote session keyed `prioritize:{slug}:{songID}`, 30-second expiry, single-instance only.
- Threshold: `max(2, uniqueConnectedUserIDs(slug) / 2 + 1)` (mirrors R09b's strict-majority + two-voter floor); unique-connected via `(*RoomWSHub).UniqueConnectedUserIDs(slug)` de-duplicated by `userID`.
- On a passed vote, a queue-owned `roomqueue.Interactor` operation:
  - re-resolves the target by song ID (index used only as a hint);
  - rejects the current song (sentinel maps to `400`);
  - detects stale removal or queue movement as stale (sentinel maps to `409` / dedicated stale sentinel);
  - runs under the room queue mutation lock;
  - moves the target immediately after the current song;
  - persists once;
  - emits the existing `room_queue_song_prioritized` event (NO new WS event constant);
  - emits `room_vote_updated` and `room_vote_resolved` (mirrors R09b).
- A discriminator such as `reason: "vote"` may be introduced additively to `room_queue_song_prioritized` without breaking the R07d payload, only if it can be done additively; otherwise the accompanying `room_vote_resolved` event identifies the cause.
- Sprint placement: **separate backend parity slice before R14c** (R14b stays migration-only).

#### Phase C — frontend cutover (R14d)

- `RoomView` becomes the only supported view; `DashboardView` is removed or redirects to a Welcome/room entry surface.
- `globalStore.queueState`, `globalStore.voteSessions`, `globalStore.autoQueueConfig` are cleared.
- `globalStore.currentUser` is preserved (RoomView still reads it).
- `globalStore.roomQueues[slug]` per-slug slices are preserved (RoomView reads them).
- The global `WebSocketClient` is closed and not reconnected.
- Stale bookmarks, stale local storage, archived-room view, and `410` responses are handled with clear UX (redirect to room entry or to a "room no longer exists" surface).
- Rollback behaviour: if Phase C verification fails, restore the `pg_dump` snapshot, restart the pre-migration binary, re-open the global `/ws` and global REST routes; per-room clients remain connected.

#### Phase D — schema cleanup (R14e)

- Drop the legacy `queue_state`, `activities`, `auto_queue_config`, and `play_history` tables.
- Remove `0001_initial.up.sql` rows only used by the legacy state. The rest of `0001` — `users`, `user_sessions`, `priority_transactions` — stays.
- Schema version bumps from 8 to 9.
- Phase D is destructive cleanup only and runs **after** a verified rollback window AND after no runtime path references the legacy tables.

### WebSocket cutover

- `/ws` returns `410 Gone` in Phase B (R14c). No synthesized default-room fallback. No broadcast on the retired endpoint.
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- `seq_num` continuity: per-room sequence numbers restart from the per-room hub's own counter (which is independent of the global hub's counter); this is the existing R07b behaviour and is not changed by R14a. The first per-room `room_queue_sync` after migration lands carries the migrated room's queue state under the migrated room's `seq_num` allocation.
- `/ws` MUST NOT silently join an arbitrary room under any circumstance.
- Per-room event families and initial sync ownership remain per-room.

### Frontend transition & recovery

- Where users land after upgrade: when no active room is selected, redirect to a room entry surface (or Welcome). Users with the migrated room bookmarked land directly on the migrated room's `RoomView`.
- The migrated room is presented to its initial host and to other existing accounts via the existing room-frontend entry points; no automatic join.
- Global queue / store / WebSocket state is cleared (per Phase C); per-slug `roomQueues[slug]` slices preserved; `currentUser` preserved.
- Stale bookmarks, stale local storage, archived migrated room, and `410` responses surface as clear UI states; the frontend MUST NOT attempt to fall back to the global endpoints.
- Rollback story: if the new backend is rolled back while a newer frontend is deployed, the frontend MUST handle `410` responses and the global-endpoint absence gracefully (the existing `RoomView` continues to work because it only uses room routes; `DashboardView` shows a clear "service temporarily unavailable" surface).
- Frontend changes are a later implementation slice (R14d), not R14a work.

### Deployment, backup, and rollback gates

- Required PostgreSQL `pg_dump` BEFORE R14b migration; retained ≥ 30 days (mirrors ADR 002 §9).
- Application downtime / maintenance window is required during Phase A.
- `migrate-data --dry-run` MUST be run before any commit.
- Target verification (`verifyWithinTx` + `verifyAfterCommit`) MUST pass before source cleanup is considered.
- A rollback point MUST exist before accepting writes on the room-authoritative version (R14c).
- Migration succeeds but application startup fails: rollback path restores `pg_dump` and the pre-migration binary.
- **Explicit warning:** rollback after new room writes may require forward recovery or data reconciliation.

## Future sprint sequence

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R09h — Room vote-to-prioritize parity** | Backend room-vote-prioritize parity. Adds `POST /api/rooms/{slug}/vote/prioritize` per § Phase B. **BLOCKING PREREQUISITE for R14c.** | Reuses existing `room_queue_song_prioritized` event. No new WS event constant. |
| **R14b — migration mechanism** | Backend/migration only. New `room_activities` table + repo; host bootstrap via `--host-user-id`; `migration_marker` hashes for new tables; dry-run; report with PII redaction; sequence resync; verifyWithinTx / verifyAfterCommit. **NO endpoint removal. NO frontend changes.** | Reuses R03 advisory-lock / tx / marker patterns verbatim. |
| **R14c — room-authoritative runtime + `410` retirement** | Backend contracts. Returns `410 Gone` on the listed global routes and `/ws` (per § Phase B); `/api/vote/prioritize` blocked on R09h. Per-room runtime is the only authoritative path. | NO frontend changes in R14c. |
| **R14d — frontend global-path removal** | Frontend only. DashboardView removed / redirected; clear `globalStore.queueState` / `voteSessions` / `autoQueueConfig`; close global `WebSocketClient`; preserve `currentUser` + `roomQueues[slug]`. | Backend contracts are fixed by R14c. |
| **R14e — schema cleanup** | Backend only. Drop legacy `queue_state`, `activities`, `auto_queue_config`, `play_history`; bump schema to 9. | Runs after a verified rollback window and after no runtime path references the legacy tables. |

Sprint names may be renamed if evidence requires it, but R09h / R14b / R14c / R14d / R14e MUST NOT be combined; migration, destructive cleanup, frontend rewrite, and R09h parity must remain independently reviewable.

## Validation, errors, and test inventory expectations

R14a documents the expected test inventory; tests are NOT in R14a scope.

### Validation rules

- Migrated room slug: server-side regex / length rules mirror the existing room-slug validation in `internal/domain/repository/room_repository.go`.
- Migrated room display name: explicitly decide whether it must equal the slug or may differ under the current room model. **R14a recommends allowing them to differ**, mirroring the existing room model.
- `--host-user-id`: positive integer; resolved by PK lookup against `users`.
- Idempotency: re-run = no-op exit 0; hash drift = reject.
- Archived-room / missing-host failures map to explicit sentinels.

### Errors and the `410 Gone` envelope

- `410 Gone` envelope: `{ error: "gone", code: "global_contract_retired", successor: "/api/rooms/{slug}/...", documentation: "documents/00-project-management/SPRINTS/022-..." }`.
- HTTP `Link: </api/rooms/{slug}/...>; rel="successor-version"` header.
- Conflict and idempotency errors return explicit sentinels; the migrator CLI surfaces a human-readable line with the failing table and the expected-vs-actual hash.

### Migration report fields

- Operator-supplied numeric `users.id` for the host.
- Numeric row counts per table.
- SHA256 hashes per migrated table.
- Sequence names resynced.
- Durations per phase.
- No email, OAuth tokens, display names, session tokens, credentials, cookies, or `Authorization` headers.

### Future test matrix (NOT in R14a scope)

- R14b: focused migration tests — `room_activities` lossless copy; `room_queue_state` TEXT→JSONB round-trip; sequence resync; idempotent re-run; hash-drift rejection; offline CLI smoke; advisory-lock conflict; transaction rollback on mid-flight failure; report PII redaction; dry-run output.
- R14b repository tests: `RoomActivityRepository.AddActivity` / `GetActivities` (R14b builds).
- R14c: HTTP `410 Gone` envelope tests; `Link` header tests; `/ws` upgrade rejection with `410 Gone` envelope in body; WebSocket event inventory unchanged on per-room endpoint; success-path routing.
- R09h: `room_vote_prioritize` interactor tests (session create, vote, threshold, expiry); HTTP handler tests (auth, body shape, success, 400 / 403 / 404 / 409 mapping); race-sensitive tests (`SetEnabled`-style interleaving, member removal mid-vote, archived-room mid-vote); WebSocket event emission tests (`room_vote_updated`, `room_vote_resolved`, `room_queue_song_prioritized`).
- R14d: frontend tests for `globalStore` clearing on cutover; `currentUser` preservation; `roomQueues[slug]` preservation; global `WebSocketClient` close; `410` toast / redirect; `DashboardView` removal.
- R14e: focused cleanup tests; legacy-table-drop verifies no runtime path references them.

## Issue #17 requirement mapping

Every Issue #17 (Epic) requirement maps to either an R14a decision or an explicit defer.

| #17 Requirement | R14a status |
| --- | --- |
| Migrate legacy global state into one PO-named room | R14a § Source-to-target mapping; R14b implements. |
| Atomic offline migration (no startup magic) | R14a § Atomicity and idempotency; R03 reuse verbatim. |
| Idempotent migration; no `--force` / `--reset`; hash drift rejected | R14a § Atomicity and idempotency. |
| No permanent `main`, `room 0`, hidden default room | R14a § Phase A explicit; ADR 003 §3.4. |
| Single source of truth after cutover | R14a § Source-to-target mapping; legacy tables read-only then dropped. |
| Old global routes transition via explicit `410 Gone` (no hidden default-room proxy) | R14a § Phase B; ADR 003 §3.1. |
| `/ws` cutover with no synthesized default-room fallback | R14a § WebSocket cutover; ADR 003 §3.4. |
| Frontend cutover with state-reset, navigation, recovery | R14a § Frontend transition & recovery. |
| R13 authorization hardening remains separate | R14a § Out of scope. |
| Final implementation plan split into small future sprints with migration gates, verification, rollback | R14a § Future sprint sequence + Deployment, backup, and rollback gates. |
| `410 Gone` envelope with documentation pointer | R14a § Errors and the `410 Gone` envelope. |
| Migrated room identity contract (slug, name, host, conflict) | R14a § Migration identity contract. |
| Source rows deleted, archived, or retained as evidence | R14a § Source-to-target mapping: retained as migration evidence, read-only for rollback window, dropped in R14e. |
| Activities handled explicitly (not silently dropped) | R14a § Activities target: new `room_activities` table + dedicated repo. |
| `/api/vote/prioritize` parity before retirement | R14a § Phase B: blocked on R09h. |
| Frontend rollback behaviour if newer frontend with older backend | R14a § Frontend transition & recovery. |
| R14a documentation-only | R14a § Out of scope (explicit). |

## ADR reconciliation

R14a supersedes parts of ADR 001 and parts of ADR 002 §11/§13 via the new ADR 003 (`documents/00-project-management/ADRS/003-legacy-global-state-migration-and-contract-retirement.md`). ADR 003 is **Proposed** (R14a contract; pending Architect review and Product Owner acceptance). ADR 001 body §3–§18 is unchanged byte-for-byte except for one metadata supersession note near the header. ADR 002 body is unchanged (the §11/§13 R06-fold attribution is reconciled inside ADR 003 rather than edited in ADR 002). ADR 001 §3 Decision 2 (no permanent `main` room), §4 (lifecycle), §5 (lease model), §7 (invite model), §16 (deferred work), and the "single-instance only" posture remain authoritative.

## Verification evidence expectations

R14a Builder evidence MUST include:

- `git diff --check` — clean.
- `git diff --name-only` contains only documents under `documents/00-project-management/`. No `*.go`, `*.vue`, `*.sql`, `Dockerfile*`, `nginx/*`, `*.env*`, `package*.json`, `package-lock.json`, `go.mod`, `go.sum`, anything under `cmd/`, `internal/`, `frontend/src/`, `docker/`, `certs/`, `letsencrypt-*`, `extension/`, `.gitignore`, or `.gitattributes`.
- File references throughout the R14a documents that prove the current route and table inventory (this document's § Current behavior cites every file the inventory requires).
- A mapping table for every Issue #17 requirement to an R14a decision or explicit defer (see § Issue #17 requirement mapping).
- Confirmation that no runtime files changed.
- Confirmation that R13, R10f+, R11b+, and R12 were not silently pulled into scope.
- Confirmation that the legacy global tables were NOT dropped, archived, or mutated in R14a.
- Confirmation that ADR 003 does not mark itself Accepted.
- Confirmation that R14b+ / R09h are NOT marked active in any tracker.

No runtime command may be claimed as passing unless actually run. Backend / frontend test suites are optional for a docs-only diff; if a touched documentation check or link checker exists, run the focused check.

## Risks and review focus

Risk level: **High**. This sprint is documentation-only, but it shapes future data-loss and breaking-contract work.

Review must focus on:

- whether every live global source has a lossless target;
- whether activities are handled explicitly (R14a designs a new `room_activities` table; R14b builds it);
- whether the host bootstrap is deterministic and server / operator controlled;
- whether retry, backup, rollback, and source cleanup timing are safe;
- whether global route retirement can accidentally select or mutate the wrong room;
- whether frontend and backend can be deployed in a compatible order;
- whether security hardening is honestly deferred rather than implied complete (R13 is separate);
- whether the future slices are small enough to review independently (R09h / R14b / R14c / R14d / R14e are NOT combined).

## Builder reasoning effort

**High.** This is a cross-layer contract and data-migration planning sprint with data-loss, compatibility, and deployment risk. The output must be precise enough that later Builders do not invent behaviour while implementing R09h / R14b–R14e.

## Cross-references

- ADR 003 — Legacy global-state migration and contract retirement: [`../ADRS/003-legacy-global-state-migration-and-contract-retirement.md`](../ADRS/003-legacy-global-state-migration-and-contract-retirement.md).
- ADR 001 — Room architecture and contracts (metadata-only supersession note): [`../ADRS/001-room-architecture-and-contracts.md`](../ADRS/001-room-architecture-and-contracts.md).
- ADR 002 — PostgreSQL migration design (R06-fold attribution reconciled via ADR 003): [`../ADRS/002-postgresql-migration-design.md`](../ADRS/002-postgresql-migration-design.md).
- Room epic sequence: [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md).
- R03 migrator patterns: [`009-sqlite-to-postgresql-data-migration.md`](./009-sqlite-to-postgresql-data-migration.md).
- R07d room-queue prioritize runtime: [`012-room-scoped-playback-queue.md`](./012-room-scoped-playback-queue.md) (R07d section).
- R09b room-vote runtime (mirrors R09h session shape): [`014-player-control-semantics.md`](./014-player-control-semantics.md) (R09b Implementation summary).
- R09e / R09f / R09g room auto-queue contract + runtime: [`021-room-auto-queue-contract-design.md`](./021-room-auto-queue-contract-design.md).
- R10a / R10b / R10c / R10d room deletion: [`015-room-deletion-and-membership-removal.md`](./015-room-deletion-and-membership-removal.md).
- Issue #17 (epic): the GitHub issue referenced from the sprint directive.
- Issue #20 (R14a spec): the GitHub issue this document implements.
