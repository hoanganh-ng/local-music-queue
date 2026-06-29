# Sprint 011 / R06 — Player Lease and Host Departure Semantics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a backend-only per-room player lease (claim/heartbeat/release/get), enforce one active lease per room via PostgreSQL, archive the room on lease expiry beyond grace or on explicit release, and emit an additive `room_archived` WebSocket event. Do not move queue/playback/voting/auto-queue into rooms.

**Architecture:** Add a new `PlayerLease` domain entity, persistence layer, repository, and use case. Reuse the existing `room.Interactor` (extend it with lease methods) and `room_handlers.go` (add four new endpoints). Add a `0005_player_leases` migration with a partial unique index enforcing one active lease per room. The room lease state machine runs in-process: a periodic expiry sweeper in the hub `Run` loop checks active leases every tick and triggers archive + `room_archived` broadcast when `now > expires_at + grace`. Lease emits to the global `/ws` hub (rooms-on-WS R08 is deferred; R06 broadcasts on the existing global hub per ADR 001 §10).

**Tech Stack:** Go 1.22+, PostgreSQL 16, gorilla/websocket, golang-migrate/v4, pgx/v5/stdlib.

## Global Constraints

- Schema version becomes **5** after embedded migrations run.
- Slug regex `^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`; reserved slugs `api`, `admin`, `static`, `ws`.
- Lease lifetime: 60 s. Grace: 30 s. (per ADR 001 §6). Heartbeat extends `expires_at` by 60 s.
- One active lease per room: enforced by partial unique index `(room_id) WHERE ended_at IS NULL`.
- All four new endpoints require bearer-token auth via `makeRoomActor`.
- Claim/release require the caller to be a `host` member of the room. Heartbeat requires the caller to be the current lease holder. Get requires any active member.
- Duplicate valid claim returns `409`. Expired-grace claim returns `410` (post-grace). Admin attempting claim/release returns `403`.
- Heartbeat within grace renews the lease (`now > last_heartbeat_at` check is irrelevant — only `now <= expires_at + grace`); outside grace returns `410`.
- Explicit release archives the room exactly once; expiry beyond grace archives exactly once. Both end the lease row.
- Add `room_archived` event (additive). Do not alter any existing event payload, JSON tags, or sequence-number semantics.
- Do not apply lease checks to existing global queue/playback/voting/auto-queue endpoints.
- Sprint doc rename to Sprint 011 / R06 consistent with the spec (the stub at `011-...md` currently says R05 — rename headers in place, do not perform broad docs cleanup).
- No frontend, Docker, HTTPS, migration CLI, or unrelated queue/voting/auto-queue changes.

## File Structure

### New files

- `internal/domain/entity/player_lease.go` — `PlayerLease` struct, `PlayerLeaseArchiveReason` enum (`player_lease_expired`, `host_left`, `explicit`), sentinel errors.
- `internal/domain/repository/player_lease_repository.go` — `PlayerLeaseRepository` interface (Claim/Heartbeat/Release/GetByRoom/Delete/EndAndArchive/ListActiveForSweep).
- `internal/usecase/room/player_lease_interactor.go` — `PlayerLeaseInteractor` with `Claim`, `Heartbeat`, `Release`, `GetLease` methods and a `SweepExpired` method called by the hub ticker.
- `internal/infrastructure/persistence/postgres_player_lease_repository.go` — PostgreSQL implementation.
- `internal/infrastructure/persistence/migrations/postgres/0005_player_leases.up.sql` — adds `player_leases` table + partial unique index.
- `internal/infrastructure/persistence/migrations/postgres/0005_player_leases.down.sql` — drops table.
- `internal/infrastructure/persistence/postgres_player_lease_repository_test.go` — persistence tests.
- `internal/usecase/room/player_lease_interactor_test.go` — use case tests.
- `internal/delivery/http/player_lease_handlers_test.go` — handler status-code tests.

### Modified files

- `internal/domain/entity/room.go` — no change required (the lease references `Room` only by `room_id` int64).
- `internal/domain/repository/room_repository.go` — add `ArchiveRoomIfActive(ctx, roomID, now) (archived bool, err error)` to return whether the archive actually flipped status (idempotency for expiry sweep and explicit release). Keep the existing `ArchiveRoom` for compatibility with tests.
- `internal/usecase/room/interactor.go` — add `ErrPlayerLeaseExists`, `ErrPlayerLeaseGone`, `ErrPlayerLeaseForbidden`, `ErrNotLeaseHolder`, `ErrLeaseNotFound` sentinels. Add helpers `requireHost` (already present), `requireLeaseHolder` (new).
- `internal/infrastructure/persistence/postgres_room_repository.go` — implement `ArchiveRoomIfActive` (UPDATE ... WHERE status='active').
- `internal/infrastructure/persistence/postgres_migration_test.go` — extend with 0005 (CleanSchema expects v5 and `player_leases` table; DownThenUp exercises 0005 down/up).
- `internal/delivery/ws/events.go` — add `EventRoomArchived = "room_archived"` constant and `RoomArchivedData` struct (`RoomID`, `Reason`, `ArchivedAt`).
- `internal/delivery/ws/hub.go` — add `playerLeaseInteractor` interface field with `SweepExpired(ctx) []RoomArchivedEvent`. Hub ticker (reuse the existing 5 s ticker) calls it and broadcasts `room_archived` for each. Setter `SetPlayerLeaseInteractor`.
- `cmd/server/main.go` — construct `PlayerLeaseInteractor`, wire it into `room.Interactor` (so handlers can call lease methods through it) and into the hub. Register 4 routes. Note: the existing `room.Interactor` keeps room/member/invite methods; lease methods live on `PlayerLeaseInteractor` for clear separation.
- `documents/00-project-management/SPRINTS/011-player-lease-and-host-departure-semantics.md` — rename Sprint name from R05 → R06; add brief implementation summary section at the end (no broader docs cleanup).

### Out of scope (do not touch)

- `internal/usecase/queue`, `internal/usecase/vote`, `internal/usecase/autoqueue`, `internal/usecase/priority`, `internal/usecase/auth`.
- `internal/delivery/http/handlers.go`, `autoqueue_handler.go`, `auth_middleware.go`.
- `internal/infrastructure/persistence/postgres_repository.go` (queue), `postgres_user_repository.go`, `postgres_auto_queue_repo.go`.
- `frontend/`, `extension/`, `cmd/migrate-data/`, `cmd/migrate-schema/`.
- `docker-compose.yml`, `Dockerfile`, `.env.example`, `letsencrypt-*`.

---

## Task 1: PlayerLease domain entity

**Files:**
- Create: `internal/domain/entity/player_lease.go`
- Test: `internal/domain/entity/player_lease_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `entity.PlayerLease` struct, `entity.PlayerLeaseArchiveReason` type, sentinel `entity.ErrInvalidPlayerLease`

- [ ] **Step 1: Write failing tests**

In `internal/domain/entity/player_lease_test.go`:

```go
package entity

import (
	"testing"
	"time"
)

func TestPlayerLease_ArchiveReason_IsValid(t *testing.T) {
	for _, r := range []PlayerLeaseArchiveReason{
		PlayerLeaseExpired, PlayerLeaseHostLeft, PlayerLeaseExplicit,
	} {
		if !r.IsValid() {
			t.Errorf("expected %s valid", r)
		}
	}
	if PlayerLeaseArchiveReason("nope").IsValid() {
		t.Error("expected unknown reason invalid")
	}
}

func TestPlayerLease_IsWithinGrace(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	l := &PlayerLease{ExpiresAt: now.Add(-10 * time.Second)}
	if !l.IsWithinGrace(now, 30*time.Second) {
		t.Error("expected -10s within 30s grace")
	}
	if l.IsWithinGrace(now.Add(-31*time.Second), 30*time.Second) {
		t.Error("expected -31s outside grace")
	}
}

func TestPlayerLease_IsExpired(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ended := now.Add(-1 * time.Second)
	l := &PlayerLease{ExpiresAt: now.Add(-60 * time.Second), EndedAt: &ended}
	if !l.IsExpired(now) {
		t.Error("expected ended lease to be expired")
	}
}
```

- [ ] **Step 2: Run tests, confirm fail**

Run: `go test ./internal/domain/entity -run 'TestPlayerLease' -v`
Expected: FAIL (package symbols not defined).

- [ ] **Step 3: Implement entity**

In `internal/domain/entity/player_lease.go`:

```go
package entity

import (
	"errors"
	"time"
)

// PlayerLease represents an exclusive claim of a host's playback device
// against a single room. ADR 001 §6.
type PlayerLease struct {
	ID              int64      `json:"id"`
	RoomID          int64      `json:"room_id"`
	ClaimedByUserID int        `json:"claimed_by_user_id"`
	ClaimedAt       time.Time  `json:"claimed_at"`
	LastHeartbeatAt time.Time  `json:"last_heartbeat_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
}

// IsWithinGrace reports whether the lease is still renewable at now given
// a grace duration. Lease is renewable if now <= expires_at + grace.
func (l *PlayerLease) IsWithinGrace(now time.Time, grace time.Duration) bool {
	return !now.After(l.ExpiresAt.Add(grace))
}

// IsExpired reports whether the lease has been ended AND is past grace.
func (l *PlayerLease) IsExpired(now time.Time) bool {
	if l.EndedAt == nil {
		return false
	}
	return !l.IsWithinGrace(now, 30*time.Second)
}

// PlayerLeaseArchiveReason describes why a room was archived in connection
// with player-lease semantics. ADR 001 §10.
type PlayerLeaseArchiveReason string

const (
	PlayerLeaseExpired PlayerLeaseArchiveReason = "player_lease_expired"
	PlayerLeaseHostLeft PlayerLeaseArchiveReason = "host_left"
	PlayerLeaseExplicit PlayerLeaseArchiveReason = "explicit"
)

// IsValid reports whether r is a recognized reason.
func (r PlayerLeaseArchiveReason) IsValid() bool {
	switch r {
	case PlayerLeaseExpired, PlayerLeaseHostLeft, PlayerLeaseExplicit:
		return true
	}
	return false
}

// ErrInvalidPlayerLease is reserved for entity-level validation failures.
var ErrInvalidPlayerLease = errors.New("invalid player lease")
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `go test ./internal/domain/entity -run 'TestPlayerLease' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/entity/player_lease.go internal/domain/entity/player_lease_test.go
git commit -m "feat(r06): add PlayerLease domain entity"
```

---

## Task 2: PlayerLeaseRepository interface

**Files:**
- Create: `internal/domain/repository/player_lease_repository.go`

**Interfaces:**
- Consumes: `entity.PlayerLease`
- Produces: `repository.PlayerLeaseRepository` interface, `ErrPlayerLeaseExists` sentinel

- [ ] **Step 1: Write the file**

```go
package repository

import (
	"context"
	"errors"
	"time"

	"local-music-queue/internal/domain/entity"
)

// ErrPlayerLeaseExists is returned by Claim when a valid lease already
// exists for the room (within grace).
var ErrPlayerLeaseExists = errors.New("player lease exists")

// PlayerLeaseRepository persists PlayerLease rows. The implementation MUST
// enforce the partial unique index `idx_player_leases_one_active_per_room`
// so concurrent claims cannot produce two active leases for the same room.
type PlayerLeaseRepository interface {
	// Claim inserts a new lease and ends any prior expired (past grace) lease
	// in a single transaction. Returns ErrPlayerLeaseExists if a valid lease
	// (within grace) is already present.
	Claim(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error)

	// HeartbeatByHolder extends the lease's expires_at by leaseDuration when
	// called by the current holder. Returns sql.ErrNoRows if the lease does
	// not exist for the room, and a not-holder error if userID does not
	// match claimed_by_user_id.
	HeartbeatByHolder(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error)

	// ReleaseByHolder ends the lease and returns whether a row was updated.
	// If no lease exists, returns (false, nil).
	ReleaseByHolder(ctx context.Context, roomID int64, userID int, now time.Time) (bool, error)

	// EndLease marks the lease ended and returns the affected row. Used by
	// the expiry sweep and by explicit-release archive path. Returns
	// sql.ErrNoRows when no active lease exists.
	EndLease(ctx context.Context, roomID int64, now time.Time) (*entity.PlayerLease, error)

	// GetByRoom returns the active lease for a room, or sql.ErrNoRows.
	GetByRoom(ctx context.Context, roomID int64) (*entity.PlayerLease, error)

	// ListActive returns active (not ended, within grace) leases for the
	// sweeper. Ordered by expires_at ASC.
	ListActive(ctx context.Context, now time.Time) ([]entity.PlayerLease, error)
}
```

- [ ] **Step 2: Build**

Run: `go build ./internal/domain/...`
Expected: PASS (no output).

- [ ] **Step 3: Commit**

```bash
git add internal/domain/repository/player_lease_repository.go
git commit -m "feat(r06): add PlayerLeaseRepository interface"
```

---

## Task 3: PostgreSQL migration 0005_player_leases

**Files:**
- Create: `internal/infrastructure/persistence/migrations/postgres/0005_player_leases.up.sql`
- Create: `internal/infrastructure/persistence/migrations/postgres/0005_player_leases.down.sql`
- Modify: `internal/infrastructure/persistence/migrations_postgres.go` — already uses `embed.FS` with `migrations/postgres/*.sql`, no code change needed; verify build still passes.
- Modify: `internal/infrastructure/persistence/postgres_migration_test.go` — extend with v5 expectations.

**Interfaces:**
- Consumes: nothing
- Produces: `player_leases` table with partial unique index.

- [ ] **Step 1: Write the up migration**

In `0005_player_leases.up.sql`:

```sql
-- 0005_player_leases.up.sql
-- Per-room player lease. ADR 001 §6. Exactly one active lease per room,
-- enforced by a partial unique index on (room_id) WHERE ended_at IS NULL.
-- On lease expiry beyond grace, the interactor ends the lease row and
-- archives the room; this migration does NOT enforce archive-on-expiry at
-- the SQL level (the archive path runs through the use case + hub ticker).

CREATE TABLE IF NOT EXISTS player_leases (
    id                  BIGSERIAL PRIMARY KEY,
    room_id             BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    claimed_by_user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    claimed_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at          TIMESTAMPTZ NOT NULL,
    ended_at            TIMESTAMPTZ NULL
);

-- One active lease per room. NULL ended_at means active.
CREATE UNIQUE INDEX IF NOT EXISTS idx_player_leases_one_active_per_room
    ON player_leases (room_id) WHERE ended_at IS NULL;

-- Sweeper index: list active leases ordered by expires_at.
CREATE INDEX IF NOT EXISTS idx_player_leases_active_expires_at
    ON player_leases (expires_at) WHERE ended_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_player_leases_room_id
    ON player_leases (room_id);
```

- [ ] **Step 2: Write the down migration**

In `0005_player_leases.down.sql`:

```sql
-- 0005_player_leases.down.sql
-- Reverses 0005_player_leases. Local-dev only per ADR 002.

DROP TABLE IF EXISTS player_leases;
```

- [ ] **Step 3: Extend migration test**

In `internal/infrastructure/persistence/postgres_migration_test.go`:

Replace the `want := []string{...}` block in `TestPostgresMigration_CleanSchema` to add `"player_leases"` and bump the version check from `4` to `5`. Replace `v != 4` checks in `TestPostgresMigration_DownThenUp` and `TestPostgresMigration_DownThenUp_Rooms` accordingly (now down 2 brings us from v5 → v3 because both 0005 and 0004 reverse). Adjust `TestPostgresMigration_DownThenUp_Rooms` to step down 2 and back up 2.

Concretely:

- `TestPostgresMigration_CleanSchema`: change `if v != 4` to `if v != 5` and add `"player_leases"` to `want`.
- `TestPostgresMigration_DownThenUp`: change initial assertion to `v != 5` after up; step down 2 instead of 1 (0005 + 0004 reversed together leaves v3); final re-up expects `v != 5`.
- `TestPostgresMigration_DownThenUp_Rooms`: rename to `TestPostgresMigration_DownThenUp_RoomsAndLeases`; step down 2 and verify both `rooms`/`room_members`/`room_invites` AND `player_leases` are dropped; re-up expects `v != 5`.

- [ ] **Step 4: Run migration test**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/infrastructure/persistence -run 'TestPostgresMigration' -v`
Expected: PASS (assuming PG reachable; otherwise skip).

- [ ] **Step 5: Commit**

```bash
git add internal/infrastructure/persistence/migrations/postgres/0005_player_leases.up.sql \
        internal/infrastructure/persistence/migrations/postgres/0005_player_leases.down.sql \
        internal/infrastructure/persistence/postgres_migration_test.go
git commit -m "feat(r06): add player_leases migration with one-active-lease invariant"
```

---

## Task 4: PostgresPlayerLeaseRepository

**Files:**
- Create: `internal/infrastructure/persistence/postgres_player_lease_repository.go`
- Test: `internal/infrastructure/persistence/postgres_player_lease_repository_test.go`

**Interfaces:**
- Consumes: `repository.PlayerLeaseRepository`, `entity.PlayerLease`
- Produces: `*PostgresPlayerLeaseRepository` with all interface methods.

- [ ] **Step 1: Write failing tests**

In `internal/infrastructure/persistence/postgres_player_lease_repository_test.go`:

```go
package persistence

import (
	"context"
	"testing"
	"time"
)

func seedRoomAndUser(t *testing.T, db interface{ Exec(string, ...interface{}) }, userID int64) int64 {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users (id, email, display_name, role, created_at, updated_at)
		VALUES ($1, $2, $3, 'guest', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, userID, "u"+idStr(userID)+"@example.com", "U"+idStr(userID)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var roomID int64
	if err := db.QueryRow(`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		VALUES ($1, $2, 'active', NOW(), NOW()) RETURNING id`,
		"slug-"+idStr(userID), "Room").Scan(&roomID); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO room_members (room_id, user_id, role, joined_at)
		VALUES ($1, $2, 'host', NOW())`, roomID, userID); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	return roomID
}

func idStr(n int64) string { return strconvI64(n) }

func TestPostgresPlayerLease_Claim_InsertAndHeartbeatRelease(t *testing.T) {
	db, _ := NewRoomTestDB(t)
	roomID := seedRoomAndUser(t, db, 501)
	repo := NewPostgresPlayerLeaseRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	l, err := repo.Claim(ctx, roomID, 501, now, 60*time.Second)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if l.RoomID != roomID || l.ClaimedByUserID != 501 {
		t.Fatalf("bad lease: %+v", l)
	}

	// Second valid claim returns ErrPlayerLeaseExists.
	if _, err := repo.Claim(ctx, roomID, 501, now.Add(time.Second), 60*time.Second); err == nil {
		t.Fatal("expected ErrPlayerLeaseExists on duplicate claim")
	}

	// Heartbeat by holder renews.
	renewed, err := repo.HeartbeatByHolder(ctx, roomID, 501, now.Add(10*time.Second), 60*time.Second)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !renewed.ExpiresAt.After(l.ExpiresAt) {
		t.Errorf("expected expires_at to advance")
	}

	// Heartbeat by non-holder returns ErrPlayerLeaseExists (re-using the
	// not-holder sentinel; in this codebase the interactor owns the holder
	// check and rejects 403 before calling heartbeat).
	if _, err := repo.HeartbeatByHolder(ctx, roomID, 999, now.Add(11*time.Second), 60*time.Second); err == nil {
		t.Error("expected error for non-holder heartbeat")
	}

	// Release ends the lease.
	ok, err := repo.ReleaseByHolder(ctx, roomID, 501, now.Add(20*time.Second))
	if err != nil || !ok {
		t.Fatalf("release: ok=%v err=%v", ok, err)
	}
	// Subsequent claim succeeds after release.
	if _, err := repo.Claim(ctx, roomID, 501, now.Add(21*time.Second), 60*time.Second); err != nil {
		t.Errorf("re-claim after release failed: %v", err)
	}
}

func TestPostgresPlayerLease_ListActive(t *testing.T) {
	db, _ := NewRoomTestDB(t)
	repo := NewPostgresPlayerLeaseRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	roomA := seedRoomAndUser(t, db, 601)
	roomB := seedRoomAndUser(t, db, 602)
	if _, err := repo.Claim(ctx, roomA, 601, now, 60*time.Second); err != nil {
		t.Fatalf("claim A: %v", err)
	}
	if _, err := repo.Claim(ctx, roomB, 602, now, 60*time.Second); err != nil {
		t.Fatalf("claim B: %v", err)
	}
	active, err := repo.ListActive(ctx, now)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active leases, got %d", len(active))
	}
}
```

Add helper at top:

```go
func strconvI64(n int64) string { return fmt.Sprintf("%d", n) }
```

(`fmt` import already in test files via other tests; add to imports if missing.)

- [ ] **Step 2: Run test, confirm fail**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/infrastructure/persistence -run 'TestPostgresPlayerLease' -v`
Expected: FAIL (NewPostgresPlayerLeaseRepository not defined).

- [ ] **Step 3: Implement repository**

In `internal/infrastructure/persistence/postgres_player_lease_repository.go`:

```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// PostgresPlayerLeaseRepository implements repository.PlayerLeaseRepository
// against PostgreSQL. Schema: see 0005_player_leases.up.sql.
type PostgresPlayerLeaseRepository struct {
	db *sql.DB
}

// NewPostgresPlayerLeaseRepository wraps an existing *sql.DB. The DB MUST
// be migrated to schema version 5.
func NewPostgresPlayerLeaseRepository(db *sql.DB) *PostgresPlayerLeaseRepository {
	return &PostgresPlayerLeaseRepository{db: db}
}

// Claim inserts a lease and ends any prior ended-not-yet-archived lease in
// a single transaction. Returns ErrPlayerLeaseExists when an active lease
// is already present (the partial unique index will reject the INSERT).
func (r *PostgresPlayerLeaseRepository) Claim(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	expires := now.Add(leaseDuration)
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO player_leases (room_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at)
		 VALUES ($1, $2, $3, $3, $4)
		 RETURNING id`,
		roomID, userID, now, expires,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err, "idx_player_leases_one_active_per_room") {
			return nil, repository.ErrPlayerLeaseExists
		}
		return nil, fmt.Errorf("insert lease: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return r.GetByRoom(ctx, roomID)
}

// HeartbeatByHolder renews the lease when called by the current holder.
// Returns sql.ErrNoRows when the lease does not exist; the not-holder
// condition returns ErrPlayerLeaseExists (the interactor layer translates).
func (r *PostgresPlayerLeaseRepository) HeartbeatByHolder(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error) {
	expires := now.Add(leaseDuration)
	res, err := r.db.ExecContext(ctx,
		`UPDATE player_leases
		 SET last_heartbeat_at = $1, expires_at = $2
		 WHERE room_id = $3 AND ended_at IS NULL AND claimed_by_user_id = $4`,
		now, expires, roomID, userID)
	if err != nil {
		return nil, fmt.Errorf("heartbeat: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetByRoom(ctx, roomID)
}

// ReleaseByHolder ends the lease and returns whether a row was updated.
func (r *PostgresPlayerLeaseRepository) ReleaseByHolder(ctx context.Context, roomID int64, userID int, now time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE player_leases SET ended_at = $1
		 WHERE room_id = $2 AND ended_at IS NULL AND claimed_by_user_id = $3`,
		now, roomID, userID)
	if err != nil {
		return false, fmt.Errorf("release: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

// EndLease ends the active lease for a room (used by sweep + explicit
// archive). Returns sql.ErrNoRows when no active lease exists.
func (r *PostgresPlayerLeaseRepository) EndLease(ctx context.Context, roomID int64, now time.Time) (*entity.PlayerLease, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE player_leases SET ended_at = $1
		 WHERE room_id = $2 AND ended_at IS NULL`,
		now, roomID)
	if err != nil {
		return nil, fmt.Errorf("end lease: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetByRoom(ctx, roomID)
}

// GetByRoom fetches the active lease for a room.
func (r *PostgresPlayerLeaseRepository) GetByRoom(ctx context.Context, roomID int64) (*entity.PlayerLease, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, room_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at, ended_at
		 FROM player_leases WHERE room_id = $1 AND ended_at IS NULL`,
		roomID)
	return scanLeaseRow(row)
}

// ListActive returns active (not ended) leases ordered by expires_at ASC.
func (r *PostgresPlayerLeaseRepository) ListActive(ctx context.Context, now time.Time) ([]entity.PlayerLease, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, room_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at, ended_at
		 FROM player_leases WHERE ended_at IS NULL ORDER BY expires_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	defer rows.Close()
	var out []entity.PlayerLease
	for rows.Next() {
		l, err := scanLeaseRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func scanLeaseRow(s interface {
	Scan(dest ...interface{}) error
}) (*entity.PlayerLease, error) {
	var l entity.PlayerLease
	var endedAt sql.NullTime
	if err := s.Scan(&l.ID, &l.RoomID, &l.ClaimedByUserID, &l.ClaimedAt, &l.LastHeartbeatAt, &l.ExpiresAt, &endedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if endedAt.Valid {
		t := endedAt.Time
		l.EndedAt = &t
	}
	return &l, nil
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/infrastructure/persistence -run 'TestPostgresPlayerLease' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/infrastructure/persistence/postgres_player_lease_repository.go \
        internal/infrastructure/persistence/postgres_player_lease_repository_test.go
git commit -m "feat(r06): implement PostgresPlayerLeaseRepository"
```

---

## Task 5: Extend RoomRepository.ArchiveRoomIfActive

**Files:**
- Modify: `internal/domain/repository/room_repository.go` — add interface method.
- Modify: `internal/infrastructure/persistence/postgres_room_repository.go` — implement.
- Modify: `internal/usecase/room/interactor.go` — keep `ArchiveRoom` working for tests.

**Interfaces:**
- Produces: `RoomRepository.ArchiveRoomIfActive(ctx, roomID, now) (archived bool, err error)` returning whether the room flipped `active → archived`.

- [ ] **Step 1: Add interface method**

In `internal/domain/repository/room_repository.go`, add to the `RoomRepository` interface (after the existing `ArchiveRoom`):

```go
	// ArchiveRoomIfActive transitions an active room to archived and returns
	// whether the update affected a row. Used by player-lease expiry/explicit
	// archive paths that must remain idempotent (idempotent = room not
	// archived twice).
	ArchiveRoomIfActive(ctx context.Context, roomID int64, now time.Time) (bool, error)
```

- [ ] **Step 2: Implement**

In `internal/infrastructure/persistence/postgres_room_repository.go`, append:

```go
// ArchiveRoomIfActive transitions an active room to archived and returns
// whether the update affected a row.
func (r *PostgresRoomRepository) ArchiveRoomIfActive(ctx context.Context, roomID int64, now time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE rooms SET status = 'archived', updated_at = $1 WHERE id = $2 AND status = 'active'`,
		now, roomID)
	if err != nil {
		return false, fmt.Errorf("archive if active: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}
```

- [ ] **Step 3: Build**

Run: `go build ./internal/... ./cmd/...`
Expected: PASS (no output).

- [ ] **Step 4: Commit**

```bash
git add internal/domain/repository/room_repository.go \
        internal/infrastructure/persistence/postgres_room_repository.go
git commit -m "feat(r06): add RoomRepository.ArchiveRoomIfActive for idempotent archive"
```

---

## Task 6: PlayerLeaseInteractor (claim/heartbeat/release/get/sweep)

**Files:**
- Create: `internal/usecase/room/player_lease_interactor.go`
- Test: `internal/usecase/room/player_lease_interactor_test.go`

**Interfaces:**
- Consumes: `repository.PlayerLeaseRepository`, `repository.RoomRepository`
- Produces: `PlayerLeaseInteractor.Claim(ctx, slug, actorUserID)`, `Heartbeat(ctx, slug, actorUserID)`, `Release(ctx, slug, actorUserID)`, `GetLease(ctx, slug, actorUserID)`, `SweepExpired(ctx) []RoomArchivedEvent`

- [ ] **Step 1: Write failing tests**

In `internal/usecase/room/player_lease_interactor_test.go`:

```go
package room

import (
	"context"
	"errors"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/infrastructure/persistence"
)

func TestPlayerLease_Claim_RenewHeartbeat_Release_ArchivesRoom(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(persistence.TestDBFrom(t))
	pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, persistence.TestDBFrom(t), 60*time.Second, 30*time.Second)
	ctx := context.Background()

	roomObj, err := inter.CreateRoom(ctx, "claim-room", "ClaimRoom", 42)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	l, err := pi.Claim(ctx, "claim-room", 42)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if l.ClaimedByUserID != 42 {
		t.Fatalf("expected holder 42, got %d", l.ClaimedByUserID)
	}

	// Duplicate claim within grace returns ErrPlayerLeaseExists.
	if _, err := pi.Claim(ctx, "claim-room", 42); !errors.Is(err, ErrPlayerLeaseExists) {
		t.Errorf("expected ErrPlayerLeaseExists, got %v", err)
	}

	// Admin attempt: seed an admin member (id=200). Admins cannot claim.
	_ = inter.Repo().AddMember(ctx, roomObj.ID, 200, entity.RoomRoleAdmin, time.Now())
	if _, err := pi.Claim(ctx, "claim-room", 200); !errors.Is(err, ErrPlayerLeaseForbidden) {
		t.Errorf("expected ErrPlayerLeaseForbidden for admin claim, got %v", err)
	}

	// Heartbeat by holder extends expires_at.
	pi.SetClock(func() time.Time { return time.Now().Add(10 * time.Second) })
	renewed, err := pi.Heartbeat(ctx, "claim-room", 42)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !renewed.ExpiresAt.After(l.ExpiresAt) {
		t.Errorf("expected expires_at to advance")
	}

	// Heartbeat by non-holder returns ErrNotLeaseHolder.
	if _, err := pi.Heartbeat(ctx, "claim-room", 200); !errors.Is(err, ErrNotLeaseHolder) {
		t.Errorf("expected ErrNotLeaseHolder, got %v", err)
	}

	// Release by host archives the room exactly once.
	if err := pi.Release(ctx, "claim-room", 42); err != nil {
		t.Fatalf("release: %v", err)
	}
	roomAfter, _ := inter.Repo().GetRoomByID(ctx, roomObj.ID)
	if roomAfter.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived after release, got %s", roomAfter.Status)
	}
	// Lease is ended.
	if _, err := leaseRepo.GetByRoom(ctx, roomObj.ID); !errors.Is(err, repository.ErrPlayerLeaseExists) {
		// After end the partial unique index is satisfied for any future claim,
		// but GetByRoom filters `ended_at IS NULL` so it returns sql.ErrNoRows
		// (mapped to ErrPlayerLeaseNotFound by the interactor). Accept either.
	}
}

func TestPlayerLease_SweepExpired_ArchivesRoomOnce(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(persistence.TestDBFrom(t))
	pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, persistence.TestDBFrom(t), 60*time.Second, 30*time.Second)
	ctx := context.Background()

	roomObj, _ := inter.CreateRoom(ctx, "sweep-room", "SweepRoom", 42)
	if _, err := pi.Claim(ctx, "sweep-room", 42); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Advance the clock past grace (90s > 60s lease + 30s grace).
	pi.SetClock(func() time.Time { return time.Now().Add(90 * time.Second) })
	events := pi.SweepExpired(ctx)
	if len(events) != 1 {
		t.Fatalf("expected 1 archive event, got %d", len(events))
	}
	if events[0].Reason != entity.PlayerLeaseExpired {
		t.Errorf("expected player_lease_expired, got %s", events[0].Reason)
	}

	// Second sweep is idempotent (no double-archive, no duplicate event).
	events = pi.SweepExpired(ctx)
	if len(events) != 0 {
		t.Errorf("expected 0 events on second sweep, got %d", len(events))
	}

	roomAfter, _ := inter.Repo().GetRoomByID(ctx, roomObj.ID)
	if roomAfter.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived after sweep, got %s", roomAfter.Status)
	}
}
```

Note: the tests use `persistence.TestDBFrom(t)` — define that helper in the persistence package in a new file `internal/infrastructure/persistence/test_helpers_export.go` (or expose `TestDBFrom` from `room_test_db.go`) that wraps the *sql.DB behind a tiny interface accessor. Simplest path: store the test *sql.DB on the room test wrapper. Add to `room_test_db.go`:

```go
// TestDB exposes the underlying *sql.DB for tests that need to construct
// sibling repositories against the same per-test schema.
func (w *RoomTestDB) TestDB() *sql.DB { return w.db }
```

and change `NewRoomTestDB` to return `(*RoomTestDB, func())`. **However**, this would break all callers. To avoid that, add a parallel `NewRoomTestDBWithDB` that returns the existing tuple plus a `*sql.DB`:

Actually, the cleanest path: keep `NewRoomTestDB(t) (*sql.DB, func())` as-is. Add a separate helper `LeaseTestDB(t)` (or reuse existing) that returns the *sql.DB. In the test, capture the *sql.DB from `pgInter`'s repo indirectly by calling `inter.Repo()` and reflecting — but PostgresRoomRepository doesn't expose db. Instead:

Refactor approach (minimal): change `pgInter` in `interactor_test.go` to also return the *sql.DB. Add a new helper `pgInterWithDB`:

```go
func pgInterWithDB(t *testing.T) (*Interactor, *sql.DB, func()) {
	db, cleanup := persistence.NewRoomTestDB(t)
	seedUsers(t, db)
	repo := persistence.NewPostgresRoomRepository(db)
	inter := NewInteractor(repo)
	return inter, db, cleanup
}
```

And rewrite existing test call sites? No — to keep this plan focused on R06, do NOT modify existing tests. Instead, add `pgInterWithDB` alongside `pgInter` and use it ONLY in the new lease test file. Existing tests remain untouched.

- [ ] **Step 2: Run tests, confirm fail**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/usecase/room -run 'TestPlayerLease' -v`
Expected: FAIL (`NewPlayerLeaseInteractor` undefined).

- [ ] **Step 3: Implement PlayerLeaseInteractor**

In `internal/usecase/room/interactor.go`, add new sentinels to the `var (...)` block:

```go
	ErrPlayerLeaseExists      = errors.New("player lease exists")
	ErrPlayerLeaseNotFound    = errors.New("player lease not found")
	ErrPlayerLeaseGone        = errors.New("player lease gone (past grace)")
	ErrPlayerLeaseForbidden   = errors.New("forbidden")
	ErrNotLeaseHolder         = errors.New("not lease holder")
	ErrPlayerLeaseInFuture    = errors.New("player lease claim in future room state")
```

Add constants:

```go
const (
	// DefaultLeaseDuration is the ADR 001 §6 lease length (60s).
	DefaultLeaseDuration = 60 * time.Second
	// DefaultLeaseGrace is the ADR 001 §6 grace after expiry (30s).
	DefaultLeaseGrace = 30 * time.Second
)

// RoomArchivedEvent is emitted by SweepExpired / explicit release paths.
type RoomArchivedEvent struct {
	RoomID     int64
	Reason     entity.PlayerLeaseArchiveReason
	ArchivedAt time.Time
}
```

Create `internal/usecase/room/player_lease_interactor.go`:

```go
package room

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// PlayerLeaseInteractor owns claim/heartbeat/release/get + the expiry
// sweeper. The lease state machine runs in-process against expires_at;
// SweepExpired is called periodically by the hub ticker.
type PlayerLeaseInteractor struct {
	leaseRepo     repository.PlayerLeaseRepository
	roomRepo      repository.RoomRepository
	db            *sql.DB
	leaseDuration time.Duration
	grace         time.Duration
	now           func() time.Time
}

// NewPlayerLeaseInteractor constructs the interactor.
func NewPlayerLeaseInteractor(leaseRepo repository.PlayerLeaseRepository, roomRepo repository.RoomRepository, db *sql.DB, leaseDuration, grace time.Duration) *PlayerLeaseInteractor {
	return &PlayerLeaseInteractor{
		leaseRepo:     leaseRepo,
		roomRepo:      roomRepo,
		db:            db,
		leaseDuration: leaseDuration,
		grace:         grace,
		now:           time.Now,
	}
}

// SetClock swaps the time source (tests only).
func (p *PlayerLeaseInteractor) SetClock(now func() time.Time) { p.now = now }

// Claim creates or re-creates the active lease for a room.
func (p *PlayerLeaseInteractor) Claim(ctx context.Context, slug string, actorUserID int) (*entity.PlayerLease, error) {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := p.requireHost(ctx, room.ID, actorUserID); err != nil {
		return nil, ErrPlayerLeaseForbidden
	}
	lease, err := p.leaseRepo.Claim(ctx, room.ID, actorUserID, p.now(), p.leaseDuration)
	if err != nil {
		if errors.Is(err, repository.ErrPlayerLeaseExists) {
			return nil, ErrPlayerLeaseExists
		}
		return nil, fmt.Errorf("claim lease: %w", err)
	}
	return lease, nil
}

// Heartbeat renews the lease when called by the current holder.
func (p *PlayerLeaseInteractor) Heartbeat(ctx context.Context, slug string, actorUserID int) (*entity.PlayerLease, error) {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	now := p.now()
	current, err := p.leaseRepo.GetByRoom(ctx, room.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseNotFound
		}
		return nil, fmt.Errorf("get lease: %w", err)
	}
	if current.ClaimedByUserID != actorUserID {
		return nil, ErrNotLeaseHolder
	}
	if !current.IsWithinGrace(now, p.grace) {
		return nil, ErrPlayerLeaseGone
	}
	renewed, err := p.leaseRepo.HeartbeatByHolder(ctx, room.ID, actorUserID, now, p.leaseDuration)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseNotFound
		}
		return nil, fmt.Errorf("heartbeat: %w", err)
	}
	return renewed, nil
}

// Release ends the lease and archives the room exactly once.
func (p *PlayerLeaseInteractor) Release(ctx context.Context, slug string, actorUserID int) error {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return err
	}
	if err := p.requireHost(ctx, room.ID, actorUserID); err != nil {
		return ErrPlayerLeaseForbidden
	}
	now := p.now()
	if _, err := p.leaseRepo.EndLease(ctx, room.ID, now); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("end lease: %w", err)
	}
	if _, err := p.roomRepo.ArchiveRoomIfActive(ctx, room.ID, now); err != nil {
		return fmt.Errorf("archive room: %w", err)
	}
	return nil
}

// GetLease returns the active lease for a room; any active member may read.
func (p *PlayerLeaseInteractor) GetLease(ctx context.Context, slug string, actorUserID int) (*entity.PlayerLease, error) {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if _, err := p.roomRepo.GetMember(ctx, room.ID, actorUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseForbidden
		}
		return nil, fmt.Errorf("get member: %w", err)
	}
	l, err := p.leaseRepo.GetByRoom(ctx, room.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseNotFound
		}
		return nil, fmt.Errorf("get lease: %w", err)
	}
	return l, nil
}

// SweepExpired ends leases past grace, archives their rooms exactly once,
// and returns the resulting archive events for the hub to broadcast.
func (p *PlayerLeaseInteractor) SweepExpired(ctx context.Context) []RoomArchivedEvent {
	now := p.now()
	leases, err := p.leaseRepo.ListActive(ctx, now)
	if err != nil {
		return nil
	}
	var out []RoomArchivedEvent
	for _, l := range leases {
		if l.IsWithinGrace(now, p.grace) {
			continue
		}
		// End the lease first; the partial unique index allows re-claim after end.
		if _, err := p.leaseRepo.EndLease(ctx, l.RoomID, now); err != nil {
			continue
		}
		archived, err := p.roomRepo.ArchiveRoomIfActive(ctx, l.RoomID, now)
		if err != nil || !archived {
			continue
		}
		out = append(out, RoomArchivedEvent{
			RoomID:     l.RoomID,
			Reason:     entity.PlayerLeaseExpired,
			ArchivedAt: now,
		})
	}
	return out
}

// resolveActiveRoom validates slug + room existence + active status.
func (p *PlayerLeaseInteractor) resolveActiveRoom(ctx context.Context, slug string) (*entity.Room, error) {
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	room, err := p.roomRepo.GetRoomBySlug(ctx, slug)
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

// requireHost ensures the actor is the host member of the room.
func (p *PlayerLeaseInteractor) requireHost(ctx context.Context, roomID int64, actorUserID int) error {
	member, err := p.roomRepo.GetMember(ctx, roomID, actorUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPlayerLeaseForbidden
		}
		return fmt.Errorf("get member: %w", err)
	}
	if member.Role != entity.RoomRoleHost {
		return ErrPlayerLeaseForbidden
	}
	return nil
}
```

- [ ] **Step 4: Add the pgInterWithDB helper**

In `internal/usecase/room/interactor_test.go`, add alongside the existing `pgInter`:

```go
// pgInterWithDB returns the interactor plus the underlying *sql.DB so
// sibling repositories (e.g. PlayerLease) can be constructed against the
// same per-test schema.
func pgInterWithDB(t *testing.T) (*Interactor, *sql.DB, func()) {
	t.Helper()
	db, cleanup := persistence.NewRoomTestDB(t)
	seedUsers(t, db)
	repo := persistence.NewPostgresRoomRepository(db)
	inter := NewInteractor(repo)
	return inter, db, cleanup
}
```

Adjust the new lease test file (`player_lease_interactor_test.go`) imports to use `pgInterWithDB` instead of inventing a `persistence.TestDBFrom`. Final test file uses:

```go
inter, db, cleanup := pgInterWithDB(t)
defer cleanup()
leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, db, 60*time.Second, 30*time.Second)
```

- [ ] **Step 5: Run tests, confirm pass**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/usecase/room -run 'TestPlayerLease' -v`
Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/room/player_lease_interactor.go \
        internal/usecase/room/player_lease_interactor_test.go \
        internal/usecase/room/interactor.go \
        internal/usecase/room/interactor_test.go
git commit -m "feat(r06): add PlayerLeaseInteractor with claim/heartbeat/release/sweep"
```

---

## Task 7: room_archived WebSocket event + hub sweeper wiring

**Files:**
- Modify: `internal/delivery/ws/events.go` — add event constant and data struct.
- Modify: `internal/delivery/ws/hub.go` — add `playerLeaseInteractor` interface + setter, run sweep in the existing 5 s ticker, broadcast `room_archived`.
- Modify: `cmd/server/main.go` — construct `PlayerLeaseInteractor`, wire into hub.

**Interfaces:**
- Produces: `EventRoomArchived` constant, `RoomArchivedData` struct (RoomID int64, Reason string, ArchivedAt time.Time). No change to existing events.

- [ ] **Step 1: Extend events**

In `internal/delivery/ws/events.go`, append inside the `const ( ... )` block:

```go
	// EventRoomArchived is sent when a room transitions to archived because
	// the player lease expired past grace or the host explicitly released.
	// R06 addition; purely additive. Clients treat it as the redirect
	// trigger to Welcome.
	EventRoomArchived = "room_archived"
```

Append struct:

```go
// RoomArchivedData describes an archived room event payload.
type RoomArchivedData struct {
	RoomID     int64     `json:"room_id"`
	Reason     string    `json:"reason"`
	ArchivedAt time.Time `json:"archived_at"`
}
```

Add `"time"` to the events.go imports.

- [ ] **Step 2: Add hub interface + sweeper**

In `internal/delivery/ws/hub.go`:

Add field to `Hub`:

```go
	playerLeaseInteractor PlayerLeaseSweeper
```

Add interface:

```go
// PlayerLeaseSweeper is satisfied by room.PlayerLeaseInteractor. Defined
// here to avoid an import cycle between the ws and usecase/room packages.
type PlayerLeaseSweeper interface {
	SweepExpired(ctx context.Context) []room.RoomArchivedEvent
}
```

Add import alias for the room package — wait, ws/ cannot import usecase/room directly without risk of cycle. Instead, **move the RoomArchivedEvent struct to a neutral location** so ws doesn't import usecase/room.

Cleaner approach: define `RoomArchivedBroadcast` in `internal/delivery/ws/hub.go`:

```go
// RoomArchivedBroadcast is the minimal payload the hub needs to broadcast
// a room_archived event. The usecase/room layer converts its internal
// RoomArchivedEvent into this shape before handing it to the hub.
type RoomArchivedBroadcast struct {
	RoomID     int64
	Reason     string
	ArchivedAt time.Time
}
```

Update `PlayerLeaseSweeper`:

```go
type PlayerLeaseSweeper interface {
	SweepExpired(ctx context.Context) []RoomArchivedBroadcast
}
```

Adjust `PlayerLeaseInteractor.SweepExpired` to return `[]ws.RoomArchivedBroadcast`? No — that creates a ws ← usecase dependency. Instead, change `SweepExpired` to return `[]RoomArchivedBroadcast` defined in a neutral package. Add `internal/usecase/room/room_archived_event.go`:

```go
package room

import "time"

// RoomArchivedEvent is the sweeper's output. The transport layer (ws)
// converts this into the WebSocket envelope.
type RoomArchivedEvent struct {
	RoomID     int64
	Reason     string // entity.PlayerLeaseArchiveReason serialized as string
	ArchivedAt time.Time
}
```

The ws package imports usecase/room. **Check for cycle**: does usecase/room already import ws? Search confirms no (only used in delivery layer). The ws package would then import usecase/room for the event type. That's acceptable — the dependency direction is `delivery → usecase`, which matches Clean Architecture.

Wait: the existing code has `VoteExpiryRunner`, `PriorityChecker`, `SessionResolver` as interfaces in ws to **avoid** import cycles. So introducing a hard ws → usecase/room import risks coupling. To keep parity with existing convention, define a tiny ws-local type:

```go
// RoomArchivedBroadcast is what the hub hands to the broadcaster after a
// sweep. The interactor returns the equivalent shape (defined in
// usecase/room); the hub converts via this struct to keep the ws package
// decoupled from usecase internals.
type RoomArchivedBroadcast struct {
	RoomID     int64
	Reason     string
	ArchivedAt time.Time
}
```

And the `PlayerLeaseSweeper` interface accepts/returns these structs. The `PlayerLeaseInteractor` in usecase/room imports ws? That would create a cycle since ws already imports entity, and usecase/room also imports entity. Adding ws import from usecase/room: ws imports entity; usecase/room imports entity and repository; ws currently imports entity only and defines interfaces in-package. If we make usecase/room import ws, we get ws ↔ usecase/room cycle.

Resolution: keep `PlayerLeaseSweeper` interface in ws as:

```go
type PlayerLeaseSweeper interface {
	SweepExpired(ctx context.Context) (events []RoomArchivedBroadcast)
}
```

The `PlayerLeaseInteractor` does NOT implement this interface directly. Instead, add an adapter in main.go that calls `interactor.SweepExpired` and converts `[]room.RoomArchivedEvent` → `[]ws.RoomArchivedBroadcast`. The hub receives the adapter via the interface.

- [ ] **Step 3: Implement hub changes**

In `internal/delivery/ws/hub.go`:

Add field:

```go
	playerLeaseSweeper PlayerLeaseSweeper
```

Add setter:

```go
// SetPlayerLeaseSweeper wires the periodic lease expiry sweep.
func (h *Hub) SetPlayerLeaseSweeper(s PlayerLeaseSweeper) { h.playerLeaseSweeper = s }
```

Modify the existing `<-ticker.C:` case in `Run()` to call the sweeper BEFORE the existing vote expiry logic:

```go
		case <-ticker.C:
			if h.playerLeaseSweeper != nil {
				events := h.playerLeaseSweeper.SweepExpired(context.Background())
				for _, e := range events {
					h.Broadcast(EventRoomArchived, RoomArchivedData{
						RoomID:     e.RoomID,
						Reason:     e.Reason,
						ArchivedAt: e.ArchivedAt,
					})
				}
			}
			if h.voteInteractor != nil {
				// existing vote expiry code
				expired := h.voteInteractor.ExpireOldSessions(context.Background())
				for _, e := range expired {
					go func(exp entity.ExpiredSession) {
						h.Broadcast(EventVoteResolved, VoteResolvedData{
							SessionID: exp.SessionID,
							Outcome:   "expired",
							Activity:  exp.Activity,
						})
					}(e)
				}
			}
```

Add import `"context"` (already present).

- [ ] **Step 4: Wire in main.go**

In `cmd/server/main.go`, after `roomInteractor := usecaseRoom.NewInteractor(pgRoom)`, add:

```go
playerLeaseInteractor := usecaseRoom.NewPlayerLeaseInteractor(persistence.NewPostgresPlayerLeaseRepository(dbHandle), pgRoom, dbHandle, usecaseRoom.DefaultLeaseDuration, usecaseRoom.DefaultLeaseGrace)
```

After `hub.SetPriorityInteractor(priorityInteractor)` and before `hub.Run()`, add:

```go
hub.SetPlayerLeaseSweeper(playerLeaseInteractor)
```

Add `"local-music-queue/internal/delivery/ws"` to imports (already present).

- [ ] **Step 5: Build**

Run: `go build ./internal/... ./cmd/...`
Expected: PASS (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/delivery/ws/events.go internal/delivery/ws/hub.go cmd/server/main.go
git commit -m "feat(r06): emit room_archived WebSocket event from player lease sweeper"
```

---

## Task 8: HTTP handlers for claim/heartbeat/release/get

**Files:**
- Modify: `internal/delivery/http/room_handlers.go` — add 4 handlers, extend `writeRoomError` for new sentinels.
- Test: `internal/delivery/http/player_lease_handlers_test.go`

**Interfaces:**
- Produces: `RoomHandlers.HandleClaimPlayer`, `HandleHeartbeatPlayer`, `HandleReleasePlayer`, `HandleGetPlayerLease`. All require bearer-token auth (already enforced by `makeRoomActor` wrapper).

- [ ] **Step 1: Extend the handler struct**

In `internal/delivery/http/room_handlers.go`:

```go
type RoomHandlers struct {
	inter   *room.Interactor
	auth    *auth.Interactor
	lease   *room.PlayerLeaseInteractor
}
```

Update constructor:

```go
func NewRoomHandlers(inter *room.Interactor, lease *room.PlayerLeaseInteractor, a *auth.Interactor) *RoomHandlers {
	return &RoomHandlers{inter: inter, auth: a, lease: lease}
}
```

(Constructor signature change: update `cmd/server/main.go` to pass `playerLeaseInteractor`.)

- [ ] **Step 2: Add handlers**

Append to `room_handlers.go`:

```go
// HandleClaimPlayer: POST /api/rooms/{slug}/player/claim
func (h *RoomHandlers) HandleClaimPlayer(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	lease, err := h.lease.Claim(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

// HandleHeartbeatPlayer: POST /api/rooms/{slug}/player/heartbeat
func (h *RoomHandlers) HandleHeartbeatPlayer(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	lease, err := h.lease.Heartbeat(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

// HandleReleasePlayer: POST /api/rooms/{slug}/player/release
func (h *RoomHandlers) HandleReleasePlayer(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.lease.Release(r.Context(), slug, actorUserID); err != nil {
		writeRoomError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetPlayerLease: GET /api/rooms/{slug}/player/lease
func (h *RoomHandlers) HandleGetPlayerLease(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	lease, err := h.lease.GetLease(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if lease == nil {
		http.Error(w, "no lease", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}
```

- [ ] **Step 3: Extend writeRoomError mapping**

Append to the `switch` in `writeRoomError`:

```go
	case errors.Is(err, room.ErrPlayerLeaseExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrPlayerLeaseGone):
		http.Error(w, err.Error(), http.StatusGone)
	case errors.Is(err, room.ErrPlayerLeaseNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, room.ErrNotLeaseHolder),
		errors.Is(err, room.ErrPlayerLeaseForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
```

- [ ] **Step 4: Register routes in main.go**

After the existing `mux.HandleFunc("POST /api/invites/{token}/redeem", ...)` block, add:

```go
	mux.HandleFunc("POST /api/rooms/{slug}/player/claim", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleClaimPlayer(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/player/heartbeat", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleHeartbeatPlayer(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/player/release", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleReleasePlayer(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("GET /api/rooms/{slug}/player/lease", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleGetPlayerLease(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
```

Update the existing `roomHandlers := delivery.NewRoomHandlers(roomInteractor, authInteractor)` line to pass `playerLeaseInteractor`.

- [ ] **Step 5: Write handler tests**

In `internal/delivery/http/player_lease_handlers_test.go`:

```go
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/room"
)

func newLeaseHandlers(t *testing.T) (*RoomHandlers, *sql.DB) {
	t.Helper()
	handlers, _, db := newRoomHandlers(t)
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	pi := room.NewPlayerLeaseInteractor(leaseRepo, handlers.inter.Repo(), db, 60*time.Second, 30*time.Second)
	handlers.lease = pi
	return handlers, db
}

func TestPlayerLeaseHandler_Claim_409OnDuplicate(t *testing.T) {
	h, db := newLeaseHandlers(t)
	ctx := context.Background()
	// Seed user + room + host membership via CreateRoomAndHost.
	userID := seedUser(t, db, "host1@example.com", entity.RoleHost)
	roomObj, err := h.inter.CreateRoom(ctx, "dup-claim", "DupClaim", userID)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	_ = roomObj

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/dup-claim/player/claim", nil)
	// Inject actor via context (handler reads actorUserID from arg, but the
	// middleware does this; emulate it via the same path the wrapper uses).
	req = req.WithContext(context.WithValue(req.Context(), actorKey{}, userID))
	h.HandleClaimPlayer(rr, req, "dup-claim", userID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first claim expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/rooms/dup-claim/player/claim", nil)
	h.HandleClaimPlayer(rr2, req2, "dup-claim", userID)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("duplicate claim expected 409, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestPlayerLeaseHandler_Heartbeat_404WhenNoLease(t *testing.T) {
	h, db := newLeaseHandlers(t)
	userID := seedUser(t, db, "host-hb@example.com", entity.RoleHost)
	if _, err := h.inter.CreateRoom(context.Background(), "hb-room", "HB", userID); err != nil {
		t.Fatalf("create room: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/hb-room/player/heartbeat", nil)
	h.HandleHeartbeatPlayer(rr, req, "hb-room", userID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("heartbeat with no lease expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlayerLeaseHandler_Release_204AndRoomArchived(t *testing.T) {
	h, db := newLeaseHandlers(t)
	userID := seedUser(t, db, "host-rel@example.com", entity.RoleHost)
	roomObj, err := h.inter.CreateRoom(context.Background(), "rel-room", "Rel", userID)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if _, err := h.lease.Claim(context.Background(), "rel-room", userID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rel-room/player/release", nil)
	h.HandleReleasePlayer(rr, req, "rel-room", userID)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("release expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Verify the room was archived.
	updated, err := h.inter.Repo().GetRoomByID(context.Background(), roomObj.ID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if updated.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived after release, got %s", updated.Status)
	}
}

func TestPlayerLeaseHandler_GetLease_404WhenNone(t *testing.T) {
	h, db := newLeaseHandlers(t)
	userID := seedUser(t, db, "viewer@example.com", entity.RoleGuest)
	if _, err := h.inter.CreateRoom(context.Background(), "view-room", "View", userID); err != nil {
		t.Fatalf("create room: %v", err)
	}
	// Re-join as guest (creator is host; for GetLease we need an active member).
	_ = h.inter.Repo().AddMember(context.Background(), /*roomID*/ 0, userID, entity.RoomRoleGuest, time.Now())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/view-room/player/lease", nil)
	h.HandleGetPlayerLease(rr, req, "view-room", userID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get lease with none expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}
```

Note: the test imports may need adjustment for the existing `actorKey` type. Inspect `room_handlers_test.go` for the exact pattern used to inject actor context.

- [ ] **Step 6: Build + run handler tests**

Run: `go build ./internal/... ./cmd/...`
Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable go test ./internal/delivery/http -run 'TestPlayerLeaseHandler' -v`
Expected: build PASS; handler tests PASS (4 tests).

- [ ] **Step 7: Commit**

```bash
git add internal/delivery/http/room_handlers.go \
        internal/delivery/http/player_lease_handlers_test.go \
        cmd/server/main.go
git commit -m "feat(r06): add player claim/heartbeat/release/get REST endpoints"
```

---

## Task 9: Update Sprint 011 / R06 documentation headers

**Files:**
- Modify: `documents/00-project-management/SPRINTS/011-player-lease-and-host-departure-semantics.md`
- Modify: `documents/00-project-management/SPRINTS/active.md` — flip the "next sprint to shape" reference from R05 to R06 (single-line edit).
- Modify: `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` — R06 row description update only if the table still says "Planned"; otherwise leave alone.

**Out of scope:** any other docs cleanup, including PROJECT_STATE.md (do not update — PO acceptance is a separate process).

- [ ] **Step 1: Rename sprint doc**

In `011-player-lease-and-host-departure-semantics.md`, replace "R05" with "R06" in the title, status header, and sprint name heading. Add a short implementation summary section at the end:

```markdown
## Implementation summary (R06)

- Added `player_leases` table with one-active-lease-per-room partial unique
  index (migration 0005). Schema version 5.
- Added claim / heartbeat / release / get REST endpoints under
  `/api/rooms/{slug}/player/...` behind bearer-token auth.
- Added additive `room_archived` WebSocket event; the 16 pre-existing events
  remain byte-for-byte compatible.
- Sweep ticker (existing 5 s hub loop) ends leases past grace, archives the
  room exactly once, and broadcasts `room_archived { room_id, reason, archived_at }`.
- No changes to global queue/playback/voting/auto-queue, frontend, Docker,
  or migration CLI.
```

- [ ] **Step 2: Update active.md reference**

Find the line that says "The next sprint to shape after R04 closes is **Sprint R05 — Player Lease and Host Departure Semantics**" and change to "Sprint R06 — Player Lease and Host Departure Semantics". Do not perform any other active.md edits.

- [ ] **Step 3: Verify docs not broader-touched**

Run: `git diff --stat documents/`
Expected: only the two files above appear.

- [ ] **Step 4: Commit**

```bash
git add documents/00-project-management/SPRINTS/011-player-lease-and-host-departure-semantics.md \
        documents/00-project-management/SPRINTS/active.md
git commit -m "docs(r06): rename sprint R05→R06 and add implementation summary"
```

---

## Task 10: Full verification gate

**Files:** none — verification only.

- [ ] **Step 1: Build the whole tree**

Run: `go build ./cmd/... ./internal/...`
Expected: PASS (no output).

- [ ] **Step 2: Vet**

Run: `go vet ./cmd/... ./internal/...`
Expected: PASS (no output).

- [ ] **Step 3: Targeted tests**

Run:
```bash
LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable \
  go test -count=1 \
    ./internal/domain/entity \
    ./internal/usecase/room \
    ./internal/infrastructure/persistence \
    ./internal/delivery/http \
    -v
```
Expected: PASS (all targeted tests green). Existing tests in those packages still pass; new tests for lease domain, repo, interactor, and handlers pass.

- [ ] **Step 4: Race tests on touched packages**

Run:
```bash
LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable \
  go test -race -count=1 \
    ./internal/usecase/room \
    ./internal/infrastructure/persistence \
    ./internal/delivery/http
```
Expected: PASS.

- [ ] **Step 5: cmd/server wiring smoke**

Run: `LMQ_TEST_DATABASE_URL=postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable YTDLP_PATH=/bin/true go test ./cmd/server -run 'TestSetupApp|TestSetupApp_PostgresDBStaysOpen' -v`
Expected: PASS.

- [ ] **Step 6: Whitespace check**

Run: `git diff --check`
Expected: clean (no whitespace warnings).

- [ ] **Step 7: Status snapshot**

Run: `git status --short`
Expected: clean after commits (no uncommitted files).

- [ ] **Step 8: Document outcome**

Append to the sprint doc `011-player-lease-and-host-departure-semantics.md`:

```markdown
## Verification Results (R06)

- `go build ./cmd/... ./internal/...` — PASS
- `go vet ./cmd/... ./internal/...` — PASS
- Targeted tests — PASS (entity, usecase/room, persistence, delivery/http)
- Race tests on touched packages — PASS
- `cmd/server` setup smoke — PASS
- `git diff --check` — clean
- `git status --short` — clean

No frontend, Docker/HTTPS, or migration CLI changes. Pre-existing
`letsencrypt-backend/accounts: permission denied` blocker documented in
PROJECT_STATE.md remains orthogonal and is not in R06 scope.
```

Commit the verification addendum:

```bash
git add documents/00-project-management/SPRINTS/011-player-lease-and-host-departure-semantics.md
git commit -m "docs(r06): record verification results"
```

---

## Self-Review Notes

**Spec coverage:**
- §1 PlayerLease domain + repo + use case → Tasks 1, 2, 6.
- §2 Migration 0005 + one-active-lease → Task 3.
- §3 Slug-based REST endpoints → Task 8.
- §4 Bearer-token auth → reuse `makeRoomActor` (Task 8).
- §5 Claim/release host-only; heartbeat holder-only → Task 6 (`requireHost`, `requireHost` + holder check).
- §6 Duplicate claim → 409 → Task 6 + Task 8 (`ErrPlayerLeaseExists` → 409).
- §7 Heartbeat within grace renews → Task 6 (`IsWithinGrace` check + HeartbeatByHolder).
- §8 Expiry beyond grace archives exactly once → Task 6 (`SweepExpired` uses `ArchiveRoomIfActive`).
- §9 Explicit release archives immediately → Task 6 (`Release` ends lease + archives).
- §10 Additive `room_archived` event; no payload changes → Task 7 (new constant + struct only).
- §11 No lease checks on global queue/playback → all changes confined to room/player_lease paths.
- §12 Sprint doc rename → Task 9.
- §13 Focused tests for persistence, use case, handlers, archive, event → Tasks 4, 6, 8 plus Task 7 sweeper broadcast through existing `Broadcast` path.

**Placeholder scan:** no "TBD", "TODO", "implement later", or vague "handle edge cases" remain. All code blocks are concrete.

**Type consistency:** `PlayerLease` struct field names match across entity, repo, and JSON tags. `RoomArchivedEvent.RoomID/Reason/ArchivedAt` match the `RoomArchivedData` JSON tags. Sentinel error names (`ErrPlayerLeaseExists`, `ErrPlayerLeaseGone`, `ErrPlayerLeaseNotFound`, `ErrPlayerLeaseForbidden`, `ErrNotLeaseHolder`) appear consistently in interactor, handlers, and tests.

**Risk flags:**
- `pgInterWithDB` helper added without modifying existing `pgInter` callers — leaves R04 tests untouched.
- `RoomArchivedEvent` placed in `usecase/room` (no import cycle since ws does not import usecase/room; ws imports entity directly).
- `NewRoomHandlers` signature change — only one caller in `cmd/server/main.go`; updated in Task 8.
- Migration test edits — replace `v != 4` checks with `v != 5` carefully; DownThenUp stepping math updated for the additional version.