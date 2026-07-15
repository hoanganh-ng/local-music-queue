# ADR 003 — Legacy global-state migration and contract retirement

- Status: Proposed — R14a contract; supersedes parts of ADR 001 and ADR 002 §11/§13; pending Architect review and Product Owner acceptance
- Date: 2026-07-15 (revised twice after Architect reviews; first Architect review applied 2026-07-15, second Architect review applied 2026-07-15)
- Scope: Reconciles several paragraphs of ADR 001 (and one paragraph cluster of ADR 002) that have become inaccurate as the room epic landed in the renumbered R06–R11 sequence. Records the new R14a implementation-ready contract for converting the legacy global state into exactly one Product-Owner-named room and for retiring the parallel global REST/WS contracts. ADR 003 does **not** replace ADR 001's room architecture in general; it supersedes only the specific paragraphs listed below. ADR 003 is documentation-only: no runtime change is authorized by it.

> **Second-pass review-revision summary (2026-07-15).** After the second Architect review, this ADR applies 8 corrective changes from the first-pass draft: (1) schema versioning — R14b's `0009_room_activities.up.sql` lands and the schema becomes v9 (the migration runner reports the applied migration as the version, so adding a migration MUST bump it); R14e's `0010_drop_legacy_global_tables.up.sql` lands and the schema becomes v10; historical migration files `0001`..`0008` are NEVER rewritten; the v10 state is the cumulative result of running `0001`..`0010` in order; (2) R14c retires ALL listed legacy routes atomically; R09h blocks the entire R14c cutover (NOT just `/api/vote/prioritize`) because a partial cutover lets surviving legacy mutations continue writing legacy global queue state; (3) sprint execution order is `R05b → R09h → R14b → R14d → R14c → R14e`; (4) the marker is inserted in the SAME transaction as the room, host membership, and copied state — no post-commit marker write; a mid-flight crash leaves NO partial state; (5) `cmd/room-cutover` has NO `abort` subcommand because PostgreSQL session locks belong to the connection that acquired them; the only release path is connection close (process exit / `SIGTERM`); (6) R09i — Room activity runtime parity — is a new sprint that wires the room runtime to the new `room_activities` table, blocking R14c unless the Product Owner explicitly waives it; (7) retirement is `410 Gone` via repository-free tombstone handlers (NOT `404` from route unregistration); the global `/ws` upgrade is rejected with `410` in the HTTP phase; (8) R14e drops the legacy global `play_history` table only — `room_play_history` is NOT legacy and is NEVER touched.

## 1. Context

The verified `dev` baseline after accepted R11a has two parallel product models:

- PostgreSQL is the only runtime database (ADR 002 — R01/R02/R03).
- Legacy global persistence still exists: singleton `queue_state` (`id = 1`, TEXT JSON), append-only `activities`, singleton `auto_queue_config` (`id = 1`), and `play_history` (50-row cap, enforced in Go).
- Room persistence also exists: `rooms` / `room_members` / `room_invites` (R04, migration 0004); `room_queue_state` (R07a, JSONB per `rooms.id`, migration 0006); `room_auto_queue_config` + `room_play_history` (R09f, migration 0007); `player_leases` (R06, migration 0005); `room_chat_messages` (R11a, migration 0008).
- Global and room-scoped runtime contracts coexist: global `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...`, `/ws`; room-scoped `/api/rooms/{slug}/...` and `/ws/rooms/{slug}`.
- The frontend is substantially room-aware (`DashboardView.vue` is global-only; `RoomView.vue` reuses `globalStore.currentUser` and `globalStore.roomQueues[slug]`).
- Room **entry** UX is **incomplete**: no `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / invite / player-lease SPA methods exist today even though the backend routes are registered (per `cmd/server/main.go:413–486` and the inventory in `022-…md` §3). Rooms are entered only by direct URL; this is a prerequisite gap that blocks global-route retirement and is added to R14a's future-sprint sequence as a blocking prerequisite.
- Room **activity** UX is **incomplete**: the global `activities` table is fed by vote / priority / queue mutations in `usecase/vote/interactor.go`, `usecase/priority/interactor.go:81`, and `usecase/queue/interactor.go`. The room runtime has no equivalent — the new `room_activities` table has no write path. Cutover must either ship the room-activity runtime write path as a separate sprint (R09i) or the Product Owner must explicitly approve retiring live activity logging.
- No migration has yet converted the global state into a PO-named room as the sole source of truth.
- No permanent `main` room, `room 0`, or hidden default room is allowed by ADR 001 §3 Decision 2 and Epic #17.
- Live room-activity read parity (a `RoomView` activity panel) is NOT implemented in this sprint and is documented as a separate concern from R09i's runtime write path.

Several ADR 001 paragraphs — and one ADR 002 paragraph cluster (§11/§13) — were written before the room epic was renumbered and before the shim plan was deferred indefinitely. They are now historically inaccurate and must be reconciled before the R14 migration sprint can ship safely.

## 2. Supersession statement

ADR 003 supersedes **only** the following paragraphs of ADR 001 and one paragraph cluster of ADR 002. ADR 003 does NOT replace ADR 001's room architecture in general. ADR 001 remains authoritative for the decisions that ADR 003 does not replace, including: explicit rooms and no permanent default room; room lifecycle (create → active → archived); membership roles (host / admin / guest); one host per active room; player-lease separation; invite-token principles; per-room REST/WebSocket direction; single-instance limitations; account-scoped priority unless separately changed.

Superseded ADR 001 paragraphs:

- §3 Decision 6 — the compatibility shim was never built; old global routes still operate on legacy global repositories.
- §9 transition-strategy table — the R07 row's "compatibility shim that resolves the single migrated room by default … logged at startup" was never implemented; no migrated room exists today.
- §9 final route shape list — uses `{roomId}` (the code uses `{slug}`) and lists room routes that were never built in R07/R08 (`.../queue/skip|status|sync|ended|prev|volume`, `.../user/priority-balance`, `.../youtube/search`); the actual playback route family is `/api/rooms/{slug}/playback/{status,sync,skip,ended,prev,volume}`. The line `.../vote/prioritize` is **NOT** a missing build — it is a different feature (vote-driven queue mutation) and was deliberately deferred; the room equivalent lands in R09h.
- §10 global `/ws` backward-compatibility paragraph — "`/ws` … serves a synthesized 'default room' view backed by the migrated room." This paragraph was never implemented. `/ws` serves the real global `queue_state` JSON today, with no default room; R14c will retire `/ws` with `410 Gone` (NOT `404`) rather than add a default-room fallback.
- §11 migration direction paragraph — "global `queue_state` row becomes the migrated room's … R06 converts … global rows are deleted at the end of R06." R06 became Player Lease; the global rows were NOT deleted and remain the live source of truth. The conversion is exactly what R14a now shapes, executed by the R14b dedicated `cmd/room-cutover` mechanism (NOT R06, NOT a startup magic transform, NOT a reuse of R03's `cmd/migrate-data` + `migration_marker`).
- §11 table row — "Room-scoped activities … existing global `activities` table is migrated." There is currently no `room_activities` table; the migration target is a NEW table designed by R14a (see §5 below). The runtime write path lands in R09i; live read parity is deferred.
- §15 compatibility table — the R07/R08 shim + "`/ws` deprecated but live / default room" rows are inaccurate for the same reasons.
- §18 consequences mapping — the sprint → scope map (R06/R07/R08/R10/R11/R12) does NOT match the renumbered epic (R06 = Player Lease; R07 = Room queue; R09 = playback/vote/auto-queue; R10 = deletion; R11 = chat; R12 = search).

Superseded ADR 002 paragraph cluster:

- §11 (`setval` / R06 fold attribution) and §13 (R03 migrator reuse) repeatedly attribute the global → room fold to "R06". This attribution was never implemented; R06 became Player Lease. The migrator-reuse shape ADR 002 anticipated (via a `room_id` parameter) was historically associated with the R03 migrator; **R14a explicitly clarifies that the room-cutover mechanism is a NEW dedicated CLI (`cmd/room-cutover`) with a NEW dedicated marker contract (`room_cutover_marker`), distinct from R03's `cmd/migrate-data` + `migration_marker`**. R03's mechanism and marker are preserved untouched and continue to serve their existing role.

## 3. Per-section supersession detail

### 3.1 ADR 001 §3 Decision 6 — compatibility shim

Old: "Room context is mandatory on every queue, playback, voting, auto-queue, invite, member, and priority REST call after the migration sprint, and on the WebSocket connection. The old global routes transition through a documented compatibility shim to a final `410 Gone`."

Verified current behaviour: no shim exists. `cmd/server/main.go` still registers the legacy global routes (lines 389–409) operating on the legacy `QueueRepository` / `AutoQueueRepository`. Old routes return 200/4xx from legacy repositories; they do NOT resolve a migrated room.

New R14a decision: the retirement path is **explicit `410 Gone`** (Phase B, R14c), not a shim. A proxy may only be selected if identity, membership, and room-selection behaviour are unambiguous; R14a does not have that evidence, so the recommended path is `410 Gone` with a stable machine-readable error envelope, a `Link: </api/rooms>; rel="successor-version"` header, and a room-discovery pointer. See §7 below.

### 3.2 ADR 001 §9 transition-strategy table — R07 row

Old: "R07 … compatibility shim that resolves the single migrated room by default … logged at startup."

Verified current behaviour: no migrated room exists today; the shim was never built.

New R14a decision: removed. The R07 room-queue slice landed as `POST/GET /api/rooms/{slug}/queue/...` (R07a) + the per-room WebSocket delta family (R07b) + frontend narrow wiring (R07c) + the `queue/prioritize` queue mutation (R07d). No shim. The migration sprint sequence is fixed: R05b → R09h → R14b → R14d → R14c → R14e, with R09i (room activity runtime parity) as a blocking prerequisite for R14c unless the Product Owner explicitly waives it.

### 3.3 ADR 001 §9 final route shape list

Old: uses `{roomId}` as the path parameter; lists `.../queue/skip`, `.../queue/status`, `.../queue/sync`, `.../queue/ended`, `.../queue/prev`, `.../queue/volume`, `.../user/priority-balance`, `.../youtube/search`, `.../vote/prioritize` under the room scope.

Verified current behaviour: path parameter is `{slug}` (not `{roomId}`); the queue mutations live under `.../queue/{add,remove,clear,prioritize}`; the playback mutations live under `.../playback/{status,sync,skip,ended,prev,volume}`; `/api/rooms/{slug}/user/priority-balance` was never built (priority remains account-scoped per ADR 001 §3 Decision 9); `/api/rooms/{slug}/youtube/search` was never built (search remains global utility); `/api/rooms/{slug}/vote/prioritize` is the room vote parity sprint R09h, **distinct** from `/api/rooms/{slug}/queue/prioritize` (the R07d **queue** mutation that the global `/api/queue/prioritize` route replaces — already exists).

New R14a decision: ADR 001 §9's route list is replaced by the actual room route inventory in `022-…md` §3. R14a records the four `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...`, and global `/ws` routes as the explicit retirement set. **The cutover is atomic across the listed legacy routes**: R14c retires ALL of them in one step (no partial cutover).

- **Queue mutations** — global `/api/queue/{add,skip,status,sync,ended,prev,remove,clear,volume,prioritize}` already have room equivalents (`/api/rooms/{slug}/queue/{add,remove,clear,prioritize}` + `/api/rooms/{slug}/playback/{status,sync,skip,ended,prev,volume}`); they retire in R14c's Phase B with no extra parity sprint.
- **Vote-mutations** — global `/api/vote/skip` has its room equivalent (R09b's `POST /api/rooms/{slug}/vote/skip`); it retires in R14c's Phase B with no extra parity sprint.
- **Vote-prioritize** — global `/api/vote/prioritize` does NOT have a room equivalent today; its retirement is **blocked** on R09h landing. R09h is also a blocking prerequisite for the entire R14c cutover (NOT just `/api/vote/prioritize`) because a partial cutover would let surviving legacy mutations continue writing legacy global queue state after room state is declared authoritative.

### 3.4 ADR 001 §10 `/ws` default-room synthesis

Old: "`/ws` … serves a synthesized 'default room' view backed by the migrated room."

Verified current behaviour: `/ws` is built by `ws.NewHub(qInteractor.GetState)` at `cmd/server/main.go:232` and registers via `mux.HandleFunc("/ws", hub.RegisterHandler)` at `main.go:588`. The hub reads `qInteractor.GetState`, which serializes the legacy global `*entity.Queue` from `queue_state` (R00–R05 baseline). There is no default room. The 16-event global inventory is unchanged from R00; per-room events live on `/ws/rooms/{slug}` only.

New R14a decision: R14a explicitly rejects the synthesized default-room `/ws` plan. R14c retires `/ws` with `410 Gone` via a repository-free tombstone handler (NOT `404` from unregistration; NOT route deletion). The HTTP-phase rejection prevents WebSocket handshakes from succeeding on the retired endpoint.

### 3.5 ADR 001 §11 migration direction — R06 fold

Old: "global `queue_state` row becomes the migrated room's … R06 converts … global rows are deleted at the end of R06."

Verified current behaviour: R06 became Player Lease (`011-player-lease-and-host-departure-semantics.md`). The global `queue_state`, `activities`, `auto_queue_config`, and `play_history` rows were NOT deleted; they remain the live source of truth.

New R14a decision: the global → room conversion is the R05b → R09h → R14b → R14d → R14c → R14e sequence, executed by the **new** dedicated `cmd/room-cutover` mechanism with the **new** dedicated `room_cutover_marker` contract. R03's `cmd/migrate-data` + `migration_marker` are preserved untouched.

### 3.6 ADR 001 §11 — Room-scoped activities row

Old: "Room-scoped activities … existing global `activities` table is migrated."

Verified current behaviour: there is no `room_activities` table, migration, or repository. `grep -rn "room_activities\|RoomActivit"` returns zero matches across `*.go`, `*.sql`, `*.js`. Live room-activity **write** parity (the room runtime feeding `room_activities`) is NOT implemented; today only the global `activities` table is populated and is read by `delivery/http/handlers.go:92` and use cases. Live room-activity **read** parity (a `RoomView` activity panel) is also NOT implemented.

New R14a decision: R14a designs a new `room_activities` table (see §5.1). R14b builds it. The migrator copies every legacy row losslessly (preserved id/timestamp/type/user/description/count). The runtime write path lands in R09i (Room activity runtime parity), which is a blocking prerequisite for R14c unless the Product Owner explicitly waives it (recorded on Issue #20). Live read parity is deferred as a separate frontend task.

### 3.7 ADR 001 §15 compatibility table — shim + default `/ws` rows

Old: rows describing the R07/R08 shim and "`/ws` deprecated but live / default room."

Verified current behaviour: same as §3.1, §3.4.

New R14a decision: replaced by the production coordination sequence in §7 (R14b builds offline; R14d ships BEFORE R14c so the post-R14c binary has a compatible frontend bundle; R14c owns the production execution with a binary that has tombstone handlers returning `410 Gone`; R14e lands a SEPARATE forward migration that drops the legacy global tables only).

### 3.8 ADR 001 §18 consequences mapping — sprint → scope map

Old: maps R06 = Room-Scoped Persistence, R07 = REST + shim, R08 = WS, R10 = Auto-Queue, R11 = Welcome/Create/Invite/Join, R12 = Dashboard.

Verified current behaviour: the renumbered epic is R06 = Player Lease; R07 = Room queue; R09 = playback/vote/auto-queue; R10 = deletion; R11 = chat; R12 = search.

New R14a decision: the §18 map is replaced by the room epic sequence as it actually landed in `ROOM_EPIC_SPRINT_SEQUENCE.md`. R14a is documented in `022-…md` and tracked in the sequence doc.

### 3.9 ADR 002 §11 + §13 — R06 fold attribution

Old: ADR 002 §11 (line ~257) and §13 (line ~333) repeatedly attribute the global → room fold to "R06": "In R06, this row is the source for the migrated room's …"; "R06 reuses it via a `room_id` parameter."; "R06 converts … global rows are deleted at the end of R06." §13 also anticipates the R03 migrator being reused "via a `room_id` parameter to fold the global state into the Product-Owner-named room".

Verified current behaviour: same as §3.5 — R06 became Player Lease; the fold was never executed.

New R14a decision:

- This attribution is no longer fulfilled by R06 (which is Player Lease).
- The room-cutover mechanism is a **NEW** dedicated CLI (`cmd/room-cutover`), not a reuse of R03's `cmd/migrate-data`. R14a explicitly separates the two CLIs and the two marker contracts.
- The R03 migrator (SQLite → PostgreSQL) is preserved untouched. The R03 marker (`migration_marker`) is preserved untouched. R03 is NOT extended with the room-cutover logic; R14b adds a separate marker table (`room_cutover_marker`) and a separate CLI.
- ADR 002's status remains "Proposed — awaiting Architect review and Product Owner approval"; ADR 003 does not move it.

## 4. Why the old plan was not implemented

R06 became Player Lease rather than the global → room fold (the original ADR 001 §11 / ADR 002 §11/§13 attribution). Sprint renumbering placed room scoping in R07a/R07b/R07c/R07d (room queue) and R09a/R09b/R09c/R09d/R09e/R09f (playback / vote / auto-queue). Rooms were addressed by `{slug}` not `{roomId}`. The compatibility shim was deferred indefinitely because the global frontend surface never produced evidence of unambiguous identity, membership, and room-selection behaviour that would let a hidden default-room proxy work safely. The `/ws` synthesized default-room plan was never implemented; the global hub continues to serve real `queue_state`. The first R14a draft conflated the global `/api/queue/prioritize` (a queue mutation with an R07d room equivalent) with the global `/api/vote/prioritize` (a vote-driven queue mutation that needs the R09h room equivalent) — these are two distinct route families and only the second needs a parity sprint, AND the second blocks the entire R14c cutover because partial retirement is unsafe. The first R14a draft also implicitly reused R03's `cmd/migrate-data` + `migration_marker`; R14a clarifies that the room-cutover mechanism is a NEW dedicated CLI + NEW dedicated marker, distinct from R03. The first R14a draft claimed `MarshalJSON` / `UnmarshalJSON` methods on `entity.Queue` that do NOT exist; the conversion contract uses the default `encoding/json` tagged-field round-trip. The first R14a draft claimed route retirement was unregistration (HTTP 404); the second-pass review accepted `410 Gone` via repository-free tombstone handlers (NOT 404). The first R14a draft did not resolve the `room_activities` write-path question; the second-pass review added R09i as a blocking prerequisite. The first R14a draft lacked proper ordering (R14c before R14d); the second-pass review fixed the order to R05b → R09h → R14b → R14d → R14c → R14e. The first R14a draft had a typo that said R14e drops `room_play_history` rows; the corrected R14e drops the legacy global `play_history` table only. The first R14a draft kept the schema at v8 through R14b despite R14b adding `0009`; the second-pass review caught that and bumped to v9 in R14b and v10 in R14e. The first R14a draft had a `cmd/room-cutover abort` subcommand that cannot release another process's advisory lock; the second-pass review removed it. The first R14a draft's `room_cutover_marker` was written post-commit; the second-pass review moved it into the same transaction as the room, host, and copy.

The migration is now shaped by R14a and split into the future sequence in §10 (plus R05b as a blocking frontend-entry prerequisite, R09h as a blocking vote-prioritize + full-cutover prerequisite, and R09i as a blocking room-activity runtime prerequisite).

## 5. Migration identity contract

### 5.1 Activities target — new `room_activities` table

R14a designs the contract; **R14b** adds `0009_room_activities.up.sql` and lands the migration (schema v8 → v9); **R09i** wires the runtime to call `RoomActivityRepository.AddActivity` on the same events that the global runtime today appends to `activities` (vote / priority / queue mutations).

```text
room_activities
  id          BIGSERIAL PRIMARY KEY
  room_id     BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE
  timestamp   TIMESTAMPTZ NOT NULL
  type        TEXT NOT NULL
  user        TEXT NOT NULL
  description TEXT NOT NULL
```

Index: `(room_id, timestamp DESC, id DESC)`.

Migration preserves every legacy `activities` row's `id`, `timestamp`, `type`, `user` (display text), `description`, row count, and ordering. After the preserved-id insert, the migrator resynchronises the `room_activities_id_seq` via `setval(seq, MAX(id), is_called=true)`.

No `user_id` requirement during the migration. Legacy rows store display text and may represent users that cannot be resolved reliably or system-generated actions. A nullable actor id is a future-feature concern, not a migration concern.

Dedicated narrow repository contract (do NOT couple to `QueueRepository`):

```go
AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error
GetActivities(ctx context.Context, roomID int64, limit int) ([]entity.Activity, error)
```

After cutover, `room_activities` is the sole writable source of truth. The legacy `activities` table is read-only for the approved rollback window and is dropped in R14e via a SEPARATE forward migration (`0010_drop_legacy_global_tables.up.sql`). The R14e migration drops the legacy global tables only — `room_activities` is the new per-room target and is NOT dropped.

**Live room-activity runtime write parity** lands in R09i. **Live room-activity read parity** (a `RoomView` activity panel) is deferred as a separate frontend task; not in R09i or R14a scope.

### 5.2 Host bootstrap — canonical PostgreSQL `users.id`

Operator flag: `--host-user-id=<positive integer>` (the canonical `users.id` BIGINT, NOT a UUID).

Validation:

- The value is a positive integer.
- Exactly one `users` row exists with that ID. Because it is a primary key, "ambiguous" is not a meaningful outcome.
- The host is inserted into the migrated room as its sole host **inside the same migration transaction** as room creation, data conversion, and the marker row (§8 below).
- Failure to resolve the ID aborts the migration before any target state is committed.

The migrator does NOT infer the host from: the first user to join, `users.role`, `HOST_EMAILS` / `ADMIN_EMAILS`, an existing player lease, or frontend / request-supplied identity.

**Override of issue #20 §2:** the schema permits one user to be a member of multiple rooms (uniqueness is `(room_id, user_id)` only); the one-host invariant is **per room**, not per user globally. The migrator does NOT reject a host merely because they belong to another active room.

**Idempotent retry with a different `--host-user-id` MUST fail on the room/slug conflict**, not silently change the room's host.

Email may be documented as an operator-side discovery aid (used manually to look up the numeric ID) but is NOT the migration command's canonical identity input.

### 5.3 Migration CLI flags (R14b)

- `--room-slug=<slug>` (required): the slug of the migrated room.
- `--room-name=<name>` (required): the displayed room name. The existing room model permits the name to differ from the slug; R14a recommends allowing them to differ.
- `--host-user-id=<positive integer>` (required): the canonical `users.id`.
- `--dry-run` (optional; default false): same body as `up` but no writes commit; produces the same report as `plan`.

The CLI MUST reject unknown positional arguments and unknown flags. The CLI MUST NOT accept `--force` / `--reset` / `--allow-hash-drift`. The CLI MUST NOT expose a subcommand to release another process's advisory lock (PostgreSQL session locks belong to the connection that acquired them; see §8).

### 5.4 Migration report — PII posture

The migration report MAY contain:

- the operator-supplied numeric `users.id` for the host;
- numeric row counts, sha256 hashes, sequence names, table names, and durations.

The migration report MUST NOT contain:

- email addresses;
- OAuth tokens / ID token claims;
- any `display_name` / `user` text from activities rows;
- session tokens;
- any credential, cookie, or `Authorization` header value.

DSN redaction uses `config.RedactDSN` (existing R03 helper).

### 5.5 Account-table posture — no recopy

`users`, `user_sessions`, and `priority_transactions` are **account-scoped per ADR 001 §3 Decision 9** and are NOT recopied at cutover. The migrator explicitly does NOT touch these tables. The legacy `users` table is the same table the room runtime reads; there is no separate "migrated user" identity surface. There is no remap, no re-issuance, no password rotation, and no identity rewriting. Idempotency is read-only against `users` (the host lookup only); writes go to `rooms` and `room_members` (new rows). The `users.legacy_id` column introduced in R03 is preserved untouched.

## 6. Source-to-target mapping

| Source (global, R00–R05) | Target (room, R14b builds) | Conversion notes |
| --- | --- | --- |
| `queue_state` (TEXT JSON, singleton `id = 1`) | `room_queue_state` (JSONB, one row per `rooms.id`) | Real conversion contract: read legacy TEXT into a `[]byte`, decode through `encoding/json` into the tagged `entity.Queue` struct, validate all invariants (`Songs`, `CurrentIndex`, `Status`, `Elapsed`, history fields, first-song/current-song behaviour), re-encode through `encoding/json` to canonical JSON bytes, compute SHA256 over the canonical bytes for the marker record, write to `room_queue_state` with PK = `rooms.id`. No `MarshalJSON` / `UnmarshalJSON` custom methods on `entity.Queue` are assumed; the contract is the default `encoding/json` tagged-field round-trip. |
| `activities` | new `room_activities` (R14b lands `0009`; schema v9) | Lossless copy of every row with preserved `id`; `room_id` set to the migrated room's id; sequence resync. |
| `auto_queue_config` (singleton `id = 1`) | `room_auto_queue_config` (already exists, migration 0007) | Keyed by `room_id`; write the migrated room's row with the legacy singleton's values. |
| `play_history` (legacy 50-row cap, Go-enforced) | `room_play_history` (already exists, migration 0007) | See §6.1 for the id-collision handling. Lossless copy of every row with preserved `played_at`, `video_id`, `title`; per-room 50-row cap preserved (Go-enforced, matches `PlayHistoryCap`). |

After the copy, source rows are retained as migration evidence. They are read-only for the rollback window and dropped in R14e via `0010_drop_legacy_global_tables.up.sql`. After cutover, the room targets are the sole writable source of truth.

`users`, `user_sessions`, and `priority_transactions` remain account-scoped (per ADR 001 §3 Decision 9) and are NOT migrated into a room.

### 6.1 `room_play_history` id-collision handling

The legacy `play_history` table uses a global BIGSERIAL. The per-room `room_play_history` table also uses a global BIGSERIAL (it is one table shared across all rooms, identified by `room_id`). Copying the legacy rows with preserved `id` values into `room_play_history` can collide with rows that any room's auto-queue already appended since the R09f migration. The cutover migrator MUST handle this by **partitioning the id space**:

- Compute `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)` at the start of the migration.
- Insert each legacy row with `id + :legacy_id_offset` (e.g., `INSERT INTO room_play_history (id, room_id, played_at, video_id, title) SELECT id + :legacy_id_offset, :new_room_id, played_at, video_id, title FROM play_history`).
- Record the offset in the `room_cutover_marker` record so a re-run / rollback window can refer to the same shift.
- After the insert, `setval(room_play_history_id_seq, MAX(id), is_called=true)` resyncs the sequence so the next `BIGSERIAL` lands at `MAX(id) + 1`.
- The shifted ids are an implementation detail of the cutover; the per-room cap (50 rows) is enforced in `roomautoqueue` regardless of absolute id values.
- Re-run / idempotency: the `room_cutover_marker` carries SHA256 hashes of both the source (`play_history`) and the target post-copy (`room_play_history` filtered by `room_id = :new_room_id`) so a repeated `cmd/room-cutover up` is a no-op when the hashes match.

### 6.2 Schema-version plan

The migration runner reports the applied numbered migration as the schema version; adding a migration necessarily bumps the version. **No sprint can both add a migration and keep the version unchanged.**

- **Schema at start of R14a: v8** (last applied: `0008_room_chat_messages.up.sql`; current schema-version report = 8).
- **R14b lands `0009_room_activities.up.sql`** — a NEW additive migration that creates the `room_activities` table + index. Schema becomes **v9**. The schema version reported at the end of the R14b verification gate MUST be 9.
- **R14c is a runtime + binary-flag cutover; it does NOT add a migration.** Schema during R14c and during the rollback window: v9.
- **R14e lands `0010_drop_legacy_global_tables.up.sql`** which drops the legacy `queue_state`, `activities`, `auto_queue_config`, and `play_history` tables. Schema becomes **v10**. Only if R14e accepts destructive cleanup.

**Historical migration files are never rewritten.** `0001_initial.up.sql` .. `0008_room_chat_messages.up.sql` are preserved on disk byte-for-byte. R14e does NOT modify `0001_initial.up.sql` to remove rows — it lands `0010_drop_legacy_global_tables.up.sql` as a SEPARATE forward migration. The historical files remain auditable; the v10 state is the cumulative result of running `0001`..`0010` in order. `room_play_history` is the per-room table introduced by R09f (migration 0007); the R14e migration drops the **legacy global** `play_history` table only — `room_play_history` is NOT a legacy table and is NEVER touched by the `0010` migration.

## 7. Phase A–D retirement sequence

The four phases are implemented as separate sprints so each can be reviewed independently. Per the Architect reviews, the cutover is split between R14b (builds + verifies the mechanism) and R14c (owns the coordinated production execution with a binary that has `410 Gone` tombstone handlers, NOT unregistered `404` handlers).

### Phase A — offline conversion (built + verified by R14b)

R14b is a backend-only sprint that ships **the mechanism** as a new dedicated CLI and verifies it against a representative PostgreSQL snapshot. R14b lands the new `0009_room_activities.up.sql` migration (schema v8 → v9), the CLI, the marker contract, the source-to-target copy, the dry-run, the verification, and the rollback hook. R14b does NOT retire the global runtime in production. R14b's verification gate MUST pass before R14d (frontend cutover) can run.

- R14b ships `cmd/room-cutover` with subcommands:
  - `plan` (read-only snapshot + dry-run)
  - `up` (apply — see §8 for the marker-in-same-transaction contract)
  - `verify` (re-assert hash record against the marker)
  - **No `abort` subcommand.** PostgreSQL session locks belong to the connection that acquired them; another process cannot release them. The only release path is connection close (process exit / `SIGTERM` / Ctrl-C). If the operator wants to abandon a held cutover mid-flight, they stop the `cmd/room-cutover up` process; the connection closes, the lock releases, the in-flight transaction rolls back. The CLI MAY print a textual "press Ctrl-C to abandon" hint before the copy phase begins.
- R14b ships the marker table `room_cutover_marker` (single-row, `id = 1`) carrying the fields enumerated in §8.
- R14b ships the binary build flag `--room-cutover-authoritative` (default `false`; in R14b this is `false` in every deployment). When `false`, the legacy global routes continue to serve from the legacy repositories **unchanged**; the cutover migrator reads from the legacy tables and writes to the room tables, but the server still serves the global routes for concurrent testing. This is the verification state.
- R14b verification gate (must pass before R14d can run):
  - `cmd/room-cutover plan` against a fresh PostgreSQL snapshot returns a report with no PII.
  - `cmd/room-cutover up --dry-run` against the same snapshot completes with no writes and a successful SHA256 record.
  - `cmd/room-cutover up` (real run) against the same snapshot creates the room, inserts the host membership, copies the source rows into the target rows, AND inserts the marker **all in one transaction**; then commits and returns 0. (See §8 for the in-transaction marker contract.)
  - Re-running `cmd/room-cutover up` immediately after returns `already cut over; no-op` (exit 0), driven by the marker.
  - Hash drift on either source or target is detected and rejected (no `--force` / `--reset`).
  - PII redaction verified.
  - The marker preserves the R03 `migration_marker` row untouched.
  - Schema version after the R14b migration is **9** (verified by the runner's reported version).
  - `go test ./...` clean.

### Phase B — coordinated production cutover (executed by R14c)

R14c owns the **coordinated production execution**: a planned maintenance window, a redeploy with the **binary build flag `--room-cutover-authoritative=true`**, and the run of `cmd/room-cutover up` against the live PostgreSQL. R14c is the ONLY sprint where the production binary is changed. Sprint execution order is **R05b → R09h → R14b → R14d → R14c → R14e**; R14c cannot start until R05b, R09h, R09i, R14b, and R14d are all accepted (with R09i waivable only by explicit Product Owner sign-off recorded on Issue #20).

- Before the window: a `pg_dump` is taken and retained ≥30 days (mirrors ADR 002 §9). R14b's binary is already deployed and R14b's verification gate has passed. The post-R14d frontend bundle has been deployed and is in production. The blocking prerequisites (R05b, R09h, R09i) are all accepted.
- During the window:
  - The application is shut down (or restarted with a maintenance-mode flag that disables ALL mutating endpoints — global AND room).
  - `cmd/room-cutover up` runs against the live database. It pins a connection, takes the advisory lock, opens a single transaction, creates the room, inserts the host membership, copies the source rows into the target rows, **inserts the marker in the same transaction**, and commits. See §8.
  - The application restarts with `--room-cutover-authoritative=true`. In this build:
    - The legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes and the legacy `/ws` endpoint are **registered with repository-free tombstone handlers** that return `410 Gone` with the documented envelope (see §9). They are NOT unregistered, and they are NOT `404`. The room routes are the only data path; the tombstone handlers carry no repository wiring and no WS hub mutation logic. The global `/ws` upgrade is rejected during the HTTP phase (the upgrade handler returns the HTTP `410` before the WebSocket handshake completes).
    - **R14c is atomic across ALL listed legacy routes.** The cutover binary retires the listed routes in one step. A partial cutover where some legacy routes (notably `/api/vote/prioritize`) remain live is FORBIDDEN — it would let those legacy mutations continue writing legacy global queue state after room state is declared authoritative. Because R09h is a blocking prerequisite for the entire R14c cutover, this failure mode is not reachable in a normal sprint sequence.
    - The legacy repositories remain in the binary for read-only rollback support but are NOT wired into HTTP handlers or the WS hub.
    - On the cutover boundary, the application restart is the only window where the listed legacy routes transition from serving real responses to `410 Gone` — this is the documented maintenance window.
  - Legacy global repositories write paths are also disabled by `--room-cutover-authoritative=true` to prevent any tool / script / cron that bypassed HTTP from mutating the legacy tables during the rollback window. The `--safe-write` mode is OFF in this build for legacy tables.
- Frontend deployment order is captured in §11; the cutover binary is paired with the post-R05b + post-R14d frontend at production cutover time.

### Phase C — frontend cutover (R14d) — runs BEFORE R14c

R14d executes BEFORE R14c per the fixed order. R14d deploys a frontend bundle that pairs with the still-`authoritative=false` server. The legacy global routes are still live during R14d deployment; the post-R14d frontend simply does not call them.

- `RoomView` becomes the only supported view; `DashboardView` is removed or redirects to the room entry surface from R05b.
- `globalStore.queueState`, `globalStore.voteSessions`, and `globalStore.autoQueueConfig` are cleared.
- `globalStore.currentUser` is preserved (RoomView still reads it).
- `globalStore.roomQueues[slug]` per-slug slices are preserved (RoomView still reads them).
- The global `WebSocketClient` is closed and not reconnected.
- **Frontend entry UX is a prerequisite**, not part of R14d: the unbuilt SPA client methods (`api.createRoom`, `api.listRooms`, `api.getRoom`, `api.joinRoom`, the invite endpoints, `api.claimPlayerLease`, `api.heartbeatPlayerLease`, `api.releasePlayerLease`, `api.getPlayerLease`) MUST exist before R14c can run. This is the **R05b — Room entry / player-lease UI** prerequisite sprint (see §10).

### Phase D — schema cleanup (R14e)

- Land `0010_drop_legacy_global_tables.up.sql` as a SEPARATE forward migration (do NOT modify `0001_initial.up.sql`).
- The forward migration drops the **legacy global** tables: `queue_state`, `activities`, `auto_queue_config`, `play_history`. **It does NOT touch `room_play_history`** — `room_play_history` is the per-room table introduced by R09f, not a legacy table. **It does NOT touch `room_activities`** — the new table is the per-room target.
- Schema version bumps from 9 to 10 (only if R14e accepts and destructive cleanup completes).
- Phase D is destructive cleanup only and runs **after** a verified rollback window AND after no runtime path references the legacy tables.

## 8. Atomicity & idempotency (dedicated marker, in same transaction)

The future R14b room-cutover mechanism is built fresh, drawing on R03 patterns where appropriate, but **does NOT reuse R03's CLI, marker, or transactional scaffolding**, and the marker is inserted **in the same transaction** as the room, host membership, and copied state — not as a post-commit write:

- `pg_try_advisory_lock(987654321)` on a pinned connection (same key, same idiom as R03). The lock is held by this connection until process close / `SIGTERM` / `Ctrl-C`.
- **Single transaction** for the full `cmd/room-cutover up` body:
  1. `BEGIN`.
  2. `INSERT INTO rooms (slug, name, host_user_id, ...) VALUES (:slug, :name, :host_user_id, ...)` (or updates the room row).
  3. `INSERT INTO room_members (room_id, user_id, role) VALUES (:room_id, :host_user_id, 'host')` (the host membership).
  4. Source-to-target copy (legacy → room). All four sources inside the same transaction.
  5. **Marker INSERT**: `INSERT INTO room_cutover_marker (id, room_cutover_id, target_room_slug, target_room_id, host_user_id, source_hashes JSONB, target_hashes JSONB, legacy_id_offset, cutover_started_at, cutover_committed_at = cutover_started_at, binary_build_sha) VALUES (1, ...)`. The marker is inserted before COMMIT, never after.
  6. **Pre-commit verification** within the same tx: source row counts, target row counts, `queue_state.data` byte length parity, per-target SHA256 after the copy, marker record fields present.
  7. `COMMIT` (or `ROLLBACK` on any verification failure).
- **No `abort` subcommand.** Lock release is via connection close (process exit / `SIGTERM` / Ctrl-C). A held cutover is abandoned by stopping the `cmd/room-cutover up` process; the connection closes, the in-flight transaction rolls back, no marker row exists, no target data persists.
- **NEW** marker table `room_cutover_marker` (single-row, `id = 1`) — distinct from R03's `migration_marker`. The R03 marker is preserved untouched.
- Marker record fields: `room_cutover_id` (UUID), `target_room_slug`, `target_room_id`, `host_user_id`, per-source SHA256 hashes (`queue_state.data`, `activities.*`, `auto_queue_config.*`, `play_history.*`), per-target post-copy SHA256 hashes (`room_queue_state.data` for the migrated room, `room_activities.*` filtered by `room_id`, `room_auto_queue_config.*` filtered by `room_id`, `room_play_history.*` filtered by `room_id`), `legacy_id_offset` (the `MAX(room_play_history.id)` at start of migration), `cutover_started_at`, `cutover_committed_at`, `binary_build_sha`.
- Re-run on identical hash record = `already cut over; no-op`, exit 0. **Driven by the `room_cutover_marker` SHA256 record** in the same transaction, NOT by R03's `migration_marker`.
- Hash drift on any source or target = rejected with an explicit sentinel. No `--force` / `--reset`.
- Post-commit verification (after COMMIT): `cmd/room-cutover verify` subcommand re-reads the sources (now retained as migration evidence) and the targets, computes the SHA256s again, asserts they match the marker.
- `cmd/room-cutover plan` subcommand (read-only): snapshot source hashes, plan target writes, emit a JSON/text report WITHOUT committing.
- `--dry-run` mode on `cmd/room-cutover up`: same as `plan` but the connection is wired through the live DB; no writes commit.
- `config.RedactDSN` in the report.
- Sequence resync via `setval(seq, MAX(id), is_called=true)` for each migrated sequence (`room_activities_id_seq`, `room_play_history_id_seq` (post-shift)). R03's `setval` calls for `user_sessions` / `priority_transactions` / `activities` / `play_history` are unchanged and re-run only as part of a future R03 re-cutover (out of R14 scope).
- The room-cutover CLI is an offline CLI; it does NOT run inside the server process; it does NOT mutate either model concurrently with runtime writers (the application is shut down during Phase B).

## 9. WebSocket + frontend transition rules

- `/ws` is retired with `410 Gone` in the `--room-cutover-authoritative=true` build (R14c). The global WS upgrade is replaced by a 410-returning HTTP handler at the mux entry; the upgrade is refused during the HTTP phase (no WebSocket handshake succeeds). R14a explicitly does NOT use `404` for the retired `/ws`; `410 Gone` is the approved retirement posture.
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- The first per-room `room_queue_sync` after migration lands carries the migrated room's queue state under the migrated room's `seq_num` allocation (per-room sequence numbers restart from the per-room hub's own counter, which is independent of the global hub's counter; R07b behaviour).
- Frontend: preserve `globalStore.currentUser`; clear `globalStore.queueState`, `globalStore.voteSessions`, `globalStore.autoQueueConfig`; close global `WebSocketClient`; per-slug `roomQueues[slug]` slices kept.

## 10. Future sprint sequence

Sprint execution order is fixed: **`R05b → R09h → R14b → R14d → R14c → R14e`** (with R14e only after the verified rollback window). **R09i** (room-activity runtime parity) is a blocking prerequisite for R14c unless the Product Owner explicitly waives it. Reordering is NOT permitted within this sprint's scope.

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R05b — Room entry / player-lease UI** | Frontend only (no new HTTP routes; uses the existing R04 / R06 / R11a routes). Adds SPA client methods `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / invite + `api.claimPlayerLease` / `api.heartbeatPlayerLease` / `api.releasePlayerLease` / `api.getPlayerLease`; exposes them as actual room create / list / join / invite-redeem / lease-claim UI. **BLOCKING PREREQUISITE for R14c.** | The existing backend routes are already registered (per `cmd/server/main.go:413–486`); R05b only wires the SPA. |
| **R09h — Room vote-to-prioritize parity** | Backend-only. Adds `POST /api/rooms/{slug}/vote/prioritize`. **BLOCKING PREREQUISITE for the entire R14c cutover** — R14c cannot run while `/api/vote/prioritize` is still live. | Reuses existing `room_queue_song_prioritized` event. No new WS event constant. |
| **R09i — Room activity runtime parity** *(new)* | Backend-only. Wires existing room mutations (queue / playback / vote / auto-queue) to append rows to the new `room_activities` table via `RoomActivityRepository.AddActivity`. Replaces the global dashboard's silent activity append. After R09i, the room runtime has a live write path; the per-room mutators call it. **BLOCKING PREREQUISITE for R14c** unless the Product Owner explicitly waives it (recorded on Issue #20). Live read parity (a `RoomView` activity panel) is NOT in R09i's scope; it is a separate optional frontend task. | The `room_activities` table itself is built in R14b (migration `0009`). R09i wires the call sites and the read surface. |
| **R14b — room-cutover mechanism** | Backend-only. New dedicated CLI `cmd/room-cutover` with `plan` / `up` / `verify` subcommands (NO `abort`). New marker table `room_cutover_marker`. Source-to-target copy (legacy → room). NEW migration `0009_room_activities.up.sql` (schema v8 → v9). Build flag `--room-cutover-authoritative=false` (default). The marker is inserted **in the same transaction** as the room, host membership, and copied state. **NO global route retirement. NO frontend changes.** R14b's gate MUST pass before R14d can run. | Does NOT reuse R03's CLI or `migration_marker`. R03's marker is preserved untouched. |
| **R14d — frontend global-path removal** | Frontend-only. Removes / redirects `DashboardView`; clears `globalStore.queueState` / `voteSessions` / `autoQueueConfig`; closes global `WebSocketClient`; preserves `currentUser` + `roomQueues[slug]`. Deploys the post-R14d SPA bundle alongside the (still `authoritative=false`) server. **R14d ships BEFORE R14c** so the post-R14c binary has a compatible frontend bundle. | Assumes R05b + R09h + R14b have landed. R09i is not required for R14d (R09i writes to `room_activities`, which R14d does not read). |
| **R14c — coordinated production cutover** | Backend production cutover (Phase B). Maintenance window + restart with `--room-cutover-authoritative=true` binary + run `cmd/room-cutover up` against the live DB. ALL listed legacy global routes (including `/api/vote/prioritize`, because R09h has landed) retire simultaneously with `410 Gone` tombstone handlers. The global `/ws` upgrade is rejected with `410` in the HTTP phase. Pairs with the post-R14d + R05b frontend. **ATOMIC across the listed legacy routes** — no partial cutover. | Requires R05b + R09h + R14b + R14d all accepted. Blocks if R09i is not accepted (or waived). |
| **R14e — schema cleanup** | Backend-only. Lands `0010_drop_legacy_global_tables.up.sql` (a SEPARATE forward migration that drops the legacy `queue_state`, `activities`, `auto_queue_config`, `play_history` tables). **Does NOT touch `room_play_history` or `room_activities`.** Bumps schema v9 → v10. Historical migration files `0001`..`0009` remain on disk byte-for-byte; `0001_initial.up.sql` is NEVER rewritten. | Runs after the verified rollback window AND after no runtime path references the legacy tables. |

Sprint names may be renamed if evidence requires it, but R05b / R09h / R09i / R14b / R14c / R14d / R14e MUST NOT be combined; migration, destructive cleanup, frontend rewrite, frontend entry UX, room-activity runtime, and vote-prioritize parity must remain independently reviewable.

## 11. Deployment compatibility matrix

The cutover involves two build flags (`--room-cutover-authoritative` per-server, frontend bundle version per-client — pre-R05b / post-R05b / post-R14d) and three server cutover states (pre-R14b / post-R14b / post-R14c). Valid combinations only:

| Server binary | Frontend SPA | Legacy global routes | Per-room routes | Result |
| --- | --- | --- | --- | --- |
| Pre-R14b binary (default `--room-cutover-authoritative=false`) | pre-R05b (no entry UI) | Registered; serve legacy repos | Registered | Pre-cutover baseline. |
| Post-R14b binary (`--room-cutover-authoritative=false`) | pre-R05b OR post-R05b | Registered; serve legacy repos | Registered | The R14b verification state. Users can use both surfaces. |
| Post-R14c binary (`--room-cutover-authoritative=true`) | post-R14c + post-R05b + post-R14d | **Tombstone handlers; `410 Gone` with the documented envelope + `Link: </api/rooms>; rel="successor-version"`** (ALL listed legacy routes atomic; `/ws` rejected in HTTP phase) | Registered; sole authoritative path | Production cutover state. The ONLY valid post-R14c combination. |
| Post-R14c binary | post-R05b only (DashboardView present) | `410 Gone` tombstone | Registered | **BROKEN** — frontend shows DashboardView, but every legacy route returns `410`. Compatibility matrix forbids. |
| Post-R14c binary | pre-R05b | `410 Gone` tombstone | Registered | **BROKEN** — frontend cannot enter rooms. Compatibility matrix forbids. |
| Mid-window rollback: **immediately pre-cutover production release** with `--room-cutover-authoritative=false` | post-R14c + post-R05b + post-R14d | Registered; serves legacy repos | Registered | Mid-window rollback state. The `pg_dump` from the start of the window is the source of truth. Forward recovery against the `pg_dump` is required for any data that mutated after the cutover. **The rollback target is the immediately pre-cutover production release** (the same server binary deployed at window open), **NOT an "R03 binary"** (the R03 release is the unrelated SQLite-to-PostgreSQL migration). |
| Pre-R14c binary | pre-R05b / post-R05b | Registered | Registered | Pre-cutover state. Rollback target when only the room-cutover migration has been run. |

**Client-continuity claim:** the only combinations that guarantee client continuity are the documented ones. Specifically:
- The `post-R05b + post-R14d` frontend paired with the `--room-cutover-authoritative=false` (post-R14b / pre-R14c) server is safe to deploy before the cutover window — both global and room surfaces are live.
- The `post-R14c + post-R14d` frontend paired with the `--room-cutover-authoritative=true` (post-R14c) server is the production cutover state — legacy surfaces return `410` and clients see the documented discovery surface.
- A frontend relying on legacy global routes paired with a post-R14c server receives `410` from legacy surfaces. Compatibility matrix forbids this combination.

This is the client-continuity guarantee.

## 12. PO acceptance blockers

These are explicit blockers that the Product Owner must sign off on before R14c can run (and before R14e can land):

1. **Room-activity runtime parity (R09i).** R09i is a separate backend-only sprint. **It is a BLOCKING PREREQUISITE for R14c** unless the Product Owner explicitly waives it (recorded on Issue #20). The R09i gate prevents the global write path (which feeds `activities`) from running concurrently with the room runtime (which would feed `room_activities`) — the cutover would otherwise lose activity write continuity for the room.
2. **Live room-activity read parity.** Deferred as a separate frontend task; not in R09i or R14a scope. If the Product Owner wants read parity in the room frontend before accepting R14c, it must be added as a separate task and its scope settled in a focused sprint before R14d.
3. **R09h landing prerequisite for the entire R14c cutover.** R14c cannot run at all until R09h is accepted. Partial cutover where `/api/vote/prioritize` is live would let that mutation continue writing legacy global queue state after room state is declared authoritative.
4. **R05b landing prerequisite for user-facing cutover.** Until R05b is accepted, no user can create / list / join rooms from the SPA. R14c is blocked.
5. **Route-retirement response posture.** After R14c, the listed legacy routes return `410 Gone` via repository-free tombstone handlers. The envelope is `{error, code: "global_contract_retired", documentation}` plus the HTTP `Link: </api/rooms>; rel="successor-version"` header. Clients discover the migrated room via `GET /api/rooms`. The global `/ws` upgrade is rejected with `410` in the HTTP phase.
6. **Rollback posture.** Rollback after `--room-cutover-authoritative=true` is NOT a transparent rollback — it requires restarting the **immediately pre-cutover production release** with `--room-cutover-authoritative=false`. The room tables in the target release are populated by the prior `cmd/room-cutover up` run; mutations since that point would need forward recovery against the `pg_dump`. **The rollback target is NOT an "R03 binary"** (R03 is the unrelated SQLite-to-PostgreSQL migration release).
7. **Slug-collision resolution.** Operator-driven only — no automatic rename or proxy.
8. **Schema-version plan approval.** The Product Owner must approve the schema-version plan (§6.2): v8 → v9 in R14b; v9 → v10 in R14e; historical migrations preserved on disk; `0001_initial.up.sql` is NEVER rewritten.

## 13. Items already settled (informational)

- ID-collision handling for `room_play_history`: `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)` is the agreed simple rule.
- Account-table posture: no recopy (§5.5).
- Schema version plan (§6.2): v8 → v9 in R14b; v9 → v10 in R14e. Historical migrations preserved on disk.
- The R03 `cmd/migrate-data` + `migration_marker` is preserved untouched; it is the SQLite-to-PostgreSQL migrator. R14b adds a SEPARATE `cmd/room-cutover` + `room_cutover_marker`.
- The `entity.Queue` JSON conversion contract uses the default `encoding/json` tagged-field round-trip; no custom `MarshalJSON` / `UnmarshalJSON` methods are assumed.
- The queue-prioritize / vote-prioritize distinction: global `/api/queue/prioritize` has a room equivalent (R07d) and retires in R14c without a parity sprint; only `/api/vote/prioritize` needs R09h.
- Marker is inserted in the same transaction as the room, host membership, and copied state. NO post-commit marker write.
- `cmd/room-cutover` has NO `abort` subcommand. PostgreSQL session locks belong to the connection that acquired them; lock release happens through connection close.
- Retirement posture: `410 Gone` via repository-free tombstone handlers. The global `/ws` upgrade is rejected with `410` in the HTTP phase.
- Sprint execution order is fixed: `R05b → R09h → R14b → R14d → R14c → R14e`.
- R09i (room-activity runtime parity) is a blocking prerequisite for R14c unless the Product Owner explicitly waives it.
- R14e drops the legacy global `play_history` table only; `room_play_history` (per-room, R09f) is NOT touched.
- Per-room sequence numbers restart from the per-room hub's own counter.
- The cutover CLI is offline; the application is shut down during Phase B.
- A hidden default-room `/ws` is forbidden.
- The cutover binary retires ALL listed legacy global routes in one atomic step — no partial cutover.

## 14. Verification

- `git diff --check` clean.
- `git diff --name-only` contains only documents under `documents/00-project-management/`.
- ADR 001 body of §3–§18 is unchanged byte-for-byte except for the one metadata supersession note near the header.
- ADR 003 is referenced from `022-…md` and `ROOM_EPIC_SPRINT_SEQUENCE.md`.
- ADR 003 status remains `Proposed` (NOT `Accepted`) — pending Architect review and Product Owner acceptance.
- No `main`, room `0`, "default room", "implicit room", or synthesized default-room `/ws` language appears in the R14a documents (every reference is explicit prohibition).
- No usage of `cmd/migrate-data` for the room cutover anywhere in the R14a documents.
- The R03 `migration_marker` is referenced only as "preserved untouched"; never as the room-cutover's marker.
- No concrete `successor: "/api/rooms/{slug}/..."` value in the `410` envelope.
- No `MarshalJSON` / `UnmarshalJSON` claim on `entity.Queue`.
- No partial-cutover language; R09h blocks R14c entirely.
- No `abort` subcommand on `cmd/room-cutover`.
- The marker is in the same transaction as the room + host + copied state.
- Historical migrations remain on disk; `0001_initial.up.sql` is NEVER rewritten.
- Schema version plan is v8 → v9 (R14b) → v10 (R14e).
