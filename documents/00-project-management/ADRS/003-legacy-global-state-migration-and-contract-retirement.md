# ADR 003 — Legacy global-state migration and contract retirement

- Status: Proposed — R14a contract; supersedes parts of ADR 001 and ADR 002 §11/§13; pending Architect review and Product Owner acceptance
- Date: 2026-07-15 (revised after Architect review)
- Scope: Reconciles several paragraphs of ADR 001 (and one paragraph cluster of ADR 002) that have become inaccurate as the room epic landed in the renumbered R06–R11 sequence. Records the new R14a implementation-ready contract for converting the legacy global state into exactly one Product-Owner-named room and for retiring the parallel global REST/WS contracts. ADR 003 does **not** replace ADR 001's room architecture in general; it supersedes only the specific paragraphs listed below. ADR 003 is documentation-only: no runtime change is authorized by it.

> **R14a review-revision summary (2026-07-15).** This ADR was revised after the Architect's review of the first R14a draft. The corrections visible in the body: (1) the cutover sequence is split between R14b (builds + verifies the offline mechanism) and R14c (owns the coordinated production maintenance-window execution with a binary that has *disabled* global write paths); (2) the room-cutover mechanism is a **dedicated** CLI (`cmd/room-cutover`) and **dedicated** marker contract (`room_cutover_marker`), distinct from R03's `cmd/migrate-data` + `migration_marker`, which are preserved untouched; (3) `users` / `user_sessions` / `priority_transactions` are explicitly **not** recopied at cutover — they remain account-scoped; (4) a room entry / player-lease frontend prerequisite (no `createRoom` / `getRoom` / `listRooms` / `joinRoom` / invite / `player/claim` SPA methods today) is added as a blocking prerequisite for R14c; (5) the `room_play_history` id-collision handling is settled; (6) live room-activity parity is explicitly surfaced as a Product-Owner acceptance blocker (the table is designed but read parity is NOT in R14a scope); (7) the schema-version plan is corrected (R14a does NOT bump the schema; historical migrations are preserved on disk); (8) the `410 Gone` envelope drops the concrete migrated-room `successor` value and points at the standard room-discovery surface instead; (9) the JSON conversion contract is rewritten to the real semantic shape (`encoding/json` tagged fields, no `MarshalJSON` / `UnmarshalJSON` claim); (10) the queue-prioritize vs vote-prioritize routes are separated (the global `/api/queue/prioritize` has a room equivalent since R07d; only `/api/vote/prioritize` needs R09h); (11) contradictory active-sprint statements are removed by the `active.md` revision; (12) the open-items list is split into PO acceptance blockers vs informational vs already-settled; (13) impossible rollback / client-continuity claims are replaced by a deployment compatibility matrix.

## 1. Context

The verified `dev` baseline after accepted R11a has two parallel product models:

- PostgreSQL is the only runtime database (ADR 002 — R01/R02/R03).
- Legacy global persistence still exists: singleton `queue_state` (`id = 1`, TEXT JSON), append-only `activities`, singleton `auto_queue_config` (`id = 1`), and `play_history` (50-row cap, enforced in Go).
- Room persistence also exists: `rooms` / `room_members` / `room_invites` (R04, migration 0004); `room_queue_state` (R07a, JSONB per `rooms.id`, migration 0006); `room_auto_queue_config` + `room_play_history` (R09f, migration 0007); `player_leases` (R06, migration 0005); `room_chat_messages` (R11a, migration 0008).
- Global and room-scoped runtime contracts coexist: global `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...`, `/ws`; room-scoped `/api/rooms/{slug}/...` and `/ws/rooms/{slug}`.
- The frontend is substantially room-aware (`DashboardView.vue` is global-only; `RoomView.vue` reuses `globalStore.currentUser` and `globalStore.roomQueues[slug]`).
- Room **entry** UX is **incomplete**: no `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / invite / player-lease SPA methods exist today even though the backend routes are registered (per `cmd/server/main.go:413–486` and the inventory in `022-…md` §3). Rooms are entered only by direct URL; this is a prerequisite gap that blocks global-route retirement and is added to R14a's future-sprint sequence as a blocking prerequisite.
- No migration has yet converted the global state into a PO-named room as the sole source of truth.
- No permanent `main` room, `room 0`, or hidden default room is allowed by ADR 001 §3 Decision 2 and Epic #17.
- **Activity parity is NOT implemented.** The `room_activities` table is designed here but the *frontend read parity* (a `RoomView` activity panel) is **NOT** in this sprint's scope; see §5.1 and §12 for how this surfaces.

Several ADR 001 paragraphs — and one ADR 002 paragraph cluster (§11/§13) — were written before the room epic was renumbered and before the shim plan was deferred indefinitely. They are now historically inaccurate and must be reconciled before the R14 migration sprint can ship safely.

## 2. Supersession statement

ADR 003 supersedes **only** the following paragraphs of ADR 001 and one paragraph cluster of ADR 002. ADR 003 does NOT replace ADR 001's room architecture in general. ADR 001 remains authoritative for the decisions that ADR 003 does not replace, including: explicit rooms and no permanent default room; room lifecycle (create → active → archived); membership roles (host / admin / guest); one host per active room; player-lease separation; invite-token principles; per-room REST/WebSocket direction; single-instance limitations; account-scoped priority unless separately changed.

Superseded ADR 001 paragraphs:

- §3 Decision 6 — the compatibility shim was never built; old global routes still operate on legacy global repositories.
- §9 transition-strategy table — the R07 row's "compatibility shim that resolves the single migrated room by default … logged at startup" was never implemented; no migrated room exists today.
- §9 final route shape list — uses `{roomId}` (the code uses `{slug}`) and lists room routes that were never built in R07/R08 (`.../queue/skip|status|sync|ended|prev|volume`, `.../user/priority-balance`, `.../youtube/search`); the actual playback route family is `/api/rooms/{slug}/playback/{status,sync,skip,ended,prev,volume}`. The line `.../vote/prioritize` is **NOT** a missing build — it is a different feature (vote-driven queue mutation) and was deliberately deferred; the room equivalent lands in R09h.
- §10 global `/ws` backward-compatibility paragraph — "`/ws` … serves a synthesized 'default room' view backed by the migrated room." This paragraph was never implemented. `/ws` serves the real global `queue_state` JSON today, with no default room; R14c will retire `/ws` with `410 Gone` rather than add a default-room fallback.
- §11 migration direction paragraph — "global `queue_state` row becomes the migrated room's … R06 converts … global rows are deleted at the end of R06." R06 became Player Lease; the global rows were NOT deleted and remain the live source of truth. The conversion is exactly what R14a now shapes, executed by the R14b dedicated `cmd/room-cutover` mechanism (NOT R06, NOT a startup magic transform, NOT a reuse of R03's `cmd/migrate-data` + `migration_marker`).
- §11 table row — "Room-scoped activities … existing global `activities` table is migrated." There is currently no `room_activities` table; the migration target is a NEW table designed by R14a (see §5 below). Live read parity is not in this sprint's scope — see §5.1 and §12.
- §15 compatibility table — the R07/R08 shim + "`/ws` deprecated but live / default room" rows are inaccurate for the same reasons.
- §18 consequences mapping — the sprint → scope map (R06/R07/R08/R10/R11/R12) does NOT match the renumbered epic (R06 = Player Lease; R07 = Room queue; R09 = playback/vote/auto-queue; R10 = deletion; R11 = chat; R12 = search).

Superseded ADR 002 paragraph cluster:

- §11 (`setval` / R06 fold attribution) and §13 (R03 migrator reuse) repeatedly attribute the global → room fold to "R06". This attribution was never implemented; R06 became Player Lease. The migrator-reuse shape ADR 002 anticipated (via a `room_id` parameter) was historically associated with the R03 migrator; **R14a explicitly clarifies that the room-cutover mechanism is a NEW dedicated CLI (`cmd/room-cutover`) with a NEW dedicated marker contract (`room_cutover_marker`), distinct from R03's `cmd/migrate-data` + `migration_marker`**. R03's mechanism and marker are preserved untouched and continue to serve their existing role.

## 3. Per-section supersession detail

### 3.1 ADR 001 §3 Decision 6 — compatibility shim

Old: "Room context is mandatory on every queue, playback, voting, auto-queue, invite, member, and priority REST call after the migration sprint, and on the WebSocket connection. The old global routes transition through a documented compatibility shim to a final `410 Gone`."

Verified current behaviour: no shim exists. `cmd/server/main.go` still registers the legacy global routes (lines 389–409) operating on the legacy `QueueRepository` / `AutoQueueRepository`. Old routes return 200/4xx from legacy repositories; they do NOT resolve a migrated room.

New R14a decision: the retirement path is **explicit `410 Gone`** (Phase B, R14c), not a shim. A proxy may only be selected if identity, membership, and room-selection behaviour are unambiguous; R14a does not have that evidence, so the recommended path is `410 Gone` with a stable machine-readable error and a room-discovery pointer. See §7 below.

### 3.2 ADR 001 §9 transition-strategy table — R07 row

Old: "R07 … compatibility shim that resolves the single migrated room by default … logged at startup."

Verified current behaviour: no migrated room exists today; the shim was never built.

New R14a decision: removed. The R07 room-queue slice landed as `POST/GET /api/rooms/{slug}/queue/...` (R07a) + the per-room WebSocket delta family (R07b) + frontend narrow wiring (R07c) + the `queue/prioritize` queue mutation (R07d). No shim. The migration sprint sequence is R14a → R14b (mechanism) → R14c (coordinated production cutover with disabled global writes) → R14d (frontend removal) → R14e (schema cleanup), plus the blocking prerequisites R05b (room entry / lease UI) before R14c and R09h (vote-to-prioritize parity) before R14c can retire `/api/vote/prioritize`.

### 3.3 ADR 001 §9 final route shape list

Old: uses `{roomId}` as the path parameter; lists `.../queue/skip`, `.../queue/status`, `.../queue/sync`, `.../queue/ended`, `.../queue/prev`, `.../queue/volume`, `.../user/priority-balance`, `.../youtube/search`, `.../vote/prioritize` under the room scope.

Verified current behaviour: path parameter is `{slug}` (not `{roomId}`); the queue mutations live under `.../queue/{add,remove,clear,prioritize}`; the playback mutations live under `.../playback/{status,sync,skip,ended,prev,volume}`; `/api/rooms/{slug}/user/priority-balance` was never built (priority remains account-scoped per ADR 001 §3 Decision 9); `/api/rooms/{slug}/youtube/search` was never built (search remains global utility); `/api/rooms/{slug}/vote/prioritize` is the room vote parity sprint R09h, **distinct** from `/api/rooms/{slug}/queue/prioritize` (the R07d **queue** mutation that the global `/api/queue/prioritize` route replaces — already exists).

New R14a decision: ADR 001 §9's route list is replaced by the actual room route inventory in `022-…md` §3. R14a records the four `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...`, and global `/ws` routes as the explicit retirement set. **Two distinct route families are retired with different prerequisites**:

- **Queue mutations** — global `/api/queue/{add,skip,status,sync,ended,prev,remove,clear,volume,prioritize}` already have room equivalents (`/api/rooms/{slug}/queue/{add,remove,clear,prioritize}` + `/api/rooms/{slug}/playback/{status,sync,skip,ended,prev,volume}`); they retire in R14c's Phase B with no extra parity sprint.
- **Vote-mutations** — global `/api/vote/skip` has its room equivalent (R09b's `POST /api/rooms/{slug}/vote/skip`); it retires in R14c's Phase B with no extra parity sprint.
- **Vote-prioritize** — global `/api/vote/prioritize` does NOT have a room equivalent today; it is **BLOCKED** on R09h landing and acceptance before R14c may retire it. (This is the queue-prioritize / vote-prioritize distinction that the first R14a draft conflated — see review-revision note above.)

### 3.4 ADR 001 §10 `/ws` default-room synthesis

Old: "`/ws` … serves a synthesized 'default room' view backed by the migrated room."

Verified current behaviour: `/ws` is built by `ws.NewHub(qInteractor.GetState)` at `cmd/server/main.go:232` and registers via `mux.HandleFunc("/ws", hub.RegisterHandler)` at `main.go:588`. The hub reads `qInteractor.GetState`, which serializes the legacy global `*entity.Queue` from `queue_state` (R00–R05 baseline). There is no default room. The 16-event global inventory is unchanged from R00; per-room events live on `/ws/rooms/{slug}` only.

New R14a decision: R14a explicitly rejects the synthesized default-room `/ws` plan. R14c returns `410 Gone` on `/ws`; no synthesized fallback is permitted. See §7 below.

### 3.5 ADR 001 §11 migration direction — R06 fold

Old: "global `queue_state` row becomes the migrated room's … R06 converts … global rows are deleted at the end of R06."

Verified current behaviour: R06 became Player Lease (`011-player-lease-and-host-departure-semantics.md`). The global `queue_state`, `activities`, `auto_queue_config`, and `play_history` rows were NOT deleted; they remain the live source of truth.

New R14a decision: the global → room conversion is the R14a → R14b → R14c → R14d → R14e sprint sequence, executed by the **new** dedicated `cmd/room-cutover` mechanism with the **new** dedicated `room_cutover_marker` contract. R03's `cmd/migrate-data` + `migration_marker` are preserved untouched.

### 3.6 ADR 001 §11 — Room-scoped activities row

Old: "Room-scoped activities … existing global `activities` table is migrated."

Verified current behaviour: there is no `room_activities` table, migration, or repository. `grep -rn "room_activities\|RoomActivit"` returns zero matches across `*.go`, `*.sql`, `*.js`. Live room-activity **read** parity (a `RoomView` activity panel) is NOT implemented; today only the global `activities` table is populated and is read by `delivery/http/handlers.go:92` and use cases.

New R14a decision: R14a designs a new `room_activities` table (see §5.1). R14b builds it. The migrator copies every legacy row losslessly (preserved id/timestamp/type/user/description/count) and uses a dedicated narrow repository contract `AddActivity(ctx, roomID, entity.Activity) error` + `GetActivities(ctx, roomID, limit) ([]entity.Activity, error)` that is NOT coupled to `QueueRepository`. **Live read parity (a frontend activity panel) is NOT in R14a scope and is surfaced as a PO acceptance blocker** — see §12.

### 3.7 ADR 001 §15 compatibility table — shim + default `/ws` rows

Old: rows describing the R07/R08 shim and "`/ws` deprecated but live / default room."

Verified current behaviour: same as §3.1, §3.4.

New R14a decision: replaced by the production coordination sequence in §7 (R14b builds offline; R14c owns the production execution with a binary that has disabled/retired global write paths).

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

R06 became Player Lease rather than the global → room fold (the original ADR 001 §11 / ADR 002 §11/§13 attribution). Sprint renumbering placed room scoping in R07a/R07b/R07c/R07d (room queue) and R09a/R09b/R09c/R09d/R09e/R09f (playback / vote / auto-queue). Rooms were addressed by `{slug}` not `{roomId}`. The compatibility shim was deferred indefinitely because the global frontend surface never produced evidence of unambiguous identity, membership, and room-selection behaviour that would let a hidden default-room proxy work safely. The `/ws` synthesized default-room plan was never implemented; the global hub continues to serve real `queue_state`. The first R14a draft conflated the global `/api/queue/prioritize` (a queue mutation with an R07d room equivalent) with the global `/api/vote/prioritize` (a vote-driven queue mutation that needs the R09h room equivalent) — these are two distinct route families and only the second needs a parity sprint. The first R14a draft also implicitly reused R03's `cmd/migrate-data` + `migration_marker`; R14a clarifies that the room-cutover mechanism is a NEW dedicated CLI + NEW dedicated marker, distinct from R03.

The migration is now shaped by R14a and split into the future sequence in §7 (plus R05b as a blocking frontend-entry prerequisite and R09h as a blocking vote-prioritize parity prerequisite).

## 5. Migration identity contract

### 5.1 Activities target — new `room_activities` table

R14a designs the contract; R14b builds the table.

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

After cutover, `room_activities` is the sole writable source of truth. The legacy `activities` table is read-only for the approved rollback window and is dropped in R14e.

**Live read parity is NOT in R14a's scope.** R14a designs the persistence + repository contracts so future activity writes and reads are possible per-room, but the frontend activity panel that the global app exposed is **NOT** wired in this sprint. This is documented as a **Product Owner acceptance blocker** in §12: the legacy global dashboard's activity surface (`globalStore` read paths in `delivery/http/handlers.go:92` and `usecase/queue/interactor.go:GetActivities`) is removed in Phase C; if the Product Owner wants live read parity in a room before accepting R14d/R14e, it must be added as a separate frontend task.

### 5.2 Host bootstrap — canonical PostgreSQL `users.id`

Operator flag: `--host-user-id=<positive integer>` (the canonical `users.id` BIGINT, NOT a UUID).

Validation:

- The value is a positive integer.
- Exactly one `users` row exists with that ID. Because it is a primary key, "ambiguous" is not a meaningful outcome.
- The host is inserted into the migrated room as its sole host **inside the same migration transaction** as room creation and data conversion.
- Failure to resolve the ID aborts the migration before any target state is committed.

The migrator does NOT infer the host from: the first user to join, `users.role`, `HOST_EMAILS` / `ADMIN_EMAILS`, an existing player lease, or frontend / request-supplied identity.

**Override of issue #20 §2:** the schema permits one user to be a member of multiple rooms (uniqueness is `(room_id, user_id)` only); the one-host invariant is **per room**, not per user globally. The migrator does NOT reject a host merely because they belong to another active room.

**Idempotent retry with a different `--host-user-id` MUST fail on the room/slug conflict**, not silently change the room's host.

Email may be documented as an operator-side discovery aid (used manually to look up the numeric ID) but is NOT the migration command's canonical identity input.

### 5.3 Migration report — PII posture

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

### 5.4 Account-table posture — no recopy

`users`, `user_sessions`, and `priority_transactions` are **account-scoped per ADR 001 §3 Decision 9** and are NOT recopied at cutover. The migrator explicitly does NOT touch these tables. The legacy `users` table is the same table the room runtime reads; there is no separate "migrated user" identity surface. There is no remap, no re-issuance, no password rotation, and no identity rewriting. Idempotency is read-only against `users` (the host lookup only); writes go to `rooms` and `room_members` (new rows). The `users.legacy_id` column introduced in R03 is preserved untouched.

## 6. Source-to-target mapping

| Source (global, R00–R05) | Target (room, R14b builds) | Conversion notes |
| --- | --- | --- |
| `queue_state` (TEXT JSON, singleton `id = 1`) | `room_queue_state` (JSONB, one row per `rooms.id`) | Real conversion contract: read legacy TEXT into a `[]byte`, decode through `encoding/json` into a `map[string]any` (or the tagged `entity.Queue` struct), validate all invariants (`Songs`, `CurrentIndex`, `Status`, `Elapsed`, history fields, first-song/current-song behaviour), re-encode through `encoding/json` to canonical JSONB bytes, compute SHA256 over the canonical bytes for the marker record, and write to `room_queue_state` with PK = `rooms.id`. No `MarshalJSON` / `UnmarshalJSON` custom methods on `entity.Queue` are assumed; the contract is **the default `encoding/json` tagged-field round-trip**. |
| `activities` | new `room_activities` (R14b builds) | Lossless copy of every row with preserved `id`; `room_id` set to the migrated room's id; sequence resync via `setval(room_activities_id_seq, MAX(id), is_called=true)` AFTER all inserts commit. |
| `auto_queue_config` (singleton `id = 1`) | `room_auto_queue_config` (already exists, migration 0007) | Keyed by `room_id`; write the migrated room's row with the legacy singleton's values; the cap (50-row) lives in `room_play_history`, not `auto_queue_config`. |
| `play_history` (legacy 50-row cap, Go-enforced) | `room_play_history` (already exists, migration 0007) | See §6.1 for the id-collision handling. Lossless copy of every row with preserved `played_at`, `video_id`, `title`; per-room 50-row cap is enforced in `roomautoqueue` after cutover (matches R09f). |

After the copy, source rows are retained as migration evidence. They are read-only for the rollback window and dropped in R14e. After cutover, the room targets are the sole writable source of truth.

### 6.1 `room_play_history` id-collision handling

The legacy `play_history` table uses a global BIGSERIAL. The per-room `room_play_history` table also uses a global BIGSERIAL (it is one table shared across all rooms, identified by `room_id`). Copying the legacy rows with preserved `id` values into `room_play_history` can collide with rows that any room's auto-queue already appended since the R09f migration. The cutover migrator MUST handle this by **partitioning the id space**:

- Before the data copy, the migrator runs `SELECT MAX(id), COUNT(*) FROM room_play_history` and computes a `legacy_id_floor = MAX(id) + COALESCE(MAX(id), 0) - COALESCE(MIN(id), 0) + 1` reservation so the next free range is at least `MAX(id) + legacy_id_floor`. Concretely: shift each legacy row's `id` by `legacy_id_offset = MAX(room_play_history.id)` and emit `INSERT INTO room_play_history (id, room_id, played_at, video_id, title) SELECT id + :legacy_id_offset, :new_room_id, played_at, video_id, title FROM play_history`. The migrator records the offset in the `room_cutover_marker` record so a re-run or rollback window can refer to the same shift.
- After the insert, `setval(room_play_history_id_seq, MAX(id), is_called=true)` is called so the next `BIGSERIAL` value lands at `MAX(id) + 1`.
- The shifted ids are an implementation detail of the cutover; the per-room cap (50 rows) is enforced in `roomautoqueue` regardless of absolute id values.
- Re-run / idempotency: the `room_cutover_marker` record carries the SHA256 hashes of both the source (`play_history`) and the target post-copy (`room_play_history` filtered by `room_id = :new_room_id`) so a repeated `room-cutover up` is a no-op when the hashes match.

### 6.2 Schema-version plan

The schema version stays **v8** throughout R14a, R14b, R14c, R14d, and during the rollback window after R14c. The v9 bump lands in **R14e** and only if R14e actually drops legacy tables; if the rollback window must be extended or a forward-recovery path is needed, v9 is delayed. **No historical migration files are removed by R14a.** All of `0001_initial.up.sql` .. `0008_room_chat_messages.up.sql` remain on disk; a future `0009_room_activities.up.sql` (added in R14b) is additive. R14e may drop legacy tables and reduce the `0001_initial.up.sql` content to only the account-scoped tables (`users`, `user_sessions`, `priority_transactions`) so the historical migration files still describe what they originally created.

## 7. Phase A–D retirement sequence (revised cutover)

The four phases are implemented as separate sprints so each can be reviewed independently. The cutover is split between R14b (mechanism) and R14c (coordinated production execution), per the Architect review.

### Phase A — offline conversion (built + verified by R14b)

R14b is a backend-only sprint that ships **the mechanism** as a new dedicated CLI and verifies it against a representative PostgreSQL snapshot. R14b does NOT retire the global runtime in production; it lands the CLI, the marker contract, the source-to-target copy, the dry-run, the verification, and the rollback hook. R14b's verification gate MUST pass before R14c can run.

- R14b ships `cmd/room-cutover` with subcommands `plan` (read-only snapshot + dry-run), `up` (apply), and `abort` (release the advisory lock without committing). The subcommand `up` is the production cutover command.
- R14b ships the marker table `room_cutover_marker` (single-row, `id=1`) with the `room_cutover_id`, `target_room_slug`, `target_room_id`, `host_user_id`, per-source-table SHA256 hashes, per-target-table post-copy SHA256 hashes, `legacy_id_offset` for `room_play_history`, `cutover_started_at`, `cutover_committed_at`, and `binary_build_sha` (the SHA of the binary that produced the marker).
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

### Phase B — coordinated production cutover (executed by R14c)

R14c owns the **coordinated production execution**: a planned maintenance window, a redeploy with the **binary build flag `--room-cutover-authoritative=true`**, and the run of `cmd/room-cutover up` against the live PostgreSQL. R14c is the ONLY sprint where the production binary is changed.

- Before the window: a `pg_dump` is taken and retained ≥30 days (mirrors ADR 002 §9). The R14b binary is already deployed and the R14b verification gate has passed.
- During the window:
  - The application is shut down (or restarted with a maintenance-mode flag that disables ALL mutating endpoints — global AND room — while keeping reads disabled too in this state).
  - `cmd/room-cutover up` runs against the live database. It pins a connection, takes the advisory lock, opens a single transaction, copies the source rows to the target rows, commits, writes the marker.
  - The application restarts with `--room-cutover-authoritative=true`. In this build:
    - The legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes and the legacy `/ws` endpoint are **unregistered**, NOT just "must not be hit". The room routes are the only registered set.
    - The legacy repositories are still in the binary for read-only rollback support, but they are NOT wired into HTTP handlers or the WS hub.
    - On the cutover boundary, the application restart is the only window where the global routes are unreachable — this is the documented maintenance window.
  - The legacy global repositories write paths are also disabled by `--room-cutover-authoritative=true` to prevent any tool / script / cron that bypassed HTTP from mutating the legacy tables during the rollback window. The `--safe-write` mode is OFF in this build for legacy tables.
- Frontend deployment order is captured in §10's compatibility matrix; the cutover binary is paired with R14d's frontend at production cutover time.

### Phase C — frontend cutover (R14d)

- `RoomView` becomes the only supported view; `DashboardView` is removed or redirects to a Welcome / room entry surface.
- `globalStore.queueState`, `globalStore.voteSessions`, and `globalStore.autoQueueConfig` are cleared.
- `globalStore.currentUser` is preserved (RoomView still reads it).
- `globalStore.roomQueues[slug]` per-slug slices are preserved (RoomView reads them).
- The global `WebSocketClient` is closed and not reconnected.
- **Frontend entry UX is a prerequisite**, not part of R14d: the unbuilt SPA client methods (`api.createRoom`, `api.listRooms`, `api.getRoom`, `api.joinRoom`, the invite endpoints, `api.claimPlayerLease`, `api.heartbeatPlayerLease`, `api.releasePlayerLease`, `api.getPlayerLease`) MUST exist before R14c can be accepted. This is the **R05b — Room entry / player-lease UI** prerequisite sprint (see §10). R05b is the slice that ships these frontend methods and exposes them as actual UI (room create form, room list, room join, invite redeem, lease claim UI). R14d assumes these exist.

### Phase D — schema cleanup (R14e)

- Drop the legacy `queue_state`, `activities`, `auto_queue_config`, and `play_history` tables.
- Reduce `0001_initial.up.sql` to only the account-scoped tables (`users`, `user_sessions`, `priority_transactions`); preserve the historical migration files on disk so the v8 era remains auditable.
- Schema version bumps from 8 to 9 (only if R14e is accepted and destructive cleanup completes).
- Phase D is destructive cleanup only and runs **after** a verified rollback window AND after no runtime path references the legacy tables.

## 8. Atomicity & idempotency (dedicated marker, not R03 reuse)

The future R14b room-cutover mechanism is built fresh, drawing on R03 patterns where appropriate, but **does NOT reuse R03's CLI, marker, or transactional scaffolding**:

- `pg_try_advisory_lock(987654321)` on a pinned connection (same key, same idiom as R03). Deferred unlock on success or error. The lock is released in `cmd/room-cutover abort` and on process exit.
- Single transaction per `cmd/room-cutover up` invocation: `conn.BeginTx` → `tx.Commit`. `tx.Rollback` on any error.
- **NEW** marker table `room_cutover_marker` (single-row, `id = 1`) — distinct from R03's `migration_marker` table. The R03 marker is preserved untouched.
- Marker record fields: `room_cutover_id` (UUID generated by the CLI), `target_room_slug`, `target_room_id` (resolved after the room row is inserted), `host_user_id`, per-source SHA256 hashes (`queue_state.data`, `activities.*`, `auto_queue_config.*`, `play_history.*`), per-target post-copy SHA256 hashes (`room_queue_state.data` for the migrated room, `room_activities.*` filtered by `room_id`, `room_auto_queue_config.*` filtered by `room_id`, `room_play_history.*` filtered by `room_id`), `legacy_id_offset` (the `room_play_history` shift), `cutover_started_at`, `cutover_committed_at`, `binary_build_sha`.
- Re-run on identical hash record = `already cut over; no-op`, exit 0. **Driven by the `room_cutover_marker` SHA256 record, NOT by R03's `migration_marker`.**
- Hash drift on any source or target = rejected with an explicit sentinel. No `--force` / `--reset`.
- Two-phase verification:
  - **Pre-commit (within tx)**: source row counts, target row counts, `queue_state.data` byte length parity (legacy TEXT to canonical JSONB), per-target SHA256 after the copy.
  - **Post-commit (after tx)**: `cmd/room-cutover verify` subcommand re-reads the sources (which are now retained as migration evidence) and the targets, computes the SHA256s again, and asserts they match the marker.
- `cmd/room-cutover plan` subcommand (read-only): snapshot the source hashes, plan the target writes, write a JSON/text report WITHOUT committing.
- `--dry-run` mode on `cmd/room-cutover up`: same as `plan` but the connection is wired through the live DB; no writes commit.
- `config.RedactDSN` in the report.
- Sequence resync via `setval(seq, MAX(id), is_called=true)` for each migrated sequence (`room_activities_id_seq`, `room_play_history_id_seq` (post-shift), plus per-target sequences as needed). R03's `setval` calls for `user_sessions` / `priority_transactions` / `activities` / `play_history` are unchanged and re-run only as part of a future R03 re-cutover (out of R14 scope).
- The room-cutover CLI is an offline CLI; it does NOT run inside the server process; it does NOT mutate either model concurrently with runtime writers (the application is shut down during Phase B).

## 9. WebSocket + frontend transition rules

- `/ws` is **unregistered** in the `--room-cutover-authoritative=true` build (R14c). It returns HTTP 404 from the mux because the handler is not registered. (404 is the natural outcome of unregistering — R14a does NOT claim a `410 Gone` for `/ws` because there is no path to retire; the upgrade is refused.)
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- The first per-room `room_queue_sync` after migration lands carries the migrated room's queue state under the migrated room's `seq_num` allocation (per-room sequence numbers restart from the per-room hub's own counter, which is independent of the global hub's counter; this is the existing R07b behaviour and is not changed by R14a).
- Frontend: preserve `globalStore.currentUser`; clear `globalStore.queueState`, `globalStore.voteSessions`, `globalStore.autoQueueConfig`; close global `WebSocketClient`; per-slug `roomQueues[slug]` slices kept.

## 10. Future sprint sequence

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R05b — Room entry / player-lease UI (frontend + narrow backend touchups)** | Frontend only (no new HTTP routes; uses the existing R04 / R06 / R11a routes). Adds SPA client methods `api.createRoom` / `api.listRooms` / `api.getRoom` / `api.joinRoom` / invite + `api.claimPlayerLease` / `api.heartbeatPlayerLease` / `api.releasePlayerLease` / `api.getPlayerLease`; exposes them as actual room create / list / join / invite-redeem / lease-claim UI. **BLOCKING PREREQUISITE for R14c** — without it, retiring `/api/queue` / `/ws` etc. strands users on a global dashboard that has no way to enter a room. | The existing backend routes are already registered (per `cmd/server/main.go:413–486`); R05b only wires the SPA. |
| **R09h — Room vote-to-prioritize parity** | Backend room-vote-prioritize parity. Adds `POST /api/rooms/{slug}/vote/prioritize`. **BLOCKING PREREQUISITE for R14c** to retire `/api/vote/prioritize` only. | Reuses existing `room_queue_song_prioritized` event. No new WS event constant. |
| **R14b — room-cutover mechanism (backend only)** | New dedicated CLI `cmd/room-cutover` with `plan` / `up` / `abort` / `verify` subcommands. New marker table `room_cutover_marker`. Source-to-target copy (legacy → room). `--room-cutover-authoritative=false` build flag (default `false`). Pre-commit + post-commit verification. **NO global route removal. NO frontend changes.** R14b's binary ships the mechanism and verifies it; R14b's gate MUST pass before R14c can run. | Does NOT reuse R03's CLI or `migration_marker`. R03's marker is preserved untouched. |
| **R14c — coordinated production cutover** | Backend production cutover (Phase B). Maintenance window + restart with `--room-cutover-authoritative=true` binary + run `cmd/room-cutover up` against the live DB. Unregisters the legacy global `/api/queue/...` / `/api/vote/skip` / `/api/autoqueue/...` routes and the global `/ws` endpoint. Pairs with the R14d frontend in the deployment compatibility matrix (see §11). | `/api/vote/prioritize` `410` remains blocked on R09h until R09h is accepted; before R09h lands, the global route is excluded from the unregistration set. |
| **R14d — frontend global-path removal** | Frontend only. Removes / redirects `DashboardView`; clears `globalStore.queueState` / `voteSessions` / `autoQueueConfig`; closes global `WebSocketClient`; preserves `currentUser` + `roomQueues[slug]`. | Assumes R05b has landed. |
| **R14e — schema cleanup** | Backend only. Drops legacy `queue_state`, `activities`, `auto_queue_config`, `room_play_history` rows for non-migrated rooms (after the rollback window). Reduces `0001_initial.up.sql` to account-scoped tables only. Bumps schema v8 → v9. | Runs after the verified rollback window. Historical migration files remain on disk. |

Sprint names may be renamed if evidence requires it, but R05b / R09h / R14b / R14c / R14d / R14e MUST NOT be combined; migration, destructive cleanup, frontend rewrite, frontend entry UX, and vote-prioritize parity must remain independently reviewable.

## 11. Deployment compatibility matrix

The cutover involves two build flags (`--room-cutover-authoritative` per-server, R05b frontend bundle version per-client) and two room states (pre-cutover vs post-cutover binary). Valid combinations only:

| Server binary (cutover flag) | Frontend SPA | Legacy global routes | Per-room routes | Result |
| --- | --- | --- | --- | --- |
| `--room-cutover-authoritative=false` (pre-R14c) | pre-R05b (no entry UI; DashboardView only) | Registered; serve legacy repos | Registered | Pre-cutover baseline. Documented. |
| `--room-cutover-authoritative=false` (post-R14b; pre-R14c) | post-R05b (entry UI exists; user can create / list / join rooms / claim lease) | Registered; serve legacy repos | Registered | The R14b verification state. Users can use both surfaces. |
| `--room-cutover-authoritative=true` (post-R14c) | post-R14c + post-R05b + post-R14d (DashboardView removed / redirected) | **Unregistered** (HTTP 404 from mux); `/api/vote/prioritize` may still be live if R09h has not yet landed and is excluded from the unregistration set | Registered; sole authoritative path | Production cutover state. Documented. |
| `--room-cutover-authoritative=true` (post-R14c) | post-R05b only (DashboardView still present) | **Unregistered** | Registered | Frontend shows DashboardView, but the legacy routes it calls return 404. This is a **BROKEN** combination — the compatibility matrix forbids it. Either pair `--room-cutover-authoritative=true` with the R14d frontend, or keep the pre-R14c binary. |
| R03 binary (legacy) | any | Registered (legacy repos) | Registered | Pre-cutover baseline. Documented. Documented as the rollback entry point if Phase B fails: restart the R03 binary, the `room_cutover_marker` remains in place, but the legacy tables are untouched and the room tables are the new (empty / partial) copies. Forward recovery may be needed depending on what mutations landed. |
| `--room-cutover-authoritative=false` (R14c mid-window; binary restarted before `cmd/room-cutover up`) | any | Registered; serves legacy repos | Registered | Mid-window rollback state. Documented. The `pg_dump` from the start of the window is the source of truth. |

Client-continuity claim (R14a replacement): the only combinations that guarantee client continuity are the documented ones. Specifically: the `post-R05b + pre-R14c` combination is safe to deploy before the cutover window; the `post-R14c + post-R14d` combination is the production cutover state. **There is NO combination in which a frontend relying on legacy global routes is connected to a server that has unregistered those routes.** This is the client-continuity guarantee.

## 12. PO acceptance blockers

These are explicit blockers that the Product Owner must sign off on before R14c can run (and before R14e can land):

1. **Live room-activity read parity.** R14a designs the `room_activities` table and the repository contract; **live read parity (a `RoomView` activity panel that replaces the global dashboard's activity surface) is NOT in this sprint and is NOT required for R14c or R14d**. If the Product Owner wants read parity in the room frontend before accepting R14c, it must be added as a separate frontend task and its scope must be settled in a focused sprint before R14d (or R14c depending on ordering).
2. **Slug-collision resolution.** If the migrated room slug already exists in `rooms` at cutover time with a different identity, the migrator fails loudly. The recovery path is operator-driven (rename the existing room and re-run, or pick a different slug). No automatic rename or proxy.
3. **`410` / 404 envelope wording** on retired routes. The envelope `{error: "gone", code: "global_contract_retired", documentation: "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"}` is the proposed wording. The envelope does NOT carry a concrete migrated-room `successor` value (this was the first R14a draft's mistake; see review-revision note). Clients discover the migrated room through the standard `GET /api/rooms` listing endpoint, which is the documented successor-discovery surface; the `Link: </api/rooms>; rel="successor-version"` header may be added if the Product Owner wants explicit discovery.
4. **Rollback posture.** Rollback after the binary restart with `--room-cutover-authoritative=true` is **NOT a transparent rollback** — it requires either restarting the pre-R14c binary (which still serves legacy routes; the room data is partially populated) or a forward-recovery path against the `pg_dump`. The compatibility matrix in §11 is the deployment-time safeguard. The Product Owner must accept this posture.
5. **R09h landing prerequisite for `/api/vote/prioritize` retirement.** Until R09h is accepted, R14c keeps `/api/vote/prioritize` registered. The cutover binary may retire the OTHER legacy routes but explicitly EXCLUDES `/api/vote/prioritize` from the unregistration set.
6. **R05b landing prerequisite for user-facing cutover.** Until R05b is accepted, no user can create / list / join rooms from the SPA. R14c is therefore blocked.

These are PO acceptance blockers; they are settled during R14a review, not inside R14b/R14c. See §13 for the items that are NOT blockers.

## 13. Items already settled (informational)

- ID-collision handling for `room_play_history` (see §6.1).
- Account-table posture: no recopy (§5.4).
- Schema version stays v8 in R14a; v9 only if R14e lands (§6.2).
- Historical migration files preserved on disk; R14e may reduce `0001_initial.up.sql` to account-scoped tables only (§6.2).
- The R03 `migration_marker` is preserved untouched (§8).
- Per-room `room_play_history` 50-row cap is enforced in `roomautoqueue`, matching R09f (§6).
- No custom `MarshalJSON` / `UnmarshalJSON` are assumed on `entity.Queue`; the JSON conversion contract uses the default `encoding/json` tagged-field round-trip (§6).
- The global `/api/queue/prioritize` route has a room equivalent (`/api/rooms/{slug}/queue/prioritize`, R07d) and retires in R14c without a parity sprint (§3.3); only `/api/vote/prioritize` needs R09h.
- WebSocket: `/ws` is unregistered in the cutover build, returning HTTP 404 from the mux (§9); no `410 Gone` is claimed for `/ws`.
- Per-room sequence numbers restart from the per-room hub's own counter (§9).

## 14. Verification

- `git diff --check` clean.
- `git diff --name-only` contains only documents under `documents/00-project-management/`; no `*.go`, `*.vue`, `*.sql`, `Dockerfile*`, `nginx/*`, `*.env*`, `package*.json`, `package-lock.json`, `go.mod`, `go.sum`, anything under `cmd/`, `internal/`, `frontend/src/`, `docker/`, `certs/`, `letsencrypt-*`, `extension/`, `.gitignore`, or `.gitattributes`.
- ADR 001 body of §3–§18 is unchanged byte-for-byte except for the one metadata supersession note near the header.
- ADR 003 is referenced from `022-…md` and `ROOM_EPIC_SPRINT_SEQUENCE.md`.
- ADR 003 status remains `Proposed` (NOT `Accepted`) — pending Architect review and Product Owner acceptance.
- No `main`, room `0`, "default room", "implicit room", or synthesized default-room `/ws` language appears in the R14a documents (every reference is explicit prohibition).
- No usage of `cmd/migrate-data` for the room cutover anywhere in the R14a documents.
- The R03 `migration_marker` row is referenced only as "preserved untouched"; never as the room-cutover's marker.
