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
- New REST endpoints (10 total). Per ADR 001 §6, the URL-safe slug is the external room identifier; numeric DB ids stay internal:
  - `POST /api/rooms` — create room (slug + name required; returns the created `Room` JSON; does NOT return an invite token).
  - `GET /api/rooms` — list rooms (optional `?status=active|archived`).
  - `GET /api/rooms/{slug}` — get room details.
  - `GET /api/rooms/{slug}/members` — list members (requires active membership).
  - `POST /api/rooms/{slug}/invites` — create invite (host/admin only; returns plaintext token; role strings never trusted from request body).
  - `GET /api/rooms/{slug}/invites` — list invites (host/admin only).
  - `DELETE /api/rooms/{slug}/invites/{inviteId}` — revoke invite (host/admin only).
  - `POST /api/invites/{token}/redeem` — redeem invite token, join as guest (atomic: insert member + increment use_count in one transaction).
  - `POST /api/rooms/{slug}/members/{userId}/promote` — promote member to admin (host only).
  - `POST /api/rooms/{slug}/members/{userId}/demote` — demote admin to guest (host only).
- Status codes: 201 created, 200 ok, 204 no content, 400 bad request (invalid slug, past/over-30d expiry, negative max_uses), 401 unauthorized, 403 forbidden (non-member / non-host / non-host-admin), 404 not found (room, member, invite), 409 conflict (slug taken, archived room mutation), 410 gone (invite exhausted), 500 internal error.
- Exactly-one-host invariant enforced at SQL partial-unique-index AND use case layer.
- Invite tokens: 128-bit random, SHA-256 hashed (base64url), stored in `room_invites.token_hash`. Plaintext returned only from `POST /api/rooms/{slug}/invites`.
- Default invite expiry: 7 days. Maximum override: 30 days from creation. `max_uses=0` means unlimited uses; `max_uses < 0` is rejected.
- Archived-room mutation rules: archived rooms cannot accept new invites, cannot have members added/promoted/demoted; only `GET /api/rooms/{slug}` (and the internal-only archive method) is allowed. Members cannot list members of an archived room.
- Membership/role gates:
  - `CreateInvite`, `ListInvites`, `RevokeInvite`: require host OR admin membership.
  - `ListMembers`: require any active membership of the room.
  - `PromoteMember`, `DemoteMember`: require host membership; host-only.
- Atomic invite redemption: `AddMember` + `IncrementInviteUseCount` run inside one repository transaction guarded by a SQL `WHERE max_uses = 0 OR use_count < max_uses` clause; concurrent redemption cannot overrun `max_uses`, and the member insert is rolled back if the increment cannot be applied.
- Actor identity resolved via `auth.Interactor.ResolveSession` bearer token; role strings NEVER trusted from request bodies.
- No public archive endpoint; `ArchiveRoom` is internal-only.
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

- `internal/domain/entity/room.go` — domain entity (Room, RoomMember, RoomInvite), slug regex, reserved slugs, errors.
- `internal/domain/repository/room_repository.go` — repository interface (RoomRepository, including the `RedeemInviteAtomic` transaction helper) and `ErrInviteExhausted`.
- `internal/usecase/room/interactor.go` — use case interactor (CreateRoom, GetRoomBySlug, ListRooms, ListMembers, CreateInvite, RedeemInvite, ListInvites, RevokeInvite, PromoteMember, DemoteMember, ArchiveRoom). Role strings NEVER trusted from request bodies; actor identity via ResolveSession; expiry/max_uses validation; membership/role gates.
- `internal/infrastructure/persistence/postgres_room_repository.go` — PostgreSQL implementation of room repositories including `RedeemInviteAtomic`.
- `internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql` / `0004_rooms.down.sql` — adds/removes `rooms`, `room_members`, `room_invites` tables including partial unique index for exactly-one-host.
- `internal/delivery/http/room_handlers.go` — HTTP handlers for room endpoints; status-code mapping per above.
- `cmd/server/main.go` — wires room handler, registers new routes with `{slug}` path params (numeric `roomId` parse removed).
- `internal/infrastructure/persistence/migrations_postgres.go` — `embed.FS` ships four migration files.
- `internal/infrastructure/persistence/migrator.go` — schema version probe covers v4.
- Migration tests: `internal/infrastructure/persistence/postgres_migration_test.go` extended for 0004.
- Persistence tests: `internal/infrastructure/persistence/postgres_room_repository_test.go` for room repositories.
- Use case tests: `internal/usecase/room/interactor_test.go` for room use cases (slug validation, role checks, atomic redeem, expiry/max_uses).
- Handler tests: `internal/delivery/http/room_handlers_test.go` for room handlers (slug paths, invalid slug rejection, role gates).

### Decisions made

- **Slug regex:** `^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`; lowercase alphanumeric with hyphens, no leading/trailing hyphens, 3-40 chars.
- **Reserved slugs:** `api`, `admin`, `static`, `ws`.
- **External identifier:** the URL-safe slug (ADR 001 §6). Numeric DB ids stay internal and are never used in routes. Lookups by slug happen in the repository layer via `GetRoomBySlug`.
- **Status/role enums:** `entity.RoomStatus` (active, archived); `entity.RoomMemberRole` (host, admin, guest). Stored as VARCHAR strings in the DB.
- **Exactly-one-host:** enforced at SQL partial-unique-index (`WHERE role = 'host'`) AND use case layer (host gate via `requireHost`).
- **Membership/role gates:** `requireHost` (host-only) for promote/demote; `requireHostOrAdmin` (host or admin) for create/list/revoke invites; any active membership for list-members. The interactor reads the membership row directly from the DB; it never trusts role strings supplied by the caller.
- **Invite token hashing:** 128-bit random, SHA-256, base64url-encoded hash stored as `token_hash`. Plaintext returned only from `POST /api/rooms/{slug}/invites`.
- **Default expiry:** 7 days from creation. Override must be in the future and within 30 days of creation.
- **max_uses:** `0` means unlimited uses; `< 0` is rejected with 400.
- **Atomic redemption:** `RedeemInviteAtomic` runs the conditional `UPDATE room_invites SET use_count = use_count + 1 WHERE id = $1 AND ($2 = 0 OR use_count < $2)` followed by `INSERT INTO room_members`. A 0-row update returns `repository.ErrInviteExhausted`; the transaction rolls back so the member is never persisted on failure.
- **Archived-room mutation rules:** archived rooms reject new invites, invite redemption, list-members, promote, demote, and create/list/revoke invites. `GET /api/rooms/{slug}` remains the only public read; `ArchiveRoom` is internal-only.
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
- `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/usecase/room -v` — PASS (slug create/reject, duplicate slug, promote/demote permissions, archived-room rejection, invite create/redeem/revoke/expiry/max-uses, atomic max_uses=1 blocks second distinct user, atomic redeem rolls back member insert on use-count failure, invite expiry/max_uses validation, membership/role gates, get-by-slug).
- `LMQ_TEST_DATABASE_URL=... go test ./internal/infrastructure/persistence -run 'TestPostgresRoom_|TestPostgresMigration' -v` — PASS (`CleanSchema` v4, `DownThenUp` v3→v0→v4, `DownThenUp_Rooms` v4→v3→v4, create+host+unique slug, one-host invariant, list+archive, invite lifecycle, count hosts).
- `LMQ_TEST_DATABASE_URL=... go test ./internal/delivery/http -run 'TestRoomHandler_' -v` — PASS (create success/reserved/invalid, list, get 404, invalid slug 400, list members 401, list members non-member 403, promote non-host 403, create invite non-member 403, redeem generic 404).
- `YTDLP_PATH=... LMQ_TEST_DATABASE_URL=... go test ./cmd/server -run 'TestSetupApp|TestSetupApp_PostgresDBStaysOpen'` — PASS
- `LMQ_TEST_DATABASE_URL=... go test -race ./internal/usecase/room ./internal/infrastructure/persistence ./internal/delivery/http` — all PASS
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
- Atomic redemption: max_uses=1 invites cannot be redeemed twice by distinct users, and a failed redeem never leaves a partial member row

If any queue/playback/websocket/frontend code is changed unexpectedly, stop and report it instead of continuing.

## Risks and review focus

- **Slug regex brittleness.** The regex `^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$` is manually maintained; any change must preserve the no-leading/trailing-hyphen invariant.
- **Partial unique index traps.** The `UNIQUE (room_id) WHERE role = 'host'` partial index is the SQL-layer exactly-one-host enforcement; any ORM or raw-SQL bypass would silently break the invariant.
- **isUniqueViolation error-sniffing brittleness.** String-sniffing on `err.Error()` for `"23505"`/`"unique constraint"`/`"duplicate key"` is fragile.
- **Atomic-redeem SQL guard.** The `WHERE id = $1 AND ($2 = 0 OR use_count < $2)` guard is the SQL-layer max_uses overrun protection; any future change to the increment path must keep that guard.
- **Archived-room rule coverage gap.** The handler layer enforces archived-room mutation guards; verify every new handler path is covered.
- **Letsencrypt permission blocker.** Pre-existing; out of R04 scope; tracked separately in `PROJECT_STATE.md`.

## Builder reasoning effort

High. R04 ships a new domain (rooms, members, invites), three new tables with non-trivial constraints (exactly-one-host partial unique index, invite token hashing, atomic redeem with SQL max_uses guard, archived-room mutation guards), 10 new REST endpoints, the slug-as-external-id contract, and the actor-identity-via-session pattern. Decisions about slug validation, role string trust, exactly-one-host enforcement layers, invite token lifecycle (expiry, max_uses), atomic redemption semantics, and archived-room rules all have lasting implications for R05+.

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
invariant, invite token hashing, atomic invite redemption
(SQL-guarded max_uses), membership/role gates, slug-as-external-id
routes, and archived-room rules. R05 adds:
- Player lease: a host claims the player for a room session; if the
  lease expires without renewal, the player is released. R05 ships
  the lease state machine and expiry semantics.
- Host departure semantics: if the host's player lease expires and
  is not renewed, the room is archived (or otherwise transitioned per
  ADR 001). R05 does NOT auto-promote any member to host; the
  exactly-one-host invariant from R04 still requires an explicit
  `PromoteMember` call.

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
- internal/domain/entity/room.go (R04-owned; reference only)
- internal/domain/repository/room_repository.go (R04-owned; reference only)
- internal/usecase/room/interactor.go (R04-owned; reference only)
- internal/infrastructure/persistence/postgres_room_repository.go (R04-owned; reference only)
- internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql (R04-owned; target schema)
- internal/delivery/http/room_handlers.go (R04-owned; reference only)
- cmd/server/main.go

Out of scope:
- Removing users.legacy_id or migration_marker — R06.
- Room-scoped queue/playback/vote/autoqueue — R07+.
- Frontend room flow — R11+.
- Backend authorization hardening — R13.
- Global contract cleanup — R14.
- Removing the room domain — R04 added it; it does not return.
- Restoring SQLite runtime fallback — R03 removed it; it does not return.
- Auto-promotion of any member to host — R05 does not introduce this;
  the exactly-one-host invariant from R04 still requires an explicit
  PromoteMember call.
```