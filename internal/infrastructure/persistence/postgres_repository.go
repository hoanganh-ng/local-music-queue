package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"time"
)

// PostgresRepository implements the QueueRepository interface on PostgreSQL.
//
// queue_state is preserved as a single-row JSON document, mirroring the
// existing SQLite invariant. activities, users, user_sessions,
// priority_transactions, auto_queue_config, and play_history are separate
// tables accessed through the repositories in this package.
//
// Per ADR 002 §10, the *sql.DB boundary is preserved.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository wraps an existing *sql.DB that has been connected to
// PostgreSQL. It does NOT run schema migrations; that is the responsibility
// of cmd/migrate-schema and the backend startup hook (see migrations_postgres.go).
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// DB returns the underlying database connection. Used by sibling
// repositories (user, auto-queue) so they share the same connection pool.
func (r *PostgresRepository) DB() *sql.DB {
	return r.db
}

// Save stores the entire queue state as a JSON blob.
func (r *PostgresRepository) Save(ctx context.Context, queue *entity.Queue) error {
	data, err := json.Marshal(queue)
	if err != nil {
		return fmt.Errorf("failed to marshal queue: %w", err)
	}

	const query = `
		INSERT INTO queue_state (id, data, updated_at)
		VALUES (1, $1, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			data = EXCLUDED.data,
			updated_at = CURRENT_TIMESTAMP
	`
	if _, err := r.db.ExecContext(ctx, query, string(data)); err != nil {
		return fmt.Errorf("failed to save queue state: %w", err)
	}
	return nil
}

// Load retrieves the queue state.
func (r *PostgresRepository) Load(ctx context.Context) (*entity.Queue, error) {
	const query = `SELECT data FROM queue_state WHERE id = 1`
	var data string
	err := r.db.QueryRowContext(ctx, query).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no queue state found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load queue state: %w", err)
	}

	var queue entity.Queue
	if err := json.Unmarshal([]byte(data), &queue); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queue: %w", err)
	}
	return &queue, nil
}

// AddActivity logs an activity.
func (r *PostgresRepository) AddActivity(ctx context.Context, activity entity.Activity) error {
	const query = `
		INSERT INTO activities ("timestamp", type, "user", description)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := r.db.ExecContext(ctx, query,
		activity.Timestamp,
		activity.Type,
		activity.User,
		activity.Description,
	); err != nil {
		return fmt.Errorf("failed to add activity: %w", err)
	}
	return nil
}

// GetActivities retrieves recent activities, newest first.
func (r *PostgresRepository) GetActivities(ctx context.Context, limit int) ([]entity.Activity, error) {
	const query = `
		SELECT "timestamp", type, "user", description
		FROM activities
		ORDER BY "timestamp" DESC
		LIMIT $1
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get activities: %w", err)
	}
	defer rows.Close()

	var activities []entity.Activity
	for rows.Next() {
		var a entity.Activity
		var ts time.Time
		if err := rows.Scan(&ts, &a.Type, &a.User, &a.Description); err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}
		a.Timestamp = ts
		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return activities, nil
}