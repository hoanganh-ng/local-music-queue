package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

// pgInter builds a real Interactor against a per-test PG schema, after
// seeding the user rows referenced by the room_members FK. Skips when the
// DB is unreachable.
func pgInter(t *testing.T) (*Interactor, func()) {
	t.Helper()
	db, cleanup := persistence.NewRoomTestDB(t)
	seedUsers(t, db)
	repo := persistence.NewPostgresRoomRepository(db)
	inter := NewInteractor(repo)
	return inter, cleanup
}

// seedUsers inserts a small set of users (1..5) the tests reference.
// CreateRoomAndHost enforces the room_members -> users FK.
func seedUsers(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []int{1, 2, 3, 5, 42, 100, 200, 201, 202, 203, 204, 999} {
		email := fmt.Sprintf("u%d@example.com", id)
		display := fmt.Sprintf("User%d", id)
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
			 VALUES ($1, $2, $3, 'guest', 0, $4, $4)
			 ON CONFLICT (id) DO NOTHING`,
			id, email, display, now,
		); err != nil {
			t.Fatalf("seed user %d: %v", id, err)
		}
	}
}

// envOrDefault reads LMQ_TEST_DATABASE_URL with a local-dev fallback.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
	member, err := inter.Repo().GetMember(ctx, room.ID, 42)
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
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleAdmin, time.Now()); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 3, entity.RoomRoleGuest, time.Now()); err != nil {
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
	got, _ := inter.Repo().GetMember(ctx, room.ID, 3)
	if got.Role != entity.RoomRoleAdmin {
		t.Errorf("expected admin, got %s", got.Role)
	}
	// Host demotes admin to guest.
	if err := inter.DemoteMember(ctx, room.ID, 1, 3, "guest"); err != nil {
		t.Fatalf("host demote: %v", err)
	}
	got, _ = inter.Repo().GetMember(ctx, room.ID, 3)
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
	if err := inter.Repo().ArchiveRoom(ctx, room.ID, time.Now()); err != nil {
		t.Fatalf("ArchiveRoom: %v", err)
	}

	// Add guest (archived room). Use the repo directly to bypass the
	// requireActiveRoom gate inside the interactor's member mutations.
	if err := inter.Repo().AddMember(ctx, room.ID, 5, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("seed guest: %v", err)
	}
	_, _, err = inter.CreateInvite(ctx, room.ID, 1, 0, time.Now().Add(7*24*time.Hour))
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
	plaintext2, _, err := inter.CreateInvite(ctx, room.ID, 1, 1 /*max 1*/, now.Add(7*24*time.Hour))
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

func TestMain(m *testing.M) {
	// Ensure the helper DSN env var is consistent for downstream persistence
	// packages that read LMQ_TEST_DATABASE_URL.
	_ = envOrDefault("LMQ_TEST_DATABASE_URL", "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable")
	os.Exit(m.Run())
}
