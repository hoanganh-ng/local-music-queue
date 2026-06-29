package repository

import (
	"context"
	"errors"
	"time"

	"local-music-queue/internal/domain/entity"
)

// ErrPlayerLeaseExists is returned by Claim when a valid lease already
// exists for the room (within grace).
var ErrPlayerLeaseExists = errors.New("player lease exists")

// PlayerLeaseRepository persists PlayerLease rows. The implementation MUST
// enforce the partial unique index `idx_player_leases_one_active_per_room`
// so concurrent claims cannot produce two active leases for the same room.
type PlayerLeaseRepository interface {
	// Claim inserts a new lease and ends any prior expired (past grace) lease
	// in a single transaction. Returns ErrPlayerLeaseExists if a valid lease
	// (within grace) is already present.
	Claim(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error)

	// HeartbeatByHolder extends the lease's expires_at by leaseDuration when
	// called by the current holder. Returns sql.ErrNoRows if the lease does
	// not exist for the room, and a not-holder error if userID does not
	// match claimed_by_user_id.
	HeartbeatByHolder(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error)

	// ReleaseByHolder ends the lease and returns whether a row was updated.
	// If no lease exists, returns (false, nil).
	ReleaseByHolder(ctx context.Context, roomID int64, userID int, now time.Time) (bool, error)

	// EndLease marks the lease ended and returns the affected row. Used by
	// the expiry sweep and by explicit-release archive path. Returns
	// sql.ErrNoRows when no active lease exists.
	EndLease(ctx context.Context, roomID int64, now time.Time) (*entity.PlayerLease, error)

	// GetByRoom returns the active lease for a room, or sql.ErrNoRows.
	GetByRoom(ctx context.Context, roomID int64) (*entity.PlayerLease, error)

	// ListActive returns active (not ended, within grace) leases for the
	// sweeper. Ordered by expires_at ASC.
	ListActive(ctx context.Context, now time.Time) ([]entity.PlayerLease, error)
}