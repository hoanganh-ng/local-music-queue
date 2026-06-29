package entity

import (
	"errors"
	"time"
)

// PlayerLease represents an exclusive claim of a host's playback device
// against a single room. ADR 001 §6.
type PlayerLease struct {
	ID              int64      `json:"id"`
	RoomID          int64      `json:"room_id"`
	ClaimedByUserID int        `json:"claimed_by_user_id"`
	ClaimedAt       time.Time  `json:"claimed_at"`
	LastHeartbeatAt time.Time  `json:"last_heartbeat_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
}

// IsWithinGrace reports whether the lease is still renewable at now given
// a grace duration. Lease is renewable if now <= expires_at + grace.
func (l *PlayerLease) IsWithinGrace(now time.Time, grace time.Duration) bool {
	return !now.After(l.ExpiresAt.Add(grace))
}

// IsExpired reports whether the lease has been ended AND is past grace.
// Callers pass the grace duration they consider expired; the entity does
// not bake a magic number in.
func (l *PlayerLease) IsExpired(now time.Time, grace time.Duration) bool {
	if l.EndedAt == nil {
		return false
	}
	return !l.IsWithinGrace(now, grace)
}

// PlayerLeaseArchiveReason describes why a room was archived in connection
// with player-lease semantics. ADR 001 §10.
type PlayerLeaseArchiveReason string

const (
	PlayerLeaseExpired  PlayerLeaseArchiveReason = "player_lease_expired"
	PlayerLeaseHostLeft PlayerLeaseArchiveReason = "host_left"
	PlayerLeaseExplicit PlayerLeaseArchiveReason = "explicit"
)

// IsValid reports whether r is a recognized reason.
func (r PlayerLeaseArchiveReason) IsValid() bool {
	switch r {
	case PlayerLeaseExpired, PlayerLeaseHostLeft, PlayerLeaseExplicit:
		return true
	}
	return false
}

// ErrInvalidPlayerLease is reserved for entity-level validation failures.
var ErrInvalidPlayerLease = errors.New("invalid player lease")