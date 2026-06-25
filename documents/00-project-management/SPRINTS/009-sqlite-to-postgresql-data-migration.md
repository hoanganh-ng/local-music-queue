# Sprint 009 / R03 — SQLite-to-PostgreSQL Data Migration

## Status

Closed — Architect reviewed and Product Owner approved. Documentation-only closure pass. R03 is the implementation sprint for the SQLite-to-PostgreSQL data copy described in ADR 002 §11. The next sprint to shape is **R04** per [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md).

## Sprint name

Sprint 009 / R03 — SQLite-to-PostgreSQL Data Migration

## Goal

Execute the offline, one-shot, idempotent SQLite-to-PostgreSQL data migration described in ADR 002 §11 so an existing deployment's SQLite data (users, user_sessions, priority_transactions, queue_state JSON, activities, auto_queue_config, play_history) is copied into the PostgreSQL schema that R02 already laid down. Once R03 verifies the copy, R03 is the only sprint authorized to remove the SQLite runtime fallback, the `DB_PATH` env path, the `backend-db` volume, and the SQLite repository implementations.

## Current behavior (pre-R03 baseline)

- R02 left PostgreSQL as the new driver while retaining the SQLite source path, the `DB_PATH` fallback, the `backend-db` volume, and the SQLite repository implementations. The backend selected backend by `cfg.DatabaseURL` and otherwise fell back to SQLite. See [008-postgresql-foundation-with-existing-behavior-preserved.md](./008-postgresql-foundation-with-existing-behavior-preserved.md) for the closure record.
- The R02 schema (version 1, migration `0001_initial`) covered the seven tables (`queue_state`, `activities`, `users`, `user_sessions`, `priority_transactions`, `auto_queue_config`, `play_history`), the `idx_play_history_played_at` index, and the `auto_queue_config` seed row. It did NOT yet carry a migration-identity column on `users` or a durable migration marker table.
- ADR 002 §11 required R03 to ship a one-shot, idempotent, offline CLI (`cmd/migrate-data`) plus the schema support it needs.

## Desired behavior (post-R03)

- Backend is PostgreSQL-only. The server refuses to start without `DATABASE_URL` (or `POSTGRES_*` overrides). The SQLite runtime fallback, the `DB_PATH` env path, the `backend-db` volume, and the SQLite repository implementations are REMOVED.
- PostgreSQL schema version is **3** after the embedded migrations run:
  - `0001_initial` — seven tables, index, `auto_queue_config` seed row.
  - `0002_legacy_id` — adds `users.legacy_id BIGINT NULL` plus a unique index `idx_users_legacy_id` so the data migrator can record the original SQLite `users.id` mapping per ADR 002 §11.
  - `0003_migration_marker` — adds a single-row `migration_marker` table carrying per-table SHA256 hashes so a second `migrate-data` run can prove the target is the exact result of a prior successful run.
- `cmd/migrate-data up` is the operator-driven one-shot CLI. It:
  - opens the SQLite source read-only via `modernc.org/sqlite` (still pure-Go, no CGo);
  - opens the PostgreSQL target via `pgx/v5/stdlib`;
  - refuses to run against a schema older than version 3, against a dirty schema, or against a target missing `users.legacy_id`;
  - acquires the `lmq_migration` advisory lock (`pg_try_advisory_lock(987654321)`) on a pinned connection;
  - copies all seven tables inside a single transaction (users → user_sessions → priority_transactions → queue_state → activities → auto_queue_config → play_history);
  - resyncs the BIGSERIAL sequences for `user_sessions`, `priority_transactions`, `activities`, and `play_history` so future runtime INSERTs cannot collide with migrated ids;
  - runs a pre-commit in-transaction integrity check (counts, id ranges, `queue_state` byte length) and a post-commit verification, then writes the durable `migration_marker` row;
  - on a second run against the exact same target: compares the durable marker hashes (and a fresh recomputation of live target hashes) against the source. Every match is treated as the exact prior result and the CLI prints `already migrated; no-op` and exits 0. Any mismatch is treated as a dirty target and the CLI returns a non-zero error with a remediation message.
- A `users.legacy_id` row preserves the original SQLite `users.id` so dependent tables (`user_sessions`, `priority_transactions`) can FK-remap through the new PostgreSQL `users.id` while keeping a stable SQLite-identity audit trail. R06 drops `legacy_id` after the `room_id` refactor absorbs the user-identity information.
- A `migration_marker` row carries the exact, durable proof that the target is the result of a successful migration. Hashes cover `source_path`, the SQLite file bytes, and per-table SHA256 over a canonical projection (with timestamps excluded so SQLite TEXT and PostgreSQL TIMESTAMPTZ byte streams compare cleanly).
- REST and WebSocket behavior is unchanged: the 19 REST endpoints, the `/ws` route, and the 16-event WebSocket envelope from the closed Sprint 003 baseline remain registered and untouched. The single global `queue_state` JSON remains the source of truth. **No room runtime was added. No `room_id` columns were added.** Room behavior remains owned by R04 onward per the room epic sequence.

## Required context (closed sprint inputs)

- ADR 002 (`documents/00-project-management/ADRS/002-postgresql-migration-design.md`) §11 in particular.
- Closed R02 sprint record: [008-postgresql-foundation-with-existing-behavior-preserved.md](./008-postgresql-foundation-with-existing-behavior-preserved.md).
- Closed R01 sprint record: [007-postgresql-migration-design.md](./007-postgresql-migration-design.md).
- Closed R00 sprint record: [006-room-architecture-adr-contract-plan.md](./006-room-architecture-adr-contract-plan.md).

## Implementation guidance

### Files added or changed by R03

- `cmd/migrate-data/main.go` — operator CLI. Subcommand: `up`. Flags: `--sqlite` (required), `--postgres` (DSN override), `--report-file`, `--chunk-size`, `--dry-run`. Exit codes 0 / 1 / 2 (success / error / usage).
- `cmd/migrate-data/main_test.go` — focused CLI tests around flag parsing, usage errors, and DSN selection precedence (`--postgres` > `DATABASE_URL` > `MIGRATE_DATABASE_URL`).
- `internal/infrastructure/persistence/migratedata/migrator.go` — `Run(ctx, Options) (*Report, error)`, the `Options` and `Report` types, `OpenSource`, idempotency probe (`probeIdempotency` / `runMarkerPresentChecks`), advisory-lock acquisition, copy functions (`copyUsers`, `copyUserSessions`, `copyPriorityTransactions`, `copyQueueState`, `copyActivities`, `copyAutoQueueConfig`, `copyPlayHistory`), `resyncSequences`, `verifyWithinTx`, `verifyAfterCommit`, `readMarker`, `insertMarker`. Owns its own SQLite and PostgreSQL connections; does NOT go through the domain repositories.
- `internal/infrastructure/persistence/migratedata/convert.go` — value-conversion helpers (`Bool0or1ToBool`, `RemapID`).
- `internal/infrastructure/persistence/migratedata/report.go` — `Report.WriteText`, `WriteJSON`, `WriteToFile`, `MergeSourceStats`, `AllTablesMatch`, `MarkMismatched`, `AddNote`. Renders the operator-facing integrity report (stdout text + optional JSON file).
- `internal/infrastructure/persistence/migratedata/migrator_test.go` — unit tests for conversion helpers and report rendering.
- `internal/infrastructure/persistence/migrations/postgres/0002_legacy_id.up.sql` / `0002_legacy_id.down.sql` — adds / removes `users.legacy_id` plus `idx_users_legacy_id`.
- `internal/infrastructure/persistence/migrations/postgres/0003_migration_marker.up.sql` / `0003_migration_marker.down.sql` — adds / removes the `migration_marker` table.
- `internal/infrastructure/persistence/migrator.go` — no functional change; the version probe now reports schema version 3 once R03 migrations are applied.
- `internal/infrastructure/persistence/migrations_postgres.go` — `embed.FS` now ships three migration files instead of one; the `PostgresMigrationsDir` constant is unchanged.
- `cmd/server/main.go` — `initRepositories` now refuses to start without `DATABASE_URL`; the SQLite runtime fallback was removed. The defensive `RunEmbeddedMigrationsUp` startup hook was retained (it is now over the version-3 schema).
- `internal/infrastructure/config/config.go` — `DatabaseURL` is documented as required; `DBPath` was removed from the `Config` struct; `buildDatabaseURL` returns `""` when neither `DATABASE_URL` nor any `POSTGRES_*` override is set; `Load()` logs a clear message that the server will refuse to start.
- `docker-compose.yml` — the `backend` service now declares `# PostgreSQL is the only persistence layer (R03).` next to the `DATABASE_URL` env line. The `backend-db` volume and SQLite source mount are REMOVED. The `db-init` one-shot job still runs `migrate-schema up` (now against the version-3 schema set).
- `.env.example` — adds an explicit "The server refuses to start without DATABASE_URL (R03)." warning block. The dev-only `DATABASE_URL` and `POSTGRES_*` defaults remain.
- `go.mod` / `go.sum` — `modernc.org/sqlite` retained ONLY for `cmd/migrate-data` and `internal/infrastructure/persistence/migratedata` (the offline SQLite source reader). `pgx/v5/stdlib` retained for the runtime backend.

### Decisions made

- `users.legacy_id BIGINT NULL` plus a unique index `idx_users_legacy_id` is the migration identity. R06 owns the drop after the `room_id` refactor.
- The `migration_marker` table is a single-row (`id=1`) record holding per-table SHA256 hashes plus the SQLite file-bytes SHA256 and the source path. It is the durable, exact no-op / idempotency proof.
- The migrator owns its own connections: read-only `modernc.org/sqlite` for the source, transactional `pgx/v5/stdlib` for the target. It does NOT go through the domain repositories because it copies pre-migration SQLite content verbatim, including the original SQLite `users.id` (recorded as `users.legacy_id`).
- The advisory lock key is `987654321`. Pinned-connection unlock prevents accidental session leaks.
- BIGSERIAL resync uses `setval(seq, max, is_called=true)` for `user_sessions`, `priority_transactions`, `activities`, and `play_history`. Empty tables skip the resync.
- The CLI subcommand surface is `up` only. There is no `--force` or `--reset`. A dirty target (rows present without a marker, marker present with hash drift, conflicting NULL `legacy_id` values) returns a non-zero error and instructs the operator to truncate or restore from snapshot.
- `migrate-data` redaction uses `config.RedactDSN` for any DSN it prints; passwords never appear in operator logs.

### Decisions deferred

- `room_id` columns and migrated-room bootstrap — R04+ per the room epic sequence.
- Per-room JSON blob shape changes — R06 per ADR 001 §11.
- Cross-process / multi-instance room coordination.
- Room-scoped vote sessions, room-scoped auto-queue single-flight, room-scoped priority balances.
- Backend authorization hardening.
- Per-song row storage in place of the JSON blob.
- Online schema migration tooling.
- Connection pooler (PgBouncer), read replicas.
- Slug rename and archive recovery.
- Pre-migration `pg_dump --format=custom` automation on the operator's pre-flight checklist (ADR 002 §11 names 30-day retention; R03 ships the migrator only, not a backup orchestration script).

### Scope adherence

- **No room behavior.** No `room_id` columns, no room domain, no room REST routes, no room WebSocket events, no migrated-room bootstrap. All persistence is single-context.
- **Source path removal.** The SQLite runtime path, `DBPath` config field, `backend-db` volume, and SQLite repository implementations were all REMOVED in R03. `cmd/migrate-data` and `internal/infrastructure/persistence/migratedata` retain `modernc.org/sqlite` because they need to read the SQLite source offline; no production process imports it.
- **No refactor of unrelated code.** REST routes, WebSocket hub, queue JSON shape, in-memory vote sessions, in-memory session store, auto-queue single-flight, and frontend code are unchanged.

## Verification Results (R03)

The scoped fallback subset below was recorded for R03 against the per-test `search_path`-scoped PostgreSQL schema. The full-suite `go test ./...` / `go test -race ./...` / `go vet ./...` remain blocked by the pre-existing `letsencrypt-backend/accounts: permission denied` filesystem permission issue documented in `PROJECT_STATE.md`; that blocker is orthogonal to R03 (the letsencrypt test fixture path is outside the R03 surface) and is tracked separately. R03 does not silently shrink the acceptance gate; the shrink is recorded as a known deviation, consistent with the R02 closure record.

- `go build ./cmd/...` — PASS (no output)
- `go vet ./cmd/... ./internal/...` — PASS (no output)
- `LMQ_TEST_DATABASE_URL=... go test ./internal/infrastructure/config ./internal/infrastructure/persistence ./internal/infrastructure/persistence/migratedata ./internal/infrastructure/session ./internal/usecase/queue ./internal/usecase/auth ./internal/delivery/http` — all `ok`
- `LMQ_TEST_DATABASE_URL=... go test -race ./internal/infrastructure/config ./internal/infrastructure/persistence ./internal/infrastructure/persistence/migratedata ./internal/infrastructure/session ./internal/usecase/queue ./internal/usecase/auth ./internal/delivery/http` — all `ok`
- `LMQ_TEST_DATABASE_URL=... go test ./internal/infrastructure/persistence -run 'TestPostgresMigration_CleanSchema|TestPostgresMigration_DownThenUp' -v` — `TestPostgresMigration_CleanSchema` PASS, `TestPostgresMigration_DownThenUp` PASS
- `LMQ_TEST_DATABASE_URL=... go test ./internal/infrastructure/persistence/migratedata -v` — all migratedata unit tests PASS (`TestBool0or1ToBool`, `TestRemapID`, `TestReport_TextFormat`, `TestReport_JSONFormat`, `TestReport_AllTablesMatch`, `TestReport_MergeSourceStats`)
- `LMQ_TEST_DATABASE_URL=... go test ./cmd/migrate-data -v` — CLI flag / DSN-precedence / usage tests PASS
- `YTDLP_PATH=... LMQ_TEST_DATABASE_URL=... go test ./cmd/server -run 'TestSetupApp|TestSetupApp_PostgresDBStaysOpen' -v` — `TestSetupApp_PostgresDBStaysOpen` PASS, `TestSetupApp` PASS
- `docker compose --env-file .env.example config` — PASS; `backend-db` volume no longer present; `postgres` and `db-init` services present
- `git diff --check` — PASS (no whitespace/indent warnings)
- `git status --short` — clean after the closure pass

### Confirmation: parity

- REST endpoints: the 19 endpoints from the Sprint 003 baseline remain registered with the same paths and methods.
- WebSocket: `/ws` route and the 16-event envelope remain unchanged.
- Queue JSON shape: the single global `queue_state` blob is the source of truth; no per-row breakout; no `room_id`.
- In-memory behavior: vote sessions remain in-memory (30s expiry); auto-queue single-flight remains in-process; session store remains in-memory.
- Configuration: `DATABASE_URL` (with `POSTGRES_*` overrides) is required; the SQLite fallback was removed.

## Verification points

Because R03 changes the migration story, runtime backend, and Docker Compose topology, verification is the scoped subset above plus:

- `git diff --check`
- `git status --short`
- `docker compose config` against `.env.example`

If any room-related code or unrelated runtime code is changed unexpectedly, stop and report it instead of continuing.

## Risks and review focus

- **`users.legacy_id` partial-index traps.** The index is intentionally non-partial so `ON CONFLICT (legacy_id)` works against multiple `NULL`s. Any change to a partial index would silently break `copyUsers`.
- **Marker hash drift.** The marker hashes exclude timestamps on purpose (SQLite TEXT vs PostgreSQL TIMESTAMPTZ). A change to the canonical column projection would silently break idempotency probes.
- **Sequence resync.** `setval(..., max, true)` is the chosen form. Switching to `is_called=false` would re-collide migrated ids. The migrator tests should be extended if resync semantics change.
- **Operator lock-in to docker-compose defaults.** The dev-only `DATABASE_URL` in `.env.example` is explicitly commented as such; production operators must override.
- **Race between `db-init` and `backend` startup.** Resolved by `depends_on: db-init: condition: service_completed_successfully`. Any change to the dependency graph must preserve this.
- **Dirty target handling.** A target with rows but no `migration_marker` row is rejected. A target with a marker that does not match the current source hashes is rejected. Operators must TRUNCATE or restore from snapshot before retrying.
- **Letsencrypt permission blocker.** Pre-existing; out of R03 scope; tracked separately. Documented in `PROJECT_STATE.md`.

## Builder reasoning effort

High. R03 ships an offline data migrator with an exact no-op / idempotency proof, a new identity column on `users`, a durable migration marker, advisory-locked transaction, and the source-path removal. Decisions about hash projections, sequence resync semantics, and dirty-target rejection all matter for R04+ and for operators running the CLI in production.

## Handoff prompt for the next Builder (R04)

```text
You are Builder for Local Music Queue.

Sprint:
Sprint R04 — the next room epic sprint after R03 closure, per
documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md.

Product Owner approval:
Sprint R04 is to be shaped after R03 is closed. This handoff records
the constraints R04 inherits from ADR 001, ADR 002, and the closed
R02 / R03 sprints.

Do not commit, push, merge, open PRs, add room behavior, refactor
unrelated code, or advance beyond R04.

Goal:
Continue the room epic on top of the PostgreSQL-only stack that R03
left in place. R03 removed the SQLite runtime fallback; the seven-table
schema is at version 3 with users.legacy_id and migration_marker in
place. Do NOT remove users.legacy_id or migration_marker; R06 owns
those changes per ADR 001 §11.

Required context:
- documents/00-project-management/PROJECT_STATE.md
- documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
- documents/00-project-management/ADRS/001-room-architecture-and-contracts.md
- documents/00-project-management/ADRS/002-postgresql-migration-design.md
- documents/00-project-management/SPRINTS/009-sqlite-to-postgresql-data-migration.md
- documents/00-project-management/SPRINTS/008-postgresql-foundation-with-existing-behavior-preserved.md
- documents/00-project-management/SPRINTS/007-postgresql-migration-design.md
- documents/00-project-management/SPRINTS/active.md
- documents/00-project-management/SPRINTS/README.md
- cmd/migrate-data/main.go (R03-owned; reference only)
- internal/infrastructure/persistence/migratedata/migrator.go (R03-owned; reference only)
- internal/infrastructure/persistence/migrations/postgres/*.up.sql (target schema)
- internal/infrastructure/persistence/postgres_*.go (target repositories)
- internal/infrastructure/config/config.go
- .env.example
- docker-compose.yml

Out of scope:
- Removing users.legacy_id or migration_marker — R06.
- Removing the modernc.org/sqlite dependency from cmd/migrate-data — the
  data CLI still needs the SQLite source reader.
- Restoring the SQLite runtime fallback — R03 removed it; it does not return.
- Backend authorization hardening.
- Cross-process / multi-instance room coordination.
```