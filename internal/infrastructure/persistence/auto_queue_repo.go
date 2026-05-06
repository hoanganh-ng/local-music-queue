package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"local-music-queue/internal/domain"
	"time"
)

// SQLiteAutoQueueRepository implements domain.AutoQueueRepository on SQLite.
type SQLiteAutoQueueRepository struct {
	db *sql.DB
}

// NewSQLiteAutoQueueRepository creates a new auto-queue repository.
func NewSQLiteAutoQueueRepository(db *sql.DB) *SQLiteAutoQueueRepository {
	return &SQLiteAutoQueueRepository{db: db}
}

// GetConfig retrieves the auto-queue configuration.
func (r *SQLiteAutoQueueRepository) GetConfig(ctx context.Context) (*domain.AutoQueueConfig, error) {
	var enabled int
	var strategy string

	err := r.db.QueryRowContext(ctx, "SELECT enabled, strategy FROM auto_queue_config WHERE id = 1").Scan(&enabled, &strategy)
	if err != nil {
		return nil, fmt.Errorf("failed to get auto-queue config: %w", err)
	}

	return &domain.AutoQueueConfig{
		Enabled:  enabled == 1,
		Strategy: domain.AutoQueueStrategy(strategy),
	}, nil
}

// SaveConfig persists the auto-queue configuration.
func (r *SQLiteAutoQueueRepository) SaveConfig(ctx context.Context, cfg domain.AutoQueueConfig) error {
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}

	query := "INSERT OR REPLACE INTO auto_queue_config (id, enabled, strategy) VALUES (1, ?, ?)"
	_, err := r.db.ExecContext(ctx, query, enabled, string(cfg.Strategy))
	if err != nil {
		return fmt.Errorf("failed to save auto-queue config: %w", err)
	}

	return nil
}

// AppendHistory adds a song to the play history.
func (r *SQLiteAutoQueueRepository) AppendHistory(ctx context.Context, entry domain.PlayHistoryEntry) error {
	query := "INSERT INTO play_history (video_id, title, played_at) VALUES (?, ?, ?)"
	_, err := r.db.ExecContext(ctx, query, entry.VideoID, entry.Title, entry.PlayedAt)
	if err != nil {
		return fmt.Errorf("failed to append play history: %w", err)
	}

	return nil
}

// GetRecentHistory returns up to `limit` entries, newest first.
func (r *SQLiteAutoQueueRepository) GetRecentHistory(ctx context.Context, limit int) ([]domain.PlayHistoryEntry, error) {
	query := "SELECT video_id, title, played_at FROM play_history ORDER BY played_at DESC LIMIT ?"
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

	return entries, nil
}
