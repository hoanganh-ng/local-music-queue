# Sprint 008 / R02 — PostgreSQL Foundation with Existing Behavior Preserved

## Status

Closed — Architect reviewed and Product Owner approved. Single-context behavior parity gate met. R02 is closed as the implementation sprint; R03 (SQLite-to-PostgreSQL Data Migration) is the next sprint to shape.

## Sprint name

Sprint 008 / R02 — PostgreSQL Foundation with Existing Behavior Preserved

## Goal

Introduce PostgreSQL as the relational store behind the existing single-context application without changing any REST endpoint, any WebSocket event, any queue JSON shape, or any in-memory behavior. The 19-endpoint REST surface, the `/ws` route, and the 16-event WebSocket envelope from the closed Sprint 003 baseline must continue to work unchanged when the backend is pointed at PostgreSQL. SQLite remains the fallback for the duration of R02 AND R03 (per ADR 002 §8 and §13).

## Current behavior (pre-R02 baseline)

- The backend persisted all state in SQLite via the pure-Go `modernc.org/sqlite` driver. [`persistence.NewSQLiteRepository`](../../../internal/infrastructure/persistence/sqlite_repository.go) initialized seven tables, one index, and the 50-row `play_history` trigger.
- Configuration was loaded by [`config.Load`](../../../internal/infrastructure/config/config.go) with `DBPath` defaulting to `./.localdb/music_queue.db`. No `DATABASE_URL` was read.
- Docker Compose had only `backend`, `frontend`, and the `backend-db` volume. There was no `postgres` service.
- The 19 REST endpoints and the 16-event WebSocket envelope were unchanged from the closed Sprint 003 baseline. No room context was present.
- ADR 002 (`documents/00-project-management/ADRS/002-postgresql-migration-design.md`) authorized the R02 boundary: R02 introduces the driver; R02 does NOT delete the SQLite source path, the `DB_PATH` fallback, or the `backend-db` volume; R03 removes them only after the data migration is verified.

## Desired behavior

- Backend reads `DATABASE_URL` (with optional `POSTGRES_*` overrides) and uses PostgreSQL when set; falls back to `DB_PATH` (SQLite) when `DATABASE_URL` is empty.
- Backend uses the `github.com/jackc/pgx/v5/stdlib` driver.
- All seven existing tables (`queue_state`, `activities`, `users`, `user_sessions`, `priority_transactions`, `auto_queue_config`, `play_history`) plus the `idx_play_history_played_at` index exist in PostgreSQL with the same column shape that the SQLite path exposed. The 50-row `play_history` cap is enforced server-side in the repository instead of by trigger (ADR 002 §10).
- The 19 REST endpoints, the `/ws` route, and the 16-event WebSocket envelope are unchanged.
- The single global `queue_state` JSON continues to be the source of truth. No `room_id` columns. No room runtime.
- Schema migrations are versioned SQL files, embedded via `embed.FS`, applied at process start, and exposed by `cmd/migrate-schema` (subcommands: `up`, `down <steps>`, `force <version>`, `version`).
- The DSN is sanitized before logging (password redacted). Tested in `internal/infrastructure/config/config_test.go`.
- The PostgreSQL `*sql.DB` opened by `setupApp` MUST stay open for the lifetime of the returned mux. `main` owns the `Close` via the cleanup closure; `setupApp` must not close it before returning.
- SQLite source path, `DB_PATH` fallback, `backend-db` volume, and SQLite repository implementations are RETAINED through R02 AND R03. They are removed by R03 only, after R03 has migrated and verified the data.

## Required context

### Files/docs/tests/contracts to inspect

- `documents/00-project-management/PROJECT_STATE.md`
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` (Sprint R02 section)
- `documents/00-project-management/ADRS/002-postgresql-migration-design.md`
- `documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`
- `documents/00-project-management/SPRINTS/active.md`
- `documents/00-project-management/SPRINTS/007-postgresql-migration-design.md`
- `documents/00-project-management/SPRINTS/README.md`
- `cmd/server/main.go`
- `cmd/server/main_test.go`
- `cmd/server/main_pglifecycle_test.go`
- `cmd/migrate-schema/main.go`
- `internal/infrastructure/config/config.go`
- `internal/infrastructure/config/config_test.go`
- `internal/infrastructure/persistence/migrations_postgres.go`
- `internal/infrastructure/persistence/migrator.go`
- `internal/infrastructure/persistence/migrations/postgres/0001_initial.up.sql`
- `internal/infrastructure/persistence/migrations/postgres/0001_initial.down.sql`
- `internal/infrastructure/persistence/postgres_repository.go`
- `internal/infrastructure/persistence/postgres_user_repository.go`
- `internal/infrastructure/persistence/postgres_auto_queue_repo.go`
- `internal/infrastructure/persistence/postgres_repository_test.go`
- `internal/infrastructure/persistence/postgres_migration_test.go`
- `internal/infrastructure/persistence/testutil_postgres_test.go`
- `internal/infrastructure/persistence/sqlite_repository.go` (preserved)
- `.env.example`
- `docker-compose.yml`
- `Dockerfile.migrate`

### Unrelated areas not to scan/refactor

- Room runtime, room persistence, room REST routes, room WebSocket.
- Data copy CLI (`cmd/migrate-data`) — R03 owned, not started.
- `room_id` columns — R06 owned, not started.
- Frontend room UI, frontend room routing, frontend room store.
- Backend authorization hardening.
- WebSocket hub internals beyond confirming parity.

## Requirements

### Driver and configuration

- Add `github.com/jackc/pgx/v5/stdlib` to `go.mod` and `go.sum`.
- Add `DATABASE_URL` and `POSTGRES_*` overrides to `config.Load()`. `DATABASE_URL` takes precedence; `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_SSLMODE` are honored when `DATABASE_URL` is empty.
- Redact DSN password in startup log lines; cover with `config_test.go`.

### Schema migrations

- One initial migration: `internal/infrastructure/persistence/migrations/postgres/0001_initial.up.sql` with the matching `0001_initial.down.sql`.
- Migrations embedded via `embed.FS` (`migrations_postgres.go`).
- Migrator surface in `migrator.go`: `RunEmbeddedMigrationsUp`, `RunEmbeddedMigrationsDown`, `RunEmbeddedMigrationsForce`, `EmbeddedMigrationsVersion`.
- Three execution paths (per ADR 002 §7):
  - `db-init` one-shot job (authoritative in Compose).
  - `backend` process startup hook (authoritative in non-Compose; defensive in Compose).
  - `cmd/migrate-schema` CLI (operator-driven).
- All three paths read the same `embed.FS`. `ErrNoChange` is not an error.
- The migrator must not close the caller's `*sql.DB` (newMigrator returns a no-op closer).

### Repositories

- Preserve the `*sql.DB` boundary and the existing `QueueRepository`, `UserRepository`, `AutoQueueRepository` interfaces.
- Add `postgres_repository.go` (queue), `postgres_user_repository.go` (user/sessions/priority), `postgres_auto_queue_repo.go` (auto-queue config + play history).
- Type mapping: `INTEGER`/`AUTOINCREMENT` → `BIGSERIAL`; `DATETIME` → `TIMESTAMPTZ`; `INTEGER(0/1)` → `BOOLEAN`.
- The 50-row `play_history` cap is enforced server-side in the repository (after each insert, prune oldest).
- The single global `queue_state` JSON blob is preserved verbatim (no per-row breakout).

### Backend process startup and DB lifecycle

- `setupApp` selects backend from `cfg.DatabaseURL`. When set, opens a `*sql.DB`, pings, and wraps the three concrete repositories.
- The `*sql.DB` is returned to `main` via the cleanup closure. `setupApp` must NOT close it before returning; doing so would invalidate every repository handle before `ListenAndServe`.
- `main` defers `cleanup()`. The cleanup closure calls `dbHandle.Close()` only for the PostgreSQL path; SQLite path owns its handle internally.
- Backend fails fast on unreachable PostgreSQL. No "degraded mode" in R02.

### Schema migration CLI

- `cmd/migrate-schema/main.go` exposes `up`, `down <steps>`, `force <version>`, `version`.
- Reads `DATABASE_URL` (falls back to `MIGRATE_DATABASE_URL` for operator ergonomics).
- Reuses the same embedded FS as the backend process startup.

### Docker Compose and deployment

- New `postgres` service (`postgres:16-alpine`) with healthcheck, `postgres-data` named volume, and `${POSTGRES_PORT:-5432}:5432` host port.
- New `db-init` one-shot service built from `Dockerfile.migrate`, depends on `postgres: condition: service_healthy`, `restart: "no"`, runs `migrate-schema up`.
- `backend` service gains `DATABASE_URL` env var, depends on `postgres: condition: service_healthy` and `db-init: condition: service_completed_successfully`.
- Existing `backend-db` volume and the SQLite source file are RETAINED for R02 AND R03.
- `.env.example` is extended with the `POSTGRES_*` defaults and the dev-only `DATABASE_URL` (with explicit "do not use in production" comment block).

### Tests

- `postgres_repository_test.go` — conformance tests against a per-test schema. Repository behavior mirrors SQLite.
- `postgres_migration_test.go` — `TestPostgresMigration_CleanSchema` (idempotent up, expected tables/index/default row) and `TestPostgresMigration_DownThenUp` (down drops tables within the search_path-scoped schema; re-up restores).
- `testutil_postgres_test.go` — `requirePostgresDSN` and `newPostgresDB` helpers; per-test `CREATE SCHEMA` + `search_path` scoping + cleanup.
- `main_pglifecycle_test.go` (newly tracked in R02 fix pass) — focused regression: `setupApp` must not close the PostgreSQL `*sql.DB` before returning the mux. Skips cleanly when no test DSN is available.
- `config_test.go` — DSN redaction, override assembly, env precedence.
- Existing `usecase/queue`, `usecase/auth`, `delivery/http`, `infrastructure/session`, `infrastructure/config` tests continue to pass unchanged.

## Out of scope

- Room runtime, room persistence, `room_id` columns, migrated-room bootstrap.
- SQLite-to-PostgreSQL data copy CLI (`cmd/migrate-data`) — R03.
- Removal of the SQLite source path, `DB_PATH` fallback, `backend-db` volume, or SQLite repository implementations — R03 only, after data migration is verified.
- Frontend room UI / routing / store.
- Backend authorization hardening.
- Cross-process / multi-instance room coordination.
- WebSocket hub refactor.

## Implementation guidance

Concrete files added or changed by R02 (22 files, +1963 / −120):

- `internal/infrastructure/config/config.go` — `DATABASE_URL`, `POSTGRES_*` overrides, `RedactDSN`, log redaction.
- `internal/infrastructure/config/config_test.go` — redaction + env precedence coverage.
- `internal/infrastructure/persistence/migrations_postgres.go` — embedded `embed.FS` for `migrations/postgres/*.sql`.
- `internal/infrastructure/persistence/migrations/postgres/0001_initial.up.sql` — seven tables, index, default `auto_queue_config` row.
- `internal/infrastructure/persistence/migrations/postgres/0001_initial.down.sql` — drops all seven tables.
- `internal/infrastructure/persistence/migrator.go` — `RunEmbeddedMigrationsUp/Down/Force`, `EmbeddedMigrationsVersion`, no-op closer.
- `internal/infrastructure/persistence/postgres_repository.go` — queue repository.
- `internal/infrastructure/persistence/postgres_user_repository.go` — user / sessions / priority transactions.
- `internal/infrastructure/persistence/postgres_auto_queue_repo.go` — auto-queue config + play history with server-side 50-row cap.
- `internal/infrastructure/persistence/postgres_repository_test.go` — repository conformance tests.
- `internal/infrastructure/persistence/postgres_migration_test.go` — schema migration tests (clean schema + down-then-up).
- `internal/infrastructure/persistence/testutil_postgres_test.go` — DSN helper + per-test schema helper.
- `cmd/migrate-schema/main.go` — operator-driven schema migration CLI.
- `cmd/server/main.go` — `initRepositories` selects backend by `cfg.DatabaseURL`; `setupApp` returns cleanup closure that owns `dbHandle.Close()` for the PostgreSQL path.
- `cmd/server/main_test.go` and `cmd/server/api_test.go` — minor adjustments to keep parity with the new `setupApp` signature.
- `cmd/server/main_pglifecycle_test.go` — focused DB lifecycle regression (force-added; was untracked after the initial commit and tracked during the R02 fix pass).
- `Dockerfile.migrate` — small multi-stage image for the `db-init` job.
- `docker-compose.yml` — `postgres` service, `db-init` job, `postgres-data` volume, `backend` dependency and `DATABASE_URL` env; `backend-db` volume retained.
- `.env.example` — `POSTGRES_*` defaults, dev-only `DATABASE_URL` with explicit warning block.
- `go.mod` / `go.sum` — `github.com/jackc/pgx/v5` and friends.

## Execution Note

> **Status:** Sprint 008 / R02 is closed. The PostgreSQL driver swap, schema migration tooling, concrete repositories, embedded migrations, `cmd/migrate-schema` CLI, Docker Compose `postgres`/`db-init` services, `.env.example` extension, and DSN redaction are all landed on the `dev` branch. The 19 REST endpoints, the `/ws` route, and the 16-event WebSocket envelope are unchanged. The single global `queue_state` JSON is the source of truth. No `room_id` columns. No room runtime. No data copy CLI.

### Scope Adherence

- **No room behavior.** No `room_id` columns, no room domain, no room REST routes, no room WebSocket event, no migrated-room bootstrap. All persistence is single-context.
- **No data migration.** `cmd/migrate-data` was not created. The SQLite source file, the `DB_PATH` fallback, the `backend-db` volume, and the SQLite repository implementations are retained. R03 owns the data copy and the source removal.
- **No refactor of unrelated code.** `main.go` was edited only to add the backend selection and the cleanup-closure ownership; the route registrations and the WebSocket hub wiring are untouched.

### R02 fix pass

A second fix pass was applied after the initial R02 push to resolve the remaining Architect review blockers:

1. **Schema existence check scoped to current schema.** `TestPostgresMigration_DownThenUp` failed because the post-down assertion queried `information_schema.tables` without a schema filter and matched `queue_state` from concurrent or leftover test schemas. The down migration itself was correct: it drops `queue_state` inside the `search_path`-scoped test schema. The assertion was scoped to `current_schema()` to match the actual contract. Implementation unchanged.
2. **DB lifecycle regression test tracked.** `cmd/server/main_pglifecycle_test.go` existed locally but was matched by the bare `server` rule in `.gitignore`. It was force-added and committed so the focused regression is tracked alongside the rest of the test suite.
3. **DB cleanup ownership already correct.** `main` owns cleanup for the server lifetime; `setupApp` does not close the PostgreSQL `*sql.DB` before returning the mux. `initRepositories` returns the `*sql.DB` handle for the PostgreSQL path so the caller can close it on shutdown; SQLite path owns its handle internally.

### Decisions Made

- Single `DATABASE_URL` env var with `POSTGRES_*` overrides as the design target. DSN assembled by `config.Load`.
- Embedded migrations via `embed.FS`; no drift between process startup and operator-driven migrations.
- `db-init` one-shot job is authoritative in Compose; `backend` startup hook is defensive belt-and-braces after `db-init` succeeds.
- `cmd/migrate-schema` is the schema CLI (`up` / `down <steps>` / `force <version>` / `version`). `cmd/migrate-data` is reserved for R03 and does NOT share the name or the surface.
- Migrator returns a no-op closer; it must not close the caller's `*sql.DB`.
- `main` owns `dbHandle.Close()` via the cleanup closure returned by `setupApp`.
- SQLite source path, `DB_PATH` fallback, `backend-db` volume, and SQLite repository implementations are RETAINED through R02 AND R03. R03 is the only sprint authorized to remove them.
- 50-row `play_history` cap is enforced server-side in the repository (after each insert, prune oldest). The SQLite trigger is intentionally not ported.

### Decisions Deferred

- Data copy CLI (`cmd/migrate-data`) — R03.
- Removal of SQLite source path, `DB_PATH` fallback, `backend-db` volume, and SQLite repository implementations — R03 only.
- `room_id` columns and migrated-room bootstrap — R06.
- Cross-process / multi-instance room coordination.
- Room-scoped vote sessions, room-scoped auto-queue single-flight, room-scoped priority balances.
- Backend authorization hardening.
- Per-song row storage in place of the JSON blob.
- Online schema migration tooling.
- Connection pooler (PgBouncer), read replicas.
- Slug rename and archive recovery.

## Verification Results

### Second-pass verification (R02 fix pass)

- `go build ./cmd/...` — PASS (no output)
- `go vet ./cmd/... ./internal/...` — PASS (no output)
- `LMQ_TEST_DATABASE_URL=... go test ./internal/infrastructure/config ./internal/infrastructure/persistence ./internal/infrastructure/session ./internal/usecase/queue ./internal/usecase/auth ./internal/delivery/http` — all `ok`; `internal/infrastructure/persistence` 1.4s
- `LMQ_TEST_DATABASE_URL=... go test -race ./internal/infrastructure/config ./internal/infrastructure/persistence ./internal/infrastructure/session ./internal/usecase/queue ./internal/usecase/auth ./internal/delivery/http` — all `ok`; `internal/infrastructure/persistence` 3.5s
- `YTDLP_PATH=... LMQ_TEST_DATABASE_URL=... go test ./cmd/server -run 'TestSetupApp|TestSetupApp_PostgresDBStaysOpen' -v` — `TestSetupApp_PostgresDBStaysOpen` PASS (0.07s), `TestSetupApp` PASS (0.02s)
- `docker compose --env-file .env.example config` — PASS; volume names listed
- `git diff --check` — PASS (no whitespace/indent warnings)
- `git status --short` — clean after the fix pass

### Full-suite caveat

- `go test ./...`, `go test -race ./...`, and `go vet ./...` remain blocked by the pre-existing `letsencrypt-backend/accounts: permission denied` filesystem permission issue documented in `PROJECT_STATE.md` and discussed in `SPRINTS/007-postgresql-migration-design.md`. This is orthogonal to R02 (the letsencrypt test fixture path is outside the persistence / config / queue / auth / session / delivery / cmd surface that R02 touches). The blocker is out of R02 scope and is tracked separately. R02 does not silently shrink the acceptance gate; the scoped fallback verification (the subsets listed above) is the recorded deviation per ADR 002 §12.

### Confirmation: parity

- REST endpoints: the 19 endpoints from the Sprint 003 baseline remain registered with the same paths and methods.
- WebSocket: `/ws` route and the 16-event envelope remain unchanged.
- Queue JSON shape: the single global `queue_state` blob is the source of truth; no per-row breakout; no `room_id`.
- In-memory behavior: vote sessions remain in-memory (30s expiry); auto-queue single-flight remains in-process; session store remains in-memory.
- Configuration: `DB_PATH` still works when `DATABASE_URL` is empty. The fallback is RETAINED for R02 AND R03.

## Verification points

Because R02 changes runtime behavior under a new backend, verification is the scoped subset above plus the following:

- `git diff --check`
- `git status --short`
- `docker compose config` against `.env.example`

If any room-related code or data-migration code is changed unexpectedly, stop and report it instead of continuing.

## Risks and review focus

- **SQLite / PostgreSQL divergence.** The PostgreSQL repositories must preserve the SQLite contract exactly. The conformance tests in `postgres_repository_test.go` are the parity gate.
- **Migrator closing the `*sql.DB`.** `newMigrator` returns a no-op closer on purpose; the test suite covers this implicitly. Reverting this to a real closer would close the active DB inside `setupApp` and break every DB-backed request.
- **DSN redaction regression.** Any new log line that prints a DSN must go through `RedactDSN` and be covered by a test.
- **Operator lock-in to docker-compose defaults.** The dev-only `DATABASE_URL` in `.env.example` is explicitly commented as such; production operators must override.
- **Race between `db-init` and `backend` startup.** Resolved by `depends_on: db-init: condition: service_completed_successfully`. Any change to the dependency graph must preserve this.
- **Letsencrypt permission blocker.** Pre-existing; out of R02 scope; tracked separately. Documented in `PROJECT_STATE.md`.

## Builder reasoning effort

High. R02 introduces the storage foundation, the migration tooling, the operator CLI, the Docker Compose change, and the lifecycle ownership for the database handle. Decisions about who owns cleanup, who owns the migrator closer, and who owns the source-path removal all matter for R03 and R06.

## Handoff prompt for the next Builder (R03)

```text
You are Builder for Local Music Queue.

Sprint:
Sprint R03 — SQLite-to-PostgreSQL Data Migration.

Product Owner approval:
Sprint R03 is to be shaped after R02 is closed. This handoff records
the constraints R03 inherits from ADR 002 and from the closed R02 sprint.

Do not commit, push, merge, open PRs, add room behavior, refactor unrelated
code, or advance beyond R03.

Goal:
Define and execute the SQLite-to-PostgreSQL data copy path so an
existing deployment's SQLite data (users, user_sessions, priority_transactions,
queue_state JSON, activities, auto_queue_config, play_history) can be
copied into the PostgreSQL schema that R02 already laid down.

Required scope (from ADR 002 §11):
- For each of the seven tables: source, target, column mapping, FK remap,
  and trigger replacement.
- One-shot, idempotent, offline CLI: cmd/migrate-data.
- Single transaction; advisory lock (lmq_migration); per-table integrity
  report.
- Pre-migration pg_dump snapshot retained 30 days.
- After R03 verification, remove the SQLite source path, the DB_PATH
  fallback, the backend-db volume, and the SQLite repository
  implementations (R02 retained them through R03).
- R03 does NOT add room_id columns. R06 owns that.

Required context:
- documents/00-project-management/PROJECT_STATE.md
- documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md (Sprint R03 section)
- documents/00-project-management/ADRS/002-postgresql-migration-design.md (§11 in particular)
- documents/00-project-management/SPRINTS/008-postgresql-foundation-with-existing-behavior-preserved.md
- documents/00-project-management/SPRINTS/active.md
- documents/00-project-management/SPRINTS/README.md
- cmd/migrate-schema/main.go (R02-owned; reference only)
- internal/infrastructure/persistence/sqlite_repository.go (source)
- internal/infrastructure/persistence/postgres_repository.go (target)
- internal/infrastructure/persistence/postgres_user_repository.go (target)
- internal/infrastructure/persistence/postgres_auto_queue_repo.go (target)
- internal/infrastructure/persistence/migrations/postgres/0001_initial.up.sql (target schema)
- .env.example
- docker-compose.yml

Out of scope:
- Room runtime, room_id columns, room REST routes, room WebSocket events.
- Frontend room UI / routing / store.
- Backend authorization hardening.
- Cross-process / multi-instance room coordination.
- Removing R02-introduced code (the migrated-schema, repositories, CLI,
  Compose services, env vars are R02-owned and stable).
```