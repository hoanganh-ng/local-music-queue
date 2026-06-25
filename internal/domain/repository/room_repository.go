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
}
