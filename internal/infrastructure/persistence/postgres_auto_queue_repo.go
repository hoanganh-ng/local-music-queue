package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"local-music-queue/internal/domain"
	"time"
)

// PlayHistoryCap mirrors the SQLite trg_play_history_cap 50-row cap.
// Per ADR 002 §13 the cap is enforced server-side after each insert because
// PostgreSQL triggers are deferred to a future operational sprint.
const PlayHistoryCap = 50

// PostgresAutoQueueRepository implements domain.AutoQueueRepository on PostgreSQL.
//
// The 50-row play_history cap is enforced inside AppendHistory via a
// follow-up DELETE. This is the documented replacement for the SQLite
// trg_play_history_cap trigger from R02 onward. Per-room capping is R06.
type PostgresAutoQueueRepository struct {
	db *sql.DB
}

// NewPostgresAutoQueueRepository creates a new PostgreSQL-backed auto-queue
// repository.
func NewPostgresAutoQueueRepository(db *sql.DB) *PostgresAutoQueueRepository {
	return &PostgresAutoQueueRepository{db: db}
}

// GetConfig retrieves the auto-queue configuration.
func (r *PostgresAutoQueueRepository) GetConfig(ctx context.Context) (*domain.AutoQueueConfig, error) {
	const query = `SELECT enabled, strategy FROM auto_queue_config WHERE id = 1`
	var enabled bool
	var strategy string
	err := r.db.QueryRowContext(ctx, query).Scan(&enabled, &strategy)
	if err == sql.ErrNoRows {
		return &domain.AutoQueueConfig{
			Enabled:  false,
			Strategy: domain.StrategyRelated,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get auto-queue config: %w", err)
	}
	return &domain.AutoQueueConfig{
		Enabled:  enabled,
		Strategy: domain.AutoQueueStrategy(strategy),
	}, nil
}

// SaveConfig persists the auto-queue configuration.
func (r *PostgresAutoQueueRepository) SaveConfig(ctx context.Context, cfg domain.AutoQueueConfig) error {
	const query = `
		INSERT INTO auto_queue_config (id, enabled, strategy)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			strategy = EXCLUDED.strategy
	`
	_, err := r.db.ExecContext(ctx, query, cfg.Enabled, string(cfg.Strategy))
	if err != nil {
		return fmt.Errorf("failed to save auto-queue config: %w", err)
	}
	return nil
}

// AppendHistory adds a song to the play history and enforces the 50-row cap.
func (r *PostgresAutoQueueRepository) AppendHistory(ctx context.Context, entry domain.PlayHistoryEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	const insertQuery = `
		INSERT INTO play_history (video_id, title, played_at)
		VALUES ($1, $2, $3)
	`
	if _, err := tx.ExecContext(ctx, insertQuery, entry.VideoID, entry.Title, entry.PlayedAt); err != nil {
		return fmt.Errorf("failed to append play history: %w", err)
	}

	const capQuery = `
		DELETE FROM play_history
		WHERE id NOT IN (
			SELECT id FROM play_history ORDER BY played_at DESC LIMIT $1
		)
	`
	if _, err := tx.ExecContext(ctx, capQuery, PlayHistoryCap); err != nil {
		return fmt.Errorf("failed to enforce play history cap: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit play history: %w", err)
	}
	return nil
}

// GetRecentHistory returns up to `limit` entries, newest first.
func (r *PostgresAutoQueueRepository) GetRecentHistory(ctx context.Context, limit int) ([]domain.PlayHistoryEntry, error) {
	const query = `
		SELECT video_id, title, played_at
		FROM play_history
		ORDER BY played_at DESC
		LIMIT $1
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent history: %w", err)
	}
	defer rows.Close()

	var entries []domain.PlayHistoryEntry
	for rows.Next() {
		var entry domain.PlayHistoryEntry
		var playedAt time.Time
		if err := rows.Scan(&entry.VideoID, &entry.Title, &playedAt); err != nil {
			return nil, fmt.Errorf("failed to scan history entry: %w", err)
		}
		entry.PlayedAt = playedAt
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}