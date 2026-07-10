package repository

import (
	"context"
	"errors"
	"time"

	"local-music-queue/internal/domain/entity"
)

// ErrInviteExhausted is returned by RedeemInviteAtomic when the atomic
// increment would exceed max_uses. The repository layer enforces this
// guarantee at the SQL level via a conditional UPDATE.
var ErrInviteExhausted = errors.New("invite exhausted")

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

	// ArchiveRoomIfActive transitions an active room to archived and returns
	// whether the update affected a row. Used by player-lease expiry/explicit
	// archive paths that must remain idempotent (idempotent = room not
	// archived twice).
	ArchiveRoomIfActive(ctx context.Context, roomID int64, now time.Time) (bool, error)

	// EndActiveLease ends any active lease for the room (idempotent:
	// returns false when no active lease exists). Used by the host-driven
	// archive path to satisfy R10a's "archive ends active lease idempotently"
	// invariant without coupling ArchiveRoomIfActive to the lease repo.
	// Implemented as a single UPDATE against player_leases so concurrent
	// archive callers cannot race a stale ended_at.
	EndActiveLease(ctx context.Context, roomID int64, now time.Time) (leaseEnded bool, err error)

	// ArchiveRoomIfActiveAndEndLease is the R10b narrow atomic seam for
	// host archive + active lease consistency. It runs the archive
	// transition AND the lease-end inside a single DB transaction so a
	// concurrent lease claim cannot slip in between them. The lease
	// end is conditional on the room actually transitioning
	// active → archived; when the room is already archived
	// (transitioned=false), the lease is NOT mutated.
	//
	// Returns:
	//   - transitioned=true when the room was active and is now archived.
	//     leaseEnded=true when an active lease was ended in the same
	//     transaction.
	//   - transitioned=false when the room was already archived.
	//     leaseEnded is always false in this branch and the lease is
	//     NOT mutated (idempotent: a stale active lease on an
	//     already-archived room is left untouched).
	//
	// Concurrency: the archive transition uses a conditional UPDATE
	// (`WHERE id = $1 AND status = 'active'`) and the lease end runs
	// inside the same tx, so a concurrent lease Claim (which INSERTs
	// into player_leases) cannot observe an archived room with an
	// ended lease as its post-state: the tx either commits both
	// transitions together or commits neither.
	ArchiveRoomIfActiveAndEndLease(ctx context.Context, roomID int64, now time.Time) (transitioned bool, leaseEnded bool, err error)

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

	// RedeemInviteAtomic performs the member-insert + use-count-increment
	// inside a single transaction. The conditional UPDATE on room_invites
	// (`WHERE id = $1 AND (max_uses = 0 OR use_count < max_uses)`) prevents
	// concurrent redemptions from overrunning max_uses; if the row is
	// already at the limit, RowsAffected is 0 and the function returns
	// ErrInviteExhausted. The transaction is rolled back on any error, so
	// the member row never persists when the invite cannot be consumed.
	RedeemInviteAtomic(ctx context.Context, inviteID int64, roomID int64, userID int, role entity.RoomMemberRole, maxUses int, now time.Time) (*entity.RoomMember, error)

	// RemoveMemberAndEndLeaseAtomic deletes the (roomID, targetUserID)
	// membership row and, if the target user holds the active lease for
	// the room, ends the lease — both inside a single DB transaction so
	// a concurrent playback command cannot observe the post-membership-
	// delete state while the lease is still active. Returns:
	//   - memberRemoved=true if the membership row was actually deleted
	//     (false when no row matched — caller surfaces ErrMemberNotFound).
	//   - leaseEnded=true if the target held the active lease and it was
	//     ended in this call.
	// When memberRemoved=false, leaseEnded is always false and the lease
	// is NOT mutated (idempotent: a stale caller races another removal
	// and observes ErrMemberNotFound without an unintended lease mutation).
	RemoveMemberAndEndLeaseAtomic(ctx context.Context, roomID int64, targetUserID int, now time.Time) (memberRemoved bool, leaseEnded bool, err error)
}
