package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"local-music-queue/internal/domain"
)

// RoomPlayHistoryCap mirrors the global PlayHistoryCap (50 rows). The
// 50-row cap is enforced inside AppendHistory via a follow-up DELETE
// per ADR 002 §13; per-room capping is the R09f addition. PostgreSQL
// triggers are deferred to a future operational sprint.
const RoomPlayHistoryCap = 50

// PostgresRoomAutoQueueRepository implements domain.RoomAutoQueueRepository
// against PostgreSQL. Schema: see 0007_room_auto_queue.up.sql.
//
// One row per room for config + N rows per room for history (capped).
// Implementations are expected to be safe for concurrent use; the
// interactor layer additionally serializes triggers under a coordinator
// mutex.
type PostgresRoomAutoQueueRepository struct {
	db *sql.DB
}

// NewPostgresRoomAutoQueueRepository wraps an existing *sql.DB. The DB
// MUST be migrated to schema version 7.
func NewPostgresRoomAutoQueueRepository(db *sql.DB) *PostgresRoomAutoQueueRepository {
	return &PostgresRoomAutoQueueRepository{db: db}
}

// GetConfig reads the per-room auto-queue config. A missing row
// resolves to the documented default (Enabled=false,
// Strategy=StrategyRelated) so callers never see a nil config and never
// observe sql.ErrNoRows.
func (r *PostgresRoomAutoQueueRepository) GetConfig(ctx context.Context, roomID int64) (*domain.RoomAutoQueueConfig, error) {
	const query = `SELECT enabled, strategy FROM room_auto_queue_config WHERE room_id = $1`
	var enabled bool
	var strategy string
	err := r.db.QueryRowContext(ctx, query, roomID).Scan(&enabled, &strategy)
	if err == sql.ErrNoRows {
		return &domain.RoomAutoQueueConfig{
			Enabled:  false,
			Strategy: domain.StrategyRelated,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get room auto-queue config: %w", err)
	}
	return &domain.RoomAutoQueueConfig{
		Enabled:  enabled,
		Strategy: domain.AutoQueueStrategy(strategy),
	}, nil
}

// SaveConfig upserts the per-room auto-queue config. UPDATE on
// conflict, INSERT otherwise — matches the global auto_queue_config
// shape from R02.
func (r *PostgresRoomAutoQueueRepository) SaveConfig(ctx context.Context, roomID int64, cfg domain.RoomAutoQueueConfig) error {
	const query = `
		INSERT INTO room_auto_queue_config (room_id, enabled, strategy, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (room_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			strategy = EXCLUDED.strategy,
			updated_at = CURRENT_TIMESTAMP
	`
	if _, err := r.db.ExecContext(ctx, query, roomID, cfg.Enabled, string(cfg.Strategy)); err != nil {
		return fmt.Errorf("failed to save room auto-queue config: %w", err)
	}
	return nil
}

// AppendHistory inserts a room_play_history row and enforces the
// per-room 50-row cap by deleting oldest excess rows in the SAME
// transaction (mirrors the global play_history cap from R01). Per-room
// capping is the R09f addition: the DELETE is scoped to roomID so it
// never touches rows belonging to other rooms.
func (r *PostgresRoomAutoQueueRepository) AppendHistory(ctx context.Context, roomID int64, entry domain.RoomPlayHistoryEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	const insertQuery = `
		INSERT INTO room_play_history (room_id, video_id, title, played_at)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := tx.ExecContext(ctx, insertQuery, roomID, entry.VideoID, entry.Title, entry.PlayedAt); err != nil {
		return fmt.Errorf("failed to append room play history: %w", err)
	}

	const capQuery = `
		DELETE FROM room_play_history
		WHERE room_id = $1
		  AND id NOT IN (
			SELECT id FROM room_play_history WHERE room_id = $1
			ORDER BY played_at DESC LIMIT $2
		  )
	`
	if _, err := tx.ExecContext(ctx, capQuery, roomID, RoomPlayHistoryCap); err != nil {
		return fmt.Errorf("failed to enforce room play history cap: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit room play history: %w", err)
	}
	return nil
}

// GetRecentHistory returns up to `limit` entries for roomID, newest
// first. The WHERE clause scopes to roomID so other rooms' history is
// never returned.
func (r *PostgresRoomAutoQueueRepository) GetRecentHistory(ctx context.Context, roomID int64, limit int) ([]domain.RoomPlayHistoryEntry, error) {
	const query = `
		SELECT video_id, title, played_at
		FROM room_play_history
		WHERE room_id = $1
		ORDER BY played_at DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, roomID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get room recent history: %w", err)
	}
	defer rows.Close()

	var entries []domain.RoomPlayHistoryEntry
	for rows.Next() {
		var entry domain.RoomPlayHistoryEntry
		var playedAt time.Time
		if err := rows.Scan(&entry.VideoID, &entry.Title, &playedAt); err != nil {
			return nil, fmt.Errorf("failed to scan room history entry: %w", err)
		}
		entry.PlayedAt = playedAt
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
