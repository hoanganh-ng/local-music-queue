# Sprint 007 / R01 — PostgreSQL Migration Design

## Status

In progress — documentation-only. Awaiting Architect review and Product Owner approval.

## Sprint name

Sprint 007 / R01 — PostgreSQL Migration Design

## Goal

Author a PostgreSQL migration design ADR that decides whether PostgreSQL is confirmed as the target, picks the migration tooling, defines the local dev / deterministic test / production database strategies, designs the Docker Compose changes (as documentation only), defines backup/rollback, lays out the repository boundary plan, and details the SQLite-to-PostgreSQL data migration approach for users, sessions, priority transactions, queue_state JSON, activities, auto_queue_config, and play_history.

The sprint is documentation-only. No runtime code, no Docker Compose file change, no migration scripts, no tests, no deployment scripts.

## Current behavior

- The application persists all state in SQLite via the pure-Go `modernc.org/sqlite` driver.
- `persistence.NewSQLiteRepository(cfg.DBPath)` is constructed in [`cmd/server/main.go:95`](../../../cmd/server/main.go#L95) and initializes seven tables plus one index and one trigger ([`internal/infrastructure/persistence/sqlite_repository.go:39`](../../../internal/infrastructure/persistence/sqlite_repository.go#L39)).
- The user repo ([`internal/infrastructure/persistence/sqlite_user_repository.go:22`](../../../internal/infrastructure/persistence/sqlite_user_repository.go#L22)) and the auto-queue repo ([`internal/infrastructure/persistence/auto_queue_repo.go:22`](../../../internal/infrastructure/persistence/auto_queue_repo.go#L22)) share the same `*sql.DB` handle.
- Configuration is loaded by `config.Load()` ([`internal/infrastructure/config/config.go:23`](../../../internal/infrastructure/config/config.go#L23)). `DBPath` defaults to `./.localdb/music_queue.db`; no `DATABASE_URL` is read.
- The backend container `music-queue-backend` mounts `backend-db:/app/data` ([`docker-compose.yml:19`](../../../docker-compose.yml#L19)). There is no `postgres` service.
- The 19 REST endpoints and the 16-event WebSocket envelope are unchanged from the closed Sprint 003 baseline. There is no room context.
- ADR 001 ([`documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`](../ADRS/001-room-architecture-and-contracts.md)) established the room contracts and required PostgreSQL to be planned and implemented early, with R01 being the design sprint that must return a blocking reason if PostgreSQL is rejected.

## Desired behavior

The sprint must produce an approved PostgreSQL migration design ADR that:

- Confirms PostgreSQL as the target or documents a precise blocking reason.
- Recommends migration tooling.
- Defines the local development database setup.
- Defines the deterministic test database strategy.
- Defines production environment variables and secret-handling assumptions.
- Designs the Docker Compose change for later implementation.
- Defines backup and rollback assumptions.
- Defines the repository boundary plan for PostgreSQL-backed persistence.
- Defines the SQLite-to-PostgreSQL data migration approach for users, user_sessions, priority_transactions, queue_state JSON, activities, auto_queue_config, and play_history.
- Locks the compatibility requirement that R02 (PostgreSQL Foundation) preserves the current single-context behavior before any room behavior begins.
- Separates responsibilities between R02 (PostgreSQL Foundation), R03 (SQLite-to-PostgreSQL Data Migration), and R06 (Room-Scoped Persistence).
- Lists risks, deferred work, and verification categories for later implementation sprints.

## Required context

### Files/docs/tests/contracts to inspect

- `documents/00-project-management/PROJECT_STATE.md`
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`
- `documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`
- `documents/00-project-management/SPRINTS/active.md`
- `documents/00-project-management/SPRINTS/006-room-architecture-adr-contract-plan.md`
- `documents/00-project-management/SPRINTS/README.md`
- `README.md`
- `docker-compose.yml`
- `.env.example`
- `cmd/server/main.go`
- `internal/infrastructure/persistence/sqlite_repository.go`
- `internal/infrastructure/persistence/sqlite_user_repository.go`
- `internal/infrastructure/persistence/auto_queue_repo.go`
- `internal/infrastructure/persistence/migration_test.go`
- `internal/infrastructure/config/config.go`
- Nearby persistence/config files only as needed to document current storage composition.

### Unrelated areas not to scan/refactor

- Queue behavior implementation outside the persistence context.
- Voting implementation.
- Auto-queue implementation.
- WebSocket runtime.
- Frontend UI.
- YouTube / yt-dlp behavior.
- Auth implementation beyond configuration implications.

## Requirements

### Decision content for ADR 002

The ADR must explicitly decide and document:

1. **Target confirmation.** Whether PostgreSQL is confirmed as the target storage. If rejected, the ADR must give a precise blocking reason that names the dependency, the data, the operator, or the cost that blocks adoption.
2. **Migration tooling.** A specific tool (e.g. `golang-migrate`), the file format (`.up.sql` / `.down.sql`), the versioning scheme, and how migrations are embedded/applied.
3. **Local development database setup.** Image, port, volume, default credentials, `.env.example` additions, and the developer workflow.
4. **Deterministic test database strategy.** How `go test` obtains a clean, isolated PostgreSQL state per run (e.g. a per-run schema in a shared test container), how CI provisions it, and how state is cleaned up.
5. **Production environment variables and secret-handling assumptions.** `DATABASE_URL` (and optional overrides), TLS, pool sizing, secret injection, no committed credentials, and log redaction.
6. **Docker Compose design plan.** The new `postgres` service, a one-shot `db-init` migration job, the `backend` dependency change, the `postgres-data` volume, and a port-collision note. Plan only; do not modify the file in this sprint.
7. **Backup and rollback assumptions.** Backup method (`pg_dump --format=custom`), schedule, retention, restore drill, and the "restore-only, no in-place down-migration" rule.
8. **Repository boundary plan.** Preserve the `*sql.DB` boundary and the existing `QueueRepository`, `UserRepository`, `AutoQueueRepository` interfaces. Only the concrete implementation changes. Map SQLite types to PostgreSQL types.
9. **SQLite-to-PostgreSQL data migration approach.** For each of the seven tables (users, user_sessions, priority_transactions, queue_state, activities, auto_queue_config, play_history): the source, the target, the column mapping, the foreign-key remap strategy, and any trigger replacement (e.g. the 50-row play history cap).
10. **R02 compatibility requirement.** R02 must preserve the current single-context behavior — the 19 REST endpoints, the `/ws` route, the 16-event WebSocket envelope, the in-memory vote/auto-queue behavior — before any room behavior begins. The ADR must list the surfaces and the acceptance gate.
11. **Separation of responsibilities.** R02 builds the foundation. R03 runs the data migration. R06 folds the migrated state into the Product-Owner-named room. The ADR must give a clear table of who owns what.
12. **Risks, deferred work, and verification categories.** A risk register, a deferred-work list, and a verification plan for R02, R03, and R06.

### Compatibility and ordering constraints

- The ADR must respect ADR 001 §11 (queue JSON shape is preserved per room) and §12 (PostgreSQL timing).
- The ADR must not propose a schema that breaks the per-room JSON blob shape called out in ADR 001 §11.
- The ADR must not authorize any runtime change in this sprint.
- R02 (PostgreSQL Foundation) is the only sprint authorized to introduce the new driver.
- R03 (Data Migration) is the only sprint authorized to copy data between the two stores.
- R06 (Room-Scoped Persistence) is the only sprint authorized to add `room_id` columns and to bootstrap the Product-Owner-named room.

## Out of scope

- Runtime code changes.
- Backend, frontend, Docker Compose, migration, test, or deployment script changes.
- Implementation of any of R02, R03, R04, R05, R06, R07, R08, R09, R10, R11, R12, R13, R14.
- Commits, pushes, merges, issue closure, or sprint advancement by Builder.
- Choice of managed PostgreSQL provider.
- Cross-process / multi-instance room coordination.

## Implementation guidance

Create the ADR at:

- `documents/00-project-management/ADRS/002-postgresql-migration-design.md`

The ADR should be practical, decision-oriented, and explicit about what is decided now versus deferred to later sprints.

The ADR should follow the structural pattern established by ADR 001:

1. Context
2. Decision summary
3. Decision: PostgreSQL is the target
4. Migration tooling
5. Local development database setup
6. Deterministic test database strategy
7. Production environment variables and secret-handling assumptions
8. Docker Compose design plan
9. Backup and rollback assumptions
10. Repository boundary plan
11. SQLite-to-PostgreSQL data migration approach
12. R02 compatibility requirement
13. Separation of responsibilities: R02 vs R03 vs R06
14. Risks
15. Deferred work
16. Verification categories
17. Consequences for later sprints
18. Out of scope
19. Verification

Use the same link style as ADR 001: cross-repo file links with the correct relative prefix from the ADR path (e.g. `../../../cmd/server/main.go`); nearby doc links without the extra prefix.

## Execution Note

> **Status:** Sprint 007 / R01 is in progress. The ADR was authored and saved to `documents/00-project-management/ADRS/002-postgresql-migration-design.md`. No runtime, frontend, backend, configuration, deployment, migration, or test files were modified. PostgreSQL implementation, the data migration CLI, the Docker Compose change, the `.env.example` extension, the CI workflow change, and the schema migrations are NOT started. R02 (PostgreSQL Foundation) is NOT activated.

### ADR Location

- [`documents/00-project-management/ADRS/002-postgresql-migration-design.md`](../ADRS/002-postgresql-migration-design.md)

### Decisions Made

- PostgreSQL is confirmed as the target relational store. No blocking reason was found.
- `golang-migrate/migrate` v4 with the `pgx5` driver source is the migration tool. Migrations are SQL files, embedded via `embed.FS`, applied at process start. The schema migration CLI is `cmd/migrate-schema/main.go` (R02-owned, subcommands: `up`, `down <version>`, `force <version>`, `version`). The data copy CLI is a separate binary, `cmd/migrate-data/main.go` (R03-owned). The two CLIs do not share a name or surface.
- Local development uses a `postgres:16-alpine` Compose service with a named volume, healthcheck, and `DATABASE_URL` derived from `.env`. The default password is dev-only.
- Tests use a per-run `CREATE SCHEMA` inside a shared test PostgreSQL container. `TestMain` owns schema lifecycle; `search_path` scopes the `*sql.DB`. CI provisions a `postgres:16` service in the job.
- Production uses a single `DATABASE_URL` env var. No credentials are committed. Logs redact the password. TLS is required; `verify-full` is the design target.
- Docker Compose gains a `postgres` service, a `db-init` one-shot job (`Dockerfile.migrate` running `cmd/migrate-schema up`), and the `backend` service depends on both. The change is design-only in this sprint. The existing `backend-db` volume and SQLite source mount are **retained** through R02 AND R03; R03 (not R02) removes them after verifying the data migration.
- Backups are `pg_dump --format=custom`, nightly, 7-day retention minimum. Pre-migration snapshot retention is 30 days. Rollback is restore-only.
- The `*sql.DB` boundary and the existing `QueueRepository`, `UserRepository`, `AutoQueueRepository` interfaces are preserved. Only the concrete implementation changes. SQLite types are mapped to PostgreSQL (`INTEGER`/`AUTOINCREMENT` → `BIGSERIAL`, `DATETIME` → `TIMESTAMPTZ`, `INTEGER(0/1)` → `BOOLEAN`). The 50-row `play_history` cap trigger becomes a server-side `DELETE` after each insert.
- The SQLite-to-PostgreSQL data migration is a one-shot, idempotent, offline CLI (`cmd/migrate-data`) that copies all seven tables inside a single transaction, with an `lmq_migration` advisory lock and a per-table integrity report. It assumes R02's schema is already in place; it never applies schema migrations.
- R02 must preserve the current single-context behavior (19 REST endpoints, `/ws`, 16-event envelope, in-memory vote/auto-queue). R02's acceptance gate is parity against the closed Sprint 003 baseline.
- R02 owns the driver swap, the `postgres` / `db-init` Compose services, and the schema migration CLI (`cmd/migrate-schema`). R02 does **not** delete the SQLite source path, the `DB_PATH` fallback, the `backend-db` volume, or the SQLite repository implementations; those are removed by R03 only, after R03 verifies the data migration. R03 owns the data copy CLI (`cmd/migrate-data`). R06 owns the `room_id` columns, the migrated-room bootstrap, and the per-room play history cap. The ADR's §13 spells out the table of ownership.

### Schema migration execution ownership

The schema migration has three potential entry points; exactly one is authoritative per environment, the others are either absent or idempotent no-ops against the same embedded migration set:

- **`db-init` one-shot job** (`Dockerfile.migrate` running `cmd/migrate-schema up`) — authoritative in Compose. Declares `depends_on: postgres: condition: service_healthy` and exits 0 on success.
- **`backend` process startup hook** calling `migrate.Up()` against the same `embed.FS` — authoritative in non-Compose (`go run ./cmd/server`, CI). Called defensively in Compose as belt-and-braces, **after** `db-init` has succeeded.
- **`cmd/migrate-schema` CLI** — operator-driven only.

No race or drift is possible: all three paths read from the same `embed.FS`, and `golang-migrate`'s `schema_migrations` table serializes applies. In Compose, `backend` cannot race `db-init` because `backend` declares `depends_on: db-init: condition: service_completed_successfully`. The data migration CLI (`cmd/migrate-data`, R03) is separate and does not apply schema migrations; it assumes R02's schema is already in place.

- The ADR's risk register names 14 risks and the mitigation for each. The deferred-work list is the same as ADR 001's plus connection pooler, read replicas, and online schema migration tooling.

### Decisions Deferred

- Cross-process / multi-instance room coordination.
- Room-scoped vote sessions.
- Room-scoped auto-queue single-flight.
- Strong backend authorization on every endpoint.
- Per-song row storage in place of the JSON blob.
- Slug rename and archive recovery.
- Anonymous (read-only) room views.
- Cross-room moderation tools.
- Connection pooler (PgBouncer).
- Read replicas.
- Online schema migration tooling (e.g. `pg_repack`).
- Choice of managed PostgreSQL provider.

## Verification Results

- `git diff --check` — PASS (no whitespace/indent warnings)
- `git status --short --branch` — see output below.

```text
## dev...origin/dev
?? documents/00-project-management/ADRS/002-postgresql-migration-design.md
?? documents/00-project-management/SPRINTS/007-postgresql-migration-design.md
 M documents/00-project-management/SPRINTS/README.md
 M documents/00-project-management/SPRINTS/active.md
```

- Confirmation: no runtime, frontend, backend, configuration, deployment, migration, or test file was modified.
- Confirmation: PostgreSQL implementation, the data migration CLI, the Docker Compose change, the `.env.example` extension, the CI workflow change, and the schema migrations are NOT started.
- Confirmation: `ROOM_EPIC_SPRINT_SEQUENCE.md` was not modified (no contradiction, typo, or link/status correction was needed in this sprint).

## R02 verification gate — letsencrypt permission blocker

The R02 acceptance gate lists `go test ./...` PASS against the deterministic PostgreSQL test schema and `go test -race ./...` PASS (see ADR 002 §12 and §16). The pre-existing `letsencrypt-backend/accounts: permission denied` blocker documented in `PROJECT_STATE.md` is **orthogonal to R02's parity gate** because it is a filesystem-permission issue in the existing letsencrypt test fixture path, not a R02-introduced regression. R02 owns resolving that blocker for the full-suite tests if and only if the blocker blocks R02's parity run; otherwise the documented fallback is:

- **Scoped fallback verification (pre-existing blocker remains):** R02 runs the parity subset listed in ADR 002 §12 that does not touch the letsencrypt fixture path (queue interactor, user/priority/session usecases, HTTP delivery on the non-le test paths, the new PostgreSQL repository conformance tests, DSN-redaction log test, and `docker compose config`). The full-suite `go test ./...` is documented in the sprint log as "blocked by pre-existing letsencrypt permission; out of R02 scope; tracked separately." R02 does not silently shrink the acceptance gate; the shrink is recorded as a known deviation in the verification results.
- **Resolution path:** R02 owns resolving the letsencrypt permission blocker for the full-suite run when the blocker can be fixed with a fixture-path permission change inside the test harness (no secret material is exposed). If the fix requires changing operator permissions or volume mounts in production Compose, it is escalated to a separate ops sprint and R02 keeps the scoped fallback.

## Verification points

Because this is a documentation/ADR sprint, minimum verification is:

- `git diff --check`
- `git status --short --branch`

If any runtime, frontend source, backend source, config, or deployment file is changed unexpectedly, stop and report it instead of continuing.

## Risks and review focus

- Choosing PostgreSQL without surfacing a hidden blocker (e.g. operator cost, expertise gap, CGo requirement). The ADR's §3 names the alternatives and the rejected reasons.
- Migration tooling drift between dev and prod. The ADR picks `golang-migrate` for both, embedded in the binary.
- Test determinism if a shared test database is used. The ADR's §6 names a per-run schema and `search_path` scoping.
- Secret leakage via `.env.example`. The ADR's §5 requires explicit dev-only defaults and DSN redaction in logs.
- Backup/rollback being too lossy. The ADR's §9 specifies logical backups, retention windows, and a documented restore drill.
- The driver swap introducing a behavioral change. The ADR's §12 requires R02 to gate the cutover on a parity test run.
- R02/R03/R06 ownership boundaries being unclear. The ADR's §13 lists the boundary table.
- The 50-row play history cap not being preserved per room. The ADR's §13 names the per-room cap as R06's responsibility.

## Builder reasoning effort

High. The ADR decides cross-cutting storage choices that R02, R03, and R06 will all depend on. The decisions must be concrete enough to be implementable without re-debating them later.

## Handoff prompt for Builder

```text
You are Builder for Local Music Queue.

Sprint:
Sprint 007 / R01 — PostgreSQL Migration Design.

Product Owner approval:
Sprint R01 is approved and active. This is a documentation/ADR sprint only.

Do not commit, push, merge, start PostgreSQL implementation, modify runtime code, modify Docker Compose, modify migrations, modify tests, modify deployment scripts, or advance the sprint.

Goal:
Create the authoritative PostgreSQL migration design ADR before R02 (PostgreSQL Foundation) begins.

Required context:
- documents/00-project-management/PROJECT_STATE.md
- documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
- documents/00-project-management/ADRS/001-room-architecture-and-contracts.md
- documents/00-project-management/SPRINTS/active.md
- documents/00-project-management/SPRINTS/006-room-architecture-adr-contract-plan.md
- documents/00-project-management/SPRINTS/README.md
- README.md
- docker-compose.yml
- .env.example
- cmd/server/main.go
- internal/infrastructure/persistence/sqlite_repository.go
- internal/infrastructure/persistence/sqlite_user_repository.go
- internal/infrastructure/persistence/auto_queue_repo.go
- internal/infrastructure/persistence/migration_test.go
- internal/infrastructure/config/config.go

Unrelated areas not to scan/refactor:
- queue behavior implementation outside persistence context
- voting implementation
- auto-queue implementation
- WebSocket runtime
- frontend UI
- YouTube/yt-dlp behavior
- auth implementation beyond configuration implications

Tasks:
1. Inspect the required context and reconstruct the current SQLite storage composition accurately.
2. Create `documents/00-project-management/ADRS/002-postgresql-migration-design.md`.
3. In the ADR, decide and document:
   - PostgreSQL as the target (or the precise blocking reason)
   - migration tooling
   - local development database setup
   - deterministic test database strategy
   - production environment variables and secret-handling assumptions
   - Docker Compose design plan
   - backup and rollback assumptions
   - repository boundary plan for PostgreSQL-backed persistence
   - SQLite-to-PostgreSQL data migration approach for users, user_sessions, priority_transactions, queue_state JSON, activities, auto_queue_config, play_history
   - R02 compatibility requirement (preserve current single-context behavior)
   - separation of responsibilities between R02, R03, and R06
   - risks, deferred work, and verification categories for R02/R03/R06
4. Update `documents/00-project-management/SPRINTS/active.md` so Sprint R01 / 007 is the active sprint.
5. Update `documents/00-project-management/SPRINTS/README.md` to include Sprint 007 / R01 in the sprint index.
6. Only update `ROOM_EPIC_SPRINT_SEQUENCE.md` for a direct contradiction, typo, or link/status correction.
7. Do not modify runtime, frontend, backend, Docker Compose, migrations, tests, or deployment files.
8. Update this Sprint 007/R01 document with an execution note and verification results.

Verification:
- git diff --check
- git status --short --branch

Expected output:
- Files changed.
- ADR summary.
- Decisions made.
- Decisions deferred.
- Verification results.
- Confirmation that no runtime, frontend, backend, configuration, deployment, migration, or test files were modified.
- Confirmation that PostgreSQL implementation, the data migration CLI, the Docker Compose change, the .env.example extension, the CI workflow change, and the schema migrations are NOT started.
```
