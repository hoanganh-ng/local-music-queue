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

// RoomMembersBroadcaster is implemented by the delivery layer to
// publish post-mutation per-room member-state envelopes. The use case
// invokes it after a successful RemoveMemberByHost so per-room WS
// subscribers observe the targeted member-removed and members-changed
// events without usecase/room importing delivery/ws. Nil-safe: the
// interactor skips the broadcast when the seam is unset.
//
// BroadcastRoomArchived emits the per-room room_archived envelope.
// Implementation stamps archivedAt itself to keep callers free of
// time.Time imports. reason is one of the existing PlayerLeaseArchiveReason
// sentinels OR the new R10b sentinel "host_archived".
//
// BroadcastRoomMemberRemoved delivers the room_member_removed envelope
// to the removed user's per-room WS connections BEFORE the hub closes
// them (the delivery layer is responsible for the close frame order).
// The targetUserID argument identifies which connection(s) to target.
//
// BroadcastRoomMembersChanged delivers the room_members_changed
// envelope to the remaining per-room clients after a successful
// removal. excludeUserID is the user id of the removed client whose
// connections are about to be closed; production wiring MUST carry
// targetUserID through this seam so the room_members_changed envelope
// does NOT reach the removed client (which is being torn down by the
// close-frame path). Pass 0 to deliver to all clients.
//
// CloseRemovedClient sends the close-frame (code 1008) to the
// removed user's per-room WS connections after the targeted envelope
// has been delivered.
type RoomMembersBroadcaster interface {
	BroadcastRoomArchived(roomSlug string, reason string)
	BroadcastRoomMemberRemoved(roomSlug string, targetUserID int, reason string)
	BroadcastRoomMembersChanged(roomSlug string, members []entity.RoomMember, excludeUserID int)
	CloseRemovedClient(roomSlug string, targetUserID int)
}

// Interactor owns the room, invite, and membership use cases.
type Interactor struct {
	repo      repository.RoomRepository
	now       func() time.Time
	membersBC RoomMembersBroadcaster // nil-safe; set via SetMembersBroadcaster
}

// NewInteractor constructs an Interactor. The clock defaults to time.Now;
// tests may swap it via SetClock.
func NewInteractor(repo repository.RoomRepository) *Interactor {
	return &Interactor{repo: repo, now: time.Now}
}

// SetMembersBroadcaster wires the broadcaster used by RemoveMemberByHost
// to fan out the targeted room_member_removed + room_members_changed
// envelopes. Optional — when unset the interactor still succeeds; it
// just doesn't broadcast.
func (i *Interactor) SetMembersBroadcaster(b RoomMembersBroadcaster) {
	i.membersBC = b
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

// ArchiveRoomByHost is the host-driven soft-archive path. It validates
// the slug, requires the actor to be the room host, then calls the
// idempotent ArchiveRoomIfActive. Returns:
//   - ErrInvalidSlug when the slug fails SlugPattern
//   - ErrRoomNotFound when no room exists for the slug
//   - ErrForbidden when the actor is not the host
//   - nil on success (regardless of whether the room was already
//     archived; the bool return distinguishes the transition).
// The boolean `archived` is true ONLY when this call transitioned
// active → archived. Callers (HTTP handler) use it to decide whether
// to broadcast the per-room room_archived event.
//
// R10a Decision 7 requires "archive ends any active lease idempotently".
// We end the active lease via repo.EndActiveLease BEFORE the
// ArchiveRoomIfActive call so a lease can never outlive its room.
// The lease end is idempotent (no-op when no active lease exists) so a
// re-call on an already-archived room stays idempotent and still
// returns (false, nil).
func (i *Interactor) ArchiveRoomByHost(ctx context.Context, slug string, actorUserID int) (archived bool, err error) {
	if !entity.IsValidSlug(slug) {
		return false, ErrInvalidSlug
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrRoomNotFound
		}
		return false, fmt.Errorf("get room: %w", err)
	}
	if err := i.requireHost(ctx, room.ID, actorUserID); err != nil {
		return false, err
	}
	// End any active lease for this room BEFORE the archive transition
	// so a lease can never outlive its room. Idempotent: returns
	// (false, nil) when no active lease exists.
	if _, err := i.repo.EndActiveLease(ctx, room.ID, i.now()); err != nil {
		return false, fmt.Errorf("end active lease: %w", err)
	}
	transitioned, err := i.repo.ArchiveRoomIfActive(ctx, room.ID, i.now())
	if err != nil {
		return false, fmt.Errorf("archive if active: %w", err)
	}
	return transitioned, nil
}

// RemoveMemberByHost is the host-driven member-removal path. The actor
// MUST be the room host. The target validation order is:
//   - ErrInvalidSlug when the slug fails SlugPattern
//   - ErrRoomNotFound when no room exists for the slug
//   - ErrForbidden when the actor is not the host
//   - ErrArchived when the room is archived (handler maps to 409)
//   - ErrHostCannotRemoveSelf when actor == target
//   - ErrCannotRemoveHost when the target is the room host
//   - ErrMemberNotFound when the target is not a member
// On success the membership row is deleted and the active lease (if
// held by the target) is ended in one DB transaction. Returns
// `leaseEnded=true` when the target held the lease.
//
// When the broadcaster seam is wired AND the member was actually
// removed, this method invokes:
//   - BroadcastRoomMemberRemoved(roomSlug, targetUserID, "host_removed")
//     (targeted to the removed user's per-room WS connections BEFORE
//     the hub closes them — the close order is the delivery layer's
//     responsibility)
//   - BroadcastRoomMembersChanged(roomSlug, post-mutation members)
//     (per-room fan-out to remaining clients).
//   - CloseRemovedClient(roomSlug, targetUserID) — closes the removed
//     user's per-room WS connections with code 1008 AFTER the targeted
//     room_member_removed envelope has been delivered. Folding this
//     into the interactor keeps the WS-side-effect responsibility
//     inside the usecase seam so HTTP handlers stay transport-only and
//     usecase tests can pin the targeted close-frame fan-out.
// The remaining members list is loaded AFTER the delete transaction
// commits so it reflects the post-mutation state.
func (i *Interactor) RemoveMemberByHost(ctx context.Context, slug string, actorUserID int, targetUserID int) (leaseEnded bool, err error) {
	if !entity.IsValidSlug(slug) {
		return false, ErrInvalidSlug
	}
	if actorUserID == targetUserID {
		return false, ErrHostCannotRemoveSelf
	}
	if targetUserID <= 0 {
		return false, fmt.Errorf("target user id: %w", ErrInvalidSlug)
	}
	room, err := i.repo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrRoomNotFound
		}
		return false, fmt.Errorf("get room: %w", err)
	}
	if err := i.requireHost(ctx, room.ID, actorUserID); err != nil {
		return false, err
	}
	if room.Status != entity.RoomStatusActive {
		return false, ErrArchived
	}
	// Look up the target to validate host-target rule + check membership
	// (the atomic delete uses a conditional DELETE so the lookup is
	// technically redundant for the "not a member" case, but doing it
	// here lets us emit the explicit ErrCannotRemoveHost sentinel
	// before paying the DELETE cost).
	target, err := i.repo.GetMember(ctx, room.ID, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrMemberNotFound
		}
		return false, fmt.Errorf("get target: %w", err)
	}
	if target.Role == entity.RoomRoleHost {
		return false, ErrCannotRemoveHost
	}

	// Atomic membership-delete + lease-end. The repo returns
	// memberRemoved=false when no row matched (e.g. concurrent remove
	// raced us); the caller maps that to ErrMemberNotFound / 404 so
	// exactly one concurrent remove wins.
	memberRemoved, leaseEnded, err := i.repo.RemoveMemberAndEndLeaseAtomic(ctx, room.ID, targetUserID, i.now())
	if err != nil {
		return false, fmt.Errorf("remove member atomic: %w", err)
	}
	if !memberRemoved {
		return false, ErrMemberNotFound
	}

	// Fan out post-mutation envelopes. The targeted room_member_removed
	// envelope is delivered BEFORE the delivery layer closes the removed
	// user's connections — the close order is the broadcaster's
	// contract.
	if i.membersBC != nil {
		i.membersBC.BroadcastRoomMemberRemoved(room.Slug, targetUserID, "host_removed")
		// Load the post-mutation member list for the remaining-clients
		// envelope. ListMembers filters by room_id so the deleted row
		// is gone.
		remaining, lerr := i.repo.ListMembers(ctx, room.ID)
		if lerr != nil {
			// Do not fail the remove on a list error — the membership
			// delete already committed. Log via fmt.Errorf wrapping is
			// not appropriate here; instead return nil and let the
			// caller observe the truncated envelope. We surface the
			// error in test runs by returning it when ListMembers
			// returns a real error.
			return leaseEnded, fmt.Errorf("list remaining members: %w", lerr)
		}
		i.membersBC.BroadcastRoomMembersChanged(room.Slug, remaining, targetUserID)
		// Close the removed user's per-room WS connections with code
		// 1008 AFTER the targeted room_member_removed envelope has been
		// delivered. The broadcaster is responsible for the
		// send-then-close order (see the *ws.RoomWSHub adapter).
		i.membersBC.CloseRemovedClient(room.Slug, targetUserID)
	}
	return leaseEnded, nil
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