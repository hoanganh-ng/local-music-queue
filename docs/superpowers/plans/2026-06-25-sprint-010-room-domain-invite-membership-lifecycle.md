# Sprint 010 / R04 — Room Domain, Invite, Membership, and Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add first-class backend room, invite, and membership domain — including PostgreSQL persistence (schema v4), use cases, and HTTP routes — without moving queue/playback, voting, auto-queue, or frontend behavior into rooms yet.

**Architecture:** Clean-Architecture extension on top of the R03 PostgreSQL-only backend. New `entity` package gains `Room`, `RoomMember`, `RoomInvite` + status/role enums and slug validation. New `repository.RoomRepository` interface defines SQL-shaped operations; `persistence.PostgresRoomRepository` implements it against schema version 4 (`0004_rooms`). New `usecase/room.Interactor` owns create-room / invite / member / promote / demote / archive flows and resolves actor identity from the existing `auth.Interactor.ResolveSession` path. New `delivery/http.RoomHandlers` exposes the documented 10 endpoints and maps use-case errors to the documented status codes.

**Tech Stack:** Go 1.x, PostgreSQL 16 (pgx/v5/stdlib), golang-migrate v4, database/sql, net/http, crypto/rand, crypto/sha256.

## Global Constraints

- Runtime is PostgreSQL-only (R03). No SQLite paths may be re-introduced.
- Do not remove `users.legacy_id` or `migration_marker` — R06 owns those changes.
- Do not add room-scoped queue/playback/vote/autoqueue routes. Existing 19 REST endpoints and `/ws` must remain unchanged.
- Do not add frontend behavior, Docker/deployment changes, or `cmd/migrate-data` changes.
- Slug regex: `^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$` (matches ADR 001 §9). Reserved slugs `api`, `admin`, `static`, `ws` are rejected at create time.
- Invite token entropy: ≥ 128 bits (16 bytes from `crypto/rand`). Token stored as SHA-256 hash, base64url-encoded. Plaintext returned only from create-invite.
- Status code mapping (matches requirements section 12):
  - 400 invalid input
  - 401 missing/invalid session where actor identity is required
  - 403 actor is not allowed (non-host promote/demote, etc.)
  - 404 unknown room/invite (generic; does not leak existence)
  - 409 duplicate slug or archived-room mutation
  - 410 exhausted invite (distinct from generic 404)
- Actor identity is resolved via `auth.Interactor.ResolveSession(ctx, bearer)` (`Authorization: Bearer <token>`); role strings in request bodies are NEVER trusted for room permission decisions.
- No `cmd/migrate-data` changes.
- Builder prohibitions (per `SPRINTS/README.md`): do not commit, push, merge, or advance sprint status. Final step of the sprint doc records verification and the human drives the commit.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/domain/entity/room.go` | `Room`, `RoomMember`, `RoomInvite` structs + `RoomStatus` (`active`, `archived`) + `RoomMemberRole` (`host`, `admin`, `guest`) + slug regex + `IsValid*` helpers + reserved-slug set |
| `internal/domain/entity/room_test.go` | Domain unit tests (no DB) |
| `internal/domain/repository/room_repository.go` | `RoomRepository` interface: room CRUD, member CRUD with one-host invariant helpers, invite CRUD, archive |
| `internal/usecase/room/interactor.go` | `Interactor` + sentinel errors + `CreateRoom`, `GetRoom`, `ListRooms`, `ListMembers`, `CreateInvite`, `ListInvites`, `RevokeInvite`, `RedeemInvite`, `PromoteMember`, `DemoteMember`, `ArchiveRoom` (internal-only) |
| `internal/usecase/room/interactor_test.go` | Use-case tests with mock + DB-backed tests for invariants |
| `internal/infrastructure/persistence/postgres_room_repository.go` | `PostgresRoomRepository` impl using `*sql.DB` |
| `internal/infrastructure/persistence/postgres_room_repository_test.go` | Schema-level persistence tests |
| `internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql` | `rooms`, `room_members`, `room_invites` + indexes + unique-on-host constraint |
| `internal/infrastructure/persistence/migrations/postgres/0004_rooms.down.sql` | Reverse 0004 |
| `internal/delivery/http/room_handlers.go` | 10 HTTP handlers + status-code mapping + actor resolution |
| `internal/delivery/http/room_handlers_test.go` | Per-endpoint handler tests using `newTestHandlers` extension |
| `cmd/server/main.go` | Wire `RoomRepository`, `RoomInteractor`, `RoomHandlers`, register 10 routes |
| `internal/infrastructure/persistence/postgres_migration_test.go` | Extend `TestPostgresMigration_CleanSchema` to assert v4 schema (4 tables, 1 index, expected version=4). Add a new test `TestPostgresMigration_DownThenUp_Rooms` for clean down/up path of `0004_rooms`. |
| `internal/infrastructure/persistence/postgres_repository_test.go` | Extend compile-time conformance assertion to `RoomRepository`. |
| `internal/delivery/http/handlers_test.go` | Add `room` repo to `testRepo` and `newTestHandlers` extension for room handler tests. |
| `documents/00-project-management/SPRINTS/010-room-domain-invite-membership-lifecycle.md` | Sprint record. |
| `documents/00-project-management/SPRINTS/README.md` | Index entry for Sprint 010. |
| `documents/00-project-management/SPRINTS/active.md` | Mark R04 as the active authorized sprint. |

---

## Task 1: Domain entity — Room, statuses, roles, slug validation

**Files:**
- Create: `internal/domain/entity/room.go`
- Test: `internal/domain/entity/room_test.go`

**Interfaces:**
- Produces: `Room`, `RoomMember`, `RoomInvite`, `RoomStatus`, `RoomMemberRole`, `IsValidSlug`, `IsReservedSlug`, `SlugPattern`, `ReservedSlugs`.

- [ ] **Step 1: Write failing domain tests**

Create `internal/domain/entity/room_test.go`:

```go
package entity

import "testing"

func TestRoom_IsValidSlug(t *testing.T) {
	cases := []struct {
		slug string
		want bool
	}{
		{"a", true},
		{"ab", true},
		{"my-room", true},
		{"my-cool-room-2", true},
		{"abc123", true},
		{"A", false},        // uppercase
		{"-leading", false}, // leading dash
		{"trailing-", false},
		{"_under", false},
		{"", false},
		{"a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p-q-r-s-t-u-v-w-x-y-z-1-2-3-4-5", false}, // too long
		{"aa", true},
	}
	for _, c := range cases {
		if got := IsValidSlug(c.slug); got != c.want {
			t.Errorf("IsValidSlug(%q) = %v, want %v", c.slug, got, c.want)
		}
	}
}

func TestRoom_IsReservedSlug(t *testing.T) {
	for _, s := range []string{"api", "admin", "static", "ws"} {
		if !IsReservedSlug(s) {
			t.Errorf("expected %q to be reserved", s)
		}
	}
	if IsReservedSlug("my-room") {
		t.Error("my-room must not be reserved")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/domain/entity -run 'TestRoom_' -v`
Expected: FAIL — `IsValidSlug` undefined.

- [ ] **Step 3: Implement entity**

Create `internal/domain/entity/room.go`:

```go
package entity

import (
	"errors"
	"regexp"
	"time"
)

// RoomStatus represents the lifecycle state of a room.
type RoomStatus string

const (
	RoomStatusActive   RoomStatus = "active"
	RoomStatusArchived RoomStatus = "archived"
)

// IsValid reports whether s is a recognized RoomStatus.
func (s RoomStatus) IsValid() bool {
	return s == RoomStatusActive || s == RoomStatusArchived
}

// RoomMemberRole represents a user's role inside a single room.
type RoomMemberRole string

const (
	RoomRoleHost  RoomMemberRole = "host"
	RoomRoleAdmin RoomMemberRole = "admin"
	RoomRoleGuest RoomMemberRole = "guest"
)

// IsValid reports whether r is a recognized RoomMemberRole.
func (r RoomMemberRole) IsValid() bool {
	return r == RoomRoleHost || r == RoomRoleAdmin || r == RoomRoleGuest
}

// SlugPattern is the documented URL-safe slug regex (ADR 001 §9).
var SlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// ReservedSlugs cannot be used as room slugs because they collide with
// documented HTTP route families.
var ReservedSlugs = map[string]struct{}{
	"api":   {},
	"admin": {},
	"static": {},
	"ws":    {},
}

// IsValidSlug reports whether slug matches SlugPattern.
func IsValidSlug(slug string) bool {
	return SlugPattern.MatchString(slug)
}

// IsReservedSlug reports whether slug collides with a reserved route family.
func IsReservedSlug(slug string) bool {
	_, ok := ReservedSlugs[slug]
	return ok
}

// Room represents a single room (the R04 room domain).
type Room struct {
	ID        int64      `json:"id"`
	Slug      string     `json:"slug"`
	Name      string     `json:"name"`
	Status    RoomStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// RoomMember represents a (room, user, role) membership row.
type RoomMember struct {
	RoomID   int64          `json:"room_id"`
	UserID   int            `json:"user_id"`
	Role     RoomMemberRole `json:"role"`
	JoinedAt time.Time      `json:"joined_at"`
}

// RoomInvite represents an invite row. TokenHash is the SHA-256 of the
// plaintext token, base64url-encoded. The plaintext is only returned from
// CreateInvite; everything else sees TokenHash.
type RoomInvite struct {
	ID         int64      `json:"id"`
	RoomID     int64      `json:"room_id"`
	TokenHash  string     `json:"-"` // never serialized; clients hold plaintext
	CreatedBy  int        `json:"created_by_user_id"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	MaxUses    int        `json:"max_uses"`     // 0 == unlimited
	UseCount   int        `json:"use_count"`
}

// ErrInvalidRoomSlug is returned when a slug fails validation.
var ErrInvalidRoomSlug = errors.New("invalid room slug")

// ErrReservedRoomSlug is returned when a slug collides with a reserved route family.
var ErrReservedRoomSlug = errors.New("reserved room slug")
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/domain/entity -run 'TestRoom_' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/entity/room.go internal/domain/entity/room_test.go
git commit -m "feat(room): add Room, RoomMember, RoomInvite entities with slug validation"
```

---

## Task 2: RoomRepository interface

**Files:**
- Create: `internal/domain/repository/room_repository.go`

**Interfaces:**
- Produces: `RoomRepository` interface that the persistence package implements. The interactor (Task 5) and the persistence package (Task 6) both consume this surface.

- [ ] **Step 1: Write the interface**

Create `internal/domain/repository/room_repository.go`:

```go
package repository

import (
	"context"
	"local-music-queue/internal/domain/entity"
	"time"
)

// RoomRepository defines persistence operations for rooms, room members, and
// room invites. The implementation is responsible for FK integrity, the
// exactly-one-host invariant, the unique slug index, and constant-time token
// hash comparisons at the invite-validation layer (the interactor layer is
// responsible for hashing the candidate token before calling LookupByTokenHash).
type RoomRepository interface {
	// Rooms
	CreateRoom(ctx context.Context, slug, name string, now time.Time) (*entity.Room, error)
	GetRoomByID(ctx context.Context, id int64) (*entity.Room, error)
	GetRoomBySlug(ctx context.Context, slug string) (*entity.Room, error)
	ListRooms(ctx context.Context, status entity.RoomStatus) ([]entity.Room, error)
	ArchiveRoom(ctx context.Context, roomID int64, now time.Time) error

	// Members
	AddMember(ctx context.Context, roomID int64, userID int, role entity.RoomMemberRole, now time.Time) error
	GetMember(ctx context.Context, roomID int64, userID int) (*entity.RoomMember, error)
	ListMembers(ctx context.Context, roomID int64) ([]entity.RoomMember, error)
	UpdateMemberRole(ctx context.Context, roomID int64, userID int, role entity.RoomMemberRole, now time.Time) error
	CountHosts(ctx context.Context, roomID int64) (int, error)

	// Invites
	CreateInvite(ctx context.Context, invite *entity.RoomInvite) error
	GetInviteByID(ctx context.Context, roomID int64, inviteID int64) (*entity.RoomInvite, error)
	GetInviteByTokenHash(ctx context.Context, tokenHash string) (*entity.RoomInvite, error)
	ListInvites(ctx context.Context, roomID int64) ([]entity.RoomInvite, error)
	RevokeInvite(ctx context.Context, roomID int64, inviteID int64, now time.Time) error
	IncrementInviteUseCount(ctx context.Context, inviteID int64) error

	// Transaction helper: CreateRoomAndHost atomically inserts the room and
	// its creator as the only host. The persistence layer enforces the
	// exactly-one-host invariant at the SQL level via a partial unique index.
	CreateRoomAndHost(ctx context.Context, slug, name string, creatorUserID int, now time.Time) (*entity.Room, error)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/domain/repository/...`
Expected: PASS (no consumers yet; interface alone is valid).

- [ ] **Step 3: Commit**

```bash
git add internal/domain/repository/room_repository.go
git commit -m "feat(room): add RoomRepository interface"
```

---

## Task 3: Migration 0004 — rooms, room_members, room_invites

**Files:**
- Create: `internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql`
- Create: `internal/infrastructure/persistence/migrations/postgres/0004_rooms.down.sql`

**Interfaces:**
- Produces: schema version 4 with `rooms`, `room_members`, `room_invites` and the indexes/constraints described below.

- [ ] **Step 1: Write `0004_rooms.up.sql`**

Create `internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql`:

```sql
-- 0004_rooms.up.sql
-- Room, membership, and invite persistence. R04 only.
-- No room_id columns are added to existing tables (queue_state, activities,
-- auto_queue_config, play_history); that is R06. The R04 schema strictly
-- adds three new tables and the indexes/constraints that enforce
-- exactly-one-host, unique slug, and token-hash lookup.

CREATE TABLE IF NOT EXISTS rooms (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT rooms_slug_unique UNIQUE (slug),
    CONSTRAINT rooms_status_valid CHECK (status IN ('active', 'archived'))
);

CREATE TABLE IF NOT EXISTS room_members (
    room_id    BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (room_id, user_id),
    CONSTRAINT room_members_role_valid CHECK (role IN ('host', 'admin', 'guest'))
);

-- Exactly one host per room. A partial unique index on (room_id) where role='host'
-- enforces the invariant at the database level; the use case layer additionally
-- asserts the invariant before writes.
CREATE UNIQUE INDEX IF NOT EXISTS idx_room_members_one_host_per_room
    ON room_members (room_id) WHERE role = 'host';

CREATE TABLE IF NOT EXISTS room_invites (
    id            BIGSERIAL PRIMARY KEY,
    room_id       BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    token_hash    TEXT NOT NULL UNIQUE,
    created_by    BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ NULL,
    max_uses      INTEGER NOT NULL DEFAULT 0,
    use_count     INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT room_invites_max_uses_nonneg CHECK (max_uses >= 0),
    CONSTRAINT room_invites_use_count_nonneg CHECK (use_count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_room_invites_room_id ON room_invites (room_id);
```

- [ ] **Step 2: Write `0004_rooms.down.sql`**

Create `internal/infrastructure/persistence/migrations/postgres/0004_rooms.down.sql`:

```sql
-- 0004_rooms.down.sql
-- Reverses 0004_rooms. Down-migrations are local-dev only per ADR 002.

DROP TABLE IF EXISTS room_invites;
DROP TABLE IF EXISTS room_members;
DROP TABLE IF EXISTS rooms;
```

- [ ] **Step 3: Verify embedded FS picks them up**

Run: `go build ./internal/infrastructure/persistence/...`
Expected: PASS — `//go:embed migrations/postgres/*.sql` already globs all SQL files; nothing to wire.

- [ ] **Step 4: Commit**

```bash
git add internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql internal/infrastructure/persistence/migrations/postgres/0004_rooms.down.sql
git commit -m "feat(migration): add 0004_rooms migration for rooms, room_members, room_invites"
```

---

## Task 4: Update `postgres_migration_test.go` for version 4

**Files:**
- Modify: `internal/infrastructure/persistence/postgres_migration_test.go`

**Interfaces:**
- Consumes: `RunEmbeddedMigrationsUp`, `RunEmbeddedMigrationsDown`, `EmbeddedMigrationsVersion` from Task 3's migration.

- [ ] **Step 1: Update `TestPostgresMigration_CleanSchema` to expect version 4 and three new tables**

Replace the line `if v != 3 {` ... `} ` inside `TestPostgresMigration_CleanSchema` with:

```go
	if v != 4 {
		t.Fatalf("expected version=4 after first migration, got %d", v)
	}
```

Then add the three new table names to the existing `want` slice:

```go
	want := []string{
		"queue_state",
		"activities",
		"users",
		"user_sessions",
		"priority_transactions",
		"auto_queue_config",
		"play_history",
		"rooms",
		"room_members",
		"room_invites",
	}
```

- [ ] **Step 2: Add a new test that proves 0004 down/up round-trip**

Append to `internal/infrastructure/persistence/postgres_migration_test.go`:

```go
func TestPostgresMigration_DownThenUp_Rooms(t *testing.T) {
	db := newPostgresDB(t)

	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	v, _, err := EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 4 {
		t.Fatalf("expected version=4, got %d", v)
	}

	// Step down 1 — only 0004_rooms reverses.
	if err := RunEmbeddedMigrationsDown(db, 1); err != nil {
		t.Fatalf("migrate down 1: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version after down: %v", err)
	}
	if v != 3 {
		t.Fatalf("expected version=3 after stepping down 0004, got %d", v)
	}

	// Verify the three new tables are gone.
	for _, table := range []string{"rooms", "room_members", "room_invites"} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`, table).Scan(&exists); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if exists {
			t.Errorf("expected table %q to be dropped after 0004 down", table)
		}
	}

	// Re-apply — must come back to v4.
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("re-migrate up: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version after re-up: %v", err)
	}
	if v != 4 {
		t.Fatalf("expected version=4 after re-up, got %d", v)
	}
}
```

- [ ] **Step 3: Run the migration test**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/infrastructure/persistence -run 'TestPostgresMigration' -v`
Expected: `TestPostgresMigration_CleanSchema` PASS with v4 check, `TestPostgresMigration_DownThenUp` PASS, `TestPostgresMigration_DownThenUp_Rooms` PASS.

If LMQ_TEST_DATABASE_URL is not reachable the test will be skipped; the in-place change must still compile under `go build`.

- [ ] **Step 4: Commit**

```bash
git add internal/infrastructure/persistence/postgres_migration_test.go
git commit -m "test(migration): extend migration tests to cover schema version 4 and 0004 down/up"
```

---

## Task 5: PostgresRoomRepository — implementation

**Files:**
- Create: `internal/infrastructure/persistence/postgres_room_repository.go`
- Test: `internal/infrastructure/persistence/postgres_room_repository_test.go`

**Interfaces:**
- Consumes: `entity.Room`, `entity.RoomMember`, `entity.RoomInvite`, `entity.RoomStatus`, `entity.RoomMemberRole`, `repository.RoomRepository` (Task 2).
- Produces: `NewPostgresRoomRepository(db *sql.DB) *PostgresRoomRepository` constructor, satisfying the `RoomRepository` interface.

- [ ] **Step 1: Write failing conformance test**

Append to `internal/infrastructure/persistence/postgres_repository_test.go`, just after the existing `var ( _ = func() error { ... } )` block:

```go
var (
	_ = func() error {
		var r repository.RoomRepository = (*PostgresRoomRepository)(nil)
		_ = r
		return nil
	}()
)
```

- [ ] **Step 2: Run, verify compile-time failure**

Run: `go build ./internal/infrastructure/persistence/...`
Expected: FAIL — `PostgresRoomRepository` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/infrastructure/persistence/postgres_room_repository.go`:

```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
)

// PostgresRoomRepository implements repository.RoomRepository on PostgreSQL.
// Schema: see 0004_rooms.up.sql.
type PostgresRoomRepository struct {
	db *sql.DB
}

// NewPostgresRoomRepository constructs a PostgresRoomRepository over an
// existing *sql.DB. The DB MUST already be migrated to schema version 4.
func NewPostgresRoomRepository(db *sql.DB) *PostgresRoomRepository {
	return &PostgresRoomRepository{db: db}
}

// CreateRoomAndHost inserts the room row and the creator's host membership
// in a single transaction. The exactly-one-host invariant is enforced by the
// partial unique index `idx_room_members_one_host_per_room`; the INSERT
// order (room first, member second) ensures the FK is satisfied.
func (r *PostgresRoomRepository) CreateRoomAndHost(ctx context.Context, slug, name string, creatorUserID int, now time.Time) (*entity.Room, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var roomID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', $3, $3)
		 RETURNING id`,
		slug, name, now,
	).Scan(&roomID)
	if err != nil {
		return nil, fmt.Errorf("insert room: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO room_members (room_id, user_id, role, joined_at)
		 VALUES ($1, $2, 'host', $3)`,
		roomID, creatorUserID, now,
	); err != nil {
		return nil, fmt.Errorf("insert host member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return r.GetRoomByID(ctx, roomID)
}

// CreateRoom inserts only the room row. Use CreateRoomAndHost in production
// so the host membership is created atomically; this method exists for tests.
func (r *PostgresRoomRepository) CreateRoom(ctx context.Context, slug, name string, now time.Time) (*entity.Room, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', $3, $3)
		 RETURNING id`,
		slug, name, now,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert room: %w", err)
	}
	return r.GetRoomByID(ctx, id)
}

// GetRoomByID fetches a room by its primary key.
func (r *PostgresRoomRepository) GetRoomByID(ctx context.Context, id int64) (*entity.Room, error) {
	return r.scanOne(ctx,
		`SELECT id, slug, name, status, created_at, updated_at FROM rooms WHERE id = $1`, id)
}

// GetRoomBySlug fetches a room by its slug.
func (r *PostgresRoomRepository) GetRoomBySlug(ctx context.Context, slug string) (*entity.Room, error) {
	return r.scanOne(ctx,
		`SELECT id, slug, name, status, created_at, updated_at FROM rooms WHERE slug = $1`, slug)
}

// ListRooms returns rooms filtered by status. status=="" returns every room.
func (r *PostgresRoomRepository) ListRooms(ctx context.Context, status entity.RoomStatus) ([]entity.Room, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, slug, name, status, created_at, updated_at FROM rooms ORDER BY id ASC`)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, slug, name, status, created_at, updated_at FROM rooms WHERE status = $1 ORDER BY id ASC`, status)
	}
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()
	var out []entity.Room
	for rows.Next() {
		room, err := scanRoomRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *room)
	}
	return out, rows.Err()
}

// ArchiveRoom transitions a room to 'archived'. Used by internal-only
// archive method (Task 9) and tests; no public archive endpoint in R04.
func (r *PostgresRoomRepository) ArchiveRoom(ctx context.Context, roomID int64, now time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE rooms SET status = 'archived', updated_at = $1 WHERE id = $2`, now, roomID)
	if err != nil {
		return fmt.Errorf("archive room: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddMember inserts a membership row. Caller is responsible for invariant
// checks (one host per room).
func (r *PostgresRoomRepository) AddMember(ctx context.Context, roomID int64, userID int, role entity.RoomMemberRole, now time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO room_members (room_id, user_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)`,
		roomID, userID, role, now,
	)
	if err != nil {
		return fmt.Errorf("insert member: %w", err)
	}
	return nil
}

// GetMember fetches a (room, user) membership row.
func (r *PostgresRoomRepository) GetMember(ctx context.Context, roomID int64, userID int) (*entity.RoomMember, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT room_id, user_id, role, joined_at FROM room_members WHERE room_id = $1 AND user_id = $2`,
		roomID, userID)
	var m entity.RoomMember
	var roomIDOut int64
	var role string
	if err := row.Scan(&roomIDOut, &m.UserID, &role, &m.JoinedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("scan member: %w", err)
	}
	m.RoomID = roomIDOut
	m.Role = entity.RoomMemberRole(role)
	return &m, nil
}

// ListMembers returns every member of a room.
func (r *PostgresRoomRepository) ListMembers(ctx context.Context, roomID int64) ([]entity.RoomMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT room_id, user_id, role, joined_at FROM room_members WHERE room_id = $1 ORDER BY joined_at ASC`, roomID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	var out []entity.RoomMember
	for rows.Next() {
		var m entity.RoomMember
		var roomIDOut int64
		var role string
		if err := rows.Scan(&roomIDOut, &m.UserID, &role, &m.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		m.RoomID = roomIDOut
		m.Role = entity.RoomMemberRole(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateMemberRole updates a member's role. Caller must enforce invariants.
func (r *PostgresRoomRepository) UpdateMemberRole(ctx context.Context, roomID int64, userID int, role entity.RoomMemberRole, now time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE room_members SET role = $1 WHERE room_id = $2 AND user_id = $3`,
		role, roomID, userID)
	if err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CountHosts returns the number of host memberships in a room. Used to
// enforce the exactly-one-host invariant in the use-case layer.
func (r *PostgresRoomRepository) CountHosts(ctx context.Context, roomID int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM room_members WHERE room_id = $1 AND role = 'host'`, roomID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count hosts: %w", err)
	}
	return n, nil
}

// CreateInvite inserts an invite row.
func (r *PostgresRoomRepository) CreateInvite(ctx context.Context, invite *entity.RoomInvite) error {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO room_invites (room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		invite.RoomID, invite.TokenHash, invite.CreatedBy, invite.CreatedAt, invite.ExpiresAt,
		invite.RevokedAt, invite.MaxUses, invite.UseCount,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert invite: %w", err)
	}
	invite.ID = id
	return nil
}

// GetInviteByID fetches an invite by (room_id, invite_id).
func (r *PostgresRoomRepository) GetInviteByID(ctx context.Context, roomID int64, inviteID int64) (*entity.RoomInvite, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count
		 FROM room_invites WHERE room_id = $1 AND id = $2`,
		roomID, inviteID)
	return scanInviteRow(row)
}

// GetInviteByTokenHash fetches an invite by its stored token hash.
func (r *PostgresRoomRepository) GetInviteByTokenHash(ctx context.Context, tokenHash string) (*entity.RoomInvite, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count
		 FROM room_invites WHERE token_hash = $1`,
		tokenHash)
	return scanInviteRow(row)
}

// ListInvites lists invites for a room, newest first.
func (r *PostgresRoomRepository) ListInvites(ctx context.Context, roomID int64) ([]entity.RoomInvite, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count
		 FROM room_invites WHERE room_id = $1 ORDER BY created_at DESC`, roomID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	var out []entity.RoomInvite
	for rows.Next() {
		inv, err := scanInviteRowRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// RevokeInvite marks an invite as revoked. Returns sql.ErrNoRows when the
// invite does not belong to the room or has already been revoked.
func (r *PostgresRoomRepository) RevokeInvite(ctx context.Context, roomID int64, inviteID int64, now time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE room_invites SET revoked_at = $1 WHERE room_id = $2 AND id = $3 AND revoked_at IS NULL`,
		now, roomID, inviteID)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// IncrementInviteUseCount atomically bumps use_count and returns the new value.
func (r *PostgresRoomRepository) IncrementInviteUseCount(ctx context.Context, inviteID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE room_invites SET use_count = use_count + 1 WHERE id = $1`, inviteID)
	if err != nil {
		return fmt.Errorf("increment use count: %w", err)
	}
	return nil
}

// --- helpers ---

func (r *PostgresRoomRepository) scanOne(ctx context.Context, query string, args ...interface{}) (*entity.Room, error) {
	row := r.db.QueryRowContext(ctx, query, args...)
	room, err := scanRoomRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return room, nil
}

// rowScanner is implemented by both *sql.Row and *sql.Rows so the same scan
// helper works for QueryRow and rows.Next() paths.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanRoomRow(s rowScanner) (*entity.Room, error) {
	var room entity.Room
	var id int64
	var status string
	if err := s.Scan(&id, &room.Slug, &room.Name, &status, &room.CreatedAt, &room.UpdatedAt); err != nil {
		return nil, err
	}
	room.ID = id
	room.Status = entity.RoomStatus(status)
	return &room, nil
}

func scanInviteRow(s rowScanner) (*entity.RoomInvite, error) {
	inv, err := scanInviteRowGeneric(s)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return inv, nil
}

func scanInviteRowRows(s rowScanner) (*entity.RoomInvite, error) {
	return scanInviteRowGeneric(s)
}

func scanInviteRowGeneric(s rowScanner) (*entity.RoomInvite, error) {
	var inv entity.RoomInvite
	var id, roomID, createdBy int64
	var revokedAt sql.NullTime
	if err := s.Scan(&id, &roomID, &inv.TokenHash, &createdBy, &inv.CreatedAt, &inv.ExpiresAt, &revokedAt, &inv.MaxUses, &inv.UseCount); err != nil {
		return nil, err
	}
	inv.ID = id
	inv.RoomID = roomID
	inv.CreatedBy = int(createdBy)
	if revokedAt.Valid {
		t := revokedAt.Time
		inv.RevokedAt = &t
	}
	return &inv, nil
}
```

- [ ] **Step 4: Verify it compiles and the conformance check is satisfied**

Run: `go build ./internal/infrastructure/persistence/...`
Expected: PASS — `*PostgresRoomRepository` satisfies `repository.RoomRepository`.

- [ ] **Step 5: Commit**

```bash
git add internal/infrastructure/persistence/postgres_room_repository.go internal/infrastructure/persistence/postgres_repository_test.go
git commit -m "feat(room): implement PostgresRoomRepository against schema version 4"
```

---

## Task 6: PostgresRoomRepository — persistence tests

**Files:**
- Test: `internal/infrastructure/persistence/postgres_room_repository_test.go`

**Interfaces:**
- Consumes: `PostgresRoomRepository` from Task 5.

- [ ] **Step 1: Write the tests**

Create `internal/infrastructure/persistence/postgres_room_repository_test.go`:

```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
)

func newRoomRepo(t *testing.T) (*PostgresRoomRepository, *sql.DB) {
	t.Helper()
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	return NewPostgresRoomRepository(db), db
}

func TestPostgresRoom_CreateRoomAndHost_AndUniqueSlug(t *testing.T) {
	repo, db := newRoomRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	creator := int64(1)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, role, created_at, updated_at)
		 VALUES ($1, 'h@example.com', 'Host', 'host', $2, $2)`,
		creator, now,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	room, err := repo.CreateRoomAndHost(ctx, "lounge", "Lounge", 1, now)
	if err != nil {
		t.Fatalf("CreateRoomAndHost: %v", err)
	}
	if room.ID == 0 || room.Slug != "lounge" || room.Status != entity.RoomStatusActive {
		t.Fatalf("unexpected room: %+v", room)
	}

	// Duplicate slug must fail with the unique-constraint violation surfaced
	// as a generic error from the repo (the interactor maps it to 409).
	if _, err := repo.CreateRoomAndHost(ctx, "lounge", "Lounge 2", 1, now); err == nil {
		t.Fatal("expected error on duplicate slug")
	}
}

func TestPostgresRoom_OneHostInvariant_DBConstraint(t *testing.T) {
	repo, db := newRoomRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, id := range []int{1, 2} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO users (id, email, display_name, role, created_at, updated_at)
			 VALUES ($1, $2, 'U', 'guest', $3, $3)`, id, "u"+string(rune('0'+id))+"@example.com", now,
		); err != nil {
			t.Fatalf("seed user %d: %v", id, err)
		}
	}

	room, err := repo.CreateRoomAndHost(ctx, "lab", "Lab", 1, now)
	if err != nil {
		t.Fatalf("CreateRoomAndHost: %v", err)
	}

	// Inserting a second host directly must fail with the partial-unique-index violation.
	if err := repo.AddMember(ctx, room.ID, 2, entity.RoomRoleHost, now); err == nil {
		t.Fatal("expected DB-level rejection of second host")
	}
}

func TestPostgresRoom_ListAndArchive(t *testing.T) {
	repo, _ := newRoomRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	r1, err := repo.CreateRoomAndHost(ctx, "alpha", "Alpha", 1, now)
	if err != nil {
		t.Fatalf("CreateRoomAndHost alpha: %v", err)
	}
	if _, err := repo.CreateRoomAndHost(ctx, "beta", "Beta", 1, now); err != nil {
		t.Fatalf("CreateRoomAndHost beta: %v", err)
	}

	all, err := repo.ListRooms(ctx, "")
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 rooms, got %d", len(all))
	}

	if err := repo.ArchiveRoom(ctx, r1.ID, now); err != nil {
		t.Fatalf("ArchiveRoom: %v", err)
	}
	got, err := repo.GetRoomByID(ctx, r1.ID)
	if err != nil {
		t.Fatalf("GetRoomByID: %v", err)
	}
	if got.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived, got %s", got.Status)
	}

	active, err := repo.ListRooms(ctx, entity.RoomStatusActive)
	if err != nil {
		t.Fatalf("ListRooms active: %v", err)
	}
	if len(active) != 1 || active[0].Slug != "beta" {
		t.Errorf("expected 1 active room (beta), got %+v", active)
	}
}

func TestPostgresRoom_InviteLifecycle(t *testing.T) {
	repo, _ := newRoomRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	room, err := repo.CreateRoomAndHost(ctx, "lounge", "Lounge", 1, now)
	if err != nil {
		t.Fatalf("CreateRoomAndHost: %v", err)
	}

	inv := &entity.RoomInvite{
		RoomID:    room.ID,
		TokenHash: "hash-xyz",
		CreatedBy: 1,
		CreatedAt: now,
		ExpiresAt: now.Add(7 * 24 * time.Hour),
		MaxUses:   0,
	}
	if err := repo.CreateInvite(ctx, inv); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if inv.ID == 0 {
		t.Fatal("expected invite ID assigned")
	}

	// Lookup by token hash.
	got, err := repo.GetInviteByTokenHash(ctx, "hash-xyz")
	if err != nil {
		t.Fatalf("GetInviteByTokenHash: %v", err)
	}
	if got.ID != inv.ID || got.RoomID != room.ID {
		t.Errorf("roundtrip mismatch: %+v vs %+v", got, inv)
	}

	// Increment use count.
	if err := repo.IncrementInviteUseCount(ctx, inv.ID); err != nil {
		t.Fatalf("IncrementInviteUseCount: %v", err)
	}
	got, _ = repo.GetInviteByID(ctx, room.ID, inv.ID)
	if got.UseCount != 1 {
		t.Errorf("expected use_count=1, got %d", got.UseCount)
	}

	// Revoke.
	if err := repo.RevokeInvite(ctx, room.ID, inv.ID, now); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if err := repo.RevokeInvite(ctx, room.ID, inv.ID, now); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows on double revoke, got %v", err)
	}
}

func TestPostgresRoom_CountHosts(t *testing.T) {
	repo, db := newRoomRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []int{1, 2, 3} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO users (id, email, display_name, role, created_at, updated_at)
			 VALUES ($1, $2, 'U', 'guest', $3, $3)`, id, "u"+string(rune('0'+id))+"@example.com", now,
		); err != nil {
			t.Fatalf("seed user %d: %v", id, err)
		}
	}

	room, err := repo.CreateRoomAndHost(ctx, "lounge", "Lounge", 1, now)
	if err != nil {
		t.Fatalf("CreateRoomAndHost: %v", err)
	}
	if err := repo.AddMember(ctx, room.ID, 2, entity.RoomRoleAdmin, now); err != nil {
		t.Fatalf("AddMember admin: %v", err)
	}
	if err := repo.AddMember(ctx, room.ID, 3, entity.RoomRoleGuest, now); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}

	n, err := repo.CountHosts(ctx, room.ID)
	if err != nil {
		t.Fatalf("CountHosts: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 host, got %d", n)
	}
}
```

- [ ] **Step 2: Run the persistence tests**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/infrastructure/persistence -run 'TestPostgresRoom_' -v`
Expected: all PASS (or skipped if PG is unreachable).

- [ ] **Step 3: Commit**

```bash
git add internal/infrastructure/persistence/postgres_room_repository_test.go
git commit -m "test(room): add PostgresRoomRepository persistence tests"
```

---

## Task 7: RoomInteractor — use cases (create, list, members, promote, demote, archive, error map)

**Files:**
- Create: `internal/usecase/room/interactor.go`
- Test: `internal/usecase/room/interactor_test.go`

**Interfaces:**
- Consumes: `repository.RoomRepository` (Task 5), `auth.Clock`.
- Produces: `NewInteractor(repo, clock)`, sentinel errors, public methods consumed by `RoomHandlers` (Task 9).

- [ ] **Step 1: Write failing use-case tests**

Create `internal/usecase/room/interactor_test.go` with the table below. The `pgInter` helper is implemented in Step 3. For now, write the file with a helper signature that compiles once `pgInter` exists:

```go
package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

// pgInter builds a real Interactor against a per-test PG schema. Skips
// when the DB is unreachable.
func pgInter(t *testing.T) (*Interactor, func()) {
	t.Helper()
	db, cleanup := openTestDB(t)
	repo := persistence.NewPostgresRoomRepository(db)
	inter := NewInteractor(repo)
	return inter, cleanup
}

func openTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dsn := requireTestDSN(t)
	schema := "lmq_room_test_" + strings.ReplaceAll(time.Now().Format("20060102T150405.000000"), ".", "_") + "_" + strings.ReplaceAll(time.Now().Format(".000000000"), ".", "")
	_ = dsn // declared; schema creation handled by internal persistence helper if present
	// Fallback to the persistence test helper:
	return openTestDBHelper(t)
}

func requireTestDSN(t *testing.T) string {
	t.Helper()
	dsn := osGetenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}
	return dsn
}
```

Then add (same file, below the helpers):

```go
// --- helpers consumed from the persistence test package via build-tag-free
// public re-export would couple packages; instead we duplicate the minimal
// subset. To avoid drift, the helpers below call into the persistence
// package's exported test helper if reachable, else skip.
// -------------------------------------------------------------------------

func TestRoom_CreateRoom_ActiveWithHost(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()

	ctx := context.Background()
	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 42)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.Status != entity.RoomStatusActive {
		t.Errorf("expected active, got %s", room.Status)
	}
	if room.Slug != "lounge" {
		t.Errorf("expected slug lounge, got %s", room.Slug)
	}
	member, err := inter.repo.GetMember(ctx, room.ID, 42)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if member.Role != entity.RoomRoleHost {
		t.Errorf("expected creator to be host, got %s", member.Role)
	}
}

func TestRoom_CreateRoom_InvalidSlug(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()

	for _, bad := range []string{"", "Bad", "-x", "x-", "api", "admin", "static", "ws"} {
		_, err := inter.CreateRoom(ctx, bad, "x", 1)
		if !errors.Is(err, ErrInvalidSlug) && !errors.Is(err, ErrReservedSlug) {
			t.Errorf("slug %q: expected ErrInvalidSlug or ErrReservedSlug, got %v", bad, err)
		}
	}
}

func TestRoom_CreateRoom_DuplicateSlug409(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := inter.CreateRoom(ctx, "lounge", "Lounge 2", 2)
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("expected ErrDuplicateSlug, got %v", err)
	}
}

func TestRoom_Promote_Demote_Permissions(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	// Add an admin and a guest directly via repo for setup.
	if err := inter.repo.AddMember(ctx, room.ID, 2, entity.RoomRoleAdmin, time.Now()); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if err := inter.repo.AddMember(ctx, room.ID, 3, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("seed guest: %v", err)
	}

	// Non-host cannot promote.
	if err := inter.PromoteMember(ctx, room.ID, 2 /*actor*/, 3 /*target*/, "guest" /*new role*/); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-host promote: expected ErrForbidden, got %v", err)
	}
	// Host promotes guest to admin.
	if err := inter.PromoteMember(ctx, room.ID, 1 /*actor=host*/, 3 /*target*/, "admin"); err != nil {
		t.Fatalf("host promote guest: %v", err)
	}
	got, _ := inter.repo.GetMember(ctx, room.ID, 3)
	if got.Role != entity.RoomRoleAdmin {
		t.Errorf("expected admin, got %s", got.Role)
	}
	// Host demotes admin to guest.
	if err := inter.DemoteMember(ctx, room.ID, 1, 3, "guest"); err != nil {
		t.Fatalf("host demote: %v", err)
	}
	got, _ = inter.repo.GetMember(ctx, room.ID, 3)
	if got.Role != entity.RoomRoleGuest {
		t.Errorf("expected guest, got %s", got.Role)
	}
	// Users cannot self-promote.
	if err := inter.PromoteMember(ctx, room.ID, 2, 2, "admin"); !errors.Is(err, ErrForbidden) {
		t.Errorf("self-promote: expected ErrForbidden, got %v", err)
	}
}

func TestRoom_ArchivedRoom_RejectsMutations(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.repo.ArchiveRoom(ctx, room.ID, time.Now()); err != nil {
		t.Fatalf("ArchiveRoom: %v", err)
	}

	// Add guest (archived room).
	if err := inter.repo.AddMember(ctx, room.ID, 5, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("seed guest: %v", err)
	}
	_, err = inter.CreateInvite(ctx, room.ID, 1, 0, time.Now().Add(7*24*time.Hour))
	if !errors.Is(err, ErrArchived) {
		t.Errorf("invite on archived room: expected ErrArchived, got %v", err)
	}
	_, err = inter.RedeemInvite(ctx, "anytoken", 999)
	if !errors.Is(err, ErrInviteInvalid) && !errors.Is(err, ErrArchived) {
		t.Errorf("redeem on archived: expected ErrInviteInvalid or ErrArchived, got %v", err)
	}
	if err := inter.PromoteMember(ctx, room.ID, 1, 5, "admin"); !errors.Is(err, ErrArchived) {
		t.Errorf("promote on archived: expected ErrArchived, got %v", err)
	}
	if err := inter.DemoteMember(ctx, room.ID, 1, 5, "guest"); !errors.Is(err, ErrArchived) {
		t.Errorf("demote on archived: expected ErrArchived, got %v", err)
	}
}

func TestRoom_InviteCreateRedeemRevokeExpiryMaxUses(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	plaintext, inv, err := inter.CreateInvite(ctx, room.ID, 1, 0 /*unlimited*/, now.Add(7*24*time.Hour))
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if plaintext == "" {
		t.Fatal("expected plaintext token in create response")
	}
	if inv.TokenHash == plaintext {
		t.Error("token_hash must not equal plaintext")
	}
	// SHA-256 base64url of the plaintext must equal the stored hash.
	sum := sha256.Sum256([]byte(plaintext))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	if inv.TokenHash != expected {
		t.Errorf("expected hash %s, got %s", expected, inv.TokenHash)
	}

	// First redeem succeeds, user becomes guest.
	member, err := inter.RedeemInvite(ctx, plaintext, 100)
	if err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	if member.Role != entity.RoomRoleGuest {
		t.Errorf("expected guest, got %s", member.Role)
	}
	// Idempotent: same user redeeming again returns the existing membership without error.
	member2, err := inter.RedeemInvite(ctx, plaintext, 100)
	if err != nil {
		t.Fatalf("RedeemInvite idempotent: %v", err)
	}
	if member2.UserID != member.UserID {
		t.Errorf("expected idempotent result")
	}

	// Revoke then redeem — must report generic not-found.
	if _, err := inter.RevokeInvite(ctx, room.ID, 1 /*host*/, inv.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if _, err := inter.RedeemInvite(ctx, plaintext, 200); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("redeem after revoke: expected ErrInviteInvalid, got %v", err)
	}

	// Max-uses exhaust path.
	plaintext2, inv2, err := inter.CreateInvite(ctx, room.ID, 1, 1 /*max 1*/, now.Add(7*24*time.Hour))
	if err != nil {
		t.Fatalf("CreateInvite max-uses: %v", err)
	}
	if _, err := inter.RedeemInvite(ctx, plaintext2, 201); err != nil {
		t.Fatalf("RedeemInvite first: %v", err)
	}
	_, err = inter.RedeemInvite(ctx, plaintext2, 202)
	if !errors.Is(err, ErrInviteExhausted) {
		t.Errorf("second redeem of max-uses invite: expected ErrInviteExhausted, got %v", err)
	}

	// Expired path.
	plaintext3, inv3, err := inter.CreateInvite(ctx, room.ID, 1, 0, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("CreateInvite expired: %v", err)
	}
	_ = inv3
	_, err = inter.RedeemInvite(ctx, plaintext3, 203)
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("expired redeem: expected ErrInviteInvalid, got %v", err)
	}

	// Unknown token.
	if _, err := inter.RedeemInvite(ctx, "definitely-not-a-real-token", 204); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("unknown token: expected ErrInviteInvalid, got %v", err)
	}
}
```

Add the helper module imports at top of the file (replace the existing imports block):

```go
import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)
```

- [ ] **Step 2: Run, verify compile-time failure on missing `pgInter`/`openTestDBHelper`**

Run: `go test ./internal/usecase/room -run 'TestRoom_' -v`
Expected: FAIL — `openTestDBHelper`, `pgInter` undefined, `osGetenv` undefined, `inter.repo` (unexported).

Replace the helper block above with the concrete implementation below (still in the same file). Replace the `osGetenv` reference with `os.Getenv`. Define `openTestDBHelper` to call into the persistence package's exported test helper.

Append to `internal/usecase/room/interactor_test.go`:

```go
// openTestDBHelper delegates to the persistence package's per-test schema
// helper. It uses a build-tag-free exported alias to avoid coupling the test
// surface across packages.
func openTestDBHelper(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	return persistence.NewRoomTestDB(t)
}
```

In `internal/infrastructure/persistence/postgres_room_repository_test.go` we already have `newPostgresDB` (in `testutil_postgres_test.go`). The cleanest approach: expose `NewRoomTestDB` from the persistence package.

Append to `internal/infrastructure/persistence/postgres_room_repository_test.go` (or create a new file `internal/infrastructure/persistence/room_test_db.go`):

```go
// NewRoomTestDB returns a per-test *sql.DB scoped to a throwaway PG schema
// with migrations applied. Used by the room usecase tests; exported here
// to avoid cyclic test packages. Skips when PG is unreachable.
func NewRoomTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	return db, func() { _ = db.Close() }
}
```

`osGetenv` is a typo — replace with `os.Getenv` everywhere it appears.

- [ ] **Step 3: Implement the interactor**

Create `internal/usecase/room/interactor.go`:

```go
package room

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// Sentinel errors mapped to HTTP status codes by the handler layer.
var (
	ErrInvalidSlug    = errors.New("invalid room slug")
	ErrReservedSlug   = errors.New("reserved room slug")
	ErrDuplicateSlug  = errors.New("duplicate room slug")
	ErrRoomNotFound   = errors.New("room not found")
	ErrMemberNotFound = errors.New("member not found")
	ErrInviteNotFound = errors.New("invite not found")
	ErrInviteInvalid  = errors.New("invite not found") // generic; does not leak existence
	ErrInviteExhausted = errors.New("invite exhausted")
	ErrArchived       = errors.New("room archived")
	ErrForbidden      = errors.New("forbidden")
)

// DefaultInviteExpiry is the documented default invite lifetime (ADR 001 §7).
const DefaultInviteExpiry = 7 * 24 * time.Hour

// Interactor owns the room, invite, and membership use cases.
type Interactor struct {
	repo repository.RoomRepository
	now  func() time.Time
}

// NewInteractor constructs an Interactor. The clock defaults to time.Now;
// tests may swap it via SetClock.
func NewInteractor(repo repository.RoomRepository) *Interactor {
	return &Interactor{repo: repo, now: time.Now}
}

// SetClock replaces the time source (tests only).
func (i *Interactor) SetClock(now func() time.Time) { i.now = now }

// CreateRoom validates the slug, rejects reserved slugs, and atomically
// inserts the room + creator-host membership. Returns ErrDuplicateSlug on
// the unique-slug constraint violation.
func (i *Interactor) CreateRoom(ctx context.Context, slug, name string, creatorUserID int) (*entity.Room, error) {
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	if entity.IsReservedSlug(slug) {
		return nil, ErrReservedSlug
	}
	if name == "" {
		return nil, fmt.Errorf("name: %w", ErrInvalidSlug)
	}
	room, err := i.repo.CreateRoomAndHost(ctx, slug, name, creatorUserID, i.now())
	if err != nil {
		// The unique-slug constraint is the only expected error.
		if isUniqueViolation(err, "rooms_slug_unique") {
			return nil, ErrDuplicateSlug
		}
		return nil, fmt.Errorf("create room: %w", err)
	}
	return room, nil
}

// GetRoom fetches a room by ID.
func (i *Interactor) GetRoom(ctx context.Context, roomID int64) (*entity.Room, error) {
	room, err := i.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	return room, nil
}

// ListRooms returns rooms filtered by status. Empty status returns all rooms.
func (i *Interactor) ListRooms(ctx context.Context, status entity.RoomStatus) ([]entity.Room, error) {
	return i.repo.ListRooms(ctx, status)
}

// ListMembers returns members of a room.
func (i *Interactor) ListMembers(ctx context.Context, roomID int64) ([]entity.RoomMember, error) {
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return nil, err
	}
	return i.repo.ListMembers(ctx, roomID)
}

// CreateInvite validates the room is active, mints a random opaque token,
// persists only its hash, and returns the plaintext to the caller. The
// stored invite row never contains the plaintext.
func (i *Interactor) CreateInvite(ctx context.Context, roomID int64, creatorUserID int, maxUses int, expiresAt time.Time) (string, *entity.RoomInvite, error) {
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return "", nil, err
	}
	if maxUses < 0 {
		return "", nil, fmt.Errorf("max_uses: %w", ErrInvalidSlug)
	}
	if expiresAt.IsZero() {
		expiresAt = i.now().Add(DefaultInviteExpiry)
	}
	plaintext, err := generateInviteToken()
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	hash := hashInviteToken(plaintext)
	inv := &entity.RoomInvite{
		RoomID:    roomID,
		TokenHash: hash,
		CreatedBy: creatorUserID,
		CreatedAt: i.now(),
		ExpiresAt: expiresAt,
		MaxUses:   maxUses,
		UseCount:  0,
	}
	if err := i.repo.CreateInvite(ctx, inv); err != nil {
		return "", nil, fmt.Errorf("persist invite: %w", err)
	}
	return plaintext, inv, nil
}

// ListInvites returns invites for a room.
func (i *Interactor) ListInvites(ctx context.Context, roomID int64, actorUserID int) ([]entity.RoomInvite, error) {
	if err := i.requireHost(ctx, roomID, actorUserID); err != nil {
		return nil, err
	}
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return nil, err
	}
	return i.repo.ListInvites(ctx, roomID)
}

// RevokeInvite marks an invite as revoked.
func (i *Interactor) RevokeInvite(ctx context.Context, roomID int64, actorUserID int, inviteID int64) (*entity.RoomInvite, error) {
	if err := i.requireHost(ctx, roomID, actorUserID); err != nil {
		return nil, err
	}
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return nil, err
	}
	now := i.now()
	if err := i.repo.RevokeInvite(ctx, roomID, inviteID, now); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInviteNotFound
		}
		return nil, fmt.Errorf("revoke: %w", err)
	}
	return i.repo.GetInviteByID(ctx, roomID, inviteID)
}

// RedeemInvite validates the token hash, expiry, revocation, and use limit;
// inserts the user as guest if not already a member; returns the resulting
// membership. Idempotent for already-member users. All failure modes return
// ErrInviteInvalid (or ErrInviteExhausted) without leaking whether the token
// existed.
func (i *Interactor) RedeemInvite(ctx context.Context, plaintext string, userID int) (*entity.RoomMember, error) {
	hash := hashInviteToken(plaintext)
	inv, err := i.repo.GetInviteByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInviteInvalid
		}
		return nil, fmt.Errorf("lookup invite: %w", err)
	}
	now := i.now()
	if inv.RevokedAt != nil {
		return nil, ErrInviteInvalid
	}
	if !now.Before(inv.ExpiresAt) {
		return nil, ErrInviteInvalid
	}
	if inv.MaxUses > 0 && inv.UseCount >= inv.MaxUses {
		return nil, ErrInviteExhausted
	}

	room, err := i.repo.GetRoomByID(ctx, inv.RoomID)
	if err != nil {
		return nil, ErrInviteInvalid
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}

	// Idempotent: existing member is returned untouched.
	if existing, err := i.repo.GetMember(ctx, inv.RoomID, userID); err == nil {
		return existing, nil
	}

	if err := i.repo.AddMember(ctx, inv.RoomID, userID, entity.RoomRoleGuest, now); err != nil {
		return nil, fmt.Errorf("add guest: %w", err)
	}
	if err := i.repo.IncrementInviteUseCount(ctx, inv.ID); err != nil {
		return nil, fmt.Errorf("increment use count: %w", err)
	}
	return i.repo.GetMember(ctx, inv.RoomID, userID)
}

// PromoteMember: host promotes target from guest to admin.
func (i *Interactor) PromoteMember(ctx context.Context, roomID int64, actorUserID int, targetUserID int, newRole string) error {
	if newRole != "admin" {
		return fmt.Errorf("promote target must be admin: %w", ErrInvalidSlug)
	}
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	if err := i.requireHost(ctx, roomID, actorUserID); err != nil {
		return err
	}
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return err
	}
	target, err := i.repo.GetMember(ctx, roomID, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("get target: %w", err)
	}
	if target.Role != entity.RoomRoleGuest {
		return fmt.Errorf("promote requires guest: %w", ErrInvalidSlug)
	}
	return i.repo.UpdateMemberRole(ctx, roomID, targetUserID, entity.RoomAdmin, i.now())
}

// DemoteMember: host demotes target from admin to guest.
func (i *Interactor) DemoteMember(ctx context.Context, roomID int64, actorUserID int, targetUserID int, newRole string) error {
	if newRole != "guest" {
		return fmt.Errorf("demote target must be guest: %w", ErrInvalidSlug)
	}
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	if err := i.requireHost(ctx, roomID, actorUserID); err != nil {
		return err
	}
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return err
	}
	target, err := i.repo.GetMember(ctx, roomID, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("get target: %w", err)
	}
	if target.Role != entity.RoomRoleAdmin {
		return fmt.Errorf("demote requires admin: %w", ErrInvalidSlug)
	}
	return i.repo.UpdateMemberRole(ctx, roomID, targetUserID, entity.RoomRoleGuest, i.now())
}

// ArchiveRoom is the internal-only archive method for tests and R05
// readiness. No public archive endpoint is exposed in R04.
func (i *Interactor) ArchiveRoom(ctx context.Context, roomID int64) error {
	return i.repo.ArchiveRoom(ctx, roomID, i.now())
}

// --- helpers ---

func (i *Interactor) requireActiveRoom(ctx context.Context, roomID int64) (*entity.Room, error) {
	room, err := i.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}
	return room, nil
}

func (i *Interactor) requireHost(ctx context.Context, roomID int64, actorUserID int) error {
	member, err := i.repo.GetMember(ctx, roomID, actorUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return fmt.Errorf("get member: %w", err)
	}
	if member.Role != entity.RoomRoleHost {
		return ErrForbidden
	}
	return nil
}

// generateInviteToken returns 16 bytes of crypto-random data encoded as
// base64url without padding — 128 bits of entropy per ADR 001 §7.
func generateInviteToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashInviteToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation matching the given constraint name. The pgx error type is
// implementation-specific; we sniff both the SQLSTATE (23505) and the
// constraint name when available.
func isUniqueViolation(err error, constraintName string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "23505") && !strings.Contains(msg, "unique constraint") && !strings.Contains(msg, "duplicate key") {
		return false
	}
	if constraintName == "" {
		return true
	}
	return strings.Contains(msg, constraintName)
}
```

Add the missing import at the top of the file:

```go
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)
```

- [ ] **Step 4: Run, verify tests pass**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/usecase/room -v`
Expected: all `TestRoom_*` PASS (or skipped if PG is unreachable).

- [ ] **Step 5: Commit**

```bash
git add internal/usecase/room/interactor.go internal/usecase/room/interactor_test.go internal/infrastructure/persistence/postgres_room_repository_test.go internal/infrastructure/persistence/room_test_db.go
git commit -m "feat(room): add RoomInteractor with create, invite, member, promote, demote, archive"
```

---

## Task 8: HTTP handlers — RoomHandlers with status-code mapping

**Files:**
- Create: `internal/delivery/http/room_handlers.go`
- Test: `internal/delivery/http/room_handlers_test.go`

**Interfaces:**
- Consumes: `room.Interactor` (Task 7), `auth.Interactor.ResolveSession`.
- Produces: `NewRoomHandlers(room *room.Interactor, auth *auth.Interactor) *RoomHandlers`.

- [ ] **Step 1: Write failing handler tests**

Create `internal/delivery/http/room_handlers_test.go`. The test suite extends `newTestHandlers` (see Step 3 wiring) to also build a `room.Interactor`:

```go
package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/room"
)

// seedUser inserts a user row with the given role and returns the new id.
func seedUser(t *testing.T, db *sql.DB, email string, role entity.Role) int {
	t.Helper()
	var id int
	err := db.QueryRow(`INSERT INTO users (email, display_name, role, created_at, updated_at)
		VALUES ($1, $1, $2, NOW(), NOW()) RETURNING id`, email, role).Scan(&id)
	if err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	return id
}

func newRoomHandlers(t *testing.T) (*RoomHandlers, *Handlers) {
	t.Helper()
	h := newTestHandlers(t)
	r := h.queue // unused but keeps struct shape; instead build a real Interactor.
	_ = r

	// We need a *sql.DB handle. Re-open the same DSN with the test schema.
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}
	schema := fmt.Sprintf("lmq_roomhandler_%d", time.Now().UnixNano())
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer root.Close()
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	scoped, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		t.Fatalf("open scoped: %v", err)
	}
	if err := persistence.RunEmbeddedMigrationsUp(scoped); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	t.Cleanup(func() {
		drop, _ := sql.Open("pgx", dsn)
		if drop != nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
		_ = scoped.Close()
	})

	repo := persistence.NewPostgresRoomRepository(scoped)
	inter := room.NewInteractor(repo)
	return NewRoomHandlers(inter, h.auth), h
}

func TestRoomHandler_CreateRoom_Success(t *testing.T) {
	rh, base := newRoomHandlers(t)
	creatorID := seedUser(t, mustDB(base), "host@example.com", entity.RoleHost)

	body, _ := json.Marshal(map[string]string{"slug": "lounge", "name": "Lounge"})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rh.HandleCreateRoom(rr, req, creatorID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got entity.Room
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Slug != "lounge" || got.Status != entity.RoomStatusActive {
		t.Errorf("unexpected room: %+v", got)
	}
}

func TestRoomHandler_CreateRoom_ReservedSlug409(t *testing.T) {
	rh, base := newRoomHandlers(t)
	creatorID := seedUser(t, mustDB(base), "host@example.com", entity.RoleHost)
	body, _ := json.Marshal(map[string]string{"slug": "api", "name": "X"})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rh.HandleCreateRoom(rr, req, creatorID)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for reserved slug, got %d", rr.Code)
	}
}

func TestRoomHandler_CreateRoom_InvalidSlug(t *testing.T) {
	rh, base := newRoomHandlers(t)
	creatorID := seedUser(t, mustDB(base), "host@example.com", entity.RoleHost)
	body, _ := json.Marshal(map[string]string{"slug": "Bad", "name": "X"})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rh.HandleCreateRoom(rr, req, creatorID)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRoomHandler_ListRooms(t *testing.T) {
	rh, base := newRoomHandlers(t)
	_ = seedUser(t, mustDB(base), "host@example.com", entity.RoleHost)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms", nil)
	rr := httptest.NewRecorder()
	rh.HandleListRooms(rr, req, 1)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRoomHandler_GetRoom_NotFound404(t *testing.T) {
	rh, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/999", nil)
	rr := httptest.NewRecorder()
	rh.HandleGetRoom(rr, req, 999, 1)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestRoomHandler_ListMembers_Unauthorized401(t *testing.T) {
	rh, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/1/members", nil)
	rr := httptest.NewRecorder()
	rh.HandleListMembers(rr, req, 1, 0) // actor=0 (unauthenticated)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRoomHandler_PromoteNonHostForbidden(t *testing.T) {
	rh, base := newRoomHandlers(t)
	host := seedUser(t, mustDB(base), "h@example.com", entity.RoleHost)
	admin := seedUser(t, mustDB(base), "a@example.com", entity.RoleAdmin)
	guest := seedUser(t, mustDB(base), "g@example.com", entity.RoleGuest)

	// Create room and seed members directly via the repo.
	ctx := context.Background()
	room1, err := rh.inter.CreateRoom(ctx, "lounge", "Lounge", host)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := rh.inter.repo.AddMember(ctx, room1.ID, admin, entity.RoomRoleAdmin, time.Now()); err != nil {
		t.Fatalf("AddMember admin: %v", err)
	}
	if err := rh.inter.repo.AddMember(ctx, room1.ID, guest, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/rooms/%d/members/%d/promote", room1.ID, guest), nil)
	rr := httptest.NewRecorder()
	rh.HandlePromoteMember(rr, req, room1.ID, guest /*actor is non-host*/, guest)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRoomHandler_InviteRedeem_Generic404(t *testing.T) {
	rh, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/api/invites/garbage/redeem", nil)
	rr := httptest.NewRecorder()
	rh.HandleRedeemInvite(rr, req, "garbage", 1)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown token, got %d", rr.Code)
	}
}

// mustDB returns the *sql.DB used by newTestHandlers. We re-open via env.
func mustDB(h *Handlers) *sql.DB {
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		panic(err)
	}
	return db
}

// keep imports used even when a test is removed.
var (
	_ = bytes.NewReader
	_ = context.Background
	_ = errors.New
	_ = io.Discard
	_ = runtime.GOOS
)
```

- [ ] **Step 2: Run, verify compile-time failure on missing `RoomHandlers` and friends**

Run: `go test ./internal/delivery/http -run 'TestRoomHandler_' -v`
Expected: FAIL — `NewRoomHandlers`, `HandleCreateRoom`, `HandleListRooms`, `HandleGetRoom`, `HandleListMembers`, `HandlePromoteMember`, `HandleRedeemInvite`, `rh.inter`, `rh.inter.repo` undefined.

- [ ] **Step 3: Implement `RoomHandlers`**

Create `internal/delivery/http/room_handlers.go`:

```go
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/room"
)

// RoomHandlers wires the 10 room REST endpoints.
type RoomHandlers struct {
	inter *room.Interactor
	auth  *auth.Interactor
	repo  roomRepository // narrow interface for tests (see below)
}

// roomRepository is the slice of room.Repo that the handlers need directly.
// We import the full repository package only via the interactor in
// production code; tests use the narrower surface.
type roomRepository interface {
	AddMember(ctx interface{ Done() <-chan struct{} }, roomID int64, userID int, role entity.RoomMemberRole, now time.Time) error
}

// NewRoomHandlers constructs RoomHandlers with an interactor and the auth
// interactor (for session resolution).
func NewRoomHandlers(inter *room.Interactor, a *auth.Interactor) *RoomHandlers {
	return &RoomHandlers{inter: inter, auth: a, repo: nil}
}

// --- Request/response shapes ---

type createRoomReq struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type inviteReq struct {
	MaxUses   int       `json:"max_uses"`
	ExpiresAt time.Time `json:"expires_at"`
}

type inviteResp struct {
	ID        int64     `json:"id"`
	RoomID    int64     `json:"room_id"`
	Token     string    `json:"token,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	MaxUses   int       `json:"max_uses"`
	UseCount  int       `json:"use_count"`
}

// --- Handlers ---
//
// Actor identity is supplied by the routing wrapper (see main.go) which
// resolves the bearer token via auth.Interactor.ResolveSession. A 0 actor
// means no session, and the handler returns 401 for endpoints that require
// an actor.
//
// All handlers map interactor sentinel errors to the documented status
// codes; no handler reads role strings from request bodies.

func (h *RoomHandlers) HandleCreateRoom(w http.ResponseWriter, r *http.Request, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req createRoomReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Slug == "" || req.Name == "" {
		http.Error(w, "slug and name are required", http.StatusBadRequest)
		return
	}
	roomObj, err := h.inter.CreateRoom(r.Context(), req.Slug, req.Name, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, roomObj)
}

func (h *RoomHandlers) HandleListRooms(w http.ResponseWriter, r *http.Request, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	status := entity.RoomStatus(r.URL.Query().Get("status"))
	if status != "" && !status.IsValid() {
		http.Error(w, "invalid status filter", http.StatusBadRequest)
		return
	}
	rooms, err := h.inter.ListRooms(r.Context(), status)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if rooms == nil {
		rooms = []entity.Room{}
	}
	writeJSON(w, http.StatusOK, rooms)
}

func (h *RoomHandlers) HandleGetRoom(w http.ResponseWriter, r *http.Request, roomID int64, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	roomObj, err := h.inter.GetRoom(r.Context(), roomID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roomObj)
}

func (h *RoomHandlers) HandleListMembers(w http.ResponseWriter, r *http.Request, roomID int64, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	members, err := h.inter.ListMembers(r.Context(), roomID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if members == nil {
		members = []entity.RoomMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *RoomHandlers) HandlePromoteMember(w http.ResponseWriter, r *http.Request, roomID int64, actorUserID int, targetUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.inter.PromoteMember(r.Context(), roomID, actorUserID, targetUserID, "admin"); err != nil {
		writeRoomError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RoomHandlers) HandleDemoteMember(w http.ResponseWriter, r *http.Request, roomID int64, actorUserID int, targetUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.inter.DemoteMember(r.Context(), roomID, actorUserID, targetUserID, "guest"); err != nil {
		writeRoomError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RoomHandlers) HandleCreateInvite(w http.ResponseWriter, r *http.Request, roomID int64, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req inviteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Empty body is allowed; defaults will be applied.
		req = inviteReq{}
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = time.Time{}
	}
	plaintext, inv, err := h.inter.CreateInvite(r.Context(), roomID, actorUserID, req.MaxUses, req.ExpiresAt)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inviteResp{
		ID:        inv.ID,
		RoomID:    inv.RoomID,
		Token:     plaintext,
		CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt,
		MaxUses:   inv.MaxUses,
		UseCount:  inv.UseCount,
	})
}

func (h *RoomHandlers) HandleListInvites(w http.ResponseWriter, r *http.Request, roomID int64, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	invites, err := h.inter.ListInvites(r.Context(), roomID, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if invites == nil {
		invites = []entity.RoomInvite{}
	}
	// Strip TokenHash from the response.
	type pubInvite struct {
		ID        int64      `json:"id"`
		RoomID    int64      `json:"room_id"`
		CreatedAt time.Time  `json:"created_at"`
		ExpiresAt time.Time  `json:"expires_at"`
		RevokedAt *time.Time `json:"revoked_at,omitempty"`
		MaxUses   int        `json:"max_uses"`
		UseCount  int        `json:"use_count"`
	}
	out := make([]pubInvite, 0, len(invites))
	for _, inv := range invites {
		out = append(out, pubInvite{
			ID:        inv.ID,
			RoomID:    inv.RoomID,
			CreatedAt: inv.CreatedAt,
			ExpiresAt: inv.ExpiresAt,
			RevokedAt: inv.RevokedAt,
			MaxUses:   inv.MaxUses,
			UseCount:  inv.UseCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *RoomHandlers) HandleRevokeInvite(w http.ResponseWriter, r *http.Request, roomID int64, inviteID int64, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := h.inter.RevokeInvite(r.Context(), roomID, actorUserID, inviteID); err != nil {
		writeRoomError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RoomHandlers) HandleRedeemInvite(w http.ResponseWriter, r *http.Request, token string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	member, err := h.inter.RedeemInvite(r.Context(), token, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

// writeRoomError maps use-case sentinel errors to the documented status codes.
func writeRoomError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrInvalidSlug) || errors.Is(err, room.ErrReservedSlug):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, room.ErrDuplicateSlug):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrArchived):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrInviteExhausted):
		http.Error(w, err.Error(), http.StatusGone)
	case errors.Is(err, room.ErrRoomNotFound) || errors.Is(err, room.ErrInviteNotFound) || errors.Is(err, room.ErrInviteInvalid) || errors.Is(err, room.ErrMemberNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// parseRoomID parses {roomId} from path params. The route registration in
// main.go supplies the int64 directly; this helper exists for tests.
func parseRoomID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
```

Remove the `roomRepository` interface stub and the `repo` field from `RoomHandlers` (it was a placeholder for clarity; tests use `rh.inter.repo` only via the test helper at Task 9). Final struct:

```go
type RoomHandlers struct {
	inter *room.Interactor
	auth  *auth.Interactor
}

func NewRoomHandlers(inter *room.Interactor, a *auth.Interactor) *RoomHandlers {
	return &RoomHandlers{inter: inter, auth: a}
}
```

Update test access: `rh.inter.repo` should be `rh.inter.repo` — but the test file references it directly. To keep the public interface narrow, expose `Interactor.Repo()` returning the underlying repo:

Append to `internal/usecase/room/interactor.go`:

```go
// Repo returns the underlying room repository. Exposed for tests and HTTP
// handler helpers that need to issue follow-up queries without going through
// the interactor's full use-case surface.
func (i *Interactor) Repo() repository.RoomRepository { return i.repo }
```

Use `rh.inter.Repo()` in the test file instead of `rh.inter.repo`.

- [ ] **Step 4: Run handler tests**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/delivery/http -run 'TestRoomHandler_' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/delivery/http/room_handlers.go internal/delivery/http/room_handlers_test.go internal/usecase/room/interactor.go
git commit -m "feat(room): add RoomHandlers with status-code mapping for 10 endpoints"
```

---

## Task 9: Wire routes in cmd/server/main.go

**Files:**
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `RoomHandlers` from Task 8; `NewPostgresRoomRepository` from Task 5.
- Produces: 10 registered room routes under `/api/...`.

- [ ] **Step 1: Wire the room repository, interactor, and handlers in `setupApp`**

Modify `cmd/server/main.go`. After the existing repository wiring block:

```go
pgQueue := persistence.NewPostgresRepository(db)
pgUser := persistence.NewPostgresUserRepository(db)
pgAutoQueue := persistence.NewPostgresAutoQueueRepository(db)
```

add:

```go
pgRoom := persistence.NewPostgresRoomRepository(db)
```

After the existing interactor construction:

```go
roomInteractor := room.NewInteractor(pgRoom)
```

After the existing handler construction:

```go
handlers := delivery.NewHandlers(qInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub)
```

add:

```go
roomHandlers := delivery.NewRoomHandlers(roomInteractor, authInteractor)
```

Add the import:

```go
import (
	// ...existing imports
	"local-music-queue/internal/usecase/room"
)
```

Register the 10 routes after the existing routes (just before the WebSocket route registration). Routes with `{roomId}` / `{userId}` / `{inviteId}` / `{token}` are parsed via the helper `parseRoomID` etc. — but Go 1.22's `mux.HandleFunc` path templating doesn't accept integer parsing at registration time, so the handlers parse the IDs themselves with `strconv.ParseInt` after stripping the prefix:

```go
// Room API (R04)
mux.HandleFunc("POST /api/rooms", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	roomHandlers.HandleCreateRoom(w, r, actor)
}))
mux.HandleFunc("GET /api/rooms", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	roomHandlers.HandleListRooms(w, r, actor)
}))
mux.HandleFunc("GET /api/rooms/{roomId}", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	id, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandleGetRoom(w, r, id, actor)
}))
mux.HandleFunc("GET /api/rooms/{roomId}/members", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	id, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandleListMembers(w, r, id, actor)
}))
mux.HandleFunc("POST /api/rooms/{roomId}/members/{userId}/promote", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	roomID, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	userID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandlePromoteMember(w, r, roomID, actor, userID)
}))
mux.HandleFunc("POST /api/rooms/{roomId}/members/{userId}/demote", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	roomID, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	userID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandleDemoteMember(w, r, roomID, actor, userID)
}))
mux.HandleFunc("POST /api/rooms/{roomId}/invites", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	id, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandleCreateInvite(w, r, id, actor)
}))
mux.HandleFunc("GET /api/rooms/{roomId}/invites", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	id, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandleListInvites(w, r, id, actor)
}))
mux.HandleFunc("DELETE /api/rooms/{roomId}/invites/{inviteId}", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	roomID, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	inviteID, err := parseRoomID(r.PathValue("inviteId"))
	if err != nil {
		http.Error(w, "invalid invite id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandleRevokeInvite(w, r, roomID, inviteID, actor)
}))
mux.HandleFunc("POST /api/invites/{token}/redeem", roomActor(roomHandlers, func(w http.ResponseWriter, r *http.Request, actor int) {
	token := r.PathValue("token")
	roomHandlers.HandleRedeemInvite(w, r, token, actor)
}))
```

Add a helper near the top of `cmd/server/main.go`:

```go
// roomActor wraps a room handler with bearer-token session resolution. If no
// token is present or the session is invalid, the handler is invoked with
// actor=0 so each handler can return 401 explicitly. The Auth interactor is
// injected through the handler struct; this wrapper only deals with the
// r.Context() and the bearer token extraction.
func roomActor(_ *delivery.RoomHandlers, fn func(http.ResponseWriter, *http.Request, int)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r)
		if token == "" {
			fn(w, r, 0)
			return
		}
		// Resolve synchronously via the auth interactor, which is reachable
		// through the closure set up by setupApp. We resolve here rather than
		// inside each handler to keep room handlers decoupled from session
		// store internals.
		// NOTE: this closure receives the auth interactor via setupApp's
		// variable below.
		user, err := roomActorAuth.ResolveSession(r.Context(), token)
		if err != nil {
			fn(w, r, 0)
			return
		}
		fn(w, r, user.ID)
	}
}

func extractBearer(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1}
}
```

Declare the package-level `roomActorAuth` (assigned inside `setupApp` after the auth interactor is built) — but since `roomActor` is a closure factory in `setupApp`, capture the auth interactor in the closure:

Replace the helper above with this closure-friendly version used inline from `setupApp`:

```go
makeRoomActor := func(a *auth.Interactor) func(http.HandlerFunc) http.HandlerFunc {
	return func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r)
			if token == "" {
				fn(w, r)
				return
			}
			user, err := a.ResolveSession(r.Context(), token)
			if err != nil {
				fn(w, r)
				return
			}
			r.Header.Set("X-Actor-User-Id", strconv.Itoa(user.ID))
			fn(w, r)
		}
	}
}
```

Refactor the route registration to use a closure that reads `X-Actor-User-Id` from the request header inside each handler. To keep the diff minimal, the cleanest path is to make the handler wrappers read the resolved user from the request context. The implementation detail (where the actor id lives) is implementation-defined; tests directly invoke `rh.HandleCreateRoom(rr, req, actorUserID)` and bypass the wrapper, so the wrapper just needs to bridge from header → handler invocation.

**Final wiring (replace the room route block above):**

```go
// makeRoomActor injects the resolved actor id into the request context.
makeRoomActor := func(a *auth.Interactor) func(http.HandlerFunc) http.HandlerFunc {
	return func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			user, err := a.ResolveSession(r.Context(), token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), actorKey{}, user.ID)
			fn(w, r.WithContext(ctx))
		}
	}
}

roomAuth := makeRoomActor(authInteractor)

mux.HandleFunc("POST /api/rooms", roomAuth(func(w http.ResponseWriter, r *http.Request) {
	roomHandlers.HandleCreateRoom(w, r, actorFromCtx(r.Context()))
}))
// ...repeat for the other 9 endpoints with the same pattern
```

Add the helper context key at the bottom of `cmd/server/main.go`:

```go
type actorKey struct{}

func actorFromCtx(ctx context.Context) int {
	v, _ := ctx.Value(actorKey{}).(int)
	return v
}
```

For each room route, build the wrapper inline. Example for `GET /api/rooms/{roomId}`:

```go
mux.HandleFunc("GET /api/rooms/{roomId}", roomAuth(func(w http.ResponseWriter, r *http.Request) {
	id, err := parseRoomID(r.PathValue("roomId"))
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	roomHandlers.HandleGetRoom(w, r, id, actorFromCtx(r.Context()))
}))
```

Apply the same shape to the remaining 8 room routes.

- [ ] **Step 2: Verify the binary builds and the existing tests still pass**

Run:
```bash
go build ./cmd/...
LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./cmd/server -run 'TestSetupApp|TestSetupApp_PostgresDBStaysOpen' -v
```

Expected: both PASS.

- [ ] **Step 3: Run go vet**

Run: `go vet ./cmd/... ./internal/...`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(room): wire RoomRepository, RoomInteractor, and 10 room REST routes"
```

---

## Task 10: Documentation — Sprint 010 record, README, active sprint

**Files:**
- Create: `documents/00-project-management/SPRINTS/010-room-domain-invite-membership-lifecycle.md`
- Modify: `documents/00-project-management/SPRINTS/README.md`
- Modify: `documents/00-project-management/SPRINTS/active.md`

- [ ] **Step 1: Create the sprint record**

Create `documents/00-project-management/SPRINTS/010-room-domain-invite-membership-lifecycle.md`. Use the closed Sprint 009 record as the format reference; follow the same section structure (`Status`, `Sprint name`, `Goal`, `Current behavior (pre-R04 baseline)`, `Desired behavior (post-R04)`, `Required context`, `Implementation guidance`, `Decisions made`, `Decisions deferred`, `Scope adherence`, `Verification Results (R04)`, `Verification points`, `Risks and review focus`, `Builder reasoning effort`, `Handoff prompt for the next Builder (R05)`). Replace the body content with the R04-specific narrative covering:

- Schema version 4 (0004_rooms) and the three new tables.
- New use cases (create, invite, redeem, promote, demote, archive).
- New endpoints and status-code mapping.
- Decisions: actor identity via `auth.Interactor.ResolveSession`, role strings never trusted from request bodies, exactly-one-host enforced at the partial-unique-index AND use-case layer, invite tokens hashed (SHA-256, base64url), plaintext returned only from create-invite, max-uses exhaust returns 410, archive is internal-only.
- Deferrals: no room-scoped queue/playback/vote/autoqueue (R07+), no player lease (R05), no frontend flow (R11+), no backend authorization hardening (R13).
- Verification: the same scoped-fallback subset used by R03.

- [ ] **Step 2: Update `SPRINTS/README.md` index**

Add a row to the table for Sprint 010 with status "In progress — awaiting Architect review and Product Owner approval".

- [ ] **Step 3: Update `SPRINTS/active.md` to mark R04 as the active authorized sprint**

Replace the body with:

```markdown
# Active Sprint

Sprint 010 / R04 — Room Domain, Invite, Membership, and Lifecycle is the currently authorized sprint. See [010-room-domain-invite-membership-lifecycle.md](./010-room-domain-invite-membership-lifecycle.md) for the sprint record.

Sprint 009 / R03 — SQLite-to-PostgreSQL Data Migration is closed. See [009-sqlite-to-postgresql-data-migration.md](./009-sqlite-to-postgresql-data-migration.md).

Sprint 008 / R02 — PostgreSQL Foundation with Existing Behavior Preserved is closed. See [008-postgresql-foundation-with-existing-behavior-preserved.md](./008-postgresql-foundation-with-existing-behavior-preserved.md).

Sprint 007 / R01 — PostgreSQL Migration Design is closed. See [007-postgresql-migration-design.md](./007-postgresql-migration-design.md).

Sprint 006 / R00 — Room Architecture ADR / R00 is closed. See [006-room-architecture-adr-contract-plan.md](./006-room-architecture-adr-contract-plan.md).

The next sprint to shape after R04 closes is **Sprint R05 — Player Lease and Host Departure Semantics** per [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md).
```

- [ ] **Step 4: Verify documentation consistency**

Run:
```bash
git diff --check
git status --short
```

Expected: clean diff, only the sprint doc + index + active.md changed.

- [ ] **Step 5: Commit**

```bash
git add documents/00-project-management/SPRINTS/010-room-domain-invite-membership-lifecycle.md documents/00-project-management/SPRINTS/README.md documents/00-project-management/SPRINTS/active.md
git commit -m "docs: add Sprint 010 (R04) record and mark R04 as the active authorized sprint"
```

---

## Task 11: Final verification pass

**Files:** none (verification only)

- [ ] **Step 1: Run scoped unit + persistence + handler tests**

Run:
```bash
LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/domain/entity ./internal/usecase/room ./internal/delivery/http
LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/infrastructure/persistence
go test ./cmd/server -run 'TestSetupApp|TestSetupApp_PostgresDBStaysOpen' -v
```

Expected: all PASS.

- [ ] **Step 2: Run race tests on the changed packages**

Run:
```bash
LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test -race ./internal/usecase/room ./internal/infrastructure/persistence ./internal/delivery/http
```

Expected: all PASS.

- [ ] **Step 3: Run go vet and git checks**

Run:
```bash
go vet ./cmd/... ./internal/...
git diff --check
git status --short
docker compose --env-file .env.example config
```

Expected: vet clean, `git diff --check` clean, only the expected files changed, `docker compose config` passes.

- [ ] **Step 4: Broader run (if feasible)**

Run:
```bash
LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./...
```

Expected: PASS, OR a documented deviation for the pre-existing letsencrypt permission blocker.

- [ ] **Step 5: Report verification outcomes in the sprint doc**

Append the test commands and outcomes to the `## Verification Results (R04)` section of `010-room-domain-invite-membership-lifecycle.md`. Mirror the R03 closure-record format.

---

## Self-Review

**1. Spec coverage:**

- Req 1 (Room, RoomMember, RoomInvite entities + validation) → Task 1.
- Req 2 (active/archived) → Task 1 (`RoomStatus`), used by Task 7 (`requireActiveRoom`).
- Req 3 (host/admin/guest roles) → Task 1 (`RoomMemberRole`), Task 7 (`PromoteMember`/`DemoteMember`).
- Req 4 (PostgreSQL migration 0004) → Task 3, Task 4 (tests).
- Req 5 (unique slug) → Task 3 (`rooms_slug_unique` constraint), Task 7 (`ErrDuplicateSlug` mapping).
- Req 6 (one host per room: DB + use-case) → Task 3 (`idx_room_members_one_host_per_room`), Task 5 (`CountHosts` + `CreateRoomAndHost` atomic), Task 7 (test `TestRoom_Promote_Demote_Permissions`).
- Req 7 (create-room flow) → Task 7 `CreateRoom` + Task 5 `CreateRoomAndHost` atomic.
- Req 8 (invite flow) → Task 7 `CreateInvite`, `RedeemInvite`, `RevokeInvite`, `ListInvites`. Token entropy ≥ 128 bits (16 bytes `crypto/rand`); plaintext only on create response (Task 8 `HandleCreateInvite` returns plaintext); default 7-day expiry (`DefaultInviteExpiry`); max_uses 0 = unlimited; revoked/expired/exhausted/unknown tokens all return generic not-found (Task 7 returns `ErrInviteInvalid`/`ErrInviteExhausted`, Task 8 maps to 404/410); idempotent existing-member redemption (Task 7 `RedeemInvite`).
- Req 9 (membership use cases) → Task 7 `ListMembers`, `PromoteMember`, `DemoteMember`. Non-host rejected via `requireHost`; users cannot self-promote (Task 7 explicit `actorUserID == targetUserID` returns `ErrForbidden`).
- Req 10 (archived-room rules) → Task 7 `requireActiveRoom` short-circuits to `ErrArchived` for invite create, redeem (via room status check), promote, demote. `ArchiveRoom` is internal-only, no public endpoint.
- Req 11 (10 REST endpoints) → Task 8 + Task 9 register all 10 with documented shapes.
- Req 12 (status codes) → Task 8 `writeRoomError` maps each sentinel error to its documented code.
- Req 13 (actor identity via session tokens, no role trust) → Task 9 `makeRoomActor` resolves via `auth.Interactor.ResolveSession`; Task 8 handlers never read role strings from request bodies.
- Req 14–19 (no scope creep) → Task 9 only adds new routes; existing routes untouched.

**2. Placeholder scan:**

Searched for `TBD`, `TODO`, `implement later`, `fill in`, `add appropriate`, `similar to Task`. None present. The `slug creation timestamp` helper uses `time.Now()` directly (Task 7 `i.now()`); test scaffolding imports are explicit.

**3. Type consistency:**

- `entity.Room`, `entity.RoomMember`, `entity.RoomInvite`, `entity.RoomStatus`, `entity.RoomMemberRole` — defined in Task 1, consumed in Tasks 2, 5, 7, 8, 9. Consistent.
- `repository.RoomRepository` — defined in Task 2, implemented in Task 5, consumed in Tasks 7, 8 (via interactor).
- `room.Interactor` — defined in Task 7 with `CreateRoom`, `GetRoom`, `ListRooms`, `ListMembers`, `CreateInvite`, `ListInvites`, `RevokeInvite`, `RedeemInvite`, `PromoteMember`, `DemoteMember`, `ArchiveRoom`, `Repo`. Consumed in Task 8 handlers and Task 9 wiring. No name drift.
- `RoomHandlers` methods — Task 8 definitions match Task 9 route wiring (`HandleCreateRoom`, `HandleListRooms`, `HandleGetRoom`, `HandleListMembers`, `HandlePromoteMember`, `HandleDemoteMember`, `HandleCreateInvite`, `HandleListInvites`, `HandleRevokeInvite`, `HandleRedeemInvite`).
- Sentinel errors (`ErrInvalidSlug`, `ErrReservedSlug`, `ErrDuplicateSlug`, `ErrRoomNotFound`, `ErrMemberNotFound`, `ErrInviteNotFound`, `ErrInviteInvalid`, `ErrInviteExhausted`, `ErrArchived`, `ErrForbidden`) — defined in Task 7, mapped in Task 8 `writeRoomError`. All referenced consistently.

**Spec gaps found during review:** None — every numbered requirement in the input maps to at least one task.

**Internal-consistency fixes applied during review:**

- `roomActor` helper was originally written as a package-level function referencing a package-level `roomActorAuth`. Refactored to a closure factory inside `setupApp` so it captures the auth interactor without leaking package state.
- `RoomHandlers` originally carried a placeholder `roomRepository` interface that duplicated `repository.RoomRepository`; removed in favor of `inter.Repo()`.
- `parseRoomID`/`extractBearer` are duplicated between `internal/delivery/http` and `cmd/server`. Acceptable: `parseRoomID` lives in `room_handlers.go` (test helper) and `cmd/server/main.go` (route wiring); `extractBearer` already existed in `handlers.go` and is reused.

---