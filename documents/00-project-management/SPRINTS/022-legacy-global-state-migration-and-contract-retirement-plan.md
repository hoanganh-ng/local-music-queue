# R14a — Legacy Global State Migration and Contract Retirement Plan

**Status:** Closed/Accepted (planning/design — documentation only; accepted by the Product Owner on 2026-07-16). No Go runtime code, no Vue runtime code, no migrations, no routes, no WebSocket constants, and no config changes are introduced by this sprint. R14a narrows and precedes the legacy R14 stub (`019-global-contract-cleanup-and-documentation.md`); it shapes the implementation-ready contract for the future sprints, executed in the fixed order **`R05b → R09h → R14b → R09i → R14d → R14c → R14e`**. R14a supersedes parts of ADR 001 via the new ADR 003 (Accepted 2026-07-16). R13 (auth/session/authorization hardening), R10f+ (deferred room lifecycle hardening), R11b+ (deferred R11 chat slice), and R12 (search and room discovery) remain out of scope.

**Sprint name:** Legacy global state migration and contract retirement plan (planning/design only)

**Parent epic:** Issue #17 (legacy global contract cleanup). This sprint addresses the same constraint set that #17 records; #20 is the authoritative spec.

> **R14a review-revision summary (revised through the accepted Architect review passes on 2026-07-15).** This sprint doc has been revised through the accepted Architect review passes on 2026-07-15 per the corrective Builder prompts on Issue #20. The corrections visible throughout this document are: (1) the cutover is split — R14b builds + verifies the offline mechanism with a NEW dedicated `cmd/room-cutover` CLI and a NEW dedicated `room_cutover_marker` contract; R14c owns the coordinated production execution with a binary that has **repository-free tombstone handlers returning `410 Gone`** (NOT route unregistration returning 404); (2) the room-cutover mechanism does NOT reuse R03's `cmd/migrate-data` + `migration_marker` — those are preserved untouched; (3) `users` / `user_sessions` / `priority_transactions` are NOT recopied at cutover (they stay account-scoped per ADR 001 §3 Decision 9); (4) R05b (room entry / player-lease UI) is a blocking prerequisite for R14c; (5) the `room_play_history` id-collision handling is settled via `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)` recorded in the marker; (6) R09i — Room activity runtime parity — is a NEW blocking prerequisite for R14c; R09i is **BACKEND-ONLY** (write path on the existing room mutators; the activity-panel READ surface is a separate optional frontend decision); the prior conflation of "live room-activity read parity" with the write path is unwound; (7) the schema-version plan: **R14b lands `0009_room_cutover_support.up.sql`** which creates BOTH `room_activities` AND `room_cutover_marker`, and bumps v8 → v9; **R14e lands `0010_drop_legacy_global_tables.up.sql`** and bumps v9 → v10; historical migration files `0001`..`0008` are preserved byte-for-byte and `0001_initial.up.sql` is NEVER rewritten; (8) the `410 Gone` envelope drops the concrete migrated-room `successor` value, and the `Link: </api/rooms>; rel="successor-version"` header IS present on the tombstone handler response; (9) the JSON conversion contract is the default `encoding/json` tagged-field round-trip on `entity.Queue` (no custom `MarshalJSON`/`UnmarshalJSON` claimed); (10) the queue-prioritize / vote-prioritize routes are separated (only the latter needs R09h); (11) contradictory active-sprint statements are removed by the `active.md` revision; (12) the open-items list is split into PO acceptance blockers vs settled items; (13) the **rollback target is the immediately pre-cutover production release** (NOT an "R03 binary" — R03 is the unrelated SQLite-to-PostgreSQL migration release); (14) **`cmd/room-cutover` ships NO `abort` subcommand** because PostgreSQL session locks belong to the connection that acquired them — the cutover uses **deferred `pg_advisory_unlock` on the same pinned connection** (mirroring R03's idiom), with connection close / process exit / `SIGTERM` as the natural fallback if the connection fails; (15) **the marker is inserted in the same transaction** as the room, host membership, and copied state — no post-commit marker write; mid-flight crash leaves NO partial state; (16) **the marker timestamp field is `cutover_pre_commit_at`** (not `cutover_committed_at`) because the row is written before `COMMIT` and cannot truthfully reflect the DB commit time; (17) **the migration ordering is `R05b → R09h → R14b → R09i → R14d → R14c → R14e`** — R09i follows R14b because R14b's migration creates `room_activities` that R09i wires; (18) **each new migration ships a matching `.down.sql`** (the existing R03 migrations all have matching `.down.sql` files; the future R14b/R14e migrations follow the same convention); (19) **R14d's behavioral activation is deferred to the R14c maintenance window** — R14d's implementation may be accepted BEFORE R14c, but the SPA's behavior change (removing `DashboardView`, clearing global slices, closing global WS) activates only during the R14c cutover window (after successful migration) or behind an explicit cutover-state gate; pre-cutover the SPA continues to expose the legacy dashboard surface so the still-authoritative global queue remains reachable.

## Goal

Produce an implementation-ready contract for converting the legacy global state into exactly one Product-Owner-named room and for retiring the parallel global REST/WS contracts. The sprint removes ambiguity before any destructive migration or compatibility removal is implemented. It separates data conversion, frontend entry UX prerequisites, room cutover coordination, room-activity runtime parity, frontend cutover, final contract removal, and vote-prioritize parity into independently reviewable future sprints. Authentication/authorization hardening remains a separate R13 concern.

## Non-goals

- No Go runtime code, no Vue runtime code, no PostgreSQL migration in this sprint.
- No HTTP route additions or removals; no WebSocket event additions or removals; no payload changes.
- No schema version bump in R14a itself. The full plan is **v8 → v9 (R14b's `0009`) → v10 (R14e's `0010`)**; R14a does not add migrations.
- No Docker, Nginx, TLS, environment, dependency, or deployment changes.
- No automatic destructive startup migration (mirrors the offline posture of R03's `cmd/migrate-data` but uses a NEW dedicated CLI; see § Atomicity and idempotency).
- No R13 redesign; no R10f+ retention/audit/revocation/archived-room cleanup; no R11b+ chat slice; no R12 search.
- No multi-instance / cross-process safety claims (single-instance only).
- No new WebSocket event constant for vote-to-prioritize (reuses the existing `room_queue_song_prioritized` event in R09h).
- No reuse of the R03 `cmd/migrate-data` CLI or the R03 `migration_marker` table for the room cutover.
- No creation of a permanent `main`, `room 0`, hidden default room, writable global fallback, or synthesized default-room `/ws`.
- No partial cutover (R14c retires ALL listed legacy routes atomically).
- No activity READ panel in R09i (backend-only).

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
- Live room-activity read parity (activity panel) — deferred as a separate optional frontend task.
- R05b / R09h / R14b / R09i / R14d / R14c / R14e work.

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
| Room play history | `room_play_history` | `0007_room_auto_queue.up.sql:22-33` | SAME `RoomAutoQueueRepository` | SAME impl | same |
| Player leases | `player_leases` | `0005_player_leases.up.sql:8-16` | `repository.PlayerLeaseRepository` `player_lease_repository.go:18-45` | `postgres_player_lease_repository.go:40` | `:202` |
| Room chat | `room_chat_messages` | `0008_room_chat_messages.up.sql:9-15` | `repository.RoomChatMessageRepository` `room_chat_message_repository.go:15-25` | `postgres_room_chat_message_repository.go:26` | `:199`, `:200` |

**Critical:** `room_activities` and `room_cutover_marker` do NOT exist. `grep -rn "room_activities\|room_cutover_marker\|RoomActivit\|RoomCutoverMarker"` returns zero matches across `*.go`, `*.sql`, `*.js`. R14b's `0009_room_cutover_support.up.sql` creates BOTH tables atomically.

Type mismatch: `queue_state.data` is **TEXT**; `room_queue_state.data` is **JSONB**. Both serialize the same `*entity.Queue` aggregate. `entity.Queue` uses default `encoding/json` tagged-field marshalling — there are NO custom `MarshalJSON` / `UnmarshalJSON` methods on the type. The conversion contract uses the default `encoding/json` round-trip on tagged fields.

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
| `POST /api/queue/prioritize` | `:401` | `POST /api/rooms/{slug}/queue/prioritize` (R07d) — queue mutation; retires in R14c with no parity sprint |
| `GET /api/user/priority-balance` | `:402` | OUT — account-scoped per ADR 001 §3 Decision 9 |
| `GET /api/youtube/search` | `:403` | OUT — global utility |
| `POST /api/vote/skip` | `:404` | `POST /api/rooms/{slug}/vote/skip` — vote mutation; retires in R14c with no parity sprint |
| `POST /api/vote/prioritize` | `:405` | `POST /api/rooms/{slug}/vote/prioritize` — vote-driven queue mutation; blocks R14c (R09h) |
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

The `roomAuth` middleware wraps every room route.

**Room entry / lease frontend methods are MISSING from the SPA.** The backend routes exist on the server, but no `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / invite / player-lease client methods exist. R05b closes this gap (blocking prerequisite for R14c).

### WebSocket endpoints

**Global `/ws`** — registered at `main.go:588`. Hub built by `ws.NewHub(qInteractor.GetState)` at `:232`. State read is **GLOBAL**. The 16-event inventory lives at `internal/delivery/ws/events.go:11-35`: `full_sync`, `user_joined`, `song_added`, `song_skipped`, `status_changed`, `elapsed_sync`, `song_previous`, `song_removed`, `queue_cleared`, `volume_changed`, `song_prioritized`, `priority_balance_updated`, `vote_updated`, `vote_resolved`, `auto_queue_added`, `auto_queue_config_changed` (+ `error` and `room_archived`).

**Room `/ws/rooms/{slug}`** — registered at `main.go:591`. Built with three resolvers (room-by-slug, member, queue state). State read is **PER-ROOM**. Per-room events include `room_queue_sync`, `room_queue_song_added`, `room_queue_song_removed`, `room_queue_cleared`, `room_queue_song_prioritized`, `room_playback_status_changed`, `room_playback_elapsed_sync`, `room_playback_song_advanced`, `room_vote_updated`, `room_vote_resolved`, `room_playback_volume_changed`, `room_playback_song_previous`, `room_auto_queue_added`, `room_auto_queue_config_changed`, `room_member_removed`, `room_members_changed`, `room_chat_message_created`, plus per-room `room_archived`.

### Frontend entry points

- `frontend/src/router/index.js`: `/` → `DashboardView` (global); `/auth` → `AuthView`; `/rooms/:slug` → `RoomView`. No Welcome/CreateRoom/JoinRoom/Invite/PlayerLease routes.
- `frontend/src/store/index.js`: single `globalStore` (`:33`). Global slices: `currentUser`, `queueState`, `voteSessions`, `autoQueueConfig`, `connectionStatus`. Per-room slices under `globalStore.roomQueues[slug]`.
- `frontend/src/services/api.js`: global methods still present; room methods at `:196-368`. No `createRoom`/`listRooms`/`getRoom`/`joinRoom`/`invite`/`player/claim`/`player/lease` SPA methods.
- WS clients: global `frontend/src/services/websocket.js`; room `frontend/src/services/room-websocket.js`.
- `DashboardView.vue`: global-only.
- `RoomView.vue`: room-only but reuses `globalStore`. Preserves `globalStore.currentUser` + `roomQueues[slug]`.

## Desired behavior (post-R14a, sketched)

R14a does not implement any of this. R14a designs the contract.

### Source-to-target mapping

| Source (global) | Target (room, R14b builds) | Real conversion contract |
| --- | --- | --- |
| `queue_state` (TEXT JSON, singleton `id = 1`) | `room_queue_state` (JSONB, one row per `rooms.id`) | Read legacy TEXT into `[]byte`. Decode through `encoding/json` into the tagged `entity.Queue` struct. Validate invariants (`Songs`, `CurrentIndex`, `Status`, `Elapsed`, history, first-song / current-song). Re-encode through `encoding/json` to canonical JSON bytes. Compute SHA256 over the canonical bytes for the marker record. Write to `room_queue_state` with PK = `rooms.id`. **No custom `MarshalJSON` / `UnmarshalJSON` methods on `entity.Queue` are assumed**; the contract is the default `encoding/json` tagged-field round-trip. |
| `activities` | new `room_activities` (created by `0009_room_cutover_support.up.sql`) | Lossless copy with preserved `id`, `timestamp`, `type`, `user`, `description`, count, ordering; `room_id` set to the migrated room's id; sequence resync via `setval(room_activities_id_seq, MAX(id), is_called=true)`. |
| `auto_queue_config` (singleton `id = 1`) | `room_auto_queue_config` (migration 0007) | Keyed by `room_id`; write the migrated room's row with the legacy singleton's values. |
| `play_history` (legacy 50-row cap) | `room_play_history` (migration 0007) | Id-collision handling via `legacy_id_offset` (see § room_play_history id-collision handling). Per-room 50-row cap preserved. |

After the copy, source rows are retained as migration evidence. Read-only for the rollback window; dropped in R14e via a SEPARATE forward migration.

`users`, `user_sessions`, and `priority_transactions` are **account-scoped per ADR 001 §3 Decision 9** and are NOT recopied at cutover.

### room_play_history id-collision handling

`room_play_history` is one table shared across all rooms with a single global BIGSERIAL. Copying legacy `play_history.id` values into `room_play_history` can collide with rows that any room's auto-queue already appended since the R09f migration. The cutover migrator MUST handle this by **partitioning the id space**:

- `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)`.
- Insert each legacy row with `id + legacy_id_offset`.
- Record the offset in `room_cutover_marker`.
- After the insert, `setval(room_play_history_id_seq, MAX(id), is_called=true)`.
- The shifted ids are an implementation detail; the per-room cap is enforced in `roomautoqueue`.
- Re-run / idempotency: the marker carries SHA256 hashes of both source and target so a re-run is a no-op when hashes match.

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

**Marker** — new `room_cutover_marker` table (single-row, `id = 1`):

```text
room_cutover_marker
  id                     INTEGER PRIMARY KEY CHECK (id = 1)
  room_cutover_id        UUID NOT NULL
  target_room_slug       TEXT NOT NULL
  target_room_id         BIGINT NOT NULL    -- immutable audit snapshot; NO foreign key
  host_user_id           BIGINT NOT NULL    -- immutable audit snapshot; NO foreign key
  source_hashes          JSONB NOT NULL
  target_hashes          JSONB NOT NULL
  legacy_id_offset       BIGINT NOT NULL DEFAULT 0
  cutover_pre_commit_at  TIMESTAMPTZ NOT NULL   -- supplied explicitly by the CLI; NOT DEFAULT NOW()
  binary_build_sha       TEXT NOT NULL
```

The fields `target_room_id` and `host_user_id` are **immutable integer audit snapshots of the migrated room's id and the host's `users.id`** — explicitly NOT foreign keys. Rationale: the marker row is retained for the rollback window AND as migration evidence; foreign-key relationships (`REFERENCES rooms(id) ON DELETE RESTRICT` / `REFERENCES users(id) ON DELETE RESTRICT`) would permanently own the lifecycle of the room or the user and prevent future hard-delete / retention work that R10f+ defers. Migration evidence must NOT own room or user lifecycle. The room row in `rooms` and the user row in `users` continue to exist independently; if either is hard-deleted in a future R10f+ lifecycle hardening slice, the marker row is unaffected and the numeric IDs remain valid as historical references. To re-link the marker to the current row (if any) in a future reconciliation, callers can `SELECT … FROM rooms WHERE id = :target_room_id` and act on the result; reconciliation is **out of scope for R14a and any of R09h / R14b / R09i / R14d / R14c / R14e**.

The field `cutover_pre_commit_at` is named to reflect that the row is written **before** `COMMIT` and cannot truthfully reflect the database commit time. **It is supplied explicitly by the CLI at marker insertion** as a parameter (CLI process clock at the moment of marker insertion), rather than relying on `DEFAULT NOW()`. Rationale: the CLI's process clock is the authoritative recorder of the pre-commit completion timestamp; a `DEFAULT NOW()` would silently use the DB clock at insert time, which can drift from the CLI's intended completion timestamp under load or clock skew. Explicit parameter passing makes the audit timestamp the CLI's view, not a DB-side implicit. The DB commit time is implicit in the transaction's WAL position; the marker record does not need to expose it.

**Both tables are created by `0009_room_cutover_support.up.sql`** (and dropped by its matching `0009_room_cutover_support.down.sql`). The CLI does NOT create schema ad hoc.

### `cutover_pre_commit_at` (renamed)

The field is named `cutover_pre_commit_at` (NOT `cutover_committed_at`) because the row is written **before** the `COMMIT`. It cannot truthfully reflect the database commit time. The field records the pre-commit completion timestamp (the time the marker row was inserted into the transaction). The DB commit time is implicit in the transaction's WAL position; the marker record does not need to expose it.

### CLI flags (R14b)

- `--room-slug=<slug>` (required).
- `--room-name=<name>` (required).
- `--host-user-id=<positive integer>` (required) — canonical `users.id`.
- `--dry-run` (optional).
- No `--force` / `--reset` / `--allow-hash-drift`.

### Migration report — PII posture

MAY contain numeric `users.id`, row counts, sha256 hashes, sequence names, table names, durations. MUST NOT contain email, OAuth tokens, display names, session tokens, credentials.

### Atomicity and idempotency (DEDICATED mechanism, not R03 reuse)

The R14b room-cutover is built fresh. R03 patterns are referenced but R03's CLI and marker are preserved untouched.

**Lock contract:**

- `pg_try_advisory_lock(987654321)` on a **pinned connection** (same key, same idiom as R03; same key shared with R03 by convention).
- The cutover transaction holds the lock for its entire duration.
- **`pg_advisory_unlock(987654321)` is called explicitly on the same pinned connection** at the end of the transaction (mirroring R03's deferred-unlock idiom), regardless of whether the transaction committed or rolled back.
- If the connection fails before the explicit unlock, the lock is released on connection close (process exit / `SIGTERM` / Ctrl-C).
- **`cmd/room-cutover` ships NO `abort` subcommand.** PostgreSQL session locks belong to the connection that acquired them; another process cannot release them. The only release paths are: (a) explicit deferred `pg_advisory_unlock` on the same pinned connection, (b) connection close / process exit / `SIGTERM`. If the operator wants to abandon a held cutover mid-flight, they stop the `cmd/room-cutover up` process; the connection closes, the lock releases, the in-flight transaction rolls back. The CLI MAY print a textual "press Ctrl-C to abandon" hint before the copy phase begins.

**Single transaction for `cmd/room-cutover up` (in this order):**

1. `BEGIN`.
2. `INSERT INTO rooms (slug, name, ...) VALUES (...)`.
3. `INSERT INTO room_members (room_id, user_id, role) VALUES (:room_id, :host_user_id, 'host')`.
4. Source-to-target copy (legacy → room). The legacy `activities` rows are inserted into `room_activities` with preserved `id`s; after the insert, `setval(room_activities_id_seq, MAX(id), is_called=true)` is run **before** the marker INSERT in the same transaction (this resequence is the precondition that lets R09i's write path activate later; see § R09i).
5. `INSERT INTO room_cutover_marker (id, room_cutover_id, target_room_slug, target_room_id, host_user_id, source_hashes, target_hashes, legacy_id_offset, cutover_pre_commit_at, binary_build_sha) VALUES (1, :uuid, :slug, :room_id, :host_id, :source_hashes, :target_hashes, :legacy_id_offset, :cutover_pre_commit_at_supplied_by_cli, :binary_build_sha)`. `target_room_id` and `host_user_id` here are integer snapshots (NOT foreign keys); `cutover_pre_commit_at` is supplied by the CLI process clock at this moment. Marker is inserted before `COMMIT`, never after.
6. **Pre-commit verification** within the same tx: source row counts, target row counts, `queue_state.data` byte length parity, per-target SHA256 after copy, marker record fields present, `setval` already applied.
7. `COMMIT` (or `ROLLBACK` on any verification failure).
8. `pg_advisory_unlock(987654321)` on the same pinned connection (deferred unlock).

**No `abort` subcommand.** Lock release is via explicit `pg_advisory_unlock` on the same pinned connection (with connection close as fallback). The CLI rejects unknown positional arguments and unknown flags.

**NEW** `room_cutover_marker` table is distinct from R03's `migration_marker`. R03's marker is preserved untouched. `target_room_id` and `host_user_id` in the marker are integer snapshots (NOT foreign keys); future hard deletes of the room row or the user row do NOT remove or block the marker row.

**Migration files and rollback:** every migration in this work — `0009_room_cutover_support.{up,down}.sql`, `0010_drop_legacy_global_tables.{up,down}.sql` — ships a matching `.down.sql`. The `0009.down.sql` drops the `room_activities` and `room_cutover_marker` tables. The `0010.down.sql` **cannot restore data that was dropped** (it only recreates empty tables); the destructive cleanup rollback is one-way and is documented as such in `0010.down.sql`.

### Compatibility & endpoint retirement phases A→D (chosen path)

R14a chooses ONE recommended sequence. The four phases MUST be implemented as separate sprints; per the Architect reviews, the cutover is split between R14b (builds + verifies the mechanism) and R14c (owns the coordinated production execution with a binary that has `410 Gone` tombstone handlers).

#### Phase A — offline conversion (built + verified by R14b)

R14b is a backend-only sprint that ships the offline cutover mechanism. R14b lands the new `0009_room_cutover_support.up.sql` migration (creates both `room_activities` and `room_cutover_marker`; bumps v8 → v9). R14b does NOT retire the global runtime; it lands the CLI, the marker contract (with `cutover_pre_commit_at`, NOT `cutover_committed_at`), the source-to-target copy, the dry-run, the verification, and the rollback hook. R14b's verification gate MUST pass before R09i can run (R09i is the next sprint in the order; R14b creates `room_activities`).

- R14b ships `cmd/room-cutover` with subcommands: `plan` (read-only), `up` (apply), `verify` (re-assert hash record). **No `abort` subcommand** (PostgreSQL session locks belong to the connection that acquired them; release is via explicit `pg_advisory_unlock` or connection close).
- R14b ships `0009_room_cutover_support.up.sql` and `0009_room_cutover_support.down.sql`.
- R14b does **NOT** ship the runtime server startup flag and does **NOT** select activity-writer implementations. The `--room-cutover-authoritative` flag, the legacy-route registration set switch, and the no-op/real `RoomActivityRepository` selection belong to **R14c**, which owns authoritative-mode activation. R14b's binary is always pre-cutover; it lands the CLI, the migration, and the marker contract only.
- R14b verification gate (must pass before R09i can run):
  - `cmd/room-cutover plan` against a fresh PostgreSQL snapshot returns a PII-free report.
  - `cmd/room-cutover up --dry-run` against the same snapshot completes with no writes.
  - `cmd/room-cutover up` (real run) against the same snapshot creates room + host membership + copies + marker ALL IN ONE transaction; commits; returns 0.
  - Re-running `cmd/room-cutover up` immediately after returns `already cut over; no-op` (exit 0), driven by the marker.
  - Hash drift on either source or target is detected and rejected (no `--force` / `--reset`).
  - PII redaction verified.
  - R03 marker preserved untouched.
  - Schema version after R14b is **9** (verified by the runner's reported version).
  - `go test ./...` clean.

#### R09i — room-activity runtime parity (AFTER R14b, BEFORE R14d)

R09i wires the room runtime's mutations to call `RoomActivityRepository.AddActivity` on the same events that the global runtime today appends to `activities` (vote / priority / queue mutations in `usecase/vote/interactor.go`, `usecase/priority/interactor.go:81`, `usecase/queue/interactor.go`). **R09i is BACKEND-ONLY**; the activity READ panel (a `RoomView` activity surface) is a separate optional frontend task and is NOT in R09i scope.

- R09i is a blocking prerequisite for R14c unless the Product Owner explicitly waives it (recorded on Issue #20).
- R09i runs after R14b (because R14b creates `room_activities`) and before R14d.
- R09i only touches `internal/usecase/roomqueue`, `internal/usecase/roomvote`, `internal/usecase/roomautoqueue`, and the new `RoomActivityRepository`. No frontend, no global runtime changes.

**R09i write behavior must remain DISABLED until R14c has copied legacy activities and resynchronized `room_activities_id_seq`.** This is the activity-ID collision guard that mirrors the `legacy_id_offset` mechanism for `room_play_history`. Without it, both the legacy `activities` table (still writable by the global runtime during Phase A/B) and the new `room_activities` table (BIGSERIAL) could allocate overlapping IDs concurrently, and a future re-run of the lossless `activities` → `room_activities` copy would either collide with rows the room runtime already appended or lose the preserved-id invariant. The contract is:

- R09i may land its code (repo + call sites + mutator wiring) on `dev` **after R14b's gate passes**; this is in-progress work that does not affect the legacy runtime.
- R09i's write behavior is gated behind the **server runtime flag `--room-cutover-authoritative`** (NOT the SPA flag `VITE_ROOM_CUTOVER_AUTHORITATIVE`). The Go backend CANNOT consume a constant baked into a Vue bundle; the backend reads the server flag once at composition and injects either a no-op `RoomActivityRepository` or the real `room_activities`-writing implementation. The SPA flag `VITE_ROOM_CUTOVER_AUTHORITATIVE` is a **frontend build-time-only** setting (Vite `import.meta.env` baked at SPA build) that controls R14d's SPA behavioral activation; it is independent of the server flag's value at the backend composition layer.
- Concretely:
  - **R09i's binary (pre-R14c):** the backend injects a **no-op activity writer unconditionally** — `RoomActivityRepository.AddActivity` call sites are wired but each call short-circuits and returns success without touching the database. R09i does not introduce the runtime server flag; the no-op is the pre-cutover default in every pre-R14c deployment. The no-op prevents any `room_activities` ID allocation during Phase A/B.
  - **R14c's binary:** the runtime server flag exists and the backend composition selects the writer based on it. `false` selects no-op writer + legacy global handlers (this is the rollback / pre-cutover state). `true` selects the real `room_activities`-writing implementation + `410 Gone` tombstone handlers. Starting with `true` fails closed unless schema version ≥ 9 AND the `room_cutover_marker` row exists. The flip is atomic (one server restart).
- During the R14c maintenance window, the binary restart with `--room-cutover-authoritative=true` activates the gate at the backend composition layer: the no-op activity writer is replaced with the **real `room_activities`-writing implementation**, AND the legacy-route registration switches to tombstone handlers. After the flip, `setval(room_activities_id_seq, MAX(id), is_called=true)` has been applied to `room_activities_id_seq` so the global BIGSERIAL is resequenced past the highest preserved legacy `activities.id`, and the R09i writes may begin.
- The compatibility matrix pairs this with the SPA flag: the valid operational pairs are `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` + `--room-cutover-authoritative=false` (pre-R14c) and `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` + `--room-cutover-authoritative=true` (post-R14c).
- If the Product Owner explicitly waives R09i (recorded on Issue #20), the gate still applies — R09i is unused in that scenario; the cutover binary simply runs without R09i migrated code.

**This activity-ID collision guard is symmetric with the play-history `legacy_id_offset` mechanism (§ room_play_history id-collision handling).** Both ensure that the legacy-table writes (still active during Phase A) and the new-table writes (active in R09i / post-R14c) do not allocate overlapping IDs during the cutover window.

#### Phase C — frontend cutover (R14d, with deferred behavioral activation)

R14d implements the SPA changes; **the behavioral activation is deferred to the R14c maintenance window**.

- Implementation may be accepted BEFORE R14c ships (the code lands and passes its own review).
- Pre-R14c, the SPA continues to expose the legacy dashboard surface (so the still-authoritative global queue remains reachable).
- Activation happens during R14c. The frontend gate is a **single, named mechanism**: the SPA bundle exposes a `VITE_ROOM_CUTOVER_AUTHORITATIVE` flag (a Vite build-time `import.meta.env` value baked at build). Two SPA bundle variants are built and deployed: **`VITE_ROOM_CUTOVER_AUTHORITATIVE=false`** (paired with the server in `--room-cutover-authoritative=false` / pre-R14c states) and **`VITE_ROOM_CUTOVER_AUTHORITATIVE=true`** (paired with the server in `--room-cutover-authoritative=true` / post-R14c states). The SPA checks the flag once at mount time. There is **NO runtime feature flag** — the SPA is built twice and the right bundle is deployed for the right server state; no alternate mechanism (no separate env var at runtime, no separate feature-flag service) is permitted within R14a's contract. R14d's bundle-acceptance ships the `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` variant; the `true` variant is deployed as part of the R14c maintenance window **paired with the `true` server while public traffic remains closed** (see § Phase B — coordinated production cutover for the deployment sequence).
- The activation removes `DashboardView` (or redirects it to the room entry surface from R05b), clears `globalStore.queueState` / `voteSessions` / `autoQueueConfig`, closes global `WebSocketClient`, preserves `globalStore.currentUser` + `roomQueues[slug]`.
- R14d assumes R05b (room entry UI) has landed.

#### Phase B — coordinated production cutover (executed by R14c)

R14c owns the coordinated production execution: maintenance window during which (1) public traffic is closed, (2) `cmd/room-cutover up` runs and is verified against the live PostgreSQL, (3) the `--room-cutover-authoritative=true` server AND the `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` SPA bundle are deployed as a paired set while traffic remains closed, (4) paired smoke checks run against both artifacts, and (5) traffic reopens. The `true` server MUST NOT start before the marker exists; the startup guard fails closed if it does. Sprint execution order: **`R05b → R09h → R14b → R09i → R14d → R14c → R14e`**. R14c requires ALL of R05b + R09h + R14b + R09i + R14d (with R09i waivable only by explicit PO sign-off on Issue #20).

- Before the window: a `pg_dump` is taken and retained ≥30 days; R14b's binary is deployed; R14b's verification gate has passed; R14d's SPA bundle has been deployed and accepted. The immediately pre-cutover production release is the server binary with `--room-cutover-authoritative=false` AND the SPA bundle with `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` deployed as a paired set; this paired state is the canonical "before" snapshot and the rollback target.
- **R14c startup guard (fail closed).** When `--room-cutover-authoritative=true`, the server MUST refuse to serve traffic unless both schema version ≥ 9 (i.e. `0009_room_cutover_support.up.sql` has been applied) AND the `room_cutover_marker` row exists. This is the durable activation proof: the marker is inserted in the same transaction as the atomic copy + `room_activities_id_seq` resequence, so its presence proves the cutover completed. The startup guard is marker + schema-version presence only; do NOT require copied target hashes to remain unchanged on every later startup (legitimate room writes change them). `cmd/room-cutover verify` remains the operator verification command during cutover.
- **Deployment sequence during the window.** Public traffic remains closed for the entire duration. The order is:
  1. Stop public traffic / enter maintenance.
  2. Run `cmd/room-cutover up` and verify success.
  3. Deploy the server configured `--room-cutover-authoritative=true` AND the SPA bundle built `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` while traffic remains closed. (Both artifacts deploy as a paired set; the startup guard passes because the marker row exists and schema is v9.)
  4. Perform paired smoke checks against both artifacts.
  5. Reopen traffic.
  The two artifacts MUST be deployed as a paired set; the compatibility matrix forbids `true` server + `false` SPA.
- In the post-restart `--room-cutover-authoritative=true` build:
  - The activity-writer injected at composition switches from the no-op to the **real `room_activities`-writing `RoomActivityRepository`** implementation.
  - R14d's SPA `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` bundle is the active client; legacy surfaces (`DashboardView`, global queue slices, global WS) are removed/cleared/closed in the SPA.
  - The legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes and the global `/ws` endpoint are **registered with repository-free tombstone handlers** that return `410 Gone` with the documented envelope (see § Errors and the route-retired responses). They are NOT unregistered, and they are NOT `404`. The global `/ws` upgrade is **rejected with `410` in the HTTP phase**.
  - **R14c is atomic across ALL listed legacy routes.** A partial cutover would let surviving legacy mutations continue writing legacy global queue state; R09i + R09h gate the entire cutover.
- **Mid-window rollback** restores the immediately pre-cutover `--room-cutover-authoritative=false` server AND `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` SPA bundle as one pair before reopening traffic. The `pg_dump` from the start of the window is the source of truth; forward recovery handles any data that mutated after the cutover.

#### Phase D — schema cleanup (R14e)

- Land `0010_drop_legacy_global_tables.up.sql` (and matching `.down.sql`). The forward migration drops **only** the four legacy global tables that R14e is responsible for: `queue_state`, `activities`, `auto_queue_config`, `play_history`. **It does NOT touch `room_play_history`, `room_activities`, `users`, `user_sessions`, or `priority_transactions`** — those tables and their sequences (`users_id_seq`, `user_sessions_id_seq`, `priority_transactions_id_seq`) survive `0010.up.sql`. `0001_initial.up.sql` is NEVER rewritten.
- Schema bumps v9 → v10.
- **`0010_drop_legacy_global_tables.down.sql` recreates ONLY the four dropped tables with their exact legacy definitions** so that rolling forward/backward on the dropped tables is a no-op on structure. Specifically, the `.down.sql` MUST recreate **only**:
  - `queue_state` — single-row table with `id INTEGER PRIMARY KEY CHECK (id = 1)` and `data TEXT NOT NULL`. **`data` is NOT NULLABLE; the `.down.sql` MUST NOT seed `data` with `NULL`.** If the table is recreated with no rows, that satisfies the schema (the table is empty and no globals are relying on it because the cutover has unhooked all legacy repositories).
  - `activities` — recreated with its OWN `BIGSERIAL` sequence (`activities_id_seq`) and **exactly** the column structure from `0001_initial.up.sql` (`id BIGSERIAL PRIMARY KEY`, `"timestamp" TIMESTAMPTZ NOT NULL`, `type TEXT NOT NULL`, `"user" TEXT NOT NULL`, `description TEXT NOT NULL`). The legacy `activities` table has no `room_id` column; the `.down.sql` MUST NOT introduce one.
  - `auto_queue_config` — recreated with the singleton seed row **at its initial values** (the row originally inserted by `0001_initial.up.sql`: `INSERT INTO auto_queue_config (id, enabled, strategy) VALUES (1, FALSE, 'related') ON CONFLICT DO NOTHING`). This is NOT optional — without the seed row, the global autoplay toggle has no target. The seed values `enabled=FALSE` and `strategy='related'` match `0001_initial.up.sql`.
  - `play_history` — recreated with its OWN `BIGSERIAL` sequence (`play_history_id_seq`) and the column structure from `0001_initial.up.sql`, including the **`idx_play_history_played_at` index**.
- The `.down.sql` MUST NOT recreate the **account-table** objects (`users`, `user_sessions`, `priority_transactions`, `users_id_seq`, `user_sessions_id_seq`, `priority_transactions_id_seq`, the unique constraint on `users.email`, the unique constraint on `user_sessions(user_id, session_date)`, the FK constraints on `user_sessions.user_id` → `users.id` and `priority_transactions.user_id` → `users.id`). Those tables survive the `0010.up.sql` drop and the `.down.sql` MUST NOT touch them — recreating them would conflict with the existing schema.
- **Destructive cleanup reverse-copy strategy:** because the `.down.sql` recreates the four legacy tables as empty shells (without restoring their row data), rollback of a destructive cleanup does **NOT** restore the prior row contents. The reverse-copy strategy is to **rely on the retained `pg_dump`** taken at the start of the R14c maintenance window (retained ≥30 days) or on a separately approved reverse-copy mechanism. The `.down.sql` is intentionally one-way with respect to row data; this is documented in the `.down.sql` file header. Re-running `0010.up.sql` after the `.down.sql` will drop the recreated tables again — that's the same destructive cleanup re-applied, not a data recovery.
- Migration-evidence rows in `room_cutover_marker` and preserved `room_*` rows are unaffected; the `.down.sql` does NOT touch any non-`0010` table or row.

### WebSocket cutover

- `/ws` is **retired with `410 Gone`** in the `--room-cutover-authoritative=true` build (R14c). The global WS upgrade is replaced by an HTTP handler that returns `410 Gone`; the upgrade is refused in the HTTP phase.
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- `/ws` MUST NOT silently join an arbitrary room under any circumstance.

### Frontend transition & recovery

- Pre-R14c, the SPA continues to expose the legacy dashboard surface (so the still-authoritative global queue remains reachable). R14d's behavioral change activates only during R14c.
- Post-R14c, users land on the room entry surface (R05b) or `RoomView` if bookmarked.
- Stale bookmarks, stale local storage, archived-room view, and `410 Gone` responses surface as clear UI states.
- Rollback: restart the **immediately pre-cutover production release** with `--room-cutover-authoritative=false` (NOT an "R03 binary").

### Deployment compatibility matrix

The cutover involves **one runtime flag and one build-time flag, each owned by its respective side**:

- **Server side**: `--room-cutover-authoritative=true|false` is a **runtime server startup flag** (Go runtime arg parsed at `main()` startup; read once during composition; default `false`). It controls the legacy-route registration set AND the R09i activity-writer injection.
- **Client side**: `VITE_ROOM_CUTOVER_AUTHORITATIVE=true|false` is a **frontend build-time-only** setting (Vite `import.meta.env` baked at SPA build; two bundle variants — `false` shipped pre-R14c, `true` shipped as part of the R14c rollout).
- The Go backend NEVER consumes `VITE_ROOM_CUTOVER_AUTHORITATIVE` — the SPA flag does not cross the network boundary at runtime; it only ships inside the SPA bundle. Each side gates its own behavior; the gate pair (`--room-cutover-authoritative` server-side, `VITE_ROOM_CUTOVER_AUTHORITATIVE` SPA-side) MUST match in any deployed combination.

| Server binary (`--room-cutover-authoritative`) | SPA bundle (`VITE_ROOM_CUTOVER_AUTHORITATIVE`) | Legacy global routes | Per-room routes | Result |
| --- | --- | --- | --- | --- |
| Pre-R14c binary — flag does NOT exist on this binary (covers Pre-R14b, Post-R14b, Post-R09i, and Post-R14d accepted) | `false` bundle | Registered; serve legacy repos | Registered | Pre-cutover baseline (any pre-R14c binary). The R09i no-op writer is composed unconditionally in this state. |
| R14c binary with `--room-cutover-authoritative=false` | `false` bundle | Registered; serve legacy repos | Registered | R14c binary in rollback / pre-cutover operational state. R09i's no-op writer is selected by the flag. Valid paired state. |
| R14c binary with `--room-cutover-authoritative=true` AND startup guard passed (schema ≥ 9 AND `room_cutover_marker` present) | `true` bundle | **Tombstone handlers; `410 Gone` envelope** + `Link: </api/rooms>; rel="successor-version"` | Registered; sole authoritative path | Production cutover state. ONLY valid post-R14c combination. |
| R14c binary with `--room-cutover-authoritative=true` AND startup guard failed (schema < 9 OR marker absent) | n/a | Server refuses to serve traffic; exits before opening listeners | n/a | Startup guard fail-closed. The `true` binary cannot start a pre-cutover or mid-window DB. |
| R14c binary with `--room-cutover-authoritative=true` | `false` bundle (forgotten rollout) | `410 Gone` tombstone | Registered | **BROKEN** — SPA still expects legacy endpoints but server returns `410`. Forbid this combination. |
| Mid-window rollback: **immediately pre-cutover production release** paired as `--room-cutover-authoritative=false` server AND `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` SPA bundle | OFF (gate not on) | Registered; serves legacy repos | Registered | Mid-window rollback. The `pg_dump` from the start of the window is the source of truth. Forward recovery for any data that mutated after the cutover. **Not an "R03 binary"** — R03 is the unrelated SQLite-to-PostgreSQL migration release. |

**Client-continuity claim:** the only combinations that guarantee client continuity are the documented ones. The front-end gate is the single mechanism `VITE_ROOM_CUTOVER_AUTHORITATIVE` matched against `--room-cutover-authoritative`. Pre-R14c, the SPA `false` bundle continues exposing the legacy dashboard so the global queue is reachable. The compatibility matrix forbids any combination where the server's flag is `true` and the SPA bundle is `false` — the deployed artifacts must match. The R14c deployment contract enforces this by requiring traffic to remain closed until both the `true` server and the `true` SPA bundle are deployed and paired-smoke-checked; mid-window rollback restores the `false` server and `false` SPA bundle as one pair before reopening traffic.

### Deployment, backup, and rollback gates

- Required PostgreSQL `pg_dump` BEFORE R14c cutover; retained ≥30 days.
- Application downtime / maintenance window is required during Phase B.
- `cmd/room-cutover plan` (read-only) and `--dry-run` MUST be run before any commit.
- Migration succeeds but application startup fails: rollback target is the **immediately pre-cutover production release** with `--room-cutover-authoritative=false` (NOT an "R03 binary").
- Explicit warning: rollback after new room writes may require forward recovery.
- `.down.sql` files exist for every new migration; `0010.down.sql` cannot restore dropped data.

## Future sprint sequence (FIXED)

Sprint execution order is fixed at **`R05b → R09h → R14b → R09i → R14d → R14c → R14e`** (R14e only after the verified rollback window). Reordering is NOT permitted.

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R05b — Room entry / player-lease UI** | Frontend only (no new HTTP routes; uses the existing R04 / R06 / R11a routes). Adds SPA client methods (`api.createRoom`, `api.listRooms`, `api.getRoom`, `api.joinRoom`, invite methods, `api.claimPlayerLease`, `api.heartbeatPlayerLease`, `api.releasePlayerLease`, `api.getPlayerLease`); exposes them as actual room create / list / join / invite-redeem / lease-claim UI. **BLOCKING PREREQUISITE for R14c.** | Existing backend routes are registered (per `cmd/server/main.go:413–486`); R05b only wires the SPA. |
| **R09h — Room vote-to-prioritize parity** | Backend-only. Adds `POST /api/rooms/{slug}/vote/prioritize`. **BLOCKING PREREQUISITE for the ENTIRE R14c cutover** — partial cutover would let surviving legacy mutations continue writing legacy global queue state. | Reuses existing `room_queue_song_prioritized` event; no new WS event constant. |
| **R14b — room-cutover mechanism** | Backend-only. New dedicated CLI `cmd/room-cutover` with `plan` / `up` / `verify` subcommands (NO `abort`). NEW migration `0009_room_cutover_support.up.sql` (creates BOTH `room_activities` AND `room_cutover_marker`; schema v8 → v9) with matching `.down.sql`. NEW marker table `room_cutover_marker` with `cutover_pre_commit_at`. CLI flags `--room-slug` / `--room-name` / `--host-user-id`. Lock contract: `pg_advisory_unlock(987654321)` on the same pinned connection (deferred unlock), with connection close as fallback. `users` / `user_sessions` / `priority_transactions` NOT recopied. NO global route retirement. NO frontend changes. R14b's gate MUST pass before R09i can run. | Does NOT reuse R03's CLI or `migration_marker`. R03's marker is preserved untouched. |
| **R09i — Room activity runtime parity** | Backend-only slice that wires existing room mutations (queue / playback / vote / auto-queue in `internal/usecase/roomqueue`, `internal/usecase/roomvote`, `internal/usecase/roomautoqueue`) to call `RoomActivityRepository.AddActivity` on the same events that the global runtime today appends to `activities`. Replaces the global dashboard's silent activity append. After R09i, the room runtime has a live write path to `room_activities`. **R09i is BACKEND-ONLY**; the activity panel READ surface is a separate optional frontend decision (deferred). **BLOCKING PREREQUISITE for R14c** unless the Product Owner explicitly waives it (recorded on Issue #20). | The `room_activities` table itself is created in R14b (`0009`). R09i wires the call sites only. |
| **R14d — frontend global-path removal** | Frontend-only. Removes / redirects `DashboardView`; clears `globalStore.queueState` / `voteSessions` / `autoQueueConfig`; closes global `WebSocketClient`; preserves `currentUser` + `roomQueues[slug]`. **Behavioral activation deferred to R14c** — implementation may be accepted before R14c but the SPA's behavior change activates only during the R14c maintenance window behind an explicit cutover-state gate, so the still-authoritative global queue remains reachable pre-cutover. | Assumes R05b has landed. |
| **R14c — coordinated production cutover** | Backend production cutover (Phase B). Maintenance window executes in this exact order: (1) close public traffic; (2) run and verify `cmd/room-cutover up` against the live DB; (3) deploy the `--room-cutover-authoritative=true` server AND the `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` SPA bundle as a paired set while traffic remains closed; (4) paired smoke checks against both artifacts; (5) reopen traffic. ALL listed legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes AND the global `/ws` endpoint retire simultaneously with `410 Gone` tombstone handlers (`Link: </api/rooms>; rel="successor-version"` header). The global `/ws` upgrade is rejected with `410` in the HTTP phase. R14d's SPA cutover-state gate is activated at the same time. **ATOMIC across the listed legacy routes** — no partial cutover. | Requires R05b + R09h + R14b + R09i + R14d all accepted (with R09i waivable only by explicit PO sign-off). |
| **R14e — schema cleanup** | Backend-only. Lands `0010_drop_legacy_global_tables.up.sql` (a SEPARATE forward migration that drops the **legacy global** `queue_state`, `activities`, `auto_queue_config`, `play_history` tables). **Does NOT touch `room_play_history` or `room_activities`.** Lands matching `0010.down.sql` (creates empty legacy table shells; **cannot restore dropped data**; documented as such in the `.down.sql` header). Bumps schema v9 → v10. Historical migration files `0001`..`0009` remain on disk byte-for-byte; `0001_initial.up.sql` is NEVER rewritten. | Runs after the verified rollback window AND after no runtime path references the legacy tables. |

R05b / R09h / R09i / R14b / R14c / R14d / R14e MUST NOT be combined; migration, destructive cleanup, frontend rewrite, frontend entry UX, room-activity runtime, and vote-prioritize parity must remain independently reviewable.

## Validation, errors, and test inventory expectations

R14a documents the expected test inventory; tests are NOT in R14a scope.

### Validation rules

- Slug + name: server-side regex / length rules mirror the existing room-slug validation.
- `--host-user-id`: positive integer; resolved by PK lookup against `users`.
- Idempotency: re-run = no-op exit 0; hash drift = reject.
- Archived-room / missing-host failures map to explicit sentinels.

### Errors and the route-retired responses

- After R14c, the listed legacy global REST routes and `/ws` return `410 Gone` via repository-free tombstone handlers wired at the mux entry. **`410 Gone` is the approved retirement posture** (NOT `404`).
- Envelope (the actual wire response):

```json
{
  "error": "gone",
  "code": "global_contract_retired",
  "documentation": "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"
}
```

- `Link: </api/rooms>; rel="successor-version"` header IS added in the tombstone handler response.
- The global `/ws` upgrade is rejected with `410 Gone` during the HTTP phase.
- Discovery: `GET /api/rooms`.
- No concrete `successor: "/api/rooms/{slug}/..."` value in the envelope.

### Future test matrix (NOT in R14a scope)

- R05b: frontend tests for entry methods; UI walk tests.
- R09h: vote-prioritize interactor / handler / WebSocket tests.
- R09i: `RoomActivityRepository.AddActivity` call-site tests on room mutators.
- R14b: `room_activities` + `room_cutover_marker` lossless copy / SHA256 / sequence resync; idempotent re-run; hash-drift rejection; advisory-lock conflict; transaction rollback on mid-flight failure; PII redaction; dry-run output; legacy-id-offset correctness; marker-in-same-transaction-as-room-and-host-and-copy test (kill mid-flight, restart DB, verify NO partial state); CLI flag validation; deferred `pg_advisory_unlock` on the same pinned connection test; `cutover_pre_commit_at` field test; schema version 9; `.down.sql` of `0009_room_cutover_support` drops both tables cleanly.
- R14c: `410 Gone` envelope tests; `Link` header tests; `/ws` HTTP-phase rejection; atomic retirement of all listed legacy routes simultaneously; **startup guard tests** — server with `--room-cutover-authoritative=true` MUST refuse to serve traffic and exit before opening listeners when schema version < 9; MUST refuse to serve traffic and exit before opening listeners when `room_cutover_marker` is absent; MUST proceed to serve only when both conditions hold; the guard is marker + schema-version presence only and does NOT require copied target hashes to remain unchanged on later startups.
- R14d: frontend tests for `globalStore` clear on cutover; cutover-state gate enforcement; pre-cutover the SPA exposes legacy dashboard.
- R14e: schema 10; `.down.sql` of `0010` recreates empty legacy tables and is documented as one-way.

### Migration files (R14b / R14e)

- `0009_room_cutover_support.up.sql` and `0009_room_cutover_support.down.sql` — create / drop both `room_activities` and `room_cutover_marker` (with their indexes and constraints). Only `room_activities.room_id` carries a foreign key (`REFERENCES rooms(id) ON DELETE CASCADE`); `room_cutover_marker.target_room_id` and `room_cutover_marker.host_user_id` are intentionally stored as plain `BIGINT` audit snapshots WITHOUT foreign keys — migration evidence MUST NOT own room or user lifecycle.
- `0010_drop_legacy_global_tables.up.sql` and `0010_drop_legacy_global_tables.down.sql` — drop / recreate the **legacy global** tables only. The `.down.sql` header states explicitly that it cannot restore dropped data.
- Every migration follows the existing R00–R11a convention: matching `.down.sql` files; `0001_initial.up.sql` is NEVER rewritten.

## Schema-version plan

The migration runner reports the applied numbered migration as the schema version; adding a migration necessarily bumps the version. No sprint can both add a migration and keep the version unchanged.

- **Schema at start of R14a: v8** (last applied: `0008_room_chat_messages.up.sql`).
- **R14b lands `0009_room_cutover_support.up.sql`** — creates BOTH `room_activities` AND `room_cutover_marker`. Schema becomes **v9**.
- **R14c is a runtime + binary-flag cutover; it does NOT add a migration.** Schema during R14c and the rollback window: v9.
- **R14e lands `0010_drop_legacy_global_tables.up.sql`** — drops the legacy global tables. Schema becomes **v10**.

**Historical migration files are never rewritten.** `0001_initial.up.sql`..`0008_room_chat_messages.up.sql` remain on disk byte-for-byte. R14e does NOT modify `0001_initial.up.sql`; it lands `0010_drop_legacy_global_tables.up.sql` as a SEPARATE forward migration. The historical files remain auditable.

## ADR reconciliation

R14a supersedes parts of ADR 001 and the R06-fold attribution in ADR 002 §11/§13 via the new ADR 003 (Proposed). ADR 001 §3 Decision 2 (no permanent `main` room), §4 (lifecycle), §5 (lease model), §7 (invite model), §16 (deferred work), and the "single-instance only" posture remain authoritative.

## PO acceptance blockers

1. **Room-activity runtime parity (R09i)** — R09i is BACKEND-ONLY and is a BLOCKING PREREQUISITE for R14c unless the Product Owner explicitly waives it.
2. **Live room-activity read parity (frontend panel)** — deferred as a separate optional frontend task; not in R09i or R14a scope.
3. **R09h landing prerequisite for the entire R14c cutover** — partial cutover is forbidden.
4. **R05b landing prerequisite for user-facing cutover.**
5. **Route-retirement response posture** — `410 Gone` via tombstone handlers, NOT `404` unregistration.
6. **R14d behavioral activation** — gated behind an explicit cutover-state flag activated during the R14c maintenance window; pre-cutover the SPA continues exposing the legacy dashboard so the still-authoritative global queue remains reachable.
7. **Rollback posture** — rollback target is the **immediately pre-cutover production release** with `--room-cutover-authoritative=false` (NOT an "R03 binary").
8. **Schema-version plan approval** — v8 → v9 (R14b) → v10 (R14e); historical migrations preserved on disk; `0001_initial.up.sql` NEVER rewritten.
9. **`0010.down.sql` cannot restore dropped data** — destructive cleanup rollback is one-way.

## Items already settled (informational)

- ID-collision handling for `room_play_history`: `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)`.
- Account-table posture: no recopy.
- Schema version plan: v8 → v9 (R14b) → v10 (R14e). Historical migrations preserved on disk; `0001_initial.up.sql` NEVER rewritten.
- The R03 `cmd/migrate-data` + `migration_marker` is preserved untouched and is the SQLite-to-PostgreSQL migrator; it is NOT reused for the room cutover.
- The `entity.Queue` JSON conversion uses the default `encoding/json` tagged-field round-trip; no custom `MarshalJSON` / `UnmarshalJSON` methods are assumed.
- The queue-prioritize / vote-prioritize distinction: `/api/queue/prioritize` has a room equivalent; `/api/vote/prioritize` does not.
- The marker is inserted in the same transaction as the room, host membership, and copied state.
- Marker timestamp field is `cutover_pre_commit_at` (NOT `cutover_committed_at`) — written before `COMMIT`.
- Lock contract: deferred `pg_advisory_unlock(987654321)` on the same pinned connection (mirroring R03's idiom); connection close / `SIGTERM` is the fallback.
- `cmd/room-cutover` has NO `abort` subcommand.
- Retirement posture: `410 Gone` via repository-free tombstone handlers; `/ws` rejected with `410` in the HTTP phase.
- R09i is BACKEND-ONLY (write path; activity panel is a separate optional frontend task).
- Each new migration ships a matching `.down.sql`. `0010.down.sql` cannot restore dropped data.
- Sprint execution order is fixed: `R05b → R09h → R14b → R09i → R14d → R14c → R14e`.
- R14d's behavioral activation is gated behind an explicit cutover-state flag; pre-cutover the SPA continues exposing the legacy dashboard.
- R14c is atomic across the listed legacy global routes; no partial cutover.

## Issue #17 requirement mapping

Every Issue #17 (Epic) requirement maps to either an R14a decision or an explicit defer.

| #17 Requirement | R14a status |
| --- | --- |
| Migrate legacy global state into one PO-named room | R14a § Source-to-target mapping; R14b implements. |
| Atomic offline migration (no startup magic) | R14a § Atomicity and idempotency; new dedicated `cmd/room-cutover`. |
| Idempotent migration; no `--force` / `--reset`; hash drift rejected | R14a § CLI flags. |
| No permanent `main`, `room 0`, hidden default room | R14a § Non-goals; ADR 003. |
| Single source of truth after cutover | R14a § Source-to-target mapping; legacy tables read-only then dropped. |
| Old global routes transition via explicit route retirement (no hidden default-room proxy) | R14a § Compatibility & endpoint retirement phases; ADR 003 §3.1. |
| `/ws` cutover with no synthesized default-room fallback | R14a § WebSocket cutover; ADR 003 §3.4. |
| Frontend cutover with state-reset, navigation, recovery (deferred activation) | R14a § Frontend transition & recovery. |
| R13 authorization hardening remains separate | R14a § Out of scope. |
| Final implementation plan split into small future sprints with migration gates, verification, rollback | R14a § Future sprint sequence + § Deployment compatibility matrix. |
| Route-retirement envelope with documentation pointer | R14a § Errors and the route-retired responses. |
| Migrated room identity contract (slug, name, host, conflict) | R14a § Migration identity contract. |
| Source rows deleted, archived, or retained as evidence | R14a § Source-to-target mapping. |
| Activities handled explicitly (not silently dropped) | R14a § Activities target + R09i write path + read parity deferred. |
| `/api/vote/prioritize` parity before retirement | R14a § Future sprint sequence: blocked on R09h, blocks entire R14c. |
| Frontend rollback behaviour if newer frontend with older backend | R14a § Deployment compatibility matrix. |
| R14a documentation-only | R14a § Non-goals (explicit). |

## Verification evidence expectations

R14a Builder evidence MUST include:

- `git diff --check` — clean.
- `git diff --name-only` contains only documents under `documents/00-project-management/`.
- No `*.go`, `*.vue`, `*.sql`, Docker, Nginx, env, package, `go.mod`, `go.sum` touched.
- File references throughout the R14a documents that prove the current route and table inventory (this document's § Current behavior cites every file the inventory requires).
- A mapping table for every Issue #17 requirement.
- Confirmation that no runtime files changed.
- Confirmation that R13, R10f+, R11b+, and R12 were not silently pulled into scope.
- Confirmation that R09h blocks the entire R14c cutover (not just `/api/vote/prioritize`).
- Confirmation that R14c retires all listed legacy routes atomically via `410 Gone` tombstone handlers, NOT unregistered.
- Confirmation that ADR 003 does not mark itself Accepted.
- Confirmation that R05b, R09h, R09i, R14b, R14c, R14d, and R14e are NOT marked active.
- Confirmation that the migration uses `0009_room_cutover_support.{up,down}.sql` (creating both tables), and `0010_drop_legacy_global_tables.{up,down}.sql`.

## Risks and review focus

Risk level: **High**.

Review must focus on:

- whether every live global source has a lossless target;
- whether activities are handled explicitly (R14a designs `room_activities`; R14b builds the table; R09i wires the runtime write path);
- whether the host bootstrap is deterministic and server / operator controlled;
- whether retry, backup, rollback, and source cleanup timing are safe;
- whether global route retirement can accidentally select or mutate the wrong room;
- whether frontend and backend can be deployed in a compatible order (compatibility matrix + R14d's gated activation);
- whether security hardening is honestly deferred (R13 is separate);
- whether the future slices are small enough to review independently;
- whether R03's `cmd/migrate-data` + `migration_marker` are NOT reused for the room cutover;
- whether `0009_room_cutover_support` creates BOTH tables atomically;
- whether the marker timestamp is `cutover_pre_commit_at` (NOT `cutover_committed_at`);
- whether `cmd/room-cutover` ships NO `abort` subcommand;
- whether the lock contract uses deferred `pg_advisory_unlock` on the same pinned connection;
- whether R09i is BACKEND-ONLY (no activity panel in R09i scope);
- whether each new migration ships a matching `.down.sql`;
- whether `0010.down.sql` documents that it cannot restore dropped data;
- whether R14d's behavioral activation is gated behind an explicit flag activated during R14c.

## Builder reasoning effort

**High.** This is a cross-layer contract and data-migration planning sprint with data-loss, compatibility, and deployment risk. The output must be precise enough that later Builders do not invent behaviour.

## Cross-references

- ADR 003 — Legacy global-state migration and contract retirement: [`../ADRS/003-legacy-global-state-migration-and-contract-retirement.md`](../ADRS/003-legacy-global-state-migration-and-contract-retirement.md).
- ADR 001 — Room architecture and contracts (metadata-only supersession note): [`../ADRS/001-room-architecture-and-contracts.md`](../ADRS/001-room-architecture-and-contracts.md).
- ADR 002 — PostgreSQL migration design (R06-fold attribution reconciled via ADR 003): [`../ADRS/002-postgresql-migration-design.md`](../ADRS/002-postgresql-migration-design.md).
- Room epic sequence: [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md).
- R03 migrator patterns (preserved untouched; NOT reused by R14b): [`009-sqlite-to-postgresql-data-migration.md`](./009-sqlite-to-postgresql-data-migration.md).
- R07d room-queue prioritize runtime: [`012-room-scoped-playback-queue.md`](./012-room-scoped-playback-queue.md) (R07d section).
- R09b room-vote runtime (mirrors R09h session shape): [`014-player-control-semantics.md`](./014-player-control-semantics.md) (R09b Implementation summary).
- R09e / R09f / R09g room auto-queue contract + runtime: [`021-room-auto-queue-contract-design.md`](./021-room-auto-queue-contract-design.md).
- R10a / R10b / R10c / R10d room deletion: [`015-room-deletion-and-membership-removal.md`](./015-room-deletion-and-membership-removal.md).
- R11a room chat: [`016-room-chat-feature.md`](./016-room-chat-feature.md).
- Issue #17 (epic).
- Issue #20 (R14a spec).
