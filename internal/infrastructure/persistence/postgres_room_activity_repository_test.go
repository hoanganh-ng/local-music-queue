package persistence

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// PostgresRoomActivityRepository must satisfy the domain contract.
var _ repository.RoomActivityRepository = (*PostgresRoomActivityRepository)(nil)

// pgAct(t) is the equivalent seed helper for the activity repo. It reuses
// NewRoomTestDB (per-test PG schema) and the shared seedRoom helper defined
// alongside the auto-queue repo test.
func pgActRepo(t *testing.T) (*PostgresRoomActivityRepository, func()) {
	t.Helper()
	db, cleanup := NewRoomTestDB(t)
	return NewPostgresRoomActivityRepository(db), cleanup
}

// pgTime normalizes a time to what PostgreSQL TIMESTAMPTZ round-trips:
// UTC at microsecond precision.
func pgTime(y int, mo time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC)
}

// TestRoomActivityRepo_AddGet_RoundTrip pins the append+read contract: an
// activity written via AddActivity is read back verbatim by GetActivities.
func TestRoomActivityRepo_AddGet_RoundTrip(t *testing.T) {
	repo, cleanup := pgActRepo(t)
	defer cleanup()
	ctx := context.Background()
	roomID := seedRoom(t, repo.db, "ra-roundtrip")

	want := entity.Activity{
		Timestamp:   pgTime(2026, time.July, 24, 10, 0, 0),
		Type:        entity.ActivitySongAdded,
		User:        "alice",
		Description: "added a song",
	}
	if err := repo.AddActivity(ctx, roomID, want); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := repo.GetActivities(ctx, roomID, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(got))
	}
	a := got[0]
	if !a.Timestamp.Equal(want.Timestamp) {
		t.Errorf("timestamp: want %v, got %v", want.Timestamp, a.Timestamp)
	}
	if a.Type != want.Type {
		t.Errorf("type: want %q, got %q", want.Type, a.Type)
	}
	if a.User != want.User {
		t.Errorf("user: want %q, got %q", want.User, a.User)
	}
	if a.Description != want.Description {
		t.Errorf("description: want %q, got %q", want.Description, a.Description)
	}
}

// TestRoomActivityRepo_NewestFirst pins the ordering contract: activities
// with distinct timestamps come back newest first.
func TestRoomActivityRepo_NewestFirst(t *testing.T) {
	repo, cleanup := pgActRepo(t)
	defer cleanup()
	ctx := context.Background()
	roomID := seedRoom(t, repo.db, "ra-order")

	older := entity.Activity{
		Timestamp:   pgTime(2026, time.July, 24, 9, 0, 0),
		Type:        entity.ActivityUserJoined,
		User:        "bob",
		Description: "joined",
	}
	newer := entity.Activity{
		Timestamp:   pgTime(2026, time.July, 24, 11, 0, 0),
		Type:        entity.ActivitySongAdded,
		User:        "carol",
		Description: "added",
	}
	// Insert older first so insertion order != desired order.
	if err := repo.AddActivity(ctx, roomID, older); err != nil {
		t.Fatalf("add older: %v", err)
	}
	if err := repo.AddActivity(ctx, roomID, newer); err != nil {
		t.Fatalf("add newer: %v", err)
	}

	got, err := repo.GetActivities(ctx, roomID, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(got))
	}
	if !got[0].Timestamp.Equal(newer.Timestamp) {
		t.Errorf("expected newest first, got %v", got[0].Timestamp)
	}
	if !got[1].Timestamp.Equal(older.Timestamp) {
		t.Errorf("expected oldest last, got %v", got[1].Timestamp)
	}
}

// TestRoomActivityRepo_TieBreakByID pins the stable tail: when several
// activities share a timestamp, the later-inserted (higher id) row sorts
// first, giving a total, deterministic order.
func TestRoomActivityRepo_TieBreakByID(t *testing.T) {
	repo, cleanup := pgActRepo(t)
	defer cleanup()
	ctx := context.Background()
	roomID := seedRoom(t, repo.db, "ra-tie")

	ts := pgTime(2026, time.July, 24, 12, 0, 0)
	first := entity.Activity{Timestamp: ts, Type: entity.ActivityVoteCast, User: "u1", Description: "d1"}
	second := entity.Activity{Timestamp: ts, Type: entity.ActivityVoteCast, User: "u2", Description: "d2"}
	if err := repo.AddActivity(ctx, roomID, first); err != nil {
		t.Fatalf("add first: %v", err)
	}
	if err := repo.AddActivity(ctx, roomID, second); err != nil {
		t.Fatalf("add second: %v", err)
	}

	got, err := repo.GetActivities(ctx, roomID, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(got))
	}
	// Higher id (second insert) must come first on the timestamp tie.
	if got[0].Description != "d2" || got[1].Description != "d1" {
		t.Errorf("expected id-desc tie-break [d2,d1], got [%q,%q]", got[0].Description, got[1].Description)
	}
}

// TestRoomActivityRepo_LimitBounds pins that GetActivities returns at most
// limit rows, taking the newest.
func TestRoomActivityRepo_LimitBounds(t *testing.T) {
	repo, cleanup := pgActRepo(t)
	defer cleanup()
	ctx := context.Background()
	roomID := seedRoom(t, repo.db, "ra-limit")

	base := pgTime(2026, time.July, 24, 8, 0, 0)
	for i := 0; i < 5; i++ {
		a := entity.Activity{
			Timestamp:   base.Add(time.Duration(i) * time.Minute),
			Type:        entity.ActivitySongAdded,
			User:        "u",
			Description: "d",
		}
		if err := repo.AddActivity(ctx, roomID, a); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	got, err := repo.GetActivities(ctx, roomID, 3)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 (limit), got %d", len(got))
	}
	// Newest row is base+4m.
	if !got[0].Timestamp.Equal(base.Add(4 * time.Minute)) {
		t.Errorf("expected newest first at limit boundary, got %v", got[0].Timestamp)
	}
}

// TestRoomActivityRepo_NonPositiveLimit pins the short-circuit: limit <= 0
// returns an empty (non-nil) slice without touching the DB.
func TestRoomActivityRepo_NonPositiveLimit(t *testing.T) {
	repo, cleanup := pgActRepo(t)
	defer cleanup()
	ctx := context.Background()
	roomID := seedRoom(t, repo.db, "ra-zero")

	if err := repo.AddActivity(ctx, roomID, entity.Activity{
		Timestamp: pgTime(2026, time.July, 24, 8, 0, 0), Type: entity.ActivitySongAdded, User: "u", Description: "d",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	for _, limit := range []int{0, -1} {
		got, err := repo.GetActivities(ctx, roomID, limit)
		if err != nil {
			t.Fatalf("get(limit=%d): %v", limit, err)
		}
		if got == nil {
			t.Errorf("get(limit=%d): expected non-nil empty slice, got nil", limit)
		}
		if len(got) != 0 {
			t.Errorf("get(limit=%d): expected empty, got %d", limit, len(got))
		}
	}
}

// TestRoomActivityRepo_RoomScoped pins isolation: activities written to one
// room are never returned for another.
func TestRoomActivityRepo_RoomScoped(t *testing.T) {
	repo, cleanup := pgActRepo(t)
	defer cleanup()
	ctx := context.Background()
	roomA := seedRoom(t, repo.db, "ra-scope-a")
	roomB := seedRoom(t, repo.db, "ra-scope-b")

	if err := repo.AddActivity(ctx, roomA, entity.Activity{
		Timestamp: pgTime(2026, time.July, 24, 8, 0, 0), Type: entity.ActivitySongAdded, User: "a", Description: "in-a",
	}); err != nil {
		t.Fatalf("add a: %v", err)
	}

	got, err := repo.GetActivities(ctx, roomB, 10)
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected room B empty, got %d", len(got))
	}

	gotA, err := repo.GetActivities(ctx, roomA, 10)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if len(gotA) != 1 || gotA[0].Description != "in-a" {
		t.Fatalf("expected room A to own its single activity, got %+v", gotA)
	}
}
