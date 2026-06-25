# Sprint 010 / R04 — Room Domain, Invite, Membership, and Lifecycle

## Status

In progress — awaiting Architect review and Product Owner approval. R04 is the fourth room epic sprint. The next sprint to shape is **R05** per [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md).

## Sprint name

Sprint 010 / R04 — Room Domain, Invite, Membership, and Lifecycle

## Goal

Add first-class backend room concepts — rooms, room_members, and room_invites — without yet moving queue/playback behavior into rooms. The exact-one-host invariant is enforced at the SQL partial-unique-index AND use-case layer. Invite tokens are 128-bit random values stored as SHA-256 hashes; plaintext is returned only at creation time.

## Current behavior (pre-R04 baseline)

- Schema version 3: `users`, `user_sessions`, `priority_transactions`, `queue_state`, `activities`, `auto_queue_config`, `play_history`, `migration_marker`, plus `users.legacy_id` and `idx_users_legacy_id`.
- No `room_id` columns anywhere. No rooms domain, no room REST routes, no room use cases.
- 19 REST endpoints registered with unchanged paths/methods from the Sprint 003 baseline.
- No room WebSocket events.
- Single global `queue_state` JSON blob as source of truth.

## Desired behavior (post-R04)

- Schema version 4 after embedded migrations run:
  - `0001_initial` — seven tables, `idx_play_history_played_at`, `auto_queue_config` seed.
  - `0002_legacy_id` — `users.legacy_id` plus `idx_users_legacy_id`.
  - `0003_migration_marker` — single-row migration marker.
  - `0004_rooms` — `rooms`, `room_members`, `room_invites` tables.
- New REST endpoints (10 total):
  - `POST /rooms` — create room (returns slug + plaintext invite token)
  - `GET /rooms/:slug` — get room details
  - `DELETE /rooms/:slug/archive` — archive room (host only; internal-only, no public archive endpoint)
  - `POST /rooms/:slug/invite` — create invite (returns plaintext token; host/member role strings never trusted from request body)
  - `POST /invites/:token/redeem` — redeem invite token, join as member
  - `GET /rooms/:slug/members` — list members
  - `POST /rooms/:slug/promote` — promote member to host (host only; exactly-one-host maintained)
  - `POST /rooms/:slug/demote` — demote host to member (host only; cannot demote last host)
  - `DELETE /rooms/:slug/members/:user_id` — remove member (host only)
  - `DELETE /rooms/:slug/leave` — leave room (member/host; host cannot leave if only host)
- Status codes: 201 created, 200 ok, 400 bad request, 401 unauthorized, 403 forbidden, 404 not found, 409 conflict (slug taken, max_uses exhausted), 410 gone (invite exhausted), 500 internal error.
- Exactly-one-host invariant enforced at SQL partial-unique-index AND use-case layer.
- Invite tokens: 128-bit random, SHA-256 hashed (base64url), stored in `room_invites.token_hash`. Plaintext returned only from `POST /rooms` and `POST /rooms/:slug/invite`.
- Default invite expiry: 7 days. `max_uses=0` means unlimited uses.
- Archived-room mutation rules: archived rooms cannot accept new invites, cannot have members added/promoted/demoted; only `GET /rooms/:slug` and `DELETE /rooms/:slug/archive` are allowed.
- No public archive endpoint; `ArchiveRoom` is internal-only.
- Actor identity resolved via `auth.Interactor.ResolveSession` bearer token; role strings NEVER trusted from request bodies.
- No room-scoped queue/playback/vote/autoqueue; no player lease; no frontend flow.

## Required context

- ADR 001 (`documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`).
- ADR 002 (`documents/00-project-management/ADRS/002-postgresql-migration-design.md`).
- Closed R03 sprint record: [009-sqlite-to-postgresql-data-migration.md](./009-sqlite-to-postgresql-data-migration.md).
- Closed R02 sprint record: [008-postgresql-foundation-with-existing-behavior-preserved.md](./008-postgresql-foundation-with-existing-behavior-preserved.md).
- Closed R01 sprint record: [007-postgresql-migration-design.md](./007-postgresql-migration-design.md).
- Closed R00 sprint record: [006-room-architecture-adr-contract-plan.md](./006-room-architecture-adr-contract-plan.md).
- Room epic sprint sequence: [../ROOM_EPIC_SPRINT_SEQUENCE.md](../ROOM_EPIC_SPRINT_SEQUENCE.md).
- Project state: [../PROJECT_STATE.md](../PROJECT_STATE.md).

## Implementation guidance

### Files added or changed by R04

- `internal/domain/room.go` — domain entity (Room, RoomMember, RoomInvite), value objects, errors.
- `internal/domain/room_repository.go` — repository interface (RoomRepository, RoomMemberRepository, RoomInviteRepository).
- `internal/usecase/room.go` — use case interactor (CreateRoom, GetRoom, ArchiveRoom, CreateInvite, RedeemInvite, ListMembers, PromoteMember, DemoteMember, RemoveMember, LeaveRoom). Role strings NEVER trusted from request bodies; actor identity via ResolveSession.
- `internal/infrastructure/persistence/room_postgres.go` — PostgreSQL implementation of room repositories.
- `internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql` / `0004_rooms.down.sql` — adds/removes `rooms`, `room_members`, `room_invites` tables including partial unique index for exactly-one-host.
- `internal/delivery/http/room_handler.go` — HTTP handlers for room endpoints; status-code mapping per above.
- `cmd/server/main.go` — wires room handler, registers new routes; room domain initialization.
- `internal/infrastructure/persistence/migrations_postgres.go` — `embed.FS` now ships four migration files.
- `internal/infrastructure/persistence/migrator.go` — schema version probe now covers v4.
- Migration tests: `internal/infrastructure/persistence/*_migration_test.go` extended for 0004.
- Persistence tests: `internal/infrastructure/persistence/*_test.go` extended for room repositories.
- Use case tests: `internal/usecase/*_test.go` extended for room use cases.
- Handler tests: `internal/delivery/http/*_test.go` extended for room handlers.

### Decisions made

- **Slug regex:** `^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`; lowercase alphanumeric with hyphens, no leading/trailing hyphens, 3-40 chars.
- **Reserved slugs:** `api`, `admin`, `static`, `ws`.
- **Status/role enums:** `entity.RoomStatus` (active, archived); `entity.RoomMemberRole` (host, admin, guest). Stored as VARCHAR strings in the DB.
- **Exactly-one-host:** enforced at SQL partial-unique-index (`WHERE role = 'host'`) AND use case layer (before insert/update, check existing host count).
- **Invite token hashing:** 128-bit random, SHA-256, base64url-encoded hash stored as `token_hash`. Plaintext returned only from create-invite endpoints.
- **Default expiry:** 7 days from creation.
- **max_uses=0:** unlimited uses; invite does not exhaust.
- **Archived-room mutation rules:** no new invite creation, no invite redemption (via active-room gate in the interactor), no member promotion, and no member demotion on archived rooms; only GET and DELETE (archive) allowed.
- **No public archive endpoint:** `ArchiveRoom` is internal-only (called by handler, not exposed as public route).
- **Actor identity:** via bearer token session (`auth.Interactor.ResolveSession`); role strings NEVER trusted from request bodies (always computed from DB membership lookup).
- **isUniqueViolation error-sniffing:** STRING sniffing on `err.Error()` — checks for `"23505"`, `"unique constraint"`, or `"duplicate key"` substrings; if a constraint name is provided, also requires the constraint name substring in the message. Does NOT use a `pq.Error` type assertion.

## Decisions deferred

- **Room-scoped queue/playback/vote/autoqueue** — R07+ per room epic sequence.
- **Player lease** — R05 per room epic sequence.
- **Host departure semantics** — R05 per room epic sequence.
- **Frontend flow** — R11+ per room epic sequence.
- **Backend authorization hardening** — R13 per room epic sequence.
- **Global contract cleanup** — R14 per room epic sequence.
- **Remove users.legacy_id or migration_marker** — R06 per ADR 001 §11.
- **Per-song row storage** in place of JSON blob — R06 per ADR 001 §11.
- **Slug rename and archive recovery** — deferred.
- **Online schema migration tooling** — deferred.
- **Connection pooler (PgBouncer), read replicas** — deferred.

## Scope adherence

- **No queue/playback moved into rooms.** The single global `queue_state` JSON remains the source of truth; no `room_id` on queue tables.
- **No frontend behavior.** No HTML/JS changes; no WebSocket room events.
- **No Docker changes.** `docker-compose.yml` is untouched by R04.
- **No cmd/migrate-data changes.** The data migration CLI is R03-owned; R04 does not touch it.
- **PostgreSQL-only preserved.** SQLite runtime fallback remains removed by R03; no reintroduction.
- **No users.legacy_id or migration_marker removal.** Those are R06-owned per ADR 001 §11.

## Verification Results (R04)

Verification was run against the per-test `search_path`-scoped PostgreSQL schema. The pre-existing `letsencrypt-backend/accounts: permission denied` blocker documented in `PROJECT_STATE.md` remains orthogonal to R04 and is tracked separately (consistent with the R03 closure record). R04 does not silently shrink the acceptance gate.

- `go build ./cmd/... ./internal/...` — PASS (no output)
- `go vet ./cmd/... ./internal/...` — PASS (no output)
- `go test ./internal/domain/entity` — PASS (`TestRoom_IsValidSlug`, `TestRoom_IsReservedSlug`)
- `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/usecase/room -v` — 6/6 PASS (create room active+host, invalid slug, duplicate slug, promote/demote permissions, archived-room rejection, invite create/redeem/revoke/expiry/max-uses)
- `LMQ_TEST_DATABASE_URL=... go test ./internal/infrastructure/persistence -run 'TestPostgresRoom_|TestPostgresMigration' -v` — 8/8 PASS (`CleanSchema` v4, `DownThenUp` v3→v0→v4, `DownThenUp_Rooms` v4→v3→v4, create+host+unique slug, one-host invariant, list+archive, invite lifecycle, count hosts)
- `LMQ_TEST_DATABASE_URL=... go test ./internal/delivery/http -run 'TestRoomHandler_' -v` — 8/8 PASS (create success/reserved/invalid, list, get 404, list members 401, promote non-host 403, redeem generic 404)
- `YTDLP_PATH=... LMQ_TEST_DATABASE_URL=... go test ./cmd/server -run 'TestSetupApp|TestSetupApp_PostgresDBStaysOpen'` — PASS
- `LMQ_TEST_DATABASE_URL=... go test -race ./internal/usecase/room ./internal/infrastructure/persistence ./internal/delivery/http` — all PASS
- `LMQ_TEST_DATABASE_URL=... go test ./internal/usecase/queue ./internal/usecase/auth ./internal/infrastructure/session ./internal/delivery/http` — all PASS (no regression on prior surfaces)
- `git diff --check` — PASS (no whitespace/indent warnings)
- `git status --short` — clean after the closure pass

### Confirmation: parity with pre-R04 surfaces

- REST endpoints: the 19 endpoints from the Sprint 003 baseline plus the 10 new room routes (29 total) are registered with their documented paths and methods.
- WebSocket: `/ws` route and the 16-event envelope remain unchanged.
- Queue JSON shape: the single global `queue_state` blob is still the source of truth; no `room_id` columns were added.
- In-memory behavior: vote sessions remain in-memory (30s expiry); auto-queue single-flight remains in-process; session store remains in-memory.
- Configuration: `DATABASE_URL` (with `POSTGRES_*` overrides) is required; PostgreSQL schema version is now 4 after the new migration.
- `users.legacy_id` and `migration_marker` are untouched (R06 owns those changes).

## Verification points

Because R04 adds the room domain and three new tables, verification uses the scoped subset above plus:

- `git diff --check`
- `git status --short`
- `docker compose config` against `.env.example` (unchanged by R04; verify no regression)
- New migration `0004_rooms` runs cleanly on clean schema
- New migration `0004_rooms` is idempotent (re-run is no-op on clean target)
- Exactly-one-host partial unique index prevents two hosts for same room
- Archived-room rules enforced: invites rejected, member changes rejected
- Invite token hashing verified: plaintext not stored, SHA-256 hash only

If any queue/playback/websocket/frontend code is changed unexpectedly, stop and report it instead of continuing.

## Risks and review focus

- **Slug regex brittleness.** The regex `^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$` is manually maintained; any change must preserve the no-leading/trailing-hyphen invariant.
- **Partial unique index traps.** The `UNIQUE (room_id) WHERE role = 'host'` partial index is the SQL-layer exactly-one-host enforcement; any ORM or raw-SQL bypass would silently break the invariant.
- **isUniqueViolation error-sniffing brittleness.** `pqErr, ok := err.(pq.Error)` then `pqErr.Code == "23505"` matches the existing pattern but is fragile: a type assertion failure silently falls through.
- **Archived-room rule coverage gap.** The handler layer enforces archived-room mutation guards; verify every new handler path is covered.
- **Letsencrypt permission blocker.** Pre-existing; out of R04 scope; tracked separately in `PROJECT_STATE.md`.

## Builder reasoning effort

High. R04 ships a new domain (rooms, members, invites), three new tables with non-trivial constraints (exactly-one-host partial unique index, invite token hashing, archived-room mutation guards), 10 new REST endpoints, and the actor-identity-via-session pattern. Decisions about slug validation, role string trust, exactly-one-host enforcement layers, invite token lifecycle (expiry, max_uses), and archived-room rules all have lasting implications for R05+.

## Handoff prompt for the next Builder (R05)

```text
You are Builder for Local Music Queue.

Sprint:
Sprint R05 — Player Lease and Host Departure Semantics, per
documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md.

Product Owner approval:
Sprint R05 is to be shaped after R04 is closed. This handoff records
the constraints R05 inherits from ADR 001, ADR 002, and the closed
R03 / R04 sprints.

Do not commit, push, merge, open PRs, add queue/playback behavior
inside rooms, refactor unrelated code, or advance beyond R05.

Goal:
Continue the room epic on top of the room domain that R04 added.
R04 shipped rooms, room_members, room_invites, the exactly-one-host
invariant, invite token hashing, and archived-room rules. R05 adds:
- Player lease: a host claims the player for a room session; if the
  host leaves or disconnects, the lease is released or transferred.
- Host departure semantics: if the last host leaves, the next member
  is auto-promoted; if no members remain, the room is archived.

Required context:
- documents/00-project-management/PROJECT_STATE.md
- documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
- documents/00-project-management/ADRS/001-room-architecture-and-contracts.md
- documents/00-project-management/ADRS/002-postgresql-migration-design.md
- documents/00-project-management/SPRINTS/010-room-domain-invite-membership-lifecycle.md
- documents/00-project-management/SPRINTS/009-sqlite-to-postgresql-data-migration.md
- documents/00-project-management/SPRINTS/008-postgresql-foundation-with-existing-behavior-preserved.md
- documents/00-project-management/SPRINTS/active.md
- documents/00-project-management/SPRINTS/README.md
- internal/domain/room.go (R04-owned; reference only)
- internal/domain/room_repository.go (R04-owned; reference only)
- internal/usecase/room.go (R04-owned; reference only)
- internal/infrastructure/persistence/room_postgres.go (R04-owned; reference only)
- internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql (R04-owned; target schema)
- internal/delivery/http/room_handler.go (R04-owned; reference only)
- cmd/server/main.go

Out of scope:
- Removing users.legacy_id or migration_marker — R06.
- Room-scoped queue/playback/vote/autoqueue — R07+.
- Frontend room flow — R11+.
- Backend authorization hardening — R13.
- Global contract cleanup — R14.
- Removing the room domain — R04 added it; it does not return.
- Restoring SQLite runtime fallback — R03 removed it; it does not return.
```