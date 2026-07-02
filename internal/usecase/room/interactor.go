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

	// Player-lease sentinels (R06).
	ErrPlayerLeaseExists    = errors.New("player lease exists")
	ErrPlayerLeaseNotFound  = errors.New("player lease not found")
	ErrPlayerLeaseGone      = errors.New("player lease gone (past grace)")
	ErrPlayerLeaseForbidden = errors.New("forbidden")
	ErrNotLeaseHolder       = errors.New("not lease holder")

	// R10b — explicit sentinels for the host-removes-self and remove-host
	// cases, distinct from generic ErrForbidden so the HTTP layer maps them
	// to 400 with a stable body string (per R10a Decision 4).
	ErrHostCannotRemoveSelf = errors.New("host cannot remove self")
	ErrCannotRemoveHost     = errors.New("cannot remove host")
)

// DefaultInviteExpiry is the documented default invite lifetime (ADR 001 §7).
const DefaultInviteExpiry = 7 * 24 * time.Hour

// MaxInviteLifetime caps invite expiry override at 30 days from creation.
const MaxInviteLifetime = 30 * 24 * time.Hour

// DefaultLeaseDuration is the ADR 001 §6 lease length (60s).
const DefaultLeaseDuration = 60 * time.Second

// DefaultLeaseGrace is the ADR 001 §6 grace after expiry (30s).
const DefaultLeaseGrace = 30 * time.Second

// RoomArchivedEvent is emitted by the lease expiry sweeper and by the
// explicit release path. The ws transport converts this into the
// room_archived envelope.
type RoomArchivedEvent struct {
	RoomID     int64
	Reason     string // entity.PlayerLeaseArchiveReason serialized as string
	ArchivedAt time.Time
}

// RoomArchivedBroadcaster is implemented by the delivery layer to publish
// RoomArchivedEvents to subscribed clients. The usecase layer holds the
// interface (not the concrete *ws.Hub) so it can stay independent of
// delivery/ws while still fanning out archive notifications.
//
// Implementations are expected to be non-blocking from the caller's
// perspective. The explicit release path invokes BroadcastRoomArchived
// from the HTTP request goroutine (after the interactor returns). The
// sweep path invokes the equivalent Hub broadcast from goroutines that
// fan out from Hub.Run, so the hub loop never blocks on its own
// unbuffered broadcast channel.
type RoomArchivedBroadcaster interface {
	BroadcastRoomArchived(ev RoomArchivedEvent)
}

// PlaybackLeaseAuthorizer is the small seam used by R09a playback
// use cases (roomqueue.Interactor) to verify the caller is the active
// lease holder for the room before any direct playback mutation
// (status / sync / skip / ended). The interface lives in usecase/room
// so the roomqueue interactor can stay free of a concrete dependency
// on *PlayerLeaseInteractor; the concrete implementation lives in the
// same package (PlayerLeaseInteractor.RequireActiveLeaseHolder).
//
// The seam returns the existing player-lease sentinels
// (ErrPlayerLeaseNotFound, ErrNotLeaseHolder, ErrPlayerLeaseGone,
// ErrArchived) which the delivery layer maps to HTTP 404/403/410/409.
// This keeps authorization decisions identical to the heartbeat path
// and centralises the lease-holder rule in one place.
type PlaybackLeaseAuthorizer interface {
	RequireActiveLeaseHolder(ctx context.Context, slug string, actorUserID int) error
}

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

// GetRoomBySlug fetches a room by its slug (the external identifier per
// ADR 001 §6).
func (i *Interactor) GetRoomBySlug(ctx context.Context, slug string) (*entity.Room, error) {
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
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

// ListMembers returns members of an active room. Caller must be a member
// of the room (any role) per ADR 001 §7.
func (i *Interactor) ListMembers(ctx context.Context, slug string, actorUserID int) ([]entity.RoomMember, error) {
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	// Membership check: actor must be a member of the room.
	if _, err := i.repo.GetMember(ctx, room.ID, actorUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("get member: %w", err)
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}
	return i.repo.ListMembers(ctx, room.ID)
}

// CreateInvite validates the slug, rejects past or out-of-range expiry,
// rejects negative max_uses, requires actor to be host or admin member of
// the room, requires the room to be active, mints a random opaque token,
// persists only its hash, and returns the plaintext to the caller.
func (i *Interactor) CreateInvite(ctx context.Context, slug string, actorUserID int, maxUses int, expiresAt time.Time) (string, *entity.RoomInvite, error) {
	if !entity.IsValidSlug(slug) {
		return "", nil, ErrInvalidSlug
	}
	if maxUses < 0 {
		return "", nil, fmt.Errorf("max_uses: %w", ErrInvalidSlug)
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, ErrRoomNotFound
		}
		return "", nil, fmt.Errorf("get room: %w", err)
	}
	// Membership/role check: only host or admin members can mint invites.
	if err := i.requireHostOrAdmin(ctx, room.ID, actorUserID); err != nil {
		return "", nil, err
	}
	if room.Status != entity.RoomStatusActive {
		return "", nil, ErrArchived
	}
	now := i.now()
	if expiresAt.IsZero() {
		expiresAt = now.Add(DefaultInviteExpiry)
	}
	// Reject past expiry.
	if !expiresAt.After(now) {
		return "", nil, fmt.Errorf("expires_at must be in the future: %w", ErrInvalidSlug)
	}
	// Reject expiry more than MaxInviteLifetime from creation.
	if expiresAt.Sub(now) > MaxInviteLifetime {
		return "", nil, fmt.Errorf("expires_at must be within %s of creation: %w", MaxInviteLifetime, ErrInvalidSlug)
	}
	plaintext, err := generateInviteToken()
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	hash := hashInviteToken(plaintext)
	inv := &entity.RoomInvite{
		RoomID:    room.ID,
		TokenHash: hash,
		CreatedBy: actorUserID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		MaxUses:   maxUses,
		UseCount:  0,
	}
	if err := i.repo.CreateInvite(ctx, inv); err != nil {
		return "", nil, fmt.Errorf("persist invite: %w", err)
	}
	return plaintext, inv, nil
}

// ListInvites returns invites for a room. Host or admin only.
func (i *Interactor) ListInvites(ctx context.Context, slug string, actorUserID int) ([]entity.RoomInvite, error) {
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	if err := i.requireHostOrAdmin(ctx, room.ID, actorUserID); err != nil {
		return nil, err
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}
	return i.repo.ListInvites(ctx, room.ID)
}

// RevokeInvite marks an invite as revoked. Host or admin only.
func (i *Interactor) RevokeInvite(ctx context.Context, slug string, actorUserID int, inviteID int64) (*entity.RoomInvite, error) {
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	if err := i.requireHostOrAdmin(ctx, room.ID, actorUserID); err != nil {
		return nil, err
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}
	now := i.now()
	if err := i.repo.RevokeInvite(ctx, room.ID, inviteID, now); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInviteNotFound
		}
		return nil, fmt.Errorf("revoke: %w", err)
	}
	return i.repo.GetInviteByID(ctx, room.ID, inviteID)
}

// RedeemInvite validates the token hash, expiry, revocation, and use limit;
// inserts the user as guest if not already a member; returns the resulting
// membership. Idempotent for already-member users. All failure modes return
// ErrInviteInvalid (or ErrInviteExhausted) without leaking whether the token
// existed.
//
// Atomicity: the AddMember + IncrementInviteUseCount steps happen inside a
// single repository transaction guarded by a SQL-side check that rejects
// use_count beyond max_uses, preventing concurrent max_uses overrun.
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

	room, err := i.repo.GetRoomByID(ctx, inv.RoomID)
	if err != nil {
		return nil, ErrInviteInvalid
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}

	// Idempotent: existing member is returned untouched (no use_count bump).
	if existing, err := i.repo.GetMember(ctx, inv.RoomID, userID); err == nil {
		return existing, nil
	}

	// Atomic: AddMember + IncrementInviteUseCount in one transaction. The
	// SQL guard rejects an increment that would exceed max_uses so concurrent
	// redemptions cannot overrun. The member insert happens FIRST; if the
	// use-count update fails (e.g., max_uses already reached by a concurrent
	// redeemer), the transaction is rolled back and no member is left behind.
	member, err := i.repo.RedeemInviteAtomic(ctx, inv.ID, inv.RoomID, userID, entity.RoomRoleGuest, inv.MaxUses, now)
	if err != nil {
		if errors.Is(err, repository.ErrInviteExhausted) {
			return nil, ErrInviteExhausted
		}
		if errors.Is(err, sql.ErrNoRows) {
			// Invite was deleted/revoked between the read and the atomic op.
			return nil, ErrInviteInvalid
		}
		return nil, fmt.Errorf("redeem: %w", err)
	}
	return member, nil
}

// PromoteMember: host promotes target from guest to admin. The newRole arg
// is currently always "admin" (set by the handler from the route); it is
// re-validated here and never trusted from the request body.
func (i *Interactor) PromoteMember(ctx context.Context, slug string, actorUserID int, targetUserID int, newRole string) error {
	if !entity.IsValidSlug(slug) {
		return ErrInvalidSlug
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("get room: %w", err)
	}
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	if err := i.requireHost(ctx, room.ID, actorUserID); err != nil {
		return err
	}
	if room.Status != entity.RoomStatusActive {
		return ErrArchived
	}
	if newRole != "admin" {
		return fmt.Errorf("promote target must be admin: %w", ErrInvalidSlug)
	}
	target, err := i.repo.GetMember(ctx, room.ID, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("get target: %w", err)
	}
	if target.Role != entity.RoomRoleGuest {
		return fmt.Errorf("promote requires guest: %w", ErrInvalidSlug)
	}
	return i.repo.UpdateMemberRole(ctx, room.ID, targetUserID, entity.RoomRoleAdmin, i.now())
}

// DemoteMember: host demotes target from admin to guest.
func (i *Interactor) DemoteMember(ctx context.Context, slug string, actorUserID int, targetUserID int, newRole string) error {
	if !entity.IsValidSlug(slug) {
		return ErrInvalidSlug
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("get room: %w", err)
	}
	if actorUserID == targetUserID {
		return ErrForbidden
	}
	if err := i.requireHost(ctx, room.ID, actorUserID); err != nil {
		return err
	}
	if room.Status != entity.RoomStatusActive {
		return ErrArchived
	}
	if newRole != "guest" {
		return fmt.Errorf("demote target must be guest: %w", ErrInvalidSlug)
	}
	target, err := i.repo.GetMember(ctx, room.ID, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("get target: %w", err)
	}
	if target.Role != entity.RoomRoleAdmin {
		return fmt.Errorf("demote requires admin: %w", ErrInvalidSlug)
	}
	return i.repo.UpdateMemberRole(ctx, room.ID, targetUserID, entity.RoomRoleGuest, i.now())
}

// ArchiveRoom is the internal-only archive method for tests and R05
// readiness. No public archive endpoint is exposed in R04.
func (i *Interactor) ArchiveRoom(ctx context.Context, roomID int64) error {
	return i.repo.ArchiveRoom(ctx, roomID, i.now())
}

// --- helpers ---

// requireHostOrAdmin enforces the host/admin gate for invite creation,
// listing, and revocation.
func (i *Interactor) requireHostOrAdmin(ctx context.Context, roomID int64, actorUserID int) error {
	member, err := i.repo.GetMember(ctx, roomID, actorUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return fmt.Errorf("get member: %w", err)
	}
	if member.Role != entity.RoomRoleHost && member.Role != entity.RoomRoleAdmin {
		return ErrForbidden
	}
	return nil
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