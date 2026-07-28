package persistence

import (
	"context"

	"local-music-queue/internal/domain/entity"
)

// NoopRoomActivityRepository implements repository.RoomActivityRepository
// as an explicit no-op activity writer.
//
// This is the intentional pre-R14c collision guard, NOT a production
// persistence implementation. Until R14c moves reads/writes fully onto
// room-scoped storage, runtime appends into room_activities would race
// the R14b cutover tooling, which preserves legacy activity ids while
// copying rows: a BIGSERIAL-assigned runtime row can collide with a
// legacy id the cutover is about to insert. Composing this no-op into
// roomqueue / roomvote / roomautoqueue keeps the runtime activity
// production paths real and testable without writing any rows.
//
// It performs no SQL, spawns no goroutines, and holds no mutable state,
// so it is trivially safe for concurrent use.
type NoopRoomActivityRepository struct{}

// NewNoopRoomActivityRepository returns the no-op activity writer.
func NewNoopRoomActivityRepository() *NoopRoomActivityRepository {
	return &NoopRoomActivityRepository{}
}

// AddActivity discards the activity and reports success.
func (r *NoopRoomActivityRepository) AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error {
	return nil
}

// GetActivities returns a non-nil empty slice and no error for any input.
func (r *NoopRoomActivityRepository) GetActivities(ctx context.Context, roomID int64, limit int) ([]entity.Activity, error) {
	return []entity.Activity{}, nil
}
