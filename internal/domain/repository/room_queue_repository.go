package repository

import (
	"context"
	"errors"

	"local-music-queue/internal/domain/entity"
)

// ErrRoomQueueNotFound is returned by Load when no queue row exists for
// the given room. Callers should treat it as "empty queue" and seed
// entity.NewQueue(); the persistence layer never returns nil.
var ErrRoomQueueNotFound = errors.New("room queue state not found")

// RoomQueueRepository persists one queue JSON document per room. The
// shape mirrors the existing global queue_state (single JSON blob keyed
// by room_id) so the per-room Queue invariant (Songs / CurrentIndex /
// Status / Elapsed) is preserved end-to-end.
//
// Implementations are expected to be safe for concurrent use; the
// interactor layer additionally serializes mutations under a mutex.
type RoomQueueRepository interface {
	// Load returns the persisted queue for roomID. When no row exists,
	// returns ErrRoomQueueNotFound so the interactor can decide whether
	// to seed an empty queue or surface "no queue" to the caller.
	Load(ctx context.Context, roomID int64) (*entity.Queue, error)

	// Save upserts the queue JSON for roomID. The JSON is the entire
	// serialized *entity.Queue, including Songs, CurrentIndex, Status,
	// Elapsed, and History.
	Save(ctx context.Context, roomID int64, queue *entity.Queue) error
}
