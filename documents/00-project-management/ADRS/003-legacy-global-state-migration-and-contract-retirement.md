# ADR 003 — Legacy global-state migration and contract retirement

- Status: Accepted — R14a contract; supersedes parts of ADR 001 and ADR 002 §11/§13; Product Owner acceptance 2026-07-16
- Date: 2026-07-15 (revised through the accepted Architect review passes on 2026-07-15)
- Scope: Reconciles several paragraphs of ADR 001 (and one paragraph cluster of ADR 002) that have become inaccurate as the room epic landed in the renumbered R06–R11 sequence. Records the new R14a implementation-ready contract for converting the legacy global state into exactly one Product-Owner-named room and for retiring the parallel global REST/WS contracts. ADR 003 does **not** replace ADR 001's room architecture in general; it supersedes only the specific paragraphs listed below. ADR 003 is documentation-only: no runtime change is authorized by it.

> **Fifth-pass review-revision summary (2026-07-15).** After the fifth Architect review (corrective Builder prompt on Issue #20), this ADR applies 2 corrective changes from the fourth-pass draft, plus the cumulative 9 fourth-pass corrections, the cumulative 9 third-pass corrections, the cumulative 14 second-pass corrections, and the cumulative 14 first-pass corrections: (1) **server and client gate ownership is now explicit**: `--room-cutover-authoritative` is a **runtime server startup flag** (Go runtime arg parsed at `main()`; read once during composition; default `false`); the Go backend NEVER consumes `VITE_ROOM_CUTOVER_AUTHORITATIVE` (the SPA flag does NOT cross the network boundary at runtime; only ships inside the SPA bundle); R09i activity-writer injection depends ONLY on the server flag — at composition, the backend injects either a no-op writer (when `false`) or the real `room_activities`-writing implementation (when `true`); the SPA flag controls R14d's frontend behavioral activation only; (2) **`0010.down.sql` recreates ONLY the four legacy tables** that R14e drops (`queue_state`, `activities`, `auto_queue_config`, `play_history`) with their exact legacy definitions — `queue_state` has `data TEXT NOT NULL` (NOT seedable with `NULL`); `activities` has its OWN `activities_id_seq` BIGSERIAL; `auto_queue_config` has the singleton seed row at its initial values `INSERT ... VALUES (1, FALSE, 'related') ON CONFLICT DO NOTHING`; `play_history` has its OWN `play_history_id_seq` BIGSERIAL and the `idx_play_history_played_at` index; the `.down.sql` MUST NOT recreate any account-table objects (`users`, `user_sessions`, `priority_transactions`, their sequences, the unique constraint on `users.email`, the unique constraint on `user_sessions(user_id, session_date)`, the FK constraints on `user_sessions.user_id` → `users.id` and `priority_transactions.user_id` → `users.id`) — those tables survive `0010.up.sql`, and recreating their objects would conflict with the existing schema; **the documented default is that `.down.sql` cannot restore dropped data**, and the reverse-copy strategy relies on the retained `pg_dump` (≥30 days) or a separately approved reverse-copy mechanism; (3) also from the previous passes: the sprint execution order is `R05b → R09h → R14b → R09i → R14d → R14c → R14e`; (4) the new migration `0009_room_cutover_support.{up,down}.sql` creates BOTH `room_activities` AND `room_cutover_marker`; the CLI does NOT create schema ad hoc; (5) the marker table stores `target_room_id` and `host_user_id` as immutable integer audit snapshots WITHOUT foreign keys; (6) the marker timestamp field `cutover_pre_commit_at` is supplied explicitly by the CLI at marker insertion (NOT `DEFAULT NOW()`); (7) R09i write behavior must remain DISABLED until R14c has copied legacy activities and resynchronized `room_activities_id_seq`; (8) every new migration ships a matching `.down.sql`; (9) `cmd/room-cutover` ships NO `abort` subcommand; the marker is inserted in the same transaction as the room + host + copy; lock release uses deferred `pg_advisory_unlock` on the same pinned connection.

## 1. Context

The verified `dev` baseline after accepted R11a has two parallel product models:

- PostgreSQL is the only runtime database (ADR 002 — R01/R02/R03).
- Legacy global persistence still exists: singleton `queue_state` (`id = 1`, TEXT JSON), append-only `activities`, singleton `auto_queue_config` (`id = 1`), and `play_history` (50-row cap, enforced in Go).
- Room persistence also exists: `rooms` / `room_members` / `room_invites` (migration 0004); `room_queue_state` (migration 0006); `room_auto_queue_config` + `room_play_history` (migration 0007); `player_leases` (migration 0005); `room_chat_messages` (migration 0008).
- The frontend is substantially room-aware.
- Room **entry** UX is **incomplete**: no `api.createRoom` / etc. SPA methods exist today; rooms are entered only by direct URL.
- Room **activity** UX is **incomplete**: the global `activities` table is fed by global mutators; the room runtime has no equivalent write path to the new `room_activities` table.
- No migration has yet converted the global state into a PO-named room as the sole source of truth.
- No permanent `main` room, `room 0`, or hidden default room is allowed by ADR 001 §3 Decision 2 and Epic #17.
- Live room-activity **read** parity (a `RoomView` activity panel) is NOT in this sprint and is deferred as a separate optional frontend decision.

Several ADR 001 paragraphs — and one ADR 002 paragraph cluster (§11/§13) — were written before the room epic was renumbered and before the shim plan was deferred indefinitely. They are now historically inaccurate and must be reconciled before the R14 migration sprint can ship safely.

## 2. Supersession statement

ADR 003 supersedes **only** the following paragraphs of ADR 001 and one paragraph cluster of ADR 002. ADR 003 does NOT replace ADR 001's room architecture in general. ADR 001 remains authoritative for the decisions that ADR 003 does not replace.

Superseded ADR 001 paragraphs:

- §3 Decision 6 — the compatibility shim was never built; old global routes still operate on legacy global repositories.
- §9 transition-strategy table — the R07 row's "compatibility shim that resolves the single migrated room by default" was never implemented.
- §9 final route shape list — uses `{roomId}` (the code uses `{slug}`); actual playback route family is `/api/rooms/{slug}/playback/{...}`; `/vote/prioritize` is a vote mutation (R09h), distinct from R07d's `/queue/prioritize`.
- §10 global `/ws` backward-compatibility paragraph — never implemented; R14c retires `/ws` with `410 Gone` via tombstone handlers (NOT `404` from unregistration).
- §11 migration direction — R06 became Player Lease; the conversion is the R14a → R14b → R09i → R14d → R14c → R14e sequence, executed by the dedicated `cmd/room-cutover`.
- §11 table row — there is no `room_activities` table today; R14b creates it via `0009_room_cutover_support.up.sql`; R09i wires the runtime write path; live read parity is deferred.
- §15 compatibility table — shim + default `/ws` rows are inaccurate.
- §18 consequences mapping — sprint → scope map is out of date.

Superseded ADR 002 paragraph cluster:

- §11 (`setval` / R06 fold attribution) and §13 (R03 migrator reuse) attribute the global → room fold to "R06". Never implemented. The cutover is the R14b dedicated `cmd/room-cutover` with a NEW dedicated `room_cutover_marker`; R03 is preserved untouched and is NOT extended.

## 3. Per-section supersession detail

### 3.1 ADR 001 §3 Decision 6 — compatibility shim

Old: "Room context is mandatory ... The old global routes transition through a documented compatibility shim to a final `410 Gone`."

Verified current behaviour: no shim exists. `cmd/server/main.go:389–409` still operates on legacy repos.

New R14a decision: the retirement path is explicit `410 Gone` (Phase B, R14c), not a shim. R14c registers **repository-free tombstone handlers** returning `410 Gone` with the documented envelope + `Link: </api/rooms>; rel="successor-version"`; the global `/ws` upgrade is rejected with `410` in the HTTP phase.

### 3.2 ADR 001 §9 transition-strategy table — R07 row

Old: "R07 … compatibility shim that resolves the single migrated room by default."

Verified current behaviour: no migrated room exists today; the shim was never built.

New R14a decision: removed. Sprint execution order is **R05b → R09h → R14b → R09i → R14d → R14c → R14e**, with R14b landing `0009_room_cutover_support.up.sql` (creates both `room_activities` and `room_cutover_marker`).

### 3.3 ADR 001 §9 final route shape list

Old: uses `{roomId}`; lists room routes never built.

Verified current behaviour: `{slug}`; playback at `.../playback/{...}`; `/vote/prioritize` is the R09h room equivalent (vote-driven), distinct from the R07d queue mutation `/queue/prioritize`.

New R14a decision: ADR 001 §9's route list is replaced by the actual room route inventory in `022-…md` §3. The cutover is atomic across the listed legacy routes: R14c retires ALL of them in one step (no partial cutover). `/api/queue/prioritize` already has the R07d room equivalent. Only `/api/vote/prioritize` needs R09h, and R09h blocks the entire R14c cutover.

### 3.4 ADR 001 §10 `/ws` default-room synthesis

Old: "/ws serves a synthesized 'default room' view ..."

New R14a decision: R14a explicitly rejects the synthesized default-room `/ws` plan. R14c retires `/ws` with `410 Gone` via a tombstone handler (NOT `404` from unregistration; NOT deletion). The HTTP-phase rejection prevents WebSocket handshakes from succeeding.

### 3.5 ADR 001 §11 migration direction — R06 fold

Old: "global rows are deleted at the end of R06."

New R14a decision: the global → room conversion is the fixed sequence **R05b → R09h → R14b → R09i → R14d → R14c → R14e**, executed by the dedicated `cmd/room-cutover` mechanism with the dedicated `room_cutover_marker` contract. R03's `cmd/migrate-data` + `migration_marker` are preserved untouched and NOT extended.

### 3.6 ADR 001 §11 — Room-scoped activities row

Old: "Room-scoped activities … existing global `activities` table is migrated."

Verified current behaviour: there is no `room_activities` table, migration, or repository.

New R14a decision: R14b creates `room_activities` via `0009_room_cutover_support.up.sql`. R14b's migrator copies every legacy row losslessly. The runtime write path lands in **R09i** (a new blocking prerequisite for R14c); the read parity remains a separate optional frontend decision.

### 3.7 ADR 001 §15 compatibility table — shim + default `/ws` rows

New R14a decision: replaced by the production coordination sequence. R14c registers tombstone handlers; R14d's behavioral activation is gated behind an explicit flag activated during the R14c maintenance window.

### 3.8 ADR 001 §18 consequences mapping — sprint → scope map

New R14a decision: replaced by the actual renumbered sequence. The future sprints in the R14 epic are R05b / R09h / R09i / R14b / R14c / R14d / R14e.

### 3.9 ADR 002 §11 + §13 — R06 fold attribution

New R14a decision: this attribution is fulfilled by R14b (the dedicated `cmd/room-cutover`), not by R06 or by R03. R03 is the unrelated SQLite-to-PostgreSQL migration; it is preserved untouched. The room-cutover does NOT reuse R03's CLI or marker.

## 4. Why the old plan was not implemented

R06 became Player Lease. Sprint renumbering placed room scoping in R07a/R07b/R07c/R07d and R09a/R09b/R09c/R09d/R09e/R09f. Rooms addressed by `{slug}`. Compatibility shim deferred indefinitely. `/ws` synthesized default-room plan never built. The first R14a draft conflated `/api/queue/prioritize` (queue mutation, R07d equivalent exists) with `/api/vote/prioritize` (vote-driven; needs R09h). The first draft reused R03's CLI + marker; the revision separated them. The first draft claimed `MarshalJSON` / `UnmarshalJSON` on `entity.Queue` (don't exist); the revision uses the default `encoding/json` tagged-field round-trip. The first draft used `404` from route unregistration for retirement; the second-pass review reverted to `410 Gone` via tombstone handlers. The first draft did not address `room_activities` write parity; the second-pass added R09i. The first draft ordered R14c before R14d; the second-pass reversed that. The first draft's R14e typo targeted `room_play_history`; the second-pass corrected that to drop only the legacy global `play_history`. The first draft kept the schema at v8 through R14b despite R14b adding `0009`; the second-pass bumped v9 in R14b and v10 in R14e. The first draft had a `cmd/room-cutover abort` subcommand; the second-pass removed it. The first draft wrote `room_cutover_marker` post-commit; the second-pass moved it into the same transaction. The third-pass addressed: **R09i was placed after R14c / R14d in some places and before R14d in others** — the third-pass settled `R14b → R09i → R14d → R14c` so R09i follows R14b (which creates `room_activities`); **the migration files needed clear ownership** — the third-pass renamed `0009_room_activities.up.sql` to `0009_room_cutover_support.up.sql` so it creates BOTH `room_activities` AND `room_cutover_marker` (the CLI does NOT create schema ad hoc); **R14d's pre-cutover deployment hid the still-authoritative global queue** — the third-pass added an explicit cutover-state gate so the SPA continues exposing the legacy dashboard until R14c's maintenance window activates the gate. The third-pass also added explicit `pg_advisory_unlock` on the same pinned connection (deferred unlock), renamed the marker timestamp to `cutover_pre_commit_at`, and made `.down.sql` behavior explicit.

## 5. Migration identity contract

### 5.1 Activities target — new `room_activities` table

R14a designs the contract; **R14b** lands `0009_room_cutover_support.up.sql` (creates BOTH `room_activities` and `room_cutover_marker`; schema v8 → v9); **R09i** wires the runtime write path.

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

Lossless copy of every legacy `activities` row with preserved `id`, `timestamp`, `type`, `user`, `description`, count, ordering; `room_id` set to the migrated room's id; `setval(room_activities_id_seq, MAX(id), is_called=true)` after the preserved-id insert.

Dedicated narrow repository contract (do NOT couple to `QueueRepository`):

```go
AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error
GetActivities(ctx context.Context, roomID int64, limit int) ([]entity.Activity, error)
```

Live room-activity **runtime write parity** lands in R09i (backend-only). Live room-activity **read parity** is a separate optional frontend decision (NOT in R09i or R14a).

### 5.2 Marker — new `room_cutover_marker` table (also created by `0009`)

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

### 5.3 Migration file naming and `.down.sql`

- `0009_room_cutover_support.up.sql` and `0009_room_cutover_support.down.sql` — create / drop both `room_activities` AND `room_cutover_marker` (with indexes, constraints, and PKs). Only `room_activities.room_id` carries a foreign key (`REFERENCES rooms(id) ON DELETE CASCADE`); `room_cutover_marker.target_room_id` and `room_cutover_marker.host_user_id` are intentionally stored as plain `BIGINT` audit snapshots WITHOUT foreign keys — migration evidence MUST NOT own room or user lifecycle.
- `0010_drop_legacy_global_tables.up.sql` and `0010_drop_legacy_global_tables.down.sql` — drop / recreate the legacy global tables only. The `.down.sql` header states explicitly that it cannot restore dropped data.
- Every migration follows the R00–R11a convention: matching `.down.sql`. `0001_initial.up.sql` is NEVER rewritten.

### 5.4 Host bootstrap — canonical PostgreSQL `users.id`

Operator flag: `--host-user-id=<positive integer>`.

- Positive integer; PK lookup.
- Inserted as the sole host inside the **same migration transaction** as room creation, data conversion, and the marker row.
- Failure to resolve the ID aborts the migration before any target state is committed.

### 5.5 Migration CLI flags (R14b)

- `--room-slug=<slug>` (required)
- `--room-name=<name>` (required)
- `--host-user-id=<positive integer>` (required)
- `--dry-run` (optional)

The CLI MUST reject unknown positional arguments and unknown flags; MUST NOT accept `--force` / `--reset` / `--allow-hash-drift`; MUST NOT expose a subcommand to release another process's advisory lock (PostgreSQL session locks belong to the connection that acquired them; see §8).

### 5.6 Account-table posture — no recopy

`users`, `user_sessions`, `priority_transactions` remain account-scoped per ADR 001 §3 Decision 9 and are NOT recopied at cutover. The migrator does NOT touch these tables; idempotency is read-only against `users` (the host lookup only); writes go to `rooms` and `room_members`. The `users.legacy_id` column from R03 is preserved untouched.

## 6. Source-to-target mapping

| Source (global, R00–R05) | Target (room, R14b builds) | Conversion notes |
| --- | --- | --- |
| `queue_state` (TEXT JSON) | `room_queue_state` (JSONB) | Real conversion: read TEXT into `[]byte`, decode through `encoding/json` into the tagged `entity.Queue` struct, validate invariants, re-encode to canonical JSON, compute SHA256 over the canonical bytes. No custom `MarshalJSON` / `UnmarshalJSON`; default `encoding/json` tagged-field round-trip. |
| `activities` | new `room_activities` (migration 0009) | Lossless copy with preserved `id`; `room_id` set to the migrated room's id; sequence resync. |
| `auto_queue_config` (singleton id=1) | `room_auto_queue_config` (migration 0007) | Keyed by `room_id`; write the migrated room's row with the legacy singleton's values. |
| `play_history` (legacy 50-row cap, Go-enforced) | `room_play_history` (migration 0007) | Id-collision handling per §6.1. Per-room 50-row cap preserved. |

After the copy, source rows are retained as migration evidence (read-only for the rollback window) and dropped in R14e via `0010_drop_legacy_global_tables.up.sql`. After cutover, the room targets are the sole writable source of truth.

`users`, `user_sessions`, `priority_transactions` remain account-scoped and are NOT migrated into a room.

### 6.1 `room_play_history` id-collision handling

- `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)`.
- Insert each legacy row with `id + :legacy_id_offset`.
- Record the offset in `room_cutover_marker`.
- After insert, `setval(room_play_history_id_seq, MAX(id), is_called=true)`.
- The shifted ids are an implementation detail; the per-room cap is enforced in `roomautoqueue`.
- Re-run / idempotency: SHA256 hash check on source + target post-copy.

### 6.2 Schema-version plan

The migration runner reports the applied numbered migration as the schema version; adding a migration necessarily bumps the version. **No sprint can both add a migration and keep the version unchanged.**

- Schema at start of R14a: v8.
- R14b lands `0009_room_cutover_support.up.sql` — creates BOTH `room_activities` AND `room_cutover_marker` (and their indexes / constraints). Schema becomes **v9**.
- R14c is a runtime + binary-flag cutover; does not add a migration. Schema during R14c and the rollback window: v9.
- R14e lands `0010_drop_legacy_global_tables.up.sql` — drops **only** the four legacy global tables: `queue_state`, `activities`, `auto_queue_config`, `play_history`. Schema becomes **v10**.
- **`0010_drop_legacy_global_tables.down.sql` recreates ONLY the four dropped tables** with their exact legacy definitions so that rolling forward/backward on the dropped tables is a no-op on structure. The `.down.sql` MUST recreate:
  - `queue_state` — with `id INTEGER PRIMARY KEY CHECK (id = 1)` and `data TEXT NOT NULL` (the `data` column is NOT NULLABLE — the `.down.sql` MUST NOT seed `data` with `NULL`).
  - `activities` — with its OWN `activities_id_seq` BIGSERIAL and **exactly** the column structure from `0001_initial.up.sql` (`id BIGSERIAL PRIMARY KEY`, `"timestamp" TIMESTAMPTZ NOT NULL`, `type TEXT NOT NULL`, `"user" TEXT NOT NULL`, `description TEXT NOT NULL`). The legacy `activities` table has no `room_id` column; the `.down.sql` MUST NOT introduce one.
  - `auto_queue_config` — with its OWN required singleton seed row at its initial values from `0001_initial.up.sql`: `INSERT INTO auto_queue_config (id, enabled, strategy) VALUES (1, FALSE, 'related') ON CONFLICT DO NOTHING`.
  - `play_history` — with its OWN `play_history_id_seq` BIGSERIAL, the column structure from `0001_initial.up.sql`, and the **`idx_play_history_played_at` index**.
- The `.down.sql` MUST NOT recreate **account-table** objects (`users`, `user_sessions`, `priority_transactions`, `users_id_seq`, `user_sessions_id_seq`, `priority_transactions_id_seq`, the unique constraint on `users.email`, the unique constraint on `user_sessions(user_id, session_date)`, the FK constraints on `user_sessions.user_id` → `users.id` and `priority_transactions.user_id` → `users.id`) — these tables survive `0010.up.sql`, and recreating their objects would conflict with the existing schema.
- The documented default is that **`0010.down.sql` cannot restore dropped data**. The reverse-copy strategy is to **rely on the retained `pg_dump`** taken at the start of the R14c maintenance window (retained ≥30 days) or on a separately approved reverse-copy mechanism. The `.down.sql` is intentionally one-way with respect to row data; this is documented in the `.down.sql` file header. Re-running `0010.up.sql` after the `.down.sql` will drop the recreated tables again — that's the same destructive cleanup re-applied, not a data recovery.
- Migration-evidence rows in `room_cutover_marker` and preserved `room_*` rows are unaffected; the `.down.sql` does NOT touch any non-`0010` table or row.

**Historical migration files are never rewritten.** `0001_initial.up.sql`..`0008_room_chat_messages.up.sql` remain on disk byte-for-byte. R14e lands `0010_drop_legacy_global_tables.up.sql` as a SEPARATE forward migration. The historical files remain auditable.

## 7. Phase A–D retirement sequence

R14a chooses ONE recommended sequence. The four phases are implemented as separate sprints. The cutover is split between R14b (build + verify) and R14c (production execution with `410 Gone` tombstone handlers, NOT unregistered `404`).

### Phase A — offline conversion (built + verified by R14b)

R14b is a backend-only sprint that ships the offline cutover mechanism. R14b lands `0009_room_cutover_support.up.sql` (creating both `room_activities` and `room_cutover_marker`; schema v8 → v9). R14b does NOT retire the global runtime, does NOT introduce the runtime server flag, and does NOT select activity-writer implementations — those belong to R14c.

- `cmd/room-cutover` ships subcommands `plan`, `up`, `verify` — **no `abort` subcommand** (PostgreSQL session locks belong to the connection that acquired them; release is via explicit `pg_advisory_unlock` on the same pinned connection, with connection close as fallback).
- `room_cutover_marker` table created by `0009_room_cutover_support.up.sql`; PK = 1; `target_room_id` and `host_user_id` are stored as **immutable integer audit snapshots WITHOUT foreign keys** (migration evidence must NOT own room or user lifecycle — future hard-delete work under R10f+ must not be permanently blocked by the marker row).
- R14b verification gate (must pass before R09i can run): plan returns PII-free report; `--dry-run` completes with no writes; real `up` creates room + host membership + copies + marker ALL in one transaction, commits, returns 0; re-run is `already cut over; no-op` driven by the marker; hash drift rejected (no `--force`/`--reset`); PII redacted; R03 marker preserved untouched; schema version after R14b is **9**; `go test ./...` clean.

### R09i — room-activity runtime parity (BACKEND-ONLY, after R14b, before R14d)

R09i wires the room runtime's mutations (`internal/usecase/roomqueue`, `roomvote`, `roomautoqueue`) to call `RoomActivityRepository.AddActivity` on the same events that the global runtime today appends to `activities`. After R09i, the room runtime has a live write path; per-room mutators call it. **R09i is BACKEND-ONLY.** The activity READ panel is a separate optional frontend decision and is NOT in R09i's scope.

- Blocking prerequisite for R14c unless the Product Owner explicitly waives it (recorded on Issue #20).
- Runs after R14b (because R14b creates `room_activities`) and before R14d.
- No new HTTP routes; no WebSocket event additions; only mutator + repo wiring.

### Phase C — frontend cutover (R14d, with deferred behavioral activation)

R14d implements SPA changes; **the behavioral activation is deferred to the R14c maintenance window**.

- Implementation may be accepted BEFORE R14c ships.
- Pre-R14c, the SPA continues to expose the legacy dashboard surface (so the still-authoritative global queue remains reachable).
- Activation happens during R14c. The frontend gate is the **single named mechanism** `VITE_ROOM_CUTOVER_AUTHORITATIVE` (Vite build-time `import.meta.env` baked at SPA build; two bundle variants — `false` shipped pre-R14c, `true` shipped as part of the R14c rollout). The SPA is built twice and the right bundle is deployed for the right server state. There is **NO runtime feature flag** — no alternate mechanism (no separate env var at runtime, no separate feature-flag service) is permitted within R14a's contract. R14d's bundle-acceptance ships the `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` variant; the `true` variant is deployed as part of the R14c maintenance window.
- R14d assumes R05b (room entry UI) has landed.

### Phase B — coordinated production cutover (R14c)

R14c owns the coordinated production execution: maintenance window during which (1) public traffic is closed, (2) `cmd/room-cutover up` runs and is verified against the live DB, (3) the `--room-cutover-authoritative=true` server AND the `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` SPA bundle are deployed as a paired set while traffic remains closed, (4) paired smoke checks run against both artifacts, and (5) traffic reopens. The `true` server MUST NOT start before the marker exists; the startup guard below fails closed if it does. Execution order: **R05b → R09h → R14b → R09i → R14d → R14c → R14e**. R14c requires all of R05b + R09h + R14b + R09i + R14d accepted (R09i waivable only by explicit PO sign-off).

- R14c owns the runtime server startup flag `--room-cutover-authoritative`. R14b does NOT introduce the flag, do NOT read it, and do NOT switch activity-writer or route registration based on it. R14b's binary is always pre-cutover; the flag only exists in the R14c binary.
- R14c composition selects the activity-writer and the legacy-route registration set based on the flag:
  - **`false` (pre-cutover / rollback):** real legacy global handlers + no-op `RoomActivityRepository`.
  - **`true` (production cutover):** repository-free `410 Gone` tombstone handlers + real `room_activities`-writing `RoomActivityRepository`.
- **Startup guard (fail closed).** If `--room-cutover-authoritative=true`, the server MUST refuse to serve traffic unless both:
  - schema version ≥ 9 (i.e. `0009_room_cutover_support.up.sql` has been applied), AND
  - the `room_cutover_marker` row exists.
  This is the durable activation proof: the marker is inserted in the same transaction as the atomic copy + `room_activities_id_seq` resequence, so its presence proves the cutover completed. Activation with `true` before this guard passes would enable room activity writes and/or disable global behavior before the copy and sequence resync have completed. The startup guard is marker + schema-version presence only; do NOT require copied target hashes to remain unchanged on every later startup (legitimate room writes change them).
- Before the window: `pg_dump` retained ≥30 days; R14b's binary deployed; R14b's verification gate has passed; R14d's SPA bundle accepted.
- During the window: shut down the application; run `cmd/room-cutover up` against the live DB (advisory lock; single transaction — room + host + copy + marker; commit; deferred `pg_advisory_unlock`); restart with `--room-cutover-authoritative=true` (the startup guard above passes because the marker row now exists and schema is v9).
- In this build:
  - R14d's SPA cutover-state gate is activated (the deployed `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` bundle flips legacy surfaces). Legacy surfaces (`DashboardView`, global queue slices, global WS) are removed/cleared/closed in the SPA.
  - Legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes and the global `/ws` endpoint are **registered with repository-free tombstone handlers** returning `410 Gone` with the documented envelope + `Link: </api/rooms>; rel="successor-version"`. The global `/ws` upgrade is rejected with `410` in the HTTP phase.
  - **R14c is atomic across all listed legacy routes.** No partial cutover.
- Legacy write paths disabled.
- **Deployment sequence during the window.** Public traffic remains closed for the entire duration. The order is:
  1. stop public traffic / enter maintenance;
  2. run `cmd/room-cutover up` and verify success;
  3. deploy the server configured `--room-cutover-authoritative=true` AND the SPA bundle built `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` while traffic remains closed;
  4. perform paired smoke checks against both artifacts;
  5. reopen traffic.
  The two artifacts MUST be deployed as a paired set; the compatibility matrix forbids `true` server + `false` SPA. Mid-window rollback likewise restores the immediately pre-cutover `false` server AND `false` SPA bundle as one pair before reopening traffic.

### Phase D — schema cleanup (R14e)

- Land `0010_drop_legacy_global_tables.up.sql` and matching `.down.sql`. The forward migration drops the **legacy global** tables: `queue_state`, `activities`, `auto_queue_config`, `play_history`. **It does NOT touch `room_play_history` or `room_activities`.** `0001_initial.up.sql` is NEVER rewritten.
- Schema v9 → v10.
- `0010_drop_legacy_global_tables.down.sql` recreates the legacy tables as empty shells; **it cannot restore dropped data**. The destructive cleanup is one-way; this is documented in the `.down.sql` header.

## 8. Atomicity & idempotency

The R14b mechanism is built fresh; it draws on R03 patterns where appropriate but does NOT reuse R03's CLI or marker. Marker is inserted in the same transaction as room, host membership, and copied state.

**Lock contract:**

- `pg_try_advisory_lock(987654321)` on a pinned connection (same key, same idiom as R03).
- The cutover transaction holds the lock for its entire duration.
- **`pg_advisory_unlock(987654321)` is called explicitly on the same pinned connection** (deferred unlock, mirroring R03's idiom) at the end of the transaction, regardless of commit or rollback.
- If the connection fails before the explicit unlock, the lock is released on connection close.

**No `abort` subcommand.** Lock release is via explicit `pg_advisory_unlock` on the same pinned connection (with connection close as fallback).

**Single transaction for `cmd/room-cutover up` (in this order):**

1. `BEGIN`.
2. `INSERT INTO rooms ...`.
3. `INSERT INTO room_members ...` (host).
4. Source-to-target copy.
5. `INSERT INTO room_cutover_marker ...` (marker is inserted before `COMMIT`, never after).
6. Pre-commit verification within the same tx.
7. `COMMIT` (or `ROLLBACK` on verification failure).
8. `pg_advisory_unlock(987654321)` on the same pinned connection.

**NEW** `room_cutover_marker` is distinct from R03's `migration_marker`. R03's marker is preserved untouched.

Marker fields: `room_cutover_id` (UUID), `target_room_slug`, `target_room_id`, `host_user_id`, per-source SHA256 hashes, per-target post-copy SHA256 hashes, `legacy_id_offset`, `cutover_pre_commit_at`, `binary_build_sha`.

Re-run on identical hash record = `already cut over; no-op`, exit 0. Driven by `room_cutover_marker`, NOT R03.

Hash drift on any source or target = rejected with an explicit sentinel. No `--force` / `--reset`.

Post-commit verification: `cmd/room-cutover verify` re-reads sources and targets, recomputes SHA256s, asserts they match the marker.

`cmd/room-cutover plan` (read-only); `--dry-run` (no writes commit); `config.RedactDSN` in the report.

Sequence resync via `setval(seq, MAX(id), is_called=true)`. R03's `setval` calls for `user_sessions` / `priority_transactions` / `activities` / `play_history` are unchanged.

The room-cutover CLI is offline; the application is shut down during Phase B.

**Migration files and rollback:** every migration in this work — `0009_room_cutover_support.{up,down}.sql`, `0010_drop_legacy_global_tables.{up,down}.sql` — ships a matching `.down.sql`. `0009.down.sql` drops both tables. `0010.down.sql` recreates empty legacy table shells and is documented as a one-way destructive cleanup that **cannot restore dropped data**.

## 9. WebSocket + frontend transition rules

- `/ws` is **retired with `410 Gone`** in the `--room-cutover-authoritative=true` build. The upgrade handler returns `410` during the HTTP phase.
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- R14d's behavioral activation is gated behind an explicit cutover-state flag; pre-R14c the SPA continues exposing the legacy dashboard.

## 10. Future sprint sequence

Sprint execution order is fixed at **`R05b → R09h → R14b → R09i → R14d → R14c → R14e`** (with R14e only after the verified rollback window). Reordering is NOT permitted.

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R05b — Room entry / player-lease UI** | Frontend only. SPA client methods (`api.createRoom`, etc.); room create / list / join / invite-redeem / lease-claim UI. **BLOCKING PREREQUISITE for R14c.** | Existing backend routes already registered. |
| **R09h — Room vote-to-prioritize parity** | Backend-only. Adds `POST /api/rooms/{slug}/vote/prioritize`. **BLOCKING PREREQUISITE for the entire R14c cutover.** | Reuses existing `room_queue_song_prioritized` event. |
| **R14b — room-cutover mechanism** | Backend-only. NEW dedicated CLI `cmd/room-cutover` with `plan` / `up` / `verify` (NO `abort`). NEW migration `0009_room_cutover_support.up.sql` (creates BOTH `room_activities` AND `room_cutover_marker`; schema v8 → v9) with matching `.down.sql`. NEW marker table with `cutover_pre_commit_at`. CLI flags `--room-slug` / `--room-name` / `--host-user-id`. Lock contract: deferred `pg_advisory_unlock(987654321)` on the same pinned connection (with connection close as fallback). NO global route retirement. NO frontend changes. R14b's gate MUST pass before R09i can run. | Does NOT reuse R03's CLI or `migration_marker`. R03 preserved untouched. |
| **R09i — Room activity runtime parity** | Backend-only. Wires room mutations to `RoomActivityRepository.AddActivity` on the same events that the global runtime today appends to `activities`. After R09i, the room runtime has a live write path. **R09i is BACKEND-ONLY;** the activity panel READ surface is a separate optional frontend decision. **BLOCKING PREREQUISITE for R14c** unless the PO explicitly waives. | The `room_activities` table is created in R14b. R09i wires call sites only. |
| **R14d — frontend global-path removal** | Frontend-only. Removes / redirects `DashboardView`; clears global slices; closes global WS; preserves `currentUser` + `roomQueues[slug]`. **Behavioral activation deferred to R14c** — implementation may be accepted before R14c but the SPA's behavior change activates only during the R14c maintenance window behind an explicit cutover-state gate (so the still-authoritative global queue remains reachable pre-cutover). | Assumes R05b has landed. |
| **R14c — coordinated production cutover** | Backend production cutover (Phase B). Maintenance window executes in this exact order: (1) close public traffic; (2) run and verify `cmd/room-cutover up` against the live DB; (3) deploy the `--room-cutover-authoritative=true` server AND the `VITE_ROOM_CUTOVER_AUTHORITATIVE=true` SPA bundle as a paired set while traffic remains closed; (4) paired smoke checks against both artifacts; (5) reopen traffic. ALL listed legacy global `/api/queue/...` / `/api/vote/...` / `/api/autoqueue/...` routes AND `/ws` retire simultaneously with `410 Gone` tombstone handlers + `Link: </api/rooms>; rel="successor-version"` header. The global `/ws` upgrade is rejected with `410` in the HTTP phase. R14d's SPA cutover-state gate activates. **ATOMIC across the listed legacy routes** — no partial cutover. | Requires R05b + R09h + R14b + R09i + R14d all accepted (R09i waivable only by explicit PO sign-off). |
| **R14e — schema cleanup** | Backend-only. Lands `0010_drop_legacy_global_tables.up.sql` (a SEPARATE forward migration that drops the legacy global `queue_state`, `activities`, `auto_queue_config`, `play_history` tables). **Does NOT touch `room_play_history` or `room_activities`.** Lands matching `.down.sql` (creates empty legacy table shells; **cannot restore dropped data**; documented as such in the `.down.sql` header). Bumps schema v9 → v10. Historical migration files `0001`..`0009` remain on disk byte-for-byte; `0001_initial.up.sql` is NEVER rewritten. | Runs after the verified rollback window AND after no runtime path references the legacy tables. |

R05b / R09h / R09i / R14b / R14c / R14d / R14e MUST NOT be combined.

## 11. Deployment compatibility matrix

The cutover involves **one runtime flag and one build-time flag, each owned by its respective side**:

- **Server side**: `--room-cutover-authoritative=true|false` is a **runtime server startup flag** (Go runtime arg parsed at `main()` startup; read once during composition; default `false`). It controls the legacy-route registration set AND the R09i activity-writer injection. It is NOT baked at build.
- **Client side**: `VITE_ROOM_CUTOVER_AUTHORITATIVE=true|false` is a **frontend build-time-only** setting (Vite `import.meta.env` baked at SPA build; two bundle variants — `false` shipped pre-R14c, `true` shipped as part of the R14c rollout).
- The Go backend NEVER consumes `VITE_ROOM_CUTOVER_AUTHORITATIVE` — the SPA flag does not cross the network boundary at runtime; it only ships inside the SPA bundle. Each side gates its own behavior; the gate pair (`--room-cutover-authoritative` server-side, `VITE_ROOM_CUTOVER_AUTHORITATIVE` SPA-side) MUST match in any deployed combination.

| Server binary (`--room-cutover-authoritative`) | SPA bundle (`VITE_ROOM_CUTOVER_AUTHORITATIVE`) | Legacy global routes | Per-room routes | Result |
| --- | --- | --- | --- | --- |
| Pre-R14c binary — flag does NOT exist on this binary (covers Pre-R14b, Post-R14b, Post-R09i, Post-R14d accepted) | `false` bundle | Registered; serve legacy repos | Registered | Pre-cutover baseline. R09i's no-op `RoomActivityRepository` is composed unconditionally. |
| R14c binary with `--room-cutover-authoritative=false` | `false` bundle | Registered; serve legacy repos | Registered | R14c binary in rollback / pre-cutover operational state. R09i's no-op writer is selected by the flag. Valid paired state. |
| R14c binary with `--room-cutover-authoritative=true` AND startup guard passed (schema ≥ 9 AND `room_cutover_marker` present) | `true` bundle | **Tombstone handlers; `410 Gone` envelope + `Link: </api/rooms>; rel="successor-version"`** | Registered; sole authoritative path | Production cutover state. ONLY valid post-R14c combination. |
| R14c binary with `--room-cutover-authoritative=true` AND startup guard failed (schema < 9 OR marker absent) | n/a | Server refuses to serve traffic; exits before opening listeners | n/a | Startup guard fail-closed. The `true` binary cannot start a pre-cutover or mid-window DB. |
| R14c binary with `--room-cutover-authoritative=true` | `false` bundle (forgotten rollout) | `410 Gone` tombstone | Registered | **BROKEN** — SPA still expects legacy endpoints but server returns `410`. Forbid this combination. |
| Mid-window rollback: immediately pre-cutover production release with `false` server AND `false` SPA bundle (paired) | OFF (gate not on) | Registered; serves legacy repos | Registered | Mid-window rollback. The `pg_dump` from the start of the window is the source of truth. Forward recovery for any data that mutated after the cutover. **Not an "R03 binary"** — R03 is the unrelated SQLite-to-PostgreSQL migration release. |

**Client-continuity claim:** the only combinations that guarantee client continuity are the documented ones. The frontend gate is the single mechanism `VITE_ROOM_CUTOVER_AUTHORITATIVE` matched against `--room-cutover-authoritative`. Pre-R14c, the SPA `false` bundle continues exposing the legacy dashboard so the global queue is reachable. The compatibility matrix forbids any combination where the server's flag is `true` and the SPA bundle is `false` — the deployed artifacts must match. The deployment contract during the R14c maintenance window is: stop public traffic; deploy the `true` server and the `true` SPA bundle as a paired set while traffic is closed; perform paired smoke checks; reopen traffic. Mid-window rollback restores the immediately pre-cutover `false` server AND `false` SPA bundle as one pair before reopening traffic.

## 12. PO acceptance blockers

1. **Room-activity runtime parity (R09i).** R09i is BACKEND-ONLY and is a BLOCKING PREREQUISITE for R14c unless the PO explicitly waives it (recorded on Issue #20).
2. **Live room-activity read parity (frontend panel).** Deferred as a separate optional frontend task; not in R09i or R14a scope.
3. **R09h landing prerequisite for the entire R14c cutover.** Partial cutover is forbidden.
4. **R05b landing prerequisite for user-facing cutover.**
5. **Route-retirement response posture.** `410 Gone` via tombstone handlers; `Link: </api/rooms>; rel="successor-version"` header; `/ws` rejected with `410` in the HTTP phase.
6. **R14d behavioral activation.** Gated behind an explicit flag activated during the R14c maintenance window; pre-cutover the SPA continues exposing the legacy dashboard so the still-authoritative global queue remains reachable.
7. **Rollback posture.** Rollback target is the immediately pre-cutover production release with `--room-cutover-authoritative=false` (NOT an "R03 binary" — R03 is the unrelated SQLite-to-PostgreSQL migration release).
8. **Schema-version plan approval.** v8 → v9 (R14b `0009`) → v10 (R14e `0010`); historical migrations preserved on disk; `0001_initial.up.sql` NEVER rewritten.
9. **`0010.down.sql` cannot restore dropped data** — destructive cleanup rollback is one-way.

## 13. Items already settled (informational)

- ID-collision handling for `room_play_history`: `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)`.
- Account-table posture: no recopy.
- Schema version plan: v8 → v9 (R14b) → v10 (R14e). Historical migrations preserved on disk.
- The R03 `cmd/migrate-data` + `migration_marker` is preserved untouched; it is NOT reused for the room cutover.
- The `entity.Queue` JSON conversion uses the default `encoding/json` tagged-field round-trip; no custom `MarshalJSON` / `UnmarshalJSON` methods are assumed.
- The queue-prioritize / vote-prioritize distinction: `/api/queue/prioritize` has a room equivalent; `/api/vote/prioritize` does not.
- The marker is inserted in the same transaction as the room, host membership, and copied state.
- Marker timestamp field is `cutover_pre_commit_at` (NOT `cutover_committed_at`) — written before `COMMIT`.
- Lock contract: deferred `pg_advisory_unlock(987654321)` on the same pinned connection (mirroring R03's idiom); connection close / `SIGTERM` is the fallback.
- `cmd/room-cutover` has NO `abort` subcommand.
- Retirement posture: `410 Gone` via repository-free tombstone handlers; `/ws` rejected with `410` in the HTTP phase.
- R09i is BACKEND-ONLY (write path; activity panel is a separate optional frontend task).
- R14b lands `0009_room_cutover_support.{up,down}.sql`, creating BOTH `room_activities` AND `room_cutover_marker`. The CLI does NOT create schema ad hoc.
- Each new migration ships a matching `.down.sql`. `0010.down.sql` cannot restore dropped data; the destructive-cleanup one-way behavior is documented in the `.down.sql` header.
- Sprint execution order is fixed: `R05b → R09h → R14b → R09i → R14d → R14c → R14e`.
- R14d's behavioral activation is gated behind an explicit cutover-state flag activated during the R14c maintenance window; pre-cutover the SPA continues exposing the legacy dashboard.
- R14c is atomic across the listed legacy global routes; no partial cutover.

## 14. Verification

- `git diff --check` clean.
- `git diff --name-only` contains only documents under `documents/00-project-management/`.
- ADR 001 body of §3–§18 is unchanged byte-for-byte except for the one metadata supersession note near the header.
- ADR 003 is referenced from `022-…md` and `ROOM_EPIC_SPRINT_SEQUENCE.md`.
- ADR 003 status is `Accepted` (Product Owner 2026-07-16); the R14a contract is implementation-ready.
- No `main`, `room 0`, "default room", "implicit room", or synthesized default-room `/ws` language appears in the R14a documents.
- No usage of `cmd/migrate-data` for the room cutover anywhere in the R14a documents.
- The R03 `migration_marker` is referenced only as "preserved untouched"; never as the room-cutover's marker.
- No concrete `successor: "/api/rooms/{slug}/..."` value in the `410` envelope.
- No `MarshalJSON` / `UnmarshalJSON` claim on `entity.Queue`.
- No partial-cutover language; R09h + R09i block the entire R14c cutover together.
- No `abort` subcommand on `cmd/room-cutover`; lock contract uses deferred `pg_advisory_unlock`.
- The marker is in the same transaction as room + host + copy; the marker timestamp is `cutover_pre_commit_at`.
- Historical migrations remain on disk; `0001_initial.up.sql` is NEVER rewritten.
- Migration `0009_room_cutover_support.{up,down}.sql` creates / drops BOTH `room_activities` AND `room_cutover_marker` (and indexes / constraints); CLI does NOT create schema ad hoc.
- Every new migration ships a matching `.down.sql`; `0010.down.sql` cannot restore dropped data.
- R14d's behavioral activation is gated behind an explicit cutover-state flag; pre-R14c the SPA continues exposing the legacy dashboard.
- R09i is BACKEND-ONLY (no read panel in R09i).
