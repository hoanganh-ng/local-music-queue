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

// pgInterWithDB returns the interactor plus the underlying *sql.DB so
// sibling repositories (e.g. PlayerLease) can be constructed against the
// same per-test schema. Existing R04 tests use pgInter unchanged.
func pgInterWithDB(t *testing.T) (*Interactor, *sql.DB, func()) {
	t.Helper()
	db, cleanup := persistence.NewRoomTestDB(t)
	seedUsers(t, db)
	repo := persistence.NewPostgresRoomRepository(db)
	inter := NewInteractor(repo)
	return inter, db, cleanup
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
	if err := inter.PromoteMember(ctx, "lounge", 2 /*actor*/, 3 /*target*/, "guest" /*new role*/); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-host promote: expected ErrForbidden, got %v", err)
	}
	// Host promotes guest to admin.
	if err := inter.PromoteMember(ctx, "lounge", 1 /*actor=host*/, 3 /*target*/, "admin"); err != nil {
		t.Fatalf("host promote guest: %v", err)
	}
	got, _ := inter.Repo().GetMember(ctx, room.ID, 3)
	if got.Role != entity.RoomRoleAdmin {
		t.Errorf("expected admin, got %s", got.Role)
	}
	// Host demotes admin to guest.
	if err := inter.DemoteMember(ctx, "lounge", 1, 3, "guest"); err != nil {
		t.Fatalf("host demote: %v", err)
	}
	got, _ = inter.Repo().GetMember(ctx, room.ID, 3)
	if got.Role != entity.RoomRoleGuest {
		t.Errorf("expected guest, got %s", got.Role)
	}
	// Users cannot self-promote.
	if err := inter.PromoteMember(ctx, "lounge", 2, 2, "admin"); !errors.Is(err, ErrForbidden) {
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
	_, _, err = inter.CreateInvite(ctx, "lounge", 1, 0, time.Now().Add(7*24*time.Hour))
	if !errors.Is(err, ErrArchived) {
		t.Errorf("invite on archived room: expected ErrArchived, got %v", err)
	}
	_, err = inter.RedeemInvite(ctx, "anytoken", 999)
	if !errors.Is(err, ErrInviteInvalid) && !errors.Is(err, ErrArchived) {
		t.Errorf("redeem on archived: expected ErrInviteInvalid or ErrArchived, got %v", err)
	}
	if err := inter.PromoteMember(ctx, "lounge", 1, 5, "admin"); !errors.Is(err, ErrArchived) {
		t.Errorf("promote on archived: expected ErrArchived, got %v", err)
	}
	if err := inter.DemoteMember(ctx, "lounge", 1, 5, "guest"); !errors.Is(err, ErrArchived) {
		t.Errorf("demote on archived: expected ErrArchived, got %v", err)
	}
}

func TestRoom_InviteCreateRedeemRevokeExpiryMaxUses(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	plaintext, inv, err := inter.CreateInvite(ctx, "lounge", 1, 0 /*unlimited*/, now.Add(7*24*time.Hour))
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
	if _, err := inter.RevokeInvite(ctx, "lounge", 1 /*host*/, inv.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if _, err := inter.RedeemInvite(ctx, plaintext, 200); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("redeem after revoke: expected ErrInviteInvalid, got %v", err)
	}

	// Max-uses exhaust path.
	plaintext2, _, err := inter.CreateInvite(ctx, "lounge", 1, 1 /*max 1*/, now.Add(7*24*time.Hour))
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

	// Expired path: create invite that is technically valid for 1 second,
	// then advance the interactor's clock past expiry to redeem.
	plaintext3, inv3, err := inter.CreateInvite(ctx, "lounge", 1, 0, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateInvite expired: %v", err)
	}
	_ = inv3
	// Move the interactor clock forward past the expiry.
	inter.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	_, err = inter.RedeemInvite(ctx, plaintext3, 203)
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("expired redeem: expected ErrInviteInvalid, got %v", err)
	}
	// Restore real clock for subsequent assertions in this test (none remain).
	inter.SetClock(time.Now)

	// Unknown token.
	if _, err := inter.RedeemInvite(ctx, "definitely-not-a-real-token", 204); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("unknown token: expected ErrInviteInvalid, got %v", err)
	}
}

func TestRoom_InviteExpiryValidation(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Past expiry rejected.
	if _, _, err := inter.CreateInvite(ctx, "lounge", 1, 0, now.Add(-time.Hour)); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("past expiry: expected ErrInvalidSlug, got %v", err)
	}
	// >30 days rejected.
	if _, _, err := inter.CreateInvite(ctx, "lounge", 1, 0, now.Add(31*24*time.Hour)); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("over 30d expiry: expected ErrInvalidSlug, got %v", err)
	}
	// Negative max_uses rejected.
	if _, _, err := inter.CreateInvite(ctx, "lounge", 1, -1, now.Add(7*24*time.Hour)); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("negative max_uses: expected ErrInvalidSlug, got %v", err)
	}
	// max_uses=0 unlimited accepted; default 7d expiry applied.
	plaintext, inv, err := inter.CreateInvite(ctx, "lounge", 1, 0, time.Time{})
	if err != nil {
		t.Fatalf("default expiry invite: %v", err)
	}
	if inv.MaxUses != 0 {
		t.Errorf("expected max_uses=0, got %d", inv.MaxUses)
	}
	// 7-day default tolerance: expiry should be ~now+7d (allow 1 minute slack).
	expected := now.Add(DefaultInviteExpiry)
	diff := inv.ExpiresAt.Sub(expected)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("default expiry not ~now+7d: got %s expected %s (diff %s)", inv.ExpiresAt, expected, diff)
	}
	_ = plaintext
}

func TestRoom_InviteRoleChecks(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	// Seed admin and guest members.
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleAdmin, now); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 3, entity.RoomRoleGuest, now); err != nil {
		t.Fatalf("seed guest: %v", err)
	}

	// Non-member cannot create invite.
	if _, _, err := inter.CreateInvite(ctx, "lounge", 999, 1, now.Add(7*24*time.Hour)); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member create invite: expected ErrForbidden, got %v", err)
	}
	// Guest cannot create invite.
	if _, _, err := inter.CreateInvite(ctx, "lounge", 3, 1, now.Add(7*24*time.Hour)); !errors.Is(err, ErrForbidden) {
		t.Errorf("guest create invite: expected ErrForbidden, got %v", err)
	}
	// Admin can create invite.
	if _, _, err := inter.CreateInvite(ctx, "lounge", 2, 1, now.Add(7*24*time.Hour)); err != nil {
		t.Errorf("admin create invite: %v", err)
	}
	// Non-member cannot list invites.
	if _, err := inter.ListInvites(ctx, "lounge", 999); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member list invites: expected ErrForbidden, got %v", err)
	}
	// Guest cannot list invites.
	if _, err := inter.ListInvites(ctx, "lounge", 3); !errors.Is(err, ErrForbidden) {
		t.Errorf("guest list invites: expected ErrForbidden, got %v", err)
	}
	// Non-member cannot list members.
	if _, err := inter.ListMembers(ctx, "lounge", 999); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member list members: expected ErrForbidden, got %v", err)
	}
	// Member (any role) can list members.
	if _, err := inter.ListMembers(ctx, "lounge", 3); err != nil {
		t.Errorf("guest list members: %v", err)
	}
}

func TestRoom_GetRoomBySlug(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	got, err := inter.GetRoomBySlug(ctx, "lounge")
	if err != nil {
		t.Fatalf("GetRoomBySlug: %v", err)
	}
	if got.Slug != "lounge" {
		t.Errorf("expected slug lounge, got %s", got.Slug)
	}
	if _, err := inter.GetRoomBySlug(ctx, "missing"); !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("missing slug: expected ErrRoomNotFound, got %v", err)
	}
	if _, err := inter.GetRoomBySlug(ctx, "Bad"); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("bad slug: expected ErrInvalidSlug, got %v", err)
	}
}

func TestRoom_InviteRedeemAtomicity_MaxUses1_BlocksSecondUser(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	plaintext, _, err := inter.CreateInvite(ctx, "lounge", 1, 1, now.Add(7*24*time.Hour))
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	// Distinct user 201 redeems — succeeds.
	if _, err := inter.RedeemInvite(ctx, plaintext, 201); err != nil {
		t.Fatalf("RedeemInvite first: %v", err)
	}
	// Distinct user 202 redeems — must fail with ErrInviteExhausted.
	if _, err := inter.RedeemInvite(ctx, plaintext, 202); !errors.Is(err, ErrInviteExhausted) {
		t.Errorf("second distinct user redeem max_uses=1: expected ErrInviteExhausted, got %v", err)
	}
}

func TestRoom_InviteRedeemAtomicity_NoMemberWhenUseCountFails(t *testing.T) {
	// Prove the atomic path: when max_uses is already reached, the redeeming
	// user must NOT be inserted as a member (transaction rolls back).
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	_ = room
	plaintext, inv, err := inter.CreateInvite(ctx, "lounge", 1, 1, now.Add(7*24*time.Hour))
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	// First user redeems successfully.
	if _, err := inter.RedeemInvite(ctx, plaintext, 201); err != nil {
		t.Fatalf("RedeemInvite first: %v", err)
	}
	// Sanity check: invite use_count is now 1.
	gotInv, err := inter.Repo().GetInviteByID(ctx, room.ID, inv.ID)
	if err != nil {
		t.Fatalf("GetInviteByID: %v", err)
	}
	if gotInv.UseCount != 1 {
		t.Fatalf("expected use_count=1 after first redeem, got %d", gotInv.UseCount)
	}
	// Second distinct user attempts — must fail with ErrInviteExhausted.
	if _, err := inter.RedeemInvite(ctx, plaintext, 202); !errors.Is(err, ErrInviteExhausted) {
		t.Errorf("second redeem: expected ErrInviteExhausted, got %v", err)
	}
	// Confirm the member row was NOT inserted for user 202.
	if _, err := inter.Repo().GetMember(ctx, room.ID, 202); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected user 202 NOT to be a member after failed redeem, got err=%v", err)
	}
}

func TestMain(m *testing.M) {
	// Ensure the helper DSN env var is consistent for downstream persistence
	// packages that read LMQ_TEST_DATABASE_URL.
	_ = envOrDefault("LMQ_TEST_DATABASE_URL", "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable")
	os.Exit(m.Run())
}