package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// PostgresRoomQueueRepository implements repository.RoomQueueRepository
// against PostgreSQL. Schema: see 0006_room_queue_state.up.sql.
//
// One row per room; the entire *entity.Queue is JSON-encoded and stored
// in the JSONB column. The interactor layer is responsible for mutex
// serialization and invariant enforcement; this layer is a thin JSON
// shim around the SQL boundary.
type PostgresRoomQueueRepository struct {
	db *sql.DB
}

// NewPostgresRoomQueueRepository wraps an existing *sql.DB. The DB MUST
// be migrated to schema version 6.
func NewPostgresRoomQueueRepository(db *sql.DB) *PostgresRoomQueueRepository {
	return &PostgresRoomQueueRepository{db: db}
}

// Load fetches the persisted queue for roomID. When no row exists,
// returns repository.ErrRoomQueueNotFound so the interactor can seed
// entity.NewQueue() without leaking a sql.ErrNoRows sentinel upward.
func (r *PostgresRoomQueueRepository) Load(ctx context.Context, roomID int64) (*entity.Queue, error) {
	var data string
	err := r.db.QueryRowContext(ctx,
		`SELECT data::text FROM room_queue_state WHERE room_id = $1`, roomID,
	).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrRoomQueueNotFound
		}
		return nil, fmt.Errorf("load room queue: %w", err)
	}
	var q entity.Queue
	if err := json.Unmarshal([]byte(data), &q); err != nil {
		return nil, fmt.Errorf("unmarshal room queue: %w", err)
	}
	return &q, nil
}

// Save upserts the queue JSON for roomID. The JSON includes Songs,
// CurrentIndex, Status, Elapsed, and History; we store the JSONB column
// as ::text via the parameterized query so the encoding lives in Go,
// not in SQL.
func (r *PostgresRoomQueueRepository) Save(ctx context.Context, roomID int64, queue *entity.Queue) error {
	if queue == nil {
		return fmt.Errorf("save room queue: nil queue")
	}
	data, err := json.Marshal(queue)
	if err != nil {
		return fmt.Errorf("marshal room queue: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO room_queue_state (room_id, data, updated_at)
		 VALUES ($1, $2::jsonb, CURRENT_TIMESTAMP)
		 ON CONFLICT (room_id) DO UPDATE SET
		   data = EXCLUDED.data,
		   updated_at = CURRENT_TIMESTAMP`,
		roomID, string(data),
	)
	if err != nil {
		return fmt.Errorf("save room queue: %w", err)
	}
	return nil
}
