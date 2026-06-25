package room

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

// Sentinel errors mapped to HTTP status codes by the handler layer.
var (
	ErrInvalidSlug     = errors.New("invalid room slug")
	ErrReservedSlug    = errors.New("reserved room slug")
	ErrDuplicateSlug   = errors.New("duplicate room slug")
	ErrRoomNotFound    = errors.New("room not found")
	ErrMemberNotFound  = errors.New("member not found")
	ErrInviteNotFound  = errors.New("invite not found")
	ErrInviteInvalid   = errors.New("invite not found") // generic; does not leak existence
	ErrInviteExhausted = errors.New("invite exhausted")
	ErrArchived        = errors.New("room archived")
	ErrForbidden       = errors.New("forbidden")
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

// Repo returns the underlying RoomRepository. Exposed for tests that need
// to seed rows directly (e.g., AddMember of a pre-existing user).
func (i *Interactor) Repo() repository.RoomRepository { return i.repo }

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

// ListMembers returns members of an active room.
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

// ListInvites returns invites for a room. Host-only.
func (i *Interactor) ListInvites(ctx context.Context, roomID int64, actorUserID int) ([]entity.RoomInvite, error) {
	if err := i.requireHost(ctx, roomID, actorUserID); err != nil {
		return nil, err
	}
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return nil, err
	}
	return i.repo.ListInvites(ctx, roomID)
}

// RevokeInvite marks an invite as revoked. Host-only.
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
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	if err := i.requireHost(ctx, roomID, actorUserID); err != nil {
		return err
	}
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return err
	}
	if newRole != "admin" {
		return fmt.Errorf("promote target must be admin: %w", ErrInvalidSlug)
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
	return i.repo.UpdateMemberRole(ctx, roomID, targetUserID, entity.RoomRoleAdmin, i.now())
}

// DemoteMember: host demotes target from admin to guest.
func (i *Interactor) DemoteMember(ctx context.Context, roomID int64, actorUserID int, targetUserID int, newRole string) error {
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	if err := i.requireHost(ctx, roomID, actorUserID); err != nil {
		return err
	}
	if _, err := i.requireActiveRoom(ctx, roomID); err != nil {
		return err
	}
	if newRole != "guest" {
		return fmt.Errorf("demote target must be guest: %w", ErrInvalidSlug)
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
