package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"local-music-queue/internal/domain/repository"
)

// seedRoomAndUser inserts a user, a room, and a host membership for the
// given userID; returns the room's primary key.
func seedRoomAndUser(t *testing.T, db *sql.DB, userID int64) int64 {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO users (id, email, display_name, role, created_at, updated_at)
		VALUES ($1, $2, $3, 'guest', $4, $4)
		ON CONFLICT (id) DO NOTHING`, userID, fmt.Sprintf("u%d@example.com", userID), fmt.Sprintf("U%d", userID), now); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	slug := fmt.Sprintf("slug-%d-%d", userID, time.Now().UnixNano())
	var roomID int64
	if err := db.QueryRow(`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		VALUES ($1, $2, 'active', $3, $3) RETURNING id`, slug, "Room", now).Scan(&roomID); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO room_members (room_id, user_id, role, joined_at)
		VALUES ($1, $2, 'host', $3)`, roomID, userID, now); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	return roomID
}

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
	if _, err := repo.Claim(ctx, roomID, 501, now.Add(time.Second), 60*time.Second); !errors.Is(err, repository.ErrPlayerLeaseExists) {
		t.Fatalf("expected ErrPlayerLeaseExists on duplicate claim, got %v", err)
	}

	// Heartbeat by holder renews.
	renewed, err := repo.HeartbeatByHolder(ctx, roomID, 501, now.Add(10*time.Second), 60*time.Second)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !renewed.ExpiresAt.After(l.ExpiresAt) {
		t.Errorf("expected expires_at to advance")
	}

	// Heartbeat by non-holder returns sql.ErrNoRows.
	if _, err := repo.HeartbeatByHolder(ctx, roomID, 999, now.Add(11*time.Second), 60*time.Second); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for non-holder heartbeat, got %v", err)
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