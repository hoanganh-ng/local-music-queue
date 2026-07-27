# R14b – Room-cutover schema and offline migration mechanism

**Status:** R14b is the **sole active sprint on `dev`** (activated
2026-07-24; tracked as Issue #23; activation recorded on room epic
Issue #17; implementation-sequence update recorded on the accepted R14a
Issue #20). The R14b implementation was committed on `dev` at
`6b135c0e12ebbee63c713bbdff2a360b83f8e2a0`; an Architect review
(recorded on Issue #23) returned **needs fixes**. The first corrective
pass is **committed on `dev` at
`c8a73f1f20e81759828c0a9390fbb0d891e3513d` and pushed to the `github`
remote** (`github/dev`). A re-review (recorded on Issue #23) found a
narrow set of remaining issues, and the **final corrective pass**
(in-transaction pre-commit full marker verification, singleton-source
readiness, report-file permission forcing, tracker accuracy) is applied
**in the working tree on top of that committed head** — uncommitted and
**pending re-review and Product Owner acceptance**.
The Builder has performed no commit, push, merge, production migration action,
or sprint-status advancement, and no Product Owner acceptance is
claimed. R14b is the third sprint in the fixed cutover
order `R05b → R09h → R14b → R09i → R14d → R14c → R14e`; its blocking
prerequisites R05b and R09h are closed and accepted. `R09i`, `R14d`,
`R14c`, and `R14e` remain planned and NOT active.

**Sprint name:** Room-cutover schema and offline migration mechanism

## Goal

Ship — and prove correct offline — the mechanism that converts the
single legacy global state (`queue_state`, `activities`,
`auto_queue_config`, `play_history`) into exactly one Product-Owner-named
room, records a durable idempotency + integrity marker, and verifies the
copy with canonical SHA-256 hashes. R14b builds and verifies the
mechanism only; it does NOT run the production cutover, retire any legacy
route, or change any frontend surface. Those belong to R14c/R14d/R14e.

This slice conforms to ADR 003 (accepted 2026-07-16) and the R14a
contract in
[`022-legacy-global-state-migration-and-contract-retirement-plan.md`](./022-legacy-global-state-migration-and-contract-retirement-plan.md).

## Current behaviour (pre-sprint baseline)

- The legacy global state lives in the single-row `queue_state`
  (id = 1), the global `activities`, the singleton `auto_queue_config`
  (id = 1), and `play_history` — all created by `0001_initial.up.sql`.
- The room runtime (R07/R09/R10/R11) already persists per-room state in
  `room_queue_state`, `room_auto_queue_config`, and `room_play_history`
  (migrations 0006/0007), but there is **no** `room_activities` table
  and **no** migration path from the legacy global state into a room.
- R03's `cmd/migrate-data` + `migration_marker` handled the unrelated
  SQLite → PostgreSQL migration; they are NOT reused here.
- Schema version is 8 (after `0008_room_chat_messages`).

## Desired behaviour (post-sprint)

R14b lands a new migration, a new narrow repository, a dedicated
migration package, and a dedicated operator CLI — with no runtime
composition change to `cmd/server` and no route/WebSocket/frontend
changes.

### Migration `0009_room_cutover_support` (schema v8 → v9)

- `0009_room_cutover_support.up.sql` creates BOTH new tables; the CLI
  never creates schema ad hoc.
  - `room_activities` — the per-room activity target (R09i wires the
    runtime write path in a later slice; R14b only creates the table).
    `BIGSERIAL id`, `room_id` FK to `rooms`, plus
    `timestamp / type / user / description` mirroring the legacy
    `activities` columns, with a `(room_id, timestamp)` index.
  - `room_cutover_marker` — single-row (`id = 1`) durable cutover
    evidence. `target_room_id` and `host_user_id` are **immutable
    integer audit snapshots WITHOUT foreign keys** (migration evidence
    MUST NOT own room or user lifecycle). The timestamp field is named
    **`cutover_pre_commit_at`** (NOT `cutover_committed_at`) because the
    row is written before `COMMIT`, and it is **supplied explicitly by
    the CLI** (NOT `DEFAULT NOW()`). `source_hashes` and `target_hashes`
    are `JSONB` maps; `legacy_id_offset` is a `BIGINT`; `build_sha` and
    `room_cutover_id` (uuid) record provenance.
- `0009_room_cutover_support.down.sql` drops both tables. Historical
  migrations `0001`..`0008` remain on disk byte-for-byte;
  `0001_initial.up.sql` is NEVER rewritten.

### `RoomActivityRepository` (domain contract + Postgres impl)

- A narrow domain interface (`AddActivity`, `GetActivities`) with a
  `PostgresRoomActivityRepository` over `room_activities`, room-scoped,
  newest-first with a stable id tie-break and a bounded limit. It is
  **NOT** coupled to `QueueRepository` and is **NOT** composed into
  `cmd/server` in R14b (R09i owns the runtime wiring).

### `internal/infrastructure/persistence/roomcutover/` package

- `hashes.go` — canonical projection + SHA-256 hashing. `queue_state`
  is hashed via a **logical** projection: the TEXT/JSONB document is
  unmarshaled into `entity.Queue` (default `encoding/json` tagged-field
  round-trip; no custom `MarshalJSON` / `UnmarshalJSON`), its invariants
  are validated, `History` timestamps are normalized to UTC, and the
  re-marshaled canonical bytes are hashed — so a byte-different TEXT
  source and JSONB target compare equal. `activities`,
  `auto_queue_config`, and `play_history` are hashed by streaming their
  ordered rows as **typed, length-prefixed logical records** (uvarint
  field count, then uvarint byte length + canonical rendering per
  field), so no field or record boundary is ambiguous and no in-band
  byte value can collide with data. Timestamps — including
  `play_history.played_at`, which participates in BOTH the source and
  target projections — are normalized to **UTC RFC3339Nano** before
  hashing. The hash map keys are `queue_state`, `activities`,
  `auto_queue_config`, `play_history` → 64-char lowercase SHA-256 hex.
- `migrator.go` — `Plan` (read-only), `Up` (apply), `Verify`
  (re-assert). Identity is checked by a **single marker-derived
  verification routine** shared by all three paths: it validates the
  marker shape, resolves the marker's `target_room_id` to a live room,
  checks the actual room slug (and, on plan/up reruns, the supplied
  room name), confirms the recorded user still holds the `host`
  membership role, and recomputes source + target hashes and per-table
  counts against the marker. `Verify` takes NO caller-supplied
  identity — everything is derived from the durable marker.
  **First-cutover readiness** requires the target slug to be absent,
  the entire `room_activities` table to be empty, AND the legacy
  singleton source rows to be present — exactly one `queue_state` row
  (id = 1) and exactly one `auto_queue_config` row (id = 1) — so a
  missing singleton fails plan / dry-run / up up front instead of
  mid-cutover (checked in plan / dry-run / up preflight AND re-checked
  inside the locked transaction), plus a Go-side BIGINT overflow
  precheck for `MAX(play_history.id) + legacy_id_offset`. `Up` acquires
  `pg_try_advisory_lock(987654321)` on a
  pinned `*sql.Conn` (the SAME key R03 uses, so cutover / migration /
  a second cutover mutually exclude), releases via **deferred
  `pg_advisory_unlock(987654321)` on that same pinned connection** with
  connection close as the fallback, and performs the entire cutover in
  ONE transaction (order: insert room → insert host membership → copy
  state → resync sequences → in-transaction hash computation → capture
  `cutover_pre_commit_at` immediately before inserting the marker →
  **full pre-commit verification inside the same transaction** —
  source/target hash equality PLUS a reread of the just-inserted marker
  run through the same shared marker-derived verification routine
  (marker shape, resolved room/slug/name, host membership, recomputed
  hashes, per-table counts) — → `COMMIT`; a pre-commit verification
  failure therefore still rolls back the room, membership, copied rows,
  and marker). **After `COMMIT`,
  `Up` rereads the marker and re-runs the same verification routine
  against the committed state; the report claims `Verified` only after
  this post-commit proof passes.** A mid-flight fault rolls the
  whole transaction back and releases the lock. `Up` requires a
  non-placeholder build SHA before writing the marker.
- `report.go` — the PII-free integrity report (`WriteText` / `WriteJSON`
  / `WriteToFile`); every DSN is redacted via `config.RedactDSN`.

Copy rules:

- `queue_state` → `room_queue_state` (canonical JSONB).
- `activities` → `room_activities` **preserving ids verbatim** (the
  table is new/empty at v9).
- `auto_queue_config` → `room_auto_queue_config` keyed by `room_id`.
- `play_history` → `room_play_history` with
  `legacy_id_offset = COALESCE(MAX(room_play_history.id), 0)` recorded in
  the marker (the target table is shared since 0007). The target hash
  projection subtracts the offset to re-derive the legacy id space.
- `users` / `user_sessions` / `priority_transactions` are **NOT**
  recopied (account-scoped per ADR 001 §3 Decision 9).
- `room_activities` and `room_play_history` sequences are resynchronized
  after the copy (empty tables are skipped).

### `cmd/room-cutover` CLI

- Subcommands `plan` / `up` / `verify`. There is **NO** `abort`, `force`,
  `reset`, or `allow-hash-drift` subcommand: any drift fails explicitly
  and the operator investigates.
- Each subcommand parses its **own flag set** and registers ONLY the
  flags it accepts: `plan` — `--room-slug` / `--room-name` /
  `--host-user-id` / `--postgres` (DSN override; else `$DATABASE_URL`,
  else `$MIGRATE_DATABASE_URL`) / `--report-file`; `up` — plan's flags
  plus `--dry-run`; `verify` — `--postgres` / `--report-file` ONLY
  (identity is derived from the durable marker). An unregistered flag
  (e.g. `--dry-run` on `plan`, or `--room-slug` on `verify`) is a usage
  error.
- Exit codes: `0` success (including an already-cut-over no-op), `1`
  runtime / consistency / verification failure, `2` usage / flag /
  semantic error — including an invalid or reserved `--room-slug` and a
  missing DSN (no `--postgres`, `$DATABASE_URL`, or
  `$MIGRATE_DATABASE_URL`), both rejected before any connection
  attempt.
- `plan` and `up --dry-run` are fully read-only (sequences included)
  and report **expected-target evidence** — expected target hashes
  equal to the source hashes and expected per-table target counts —
  and `plan` rejects an already-taken target slug before reporting
  readiness.
- Report files (`--report-file`) are written with explicit mode `0600`
  (not left to the process umask), and a PREEXISTING report file is
  forced back to `0600` via `Chmod` — `O_CREATE`'s mode argument applies
  only to newly created files, so truncating an existing `0644` report
  must not leave it world-readable.
- Idempotency: re-running `up` with the same identity against an
  already-cut-over target re-hashes source + target, confirms they still
  match the marker, and prints `already cut over; no-op` (exit 0). Any
  drift on source OR target fails (exit 1).
- Readiness: the CLI refuses to run unless PostgreSQL is reachable,
  non-dirty, and at schema version EXACTLY 9.

### Explicit non-goals

- R14b does **NOT** introduce the runtime server startup flag
  `--room-cutover-authoritative` and does **NOT** select
  activity-writer implementations or route registration — those belong
  to R14c.
- No global route retirement (R14c). No frontend changes (R14d). No
  destructive schema drop (R14e). No composition of the new repository
  into `cmd/server` (R09i). Single-instance only; no cross-process
  safety claim beyond the advisory lock.

## Tests

Focused tests were added at each layer and run against a live
PostgreSQL instance (skipped, not failed, when PG is unreachable):

- **Hashing** (`roomcutover`, pure unit): queue invariant validation
  table; canonicalization rejects malformed / invariant-violating
  documents; `hashQueueJSON` is format-independent (reordered keys,
  whitespace, and a non-UTC timestamp hash identically); `hashesEqual`.
- **Migrator** (`roomcutover`, DB-backed): happy-path copy with
  row-level + id-preservation assertions; idempotent re-run reports
  `AlreadyCutOver` with no duplicate room; conflicting-host and
  slug-taken failures roll back; `Plan` and `--dry-run` write nothing;
  `Verify` happy path, missing-marker rejection, and source/target drift
  detection; `play_history` offset applied and hashes still equal;
  host-not-found; schema-version guards (below / above / dirty);
  advisory-lock contention; pre-commit and pre-marker fault rollback +
  lock release; placeholder build-SHA rejection; invalid / reserved slug
  rejection; a guard that `requiredSchemaVersion` matches the embedded
  migrations version.
- **CLI** (`cmd/room-cutover`): DSN precedence unit test; subprocess
  usage / flag-error exit codes (exit 2); `help` exit 0; an end-to-end
  `plan` → `up` → idempotent `up` → `verify` round trip (exit 0 each,
  durable marker + room asserted); `verify` without a marker (exit 1);
  and `--report-file` JSON output written.
- **Repository** (`persistence`, DB-backed): `RoomActivityRepository`
  round-trip, newest-first ordering, id tie-break, limit bounds,
  non-positive limit handling, and room scoping.
- **Corrective-pass regressions** (DB-backed unless noted):
  post-commit tamper detection after `COMMIT` (commit stands, exit is
  a verification failure); marker-only `Verify`; room-name drift on
  plan/up reruns; marker room deleted or slug-renamed; host membership
  demoted or deleted; `played_at` drift on source and on target;
  length-prefix ambiguity and UTC timestamp normalization (pure unit);
  preexisting `room_activities` rows rejected by plan / dry-run / up;
  `plan` slug-conflict rejection; plan + dry-run sequence invariance;
  BIGINT id+offset overflow rejection with nothing written; expanded
  CLI exit-2 matrix (invalid slug, reserved slug, missing DSN,
  identity flags on `verify`, `--dry-run` outside `up`); report-file
  `0600` permissions; accidental root `room-cutover` binary absence.
- **Final-corrective-pass regressions** (DB-backed unless noted):
  in-transaction tamper between marker insertion and the pre-commit
  verification pass is detected BEFORE `COMMIT` and rolls back
  everything (no room, no marker, no `room_activities` rows; the
  advisory lock is released and a clean retry succeeds); a missing
  `auto_queue_config` singleton and a missing `queue_state` row are each
  rejected by plan / dry-run / up with nothing written; a preexisting
  `0644` report file is forced to `0600` (pure-unit `report_test.go`
  regressions for both the create and preexisting-file paths, plus the
  CLI end-to-end `--report-file` test now seeding a `0644` file).

## Verification

Final-corrective-pass verification (2026-07-27, against a disposable
PostgreSQL 16 test container via `LMQ_TEST_DATABASE_URL`; DB-backed
tests run, not skipped):

- `go test ./internal/infrastructure/persistence/roomcutover/... ./cmd/room-cutover/... -count=1 -p 1`
  — all 47 top-level tests (83 including subtests) pass, 0 skips, 0
  failures (`ok` both packages).
- `go test -race ./internal/infrastructure/persistence/roomcutover/... ./cmd/room-cutover/... -count=1 -p 1`
  — pass (`ok` both packages).
- `go test ./internal/... ./cmd/... -p 1 -count=1` — all 24 test-bearing
  packages `ok`, zero failures.
- `go vet ./internal/... ./cmd/...` — clean.
- `git diff --check` — clean.

## Implementation summary (R14b)

Delivered files:

- `internal/infrastructure/persistence/migrations/postgres/0009_room_cutover_support.up.sql`
  + `.down.sql` (schema v8 → v9).
- `internal/domain/repository` `RoomActivityRepository` contract +
  `internal/infrastructure/persistence/postgres_room_activity_repository.go`
  + tests.
- `internal/infrastructure/persistence/roomcutover/` — `hashes.go`,
  `migrator.go`, `report.go`, plus `hashes_test.go`,
  `testhelpers_test.go`, `migrator_test.go`, and `report_test.go`.
- `cmd/room-cutover/` — `main.go`, `flags.go`, `main_test.go`.

The compiled root-level `room-cutover` binary that was accidentally
included in the baseline commit has been removed from the working tree
and `/room-cutover` added to `.gitignore` so it cannot be re-staged.

Preserved untouched: R03's `cmd/migrate-data` + `migratedata` package +
`migration_marker`, historical migrations `0001`..`0008`, the global
REST/WebSocket contracts, the per-room WebSocket inventory, voting,
playback, queue, auto-queue, chat, leases, priority balances, auth /
sessions, Docker / Nginx / deployment, and the entire frontend. The new
repository is intentionally NOT composed into `cmd/server` (R09i owns
that wiring). Schema version after R14b is 9.
