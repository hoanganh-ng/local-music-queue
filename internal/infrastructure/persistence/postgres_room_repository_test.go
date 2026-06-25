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
		`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
		 VALUES ($1, 'h@example.com', 'Host', 'host', 0, $2, $2)`,
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
			`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
			 VALUES ($1, $2, 'U', 'guest', 0, $3, $3)`, id, "u"+string(rune('0'+id))+"@example.com", now,
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
	repo, db := newRoomRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Seed the creator user before CreateRoomAndHost (AddMember inserts host member FK).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
		 VALUES (1, 'h@example.com', 'Host', 'host', 0, $1, $1)`, now,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

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
	repo, db := newRoomRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Seed the creator user before CreateRoomAndHost (AddMember inserts host member FK).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
		 VALUES (1, 'h@example.com', 'Host', 'host', 0, $1, $1)`, now,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

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
			`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
			 VALUES ($1, $2, 'U', 'guest', 0, $3, $3)`, id, "u"+string(rune('0'+id))+"@example.com", now,
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
