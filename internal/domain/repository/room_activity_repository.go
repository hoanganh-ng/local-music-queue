package repository

import (
	"context"

	"local-music-queue/internal/domain/entity"
)

// RoomActivityRepository persists the per-room activity feed. The shape
// mirrors the global activities table (a flat log of Timestamp / Type /
// User / Description rows) but is keyed by room_id so each room owns an
// independent feed.
//
// The R14b room-cutover mechanism writes directly to the room_activities
// table via its own SQL (preserving legacy activity ids), so this
// interface is intentionally narrow: it exposes only the append and
// bounded newest-first read the runtime layer needs. Runtime composition
// into cmd/server is deferred to R09i.
//
// Implementations are expected to be safe for concurrent use.
type RoomActivityRepository interface {
	// AddActivity appends a single activity to roomID's feed. The
	// row id is assigned by the database (BIGSERIAL); the entity's
	// Timestamp / Type / User / Description are stored verbatim.
	AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error

	// GetActivities returns up to limit activities for roomID, newest
	// first (ORDER BY timestamp DESC, id DESC so the ordering is total
	// and stable when timestamps collide). A non-positive limit returns
	// an empty slice without querying.
	GetActivities(ctx context.Context, roomID int64, limit int) ([]entity.Activity, error)
}
