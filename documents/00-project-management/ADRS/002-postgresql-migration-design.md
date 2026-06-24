# ADR 002 — PostgreSQL Migration Design

- Status: Proposed — awaiting Architect review and Product Owner approval
- Date: 2026-06-24
- Scope: Decides whether PostgreSQL is the target relational store, the migration tooling, local dev / CI database strategy, production secret handling, Docker Compose design plan, backup/rollback, repository boundary plan, and the SQLite-to-PostgreSQL data migration approach for users, sessions, priority transactions, queue_state JSON, activities, auto_queue_config, and play_history. Lays down the compatibility contract that the next implementation sprint (R02) preserves current single-context behavior before any room behavior begins, and clearly separates responsibilities between R02, R03, and R06. This ADR is documentation-only: no runtime change is authorized by it.

## 1. Context

The application currently persists all state in SQLite via the `modernc.org/sqlite` pure-Go driver, opened by `persistence.NewSQLiteRepository(cfg.DBPath)` in [`cmd/server/main.go:95`](../../../cmd/server/main.go#L95) and initialized through `(*SQLiteRepository).init()` in [`internal/infrastructure/persistence/sqlite_repository.go:39`](../../../internal/infrastructure/persistence/sqlite_repository.go#L39).

Current on-disk composition (reconstructed from `init()` and adjacent repos):

| Storage concern | SQLite location | Source |
| --- | --- | --- |
| Queue state | `queue_state` (single row, `id = 1`, JSON blob) | [sqlite_repository.go:41-45](../../../internal/infrastructure/persistence/sqlite_repository.go#L41) |
| Activities | `activities` (append-only, autoincrement id) | [sqlite_repository.go:47-53](../../../internal/infrastructure/persistence/sqlite_repository.go#L47) |
| Users | `users` (email UNIQUE, role, priority_balance) | [sqlite_repository.go:55-64](../../../internal/infrastructure/persistence/sqlite_repository.go#L55) |
| User sessions | `user_sessions` (`UNIQUE(user_id, session_date)`) | [sqlite_repository.go:66-74](../../../internal/infrastructure/persistence/sqlite_repository.go#L66) |
| Priority transactions | `priority_transactions` (FK to `users`) | [sqlite_repository.go:76-86](../../../internal/infrastructure/persistence/sqlite_repository.go#L76) |
| Auto-queue config | `auto_queue_config` (single row, `id = 1`) | [sqlite_repository.go:88-93](../../../internal/infrastructure/persistence/sqlite_repository.go#L88) |
| Play history | `play_history` (autoincrement id, `played_at` index, 50-row cap trigger) | [sqlite_repository.go:95-110](../../../internal/infrastructure/persistence/sqlite_repository.go#L95) |

The user repo ([`internal/infrastructure/persistence/sqlite_user_repository.go:22`](../../../internal/infrastructure/persistence/sqlite_user_repository.go#L22)) and the auto-queue repo ([`internal/infrastructure/persistence/auto_queue_repo.go:22`](../../../internal/infrastructure/persistence/auto_queue_repo.go#L22)) share the same `*sql.DB` handle exposed by `SQLiteRepository.DB()`. Configuration is loaded from environment variables by `config.Load()` in [`internal/infrastructure/config/config.go:23`](../../../internal/infrastructure/config/config.go#L23), with `DBPath` defaulting to `./.localdb/music_queue.db`. The runtime container is `music-queue-backend` and the volume `backend-db:/app/data` is declared in [`docker-compose.yml:19`](../../../docker-compose.yml#L19).

ADR 001 — Room Architecture and Contracts ([`001-room-architecture-and-contracts.md`](./001-room-architecture-and-contracts.md)) established the room domain, lifecycle, lease model, REST/WebSocket contract direction, and §11/§12 of that ADR explicitly require PostgreSQL to be adopted early unless R01 (this sprint) surfaces a blocking reason.

## 2. Decision summary

1. **PostgreSQL is confirmed as the target relational store** for the multi-room transformation. No blocking reason was found.
2. **golang-migrate** is the schema migration tool. Migrations are versioned, SQL-based, embedded into the backend binary, and applied at process start.

### Schema migration execution ownership

There are three potential entry points for schema migration. Exactly one is authoritative per environment, and the others are explicitly either absent or idempotent no-ops against the same shared embedded migration set:

| Entry point | Compose | Non-Compose (local `go run` / CI) | Authoritative? |
| --- | --- | --- | --- |
| `db-init` one-shot job (R02-built, runs `cmd/migrate-schema up`) | ✅ Runs `migrate-schema up` against `$DATABASE_URL` before `backend` starts. | ❌ Not used. | ✅ Authoritative in Compose. |
| `backend` process startup hook calling `migrate.Up()` against `embed.FS` | ✅ Called as a defensive belt-and-braces, **after** `db-init` has succeeded. | ✅ Authoritative when running `go run ./cmd/server` outside Compose. | ✅ Authoritative in non-Compose. |
| `cmd/migrate-schema` CLI (operator) | Available; not run by Compose. | Available; not run by CI / local by default. | Operator-driven only. |

**No race or drift is possible** because:

- All three paths read from the **same** `embed.FS` of `.up.sql` / `.down.sql` files. There is no second copy of the migrations on disk that can drift.
- `golang-migrate` writes applied versions to the `schema_migrations` table inside the same PostgreSQL database. The first path to run acquires the advisory lock / version-table lock and applies the missing versions; subsequent paths see an up-to-date `schema_migrations` and exit as a no-op (`ErrNoChange`).
- `db-init` declares `depends_on: postgres: condition: service_healthy` and exits 0 on success; `backend` declares `depends_on: db-init: condition: service_completed_successfully`. So in Compose, `backend`'s startup `migrate.Up()` cannot race `db-init` — by the time the backend process exists, `db-init` has already committed the schema (or exited non-zero and aborted the stack).
- The `backend` startup `migrate.Up()` is intentional belt-and-braces: it costs one extra round-trip (`SELECT MAX(version) FROM schema_migrations`) per process start and guarantees correctness if anyone ever runs `go run ./cmd/server` against a Compose-managed database by mistake.

The **data** migration CLI (`cmd/migrate-data`, R03) is **not** part of the schema-migration ownership above. It assumes R02's schema is already in place; it never applies schema migrations.
3. **Local development** uses a `docker-compose` PostgreSQL 16 service with a named volume and a developer-friendly default password, overridable by `.env`.
4. **Deterministic tests** use a dedicated per-run PostgreSQL schema (template + `CREATE SCHEMA`) inside a single PostgreSQL container; tests own schema lifecycle and never touch the developer's database.
5. **Production** uses managed PostgreSQL (RDS / Cloud SQL / self-hosted equivalent) and reads the DSN from a single `DATABASE_URL` env var. No credentials are baked into Compose, Dockerfiles, or the binary.
6. **Docker Compose** gains a `postgres` service plus a `db-init` one-shot job; backend and the new `db-init` job depend on Postgres health. The change is design-only in this sprint.
7. **Backup and rollback** are explicit: nightly logical backups (`pg_dump --format=custom`) plus a documented restore drill; rollback is "stop writing, restore from backup, redirect traffic."
8. **Repository boundaries** are kept exactly as today. The `*sql.DB` boundary and the existing `QueueRepository`, `UserRepository`, `AutoQueueRepository` interfaces survive. Only the implementation behind the boundary changes.
9. **SQLite-to-PostgreSQL data migration** is a one-shot, idempotent, offline, CLI-driven pipeline that produces a verifiable integrity report.
10. **R02 (PostgreSQL Foundation with Existing Behavior Preserved)** is the only sprint authorized to introduce the new driver and dual-write-or-cutover logic. R02 must preserve all 19 REST endpoints, the `/ws` route, the 16-event WebSocket envelope, and the existing in-memory vote/auto-queue behavior.
11. **R03 (SQLite-to-PostgreSQL Data Migration)** is the only sprint authorized to run the data migration. R06 (Room-Scoped Persistence Migration) reuses R03's idempotent migration to fold the global state into the Product-Owner-named room.
12. **Verification categories** for R02, R03, and R06 are listed at the end of this ADR.

## 3. Decision: PostgreSQL is the target

Decision: **PostgreSQL is confirmed as the target relational store for the multi-room epic.** No blocking reason was found.

Reasoning:

- ADR 001 §11/§12 require relational integrity (`FK`-enforced rooms, members, invites, room-scoped queue state, activities, auto-queue config, play history) and per-room `seq_num`/event scope that benefits from a real transaction model.
- The current SQLite design uses a single-row JSON blob for queue state and in-process maps for vote state. Room scoping needs many concurrent writers across rooms; SQLite's single-writer model is a real (if small-scale) bottleneck.
- The application already uses `database/sql` (no SQLite-specific query syntax in the repos), so a different driver can be swapped behind the existing `*sql.DB` handle.

Alternatives explicitly considered and rejected at this stage:

- **Stay on SQLite with WAL + per-process file-per-room.** Rejected: cross-process/multi-instance coordination is explicitly deferred in ADR 001 §16, but ADR 001 §3.7 also calls out that rooms should be designed against a relational store, and we want one durable target rather than a fragmented SQLite topology.
- **MySQL/MariaDB.** Rejected: not chosen; out of scope per Product Owner direction captured in ADR 001 §11.
- **Managed serverless only (e.g. Neon, Supabase).** Deferred: the deployment topology is self-hosted Compose; we standardize on the same Postgres dialect in dev and prod to keep R02 simple. The DSN abstraction supports managed variants later.

Blocking reasons explicitly considered and ruled out:

- **No operational expertise.** Ruled out: R02 onboarding includes a documented local `docker compose up` recipe; no operator work in this sprint.
- **Cost/hosting unavailable.** Ruled out: design supports either self-hosted or managed; the same `DATABASE_URL` works in both.
- **CGo required.** Ruled out: `github.com/jackc/pgx/v5/stdlib` is a pure-Go driver that registers with `database/sql` and is CGo-free.

## 4. Migration tooling

Decision: **`golang-migrate/migrate` v4**, invoked as a library from the backend at process start and as a standalone CLI for operator use.

| Concern | Decision |
| --- | --- |
| Library | `github.com/golang-migrate/migrate/v4` with the `pgx5` driver source and the `iofs` source for embedded migrations. |
| Migration file format | Plain `.up.sql` / `.down.sql` SQL files, one forward file and one reverse file per version. SQL is written against PostgreSQL 16 syntax. |
| Versioning | Numeric, sequential, monotonic, no gaps. `version` is `4` digits, zero-padded (e.g. `0004_add_room_members.sql`). The same library tracks applied versions in a `schema_migrations` table managed by `golang-migrate`. |
| Embedding | Migrations live under `internal/infrastructure/persistence/migrations/postgres/` and are embedded into the binary via `embed.FS`. The same embed is reused by the CLI subcommand. |
| Apply | `migrate.Up()` runs at backend startup. Migrations are idempotent against the `schema_migrations` table; reruns are no-ops. |
| Baseline | The first applied version creates the `schema_migrations` table and the initial schema (users, sessions, priority_transactions, activities, queue_state, auto_queue_config, play_history). The initial schema mirrors the existing SQLite shape 1:1 in PostgreSQL types. |
| CLI surface | **`cmd/migrate-schema/main.go`** (schema-only) exposes `up`, `down <version>`, `force <version>`, `version`. This CLI is owned by R02 and is the only authoritative CLI for *schema* migration. The data migration CLI is a separate binary, **`cmd/migrate-data/main.go`**, owned by R03 (see §11). The two CLIs do not share a name and do not share a surface; the old ambiguous `cmd/migrate` name is not used. |
| Database/sql compatibility | The `pgx5` driver registers with `database/sql` as `pgx`, so the existing `*sql.DB`-based repositories do not need to change their signatures. |
| Write barrier | All migrations are written for PostgreSQL semantics. No SQLite-specific syntax is preserved in the new files. |

Rejected alternatives:

- **`goose`.** Rejected: `golang-migrate` is more common in the Go ecosystem and is what the team has standardized on for prior projects. No technical blocker; tooling preference.
- **Hand-rolled `CREATE TABLE IF NOT EXISTS` like the current SQLite init.** Rejected: needs version tracking, down-migrations, and CI gates; a library is cheaper.

## 5. Local development database setup

Decision: **A dedicated `postgres` service in `docker-compose.yml`, plus a developer-friendly default password, overridable from `.env`.**

| Concern | Decision |
| --- | --- |
| Image | `postgres:16-alpine` (the LTS line in the 16.x series; minor version is the image tag default, pinned via a comment in the Compose plan). |
| Service name | `postgres`. Container name `music-queue-postgres`. |
| Port | `${POSTGRES_PORT:-5432}:5432` on the host. The default `POSTGRES_PORT` is not `1111`/`443`/`80` so it does not collide with existing services. |
| Volume | Named volume `postgres-data:/var/lib/postgresql/data` (decoupled from the existing `backend-db` volume so the SQLite file can co-exist during R02 dual-write windows if needed). |
| Env | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` are sourced from `.env` (defaults are committed-but-obviously-local). The default password in `.env.example` is `devpassword` and is documented as "do not use outside local development." |
| Healthcheck | `pg_isready -U $POSTGRES_USER -d $POSTGRES_DB` with a 5-second interval, 30-second timeout, 10 retries. Backend's `depends_on` uses `condition: service_healthy`. |
| Backend env | `DATABASE_URL=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable` is exposed to the `backend` container as an env var. |
| First-run bootstrap | The `db-init` one-shot service (see §8) runs `golang-migrate up` against `$DATABASE_URL` on every start. It is idempotent. |
| `go run` workflow | Developers running the backend without Compose must set `DATABASE_URL` to a local `postgres://...` DSN. `config.Load()` reads it via `getEnv("DATABASE_URL", "")`. |
| `.env.example` extension | A new section is added in the design plan with `POSTGRES_USER=lmq`, `POSTGRES_PASSWORD=devpassword`, `POSTGRES_DB=lmq`, `DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable`. The runtime change to `.env.example` is part of R02. |

## 6. Deterministic test database strategy

Decision: **A single shared PostgreSQL container, with each `go test` run using its own throwaway schema inside that database.**

| Concern | Decision |
| --- | --- |
| Container | A `docker-compose.test.yml` (or a `make test-db-up` target) brings up a `postgres:16-alpine` instance dedicated to the test run. The container is brought up by `make test` and torn down on exit. |
| Schema isolation | `go test ./...` triggers a `TestMain` (in a new `internal/infrastructure/persistence/testutil` package) that generates a unique schema name (`test_<unix_ns>_<pid>`), runs `CREATE SCHEMA`, applies migrations into that schema, and then runs the test suite with `search_path` set to that schema. |
| Cleanup | The schema is dropped with `DROP SCHEMA ... CASCADE` on test exit (deferred from `TestMain`). The container is removed by the harness. |
| Migrations | Migrations are applied via the same `golang-migrate` library code path used in production, with `migrate` configured to use the test schema's `search_path`. |
| Determinism | The schema is unique per `go test` invocation. No two runs share state. Tests cannot accidentally read a developer's local data because they do not touch the `public` schema. |
| Go test glue | `DATABASE_URL` is set to a DSN pointing at the test container; `search_path` is set on the connection (libpq option) so the `*sql.DB` is automatically scoped to the throwaway schema. No new package-level globals are introduced. |
| CI | GitHub Actions / CI runs the same `make test` target, which spins up a `postgres:16` service in the job, sets `DATABASE_URL` to the in-job service, and runs `go test ./...`. The CI service is provisioned by the workflow YAML; this is a workflow change, not a runtime code change, and is finalized in R02. |
| Re-running the SQLite path | The current `go test ./...` was already blocked by an unrelated `letsencrypt-backend/accounts: permission denied` blocker in `PROJECT_STATE.md`; that blocker is orthogonal to R02's parity gate. R02 owns resolving it for the full-suite run when the fix is a fixture-path permission change inside the test harness; otherwise R02 keeps a documented scoped-fallback verification (see §12). |

## 7. Production environment variables and secret-handling assumptions

Decision: **A single `DATABASE_URL` env var is the production source of truth for the database connection. Secrets are never committed. Compose, Dockerfiles, and the binary do not embed credentials.**

| Concern | Decision |
| --- | --- |
| Primary env var | `DATABASE_URL` (full libpq DSN, e.g. `postgres://user:password@host:5432/dbname?sslmode=require&pool_max_conns=10`). |
| Optional overrides | `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_SSLMODE` are honored as overrides if `DATABASE_URL` is not set. They are documented in the deployment guide in R02. |
| TLS | `sslmode=require` is the minimum for production. `sslmode=verify-full` is the recommended target and is the design default in the deployment guide. |
| Pool sizing | `pool_max_conns` is configurable through the DSN. R02 documents a sensible default (e.g. 10) and the operator can override. |
| Secret handling | All secrets are injected at runtime by the platform (Docker Compose `secrets:` block in production Compose files; managed DBs use the platform's secret manager). No plaintext passwords are checked into the repo. |
| `.env.example` | Continues to ship dev-only defaults with an explicit "do not use in production" comment. R02 updates `.env.example` to add the new vars; R02 is the implementation sprint. |
| CI / test | CI injects `DATABASE_URL` from a GitHub Actions secret / workflow variable. Local dev reads `.env`. The binary itself is credential-free. |
| Logging | The DSN is sanitized before logging (password is redacted). This is enforced in `config.Load()` and tested in R02. |
| Rotation | The DSN supports password rotation without code changes; the operator simply updates the env var and restarts. The application does not cache resolved DSN across restarts. |
| Bootstrap | The DSN must be reachable at process start. If unreachable, the backend fails fast and exits non-zero. There is no "degraded mode" in R02. |

## 8. Docker Compose design plan (R02 implements; R01 documents only)

Decision: **Add a `postgres` service, a `db-init` one-shot job, and a `postgres-data` volume. Add `DATABASE_URL` (and overrides) to the `backend` environment. The current `backend-db` volume and the SQLite source file (`music_queue.db`) are retained untouched for the duration of R02's cutover window AND through R03, because R03 is the sprint that actually copies that data into PostgreSQL. The SQLite source path, the `backend-db` volume, and the `DB_PATH`/SQLite fallback are only removed at the end of R03 (after R03 has migrated and verified the data), never at the end of R02.**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    container_name: music-queue-postgres
    environment:
      - POSTGRES_USER=${POSTGRES_USER}
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
      - POSTGRES_DB=${POSTGRES_DB}
    volumes:
      - postgres-data:/var/lib/postgresql/data
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $POSTGRES_USER -d $POSTGRES_DB"]
      interval: 5s
      timeout: 5s
      retries: 10
    restart: unless-stopped
    networks:
      - app-network

  db-init:
    build:
      context: .
      dockerfile: Dockerfile.migrate
    container_name: music-queue-db-init
    environment:
      - DATABASE_URL=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
    restart: "no"
    networks:
      - app-network

  backend:
    # existing service, retained
    environment:
      - DATABASE_URL=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
      db-init:
        condition: service_completed_successfully
    # ...

volumes:
  postgres-data:
    driver: local
```

| Concern | Decision |
| --- | --- |
| Compose version | Continues to use `version: '3.8'`. No `version` bump required for `condition: service_healthy` / `service_completed_successfully`. |
| Service ordering | `postgres` is the foundation. `db-init` waits for `postgres` healthy, runs `migrate up`, and exits 0 on success. `backend` waits for both `postgres` healthy and `db-init` success. |
| `Dockerfile.migrate` | A small multi-stage image that copies the migrations directory and the `cmd/migrate` binary, then runs `./migrate up` on container start. It is `restart: "no"` and exits cleanly. |
| Existing `backend-db` volume | Retained during R02's cutover window AND through R03. R03 is the only sprint authorized to remove it, and only after R03 has successfully migrated and verified the SQLite data in PostgreSQL. R02 does NOT remove it. |
| Existing `letsencrypt` volumes | Unchanged. Out of scope. |
| Port collisions | `POSTGRES_PORT` defaults to `5432`; explicitly documented to avoid host-port collisions with `BACKEND_PORT` (`443`/`1111`) and `FRONTEND_*_PORT` (`80`/`443`). |
| Backward compatibility for old setups | The backend continues to support the existing `DB_PATH` (SQLite) **for the duration of R02 AND R03**, gated by `DATABASE_URL` being empty. R03 is the only sprint authorized to force `DATABASE_URL` and remove the `DB_PATH` / SQLite source path, and only after R03's data migration has been verified. R02 does NOT remove the SQLite source path or the `DB_PATH` fallback. |

## 9. Backup and rollback assumptions

Decision: **Logical backups only for the first implementation. Documented restore drill. Rollback is "stop writing, restore from backup, redirect traffic." No in-place schema downgrade is supported.**

| Concern | Decision |
| --- | --- |
| Backup method | `pg_dump --format=custom --no-owner --no-privileges` against the production database. Output goes to encrypted object storage (S3 / equivalent). |
| Schedule | Nightly, with a 7-day retention minimum. R02 documents the operator's exact cron entry. |
| Pre-migration snapshot | R03 takes an explicit `pg_dump` snapshot before applying the SQLite-to-PostgreSQL data migration. The snapshot is retained for at least 30 days. |
| Pre-cutover snapshot | R02 takes an explicit `pg_dump` snapshot of the (still mostly empty) PostgreSQL database immediately before the live cutover, plus a final `sqlite3 .dump` of the source SQLite file. Both are retained. |
| Restore drill | R02 includes a documented "restore the latest backup into a scratch database and assert row counts against the post-migration report" procedure. The drill is run once during R02's CI; subsequent drifts are caught by R03/R06 verification. |
| Rollback strategy | Stop writes to the new database. Restore from the latest pre-cutover snapshot into a fresh database. Redirect traffic to a separate restored instance if needed. **There is no automated down-migration path** — the application is reverted by restoring state, not by downgrading the schema. |
| Rollback window | R02 documents that the rollback window is "as long as the snapshot is retained." R03 explicitly sets retention to 30 days for the migration snapshot. |
| Migration down-migrations | `.down.sql` files exist and are used for **local development** rollback. They are **not** used in production rollback. R02 enforces this in the deployment guide. |
| RTO / RPO | Documented in the deployment guide as TBD by the operator, with the note that logical backups are typically sufficient for this workload (small DB, infrequent writes from the queue interactor's perspective). |

## 10. Repository boundary plan for PostgreSQL-backed persistence

Decision: **The `*sql.DB` boundary and the existing repository interfaces are preserved verbatim. Only the concrete implementation behind the boundary changes. No use case, handler, or domain code is allowed to import a driver-specific type.**

| Concern | Decision |
| --- | --- |
| `*sql.DB` | Continues to be the boundary. `*SQLiteRepository.DB()` is renamed to `*PostgresRepository.DB()` (or replaced by a small `type Database interface { DB() *sql.DB }` indirection — finalized at R02 implementation time). The repo constructor accepts a `*sql.DB` and is driver-agnostic at the type level. |
| `QueueRepository` interface | Unchanged. `*PostgresQueueRepository` implements it. |
| `UserRepository` interface | Unchanged. `*PostgresUserRepository` implements it. |
| `AutoQueueRepository` interface | Unchanged. `*PostgresAutoQueueRepository` implements it. |
| Wiring in `main.go` | `persistence.NewPostgresRepository(cfg.DatabaseURL)` is the new entry point. The call to `persistence.NewSQLiteRepository(cfg.DBPath)` is removed at the end of R02. R02's commit log shows the swap. |
| `go.mod` | Adds `github.com/jackc/pgx/v5` and `github.com/golang-migrate/migrate/v4`. Removes `modernc.org/sqlite` after the cutover (R02). |
| `embed.FS` for migrations | Migrations are read from the embed FS by the `golang-migrate` library, not from disk. This keeps the binary self-contained. |
| Use cases | Untouched. `usecase/queue`, `usecase/priority`, `usecase/vote`, `usecase/autoqueue` continue to depend on the repository interfaces only. |
| Handlers | Untouched. The HTTP and WebSocket layers continue to depend on use cases only. |
| Tests | The existing `sqlite_repository_test.go` and `auto_queue_repo_test.go` are kept as a fallback during R02, then deleted at the end of R02. New `postgres_repository_test.go` is added in R02 against the deterministic test schema. |
| Type mapping | The migration script defines the canonical PostgreSQL types. `INTEGER PRIMARY KEY AUTOINCREMENT` becomes `BIGSERIAL PRIMARY KEY`. `DATETIME` becomes `TIMESTAMPTZ`. `TEXT` stays `TEXT`. `INTEGER` stays `INTEGER`/`BIGINT` as appropriate. |
| JSON storage | `queue_state.data` remains `TEXT` containing JSON. The application does not use SQLite's `JSON1` extension, so no PostgreSQL `jsonb` conversion is required in R02. (R06 may revisit.) |
| Triggers | The `trg_play_history_cap` SQLite trigger is replaced by a server-side cap (see §13). No PostgreSQL trigger is required. |

## 11. SQLite-to-PostgreSQL data migration approach

Decision: **A one-shot, idempotent, offline CLI that reads from the existing SQLite file and writes to PostgreSQL inside a single transaction, producing a written integrity report. R03 implements the CLI; R06 reuses it via a `room_id` parameter to fold the global state into the Product-Owner-named room.**

| Table | Source | Target | Notes |
| --- | --- | --- | --- |
| `users` | All rows | `users` (PostgreSQL) | Email is the natural key. Preserve `priority_balance`, `role`, `display_name`, `profile_picture`, `created_at`, `updated_at`. Map `id` (SQLite autoincrement) to the new `BIGSERIAL` and persist a `legacy_id` column for the duration of R03. The `legacy_id` is removed at the end of R06. |
| `user_sessions` | All rows | `user_sessions` | `session_date` becomes `DATE`. `first_seen_at` and `last_seen_at` become `TIMESTAMPTZ`. Foreign key remap uses the new `users.id`. |
| `priority_transactions` | All rows | `priority_transactions` | Same column-by-column. Foreign key remap uses the new `users.id`. |
| `queue_state` | The single row with `id = 1` | `queue_state` (single row with `id = 1`) | JSON is preserved verbatim. `updated_at` becomes `TIMESTAMPTZ`. In R06, this row is the source for the migrated room's `queue_state`. |
| `activities` | All rows | `activities` | All columns preserved. `timestamp` becomes `TIMESTAMPTZ`. In R06, every row gets `room_id` set to the migrated room's id. |
| `auto_queue_config` | The single row with `id = 1` | `auto_queue_config` (single row with `id = 1`) | `enabled` (0/1) becomes `BOOLEAN`. `strategy` stays `TEXT`. In R06, this row becomes the migrated room's `auto_queue_config`. |
| `play_history` | All rows | `play_history` | `played_at` becomes `TIMESTAMPTZ`. The 50-row cap trigger is replaced by a server-side cap (see §13). In R06, every row gets `room_id` set to the migrated room's id. |

### Migration pipeline (R03 implements)

| Step | Action |
| --- | --- |
| 1 | Acquire an exclusive advisory lock `lmq_migration` on the PostgreSQL connection. |
| 2 | Open the source SQLite file in read-only mode (`mode=ro`). |
| 3 | Open a single PostgreSQL transaction. |
| 4 | For each table, in the order above, copy rows in chunks of 1000 inside the transaction. |
| 5 | On success, commit. On any error, roll back. |
| 6 | After commit, run a verification query: per-table `COUNT(*)`, `MIN(id)`, `MAX(id)`, and (for `queue_state`) `OCTET_LENGTH(data)`. Persist the report to a file or stdout. |
| 7 | Release the advisory lock. |
| 8 | Exit 0 on success, non-zero on failure. The script is idempotent: re-running it after a successful run is a no-op (it checks the advisory lock and the row counts and exits early). |

### Operator steps (R03 documents)

1. Stop the backend.
2. Take a `sqlite3 .dump` of the current database and copy it to a safe location.
3. Run the data copy CLI (R03-owned, distinct from R02's schema CLI): `lmq-migrate-data up --sqlite ./music_queue.db --postgres $DATABASE_URL`.
4. Inspect the integrity report.
5. Start the backend against the new `DATABASE_URL`.
6. Verify the dashboard renders the same queue state as before.
7. Retain the SQLite dump for the documented rollback window (30 days).

### Rollback limits (R03 documents)

- **No automatic down-migration.** The CLI does not implement `down`.
- **Rollback is restore-only.** The operator must restore the previous PostgreSQL state from the pre-migration snapshot OR restart the backend against a restored SQLite file. R02's "SQLite fallback" path is removed by the end of R02, so R03's restore must use the pre-migration PostgreSQL snapshot.
- **Identity remap is one-way.** Once `users.id` is remapped and `priority_transactions.user_id` is rewritten, there is no automatic way to revert to the original `legacy_id` mapping. The mapping is preserved in the `legacy_id` column for audit only.

## 12. R02 compatibility requirement: preserve current single-context behavior

Decision: **R02 introduces PostgreSQL without changing any observable single-context behavior. The 19 REST endpoints, the `/ws` route, the 16-event WebSocket envelope, the in-memory vote sessions, and the single-process auto-queue all behave identically to the closed Sprint 003 baseline.**

| Surface | Pre-R02 (today) | R02 (after) | Room behavior |
| --- | --- | --- | --- |
| `POST /api/auth/google` | Google OAuth, session token | Identical | Not started |
| `GET /api/queue` | Reads `queue_state` JSON | Identical | Not started |
| `POST /api/queue/{add,skip,status,sync,ended,prev,remove,clear,volume,prioritize}` | Mutate queue | Identical | Not started |
| `GET /api/user/priority-balance` | Reads `users.priority_balance` | Identical | Not started |
| `GET /api/youtube/search` | yt-dlp search | Identical | Not started |
| `POST /api/vote/{skip,prioritize}` | In-memory vote sessions | Identical | Not started |
| `POST /api/autoqueue/toggle` / `GET /api/autoqueue/status` | Reads/writes `auto_queue_config` row | Identical | Not started |
| `GET /ws` | Single global hub | Identical | Not started |
| WebSocket events | 16-event envelope from `internal/delivery/ws/events.go` | Identical | Not started |

R02's acceptance gate is: **run the closed Sprint 003 verification commands (or their R02 equivalents) against the PostgreSQL-backed binary and observe no behavioral change**. Specifically:

- `go test ./internal/usecase/auth ./internal/usecase/queue ./internal/delivery/http ./internal/infrastructure/session` — PASS
- `go test -race ./...` — PASS against the deterministic test schema
- `cd frontend && npm run test:unit -- --run` — PASS
- `cd frontend && npm run build` — PASS
- `docker compose config` — PASS

### R02 verification gate — pre-existing letsencrypt permission blocker

The pre-existing `letsencrypt-backend/accounts: permission denied` blocker documented in `PROJECT_STATE.md` is orthogonal to R02's parity gate: it is a filesystem-permission issue in the existing letsencrypt test fixture path, not a R02-introduced regression. R02's policy on this blocker is:

- **Default (R02 owns the fix):** R02 resolves the blocker for the full-suite run when the fix is a fixture-path permission change inside the test harness (no secret material exposed, no production volume-mount change). The full-suite `go test ./...` must pass before R02 closes.
- **Scoped fallback (pre-existing blocker remains):** If the fix requires changing operator permissions or production Compose volume mounts, R02 escalates that work to a separate ops sprint and keeps a scoped fallback verification: the parity subset above (queue interactor, user/priority/session usecases, HTTP delivery on non-le paths, the new PostgreSQL repository conformance tests, DSN-redaction log test, `docker compose config`) must still PASS. The full-suite shrink is recorded as a known deviation in the R02 verification log, not silently dropped from the gate.

## 13. Separation of responsibilities: R02 vs R03 vs R06

| Concern | R02 (PostgreSQL Foundation) | R03 (Data Migration) | R06 (Room-Scoped Persistence) |
| --- | --- | --- | --- |
| Driver swap | ✅ Adds `pgx`. **Keeps `modernc.org/sqlite` available** behind the `DB_PATH` / SQLite source path through R02 AND R03. R02 does NOT delete or make unavailable the SQLite source data needed by R03. The SQLite path is gated by `DATABASE_URL` being empty for the duration of R02. | ✅ Authoritative: `DATABASE_URL` is required and the `DB_PATH` / SQLite source path is removed at the end of R03 (after R03 verifies the data migration). | Uses the new driver only. |
| Migrations up to v1 | ✅ Creates initial schema mirroring current SQLite shape. | Reuses the v1 schema. | Reuses the v1 schema plus R06's room-scoped migrations. |
| Repository implementation | ✅ Adds `*PostgresQueueRepository`, `*PostgresUserRepository`, `*PostgresAutoQueueRepository`. The SQLite implementations (`*SQLiteRepository`, `*SQLiteUserRepository`, `*SQLiteAutoQueueRepository`) and their tests are retained on disk through R03 and removed by R03, not by R02. | None. | None. |
| Wiring in `main.go` | ✅ Adds `persistence.NewPostgresRepository` and the runtime selection (`DATABASE_URL` → Postgres; empty `DATABASE_URL` → SQLite fallback). R02 keeps the existing `persistence.NewSQLiteRepository(cfg.DBPath)` call live and gated; R02 does NOT remove it. | ✅ Removes the `persistence.NewSQLiteRepository(cfg.DBPath)` call and the `DB_PATH` config path after the data migration is verified. Forces `DATABASE_URL`. | None. |
| Docker Compose | ✅ Adds `postgres` and `db-init` services. Updates `backend` to depend on both. The existing `backend-db` volume and SQLite source mount remain. | ✅ Removes the `backend-db` volume and any SQLite source mount from `docker-compose.yml` only after R03 verifies the data migration. | None. |
| `.env.example` | ✅ Adds `POSTGRES_*` and `DATABASE_URL` defaults with explicit "do not use in production" comments. Keeps the existing SQLite-related comments for R02/R03. | ✅ Removes the SQLite-only `DB_PATH` defaults and comments from `.env.example` after R03 verifies the data migration. | None. |
| CI | ✅ Adds a `postgres:16` service to the workflow and sets `DATABASE_URL` in the test job. | None. | None. |
| Schema migration CLI (`cmd/migrate-schema`) | ✅ Implements the schema migration CLI in `cmd/migrate-schema/main.go` (subcommands: `up`, `down <version>`, `force <version>`, `version`). | None. | None. |
| Data copy CLI (`cmd/migrate-data`) | ❌ | ✅ Implements the SQLite-to-PostgreSQL data copy CLI in `cmd/migrate-data/main.go`. Reuses R02's embedded `embed.FS` only for the destination PostgreSQL schema (it does NOT re-run schema migrations; it assumes R02's schema is already in place). | Reuses the CLI with a `--room-id` flag to re-tag rows with `room_id`. |
| Data copy | ❌ | ✅ Copies all 7 tables into PostgreSQL. | Reuses the pipeline to re-tag rows with `room_id`. |
| Identity remap | ❌ | ✅ Maps `users.id` to PostgreSQL `BIGSERIAL`, preserves `legacy_id`. | Drops the `legacy_id` column. |
| Room-scoped columns | ❌ | ❌ | ✅ Adds `room_id` to `queue_state`, `activities`, `auto_queue_config`, `play_history`. |
| Migrated room bootstrap | ❌ | ❌ | ✅ Creates the Product-Owner-named room's `RoomMember(host)` row, resolves the host user (see ADR 001 §11), fails atomically on resolution failure. |
| 50-row play history cap | ✅ Replaces the SQLite trigger with a server-side cap (`DELETE FROM play_history WHERE id NOT IN (SELECT id FROM play_history ORDER BY played_at DESC LIMIT 50)` after each insert). | Reuses R02's server-side cap. | Per-room cap (the `DELETE` query filters by `room_id`). |
| `play_history` trigger | ✅ Removes the SQLite `trg_play_history_cap` trigger. | None. | None. |
| Backup / rollback | ✅ Documents `pg_dump` cadence, the restore drill, and the rollback window. | Documents the pre-migration snapshot retention. | Documents the post-R06 snapshot retention. |
| Frontend / WebSocket / REST routes | ❌ (no route change) | ❌ (no route change) | ❌ (route change is R07, not R06) |
| Vote sessions | ❌ (still in-memory, still global) | ❌ | ❌ (room-scoping is R09) |
| Auto-queue single-flight | ❌ (still in-process, still global) | ❌ | ❌ (room-scoping is R10) |

## 14. Risks

| Risk | Mitigation |
| --- | --- |
| Driver swap introduces a behavioral change visible to users | R02's acceptance gate is parity against the closed Sprint 003 baseline; the cutover is gated on a green parity test run. |
| `pgx` transaction isolation level differs from SQLite's | R02 documents the chosen level (`READ COMMITTED` is the PostgreSQL default and is appropriate for the queue interactor's needs). R02 includes a test that exercises the queue interactor's lock behavior under the new driver. |
| `INTEGER` vs `BIGSERIAL` mismatch in `users.id` | R03's identity remap is mandatory before R06. R02 reserves a `BIGSERIAL` for `users.id` and writes a `legacy_id` column on the same table to keep the mapping auditable through R06. |
| `DATETIME` vs `TIMESTAMPTZ` mismatch in `activities.played_at` / `user_sessions.first_seen_at` | R02's initial schema uses `TIMESTAMPTZ` everywhere. R03's loader parses SQLite's `YYYY-MM-DD HH:MM:SS` strings as UTC; a unit test in R02 covers the parse path. |
| `queue_state` JSON shape drift between SQLite and PostgreSQL | The JSON is preserved verbatim byte-for-byte. R02's loader and saver use the same Go `entity.Queue` struct. |
| `play_history` cap not enforced if the server-side DELETE is skipped | R02 wraps the cap in a Go function that is called from `(*PostgresAutoQueueRepository).AppendHistory` after every insert. The function is unit-tested. |
| Docker Compose port collision with `BACKEND_PORT=443` | `POSTGRES_PORT` defaults to `5432`. R02's deployment guide explicitly states the postgres port is independent of the HTTPS port. |
| Backup retention is set too low | R02 sets the minimum retention at 7 days for normal backups and 30 days for pre-migration snapshots. R03 enforces 30 days for its snapshot. |
| Secret leakage via `.env.example` | R02's `.env.example` defaults are explicitly dev-only and are documented as such. CI does not read `.env.example`; CI injects secrets from the platform. |
| Cross-process / multi-instance coordination creeping in | R02 explicitly documents the single-instance assumption inherited from ADR 001. Cross-process coordination is deferred. |
| `golang-migrate` down-migrations being misused in production rollback | R02's deployment guide states that down-migrations are local-dev only. Production rollback is restore-only. |
| R02 cutover breaks in-flight vote sessions | Vote sessions are in-memory; they are lost on backend restart regardless. R02 documents the loss as expected, matching today's behavior. |
| `legacy_id` column never being dropped | R06's task list includes a final `ALTER TABLE users DROP COLUMN legacy_id` migration. R03's acceptance gate includes a tracking issue for this. |
| `db-init` container running with credentials in the image | `Dockerfile.migrate` is a multi-stage build that copies only the binary and the migrations. Credentials are passed at runtime via env. |

## 15. Deferred work (explicit)

- Cross-process / multi-instance room coordination (ADR 001 §16).
- Room-scoped vote sessions (ADR 001 §16, R09).
- Room-scoped auto-queue single-flight (R10).
- Strong backend authorization on every endpoint (R13).
- Per-song row storage in place of the queue JSON blob (ADR 001 §16).
- Slug rename and archive recovery (ADR 001 §16).
- Anonymous (read-only) room views (ADR 001 §16).
- Cross-room moderation tools (ADR 001 §16).
- Connection pooler (PgBouncer). R02 leaves room for it but does not provision it.
- Read replicas. R02 does not provision them; the deployment guide notes that the DSN is the only knob to change.
- Online schema migration tooling (e.g. `pg_repack`). R02's migrations are all DDL; they are short and run at startup. Online migration tooling is deferred until the migration set outgrows the startup window.

## 16. Verification categories for later implementation sprints

### R02 (PostgreSQL Foundation)

- `go test ./...` PASS against the deterministic test schema.
- `go test -race ./...` PASS.
- `go vet ./...` PASS.
- `cd frontend && npm run test:unit -- --run` PASS (no frontend changes).
- `cd frontend && npm run build` PASS.
- `docker compose config` PASS with the new services.
- Parity check: same 19 REST endpoints, same 16-event WebSocket envelope, same in-memory vote/auto-queue behavior.
- DSN redaction: log output does not contain the password from `DATABASE_URL`.
- Healthcheck: `pg_isready` returns 0 within the documented window during `docker compose up`.
- Migration apply: `migrate up` is idempotent (running it twice produces no errors).
- Repository interface conformance: `*PostgresQueueRepository`, `*PostgresUserRepository`, `*PostgresAutoQueueRepository` satisfy the existing interfaces; a compile-time assertion in R02 catches any signature drift.

### R03 (SQLite-to-PostgreSQL Data Migration)

- Row-count integrity: per-table `COUNT(*)` matches between source SQLite and target PostgreSQL.
- `queue_state` integrity: target `OCTET_LENGTH(data)` equals source `LENGTH(data)`.
- Identity remap: each user's `users.id` in PostgreSQL is a valid `BIGSERIAL`; each `priority_transactions.user_id` resolves to a real `users.id`.
- `user_sessions` FK: every `user_id` resolves; `UNIQUE(user_id, session_date)` is preserved.
- `activities` and `play_history` chronology: `MIN(timestamp)` and `MAX(timestamp)` match.
- Idempotency: running the CLI twice produces no duplicate rows and no errors.
- Atomicity: a forced error mid-pipeline rolls back the entire transaction (verified by a fault-injection test in R03).
- Rollback drill: restoring from the pre-migration snapshot into a scratch database reproduces the row counts from the integrity report.

### R06 (Room-Scoped Persistence)

- Migrated room bootstrap: the Product-Owner-named room exists; its `RoomMember(host)` row points to the resolved user; the user exists in `users`; no other user is host.
- Resolution failure: an unresolved host user (no matching email) fails the migration atomically; no partial room exists.
- `queue_state` in the migrated room: the JSON blob is byte-for-byte identical to the pre-R06 snapshot.
- `activities` and `play_history` re-tagging: every row has `room_id` set to the migrated room's id; no row retains a NULL `room_id`.
- `auto_queue_config` migration: the migrated room's row matches the pre-R06 global row exactly.
- Per-room `play_history` cap: the cap is enforced per room, not globally.
- Old global rows: deleted at the end of R06; only the migrated room remains.
- No permanent `main` room: there is no row in `rooms` with `slug = 'main'`.

## 17. Consequences for later sprints

- R02 implements the foundation defined here and is gated on the parity verification above.
- R03 implements the data migration CLI defined in §11 and is gated on the integrity verification above.
- R04–R05 build the room domain and player lease against the PostgreSQL repository from R02. They must not depend on SQLite.
- R06 reuses R03's CLI to fold the global state into the Product-Owner-named room, per ADR 001 §11.
- R07+ consume the room-scoped storage; they do not re-introduce a global storage path.
- R13 (authorization hardening) can use `users.id` (BIGSERIAL) and `user_sessions` (DATE, TIMESTAMPTZ) without further schema work.

## Out of scope for this ADR

- Runtime code, runtime configuration, Docker Compose, migrations, tests, or deployment scripts.
- Implementation of any of R02, R03, R04, R05, R06, R07, R08, R09, R10, R11, R12, R13, R14.
- Choice of managed PostgreSQL provider.
- Cross-process / multi-instance room coordination.

## Verification

This ADR is a design plan. Acceptance for Sprint R01 requires:

- This document exists at the path above.
- The accompanying Sprint 007/R01 sprint document records the ADR location and verification results.
- `git diff --check` is clean.
- `git status --short --branch` shows only the ADR, the sprint document, `SPRINTS/active.md`, and `SPRINTS/README.md` changed; no runtime, configuration, deployment, or test files were modified.
