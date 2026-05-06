package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"time"

	_ "modernc.org/sqlite" // Using pure Go SQLite driver
)

// SQLiteRepository implements the QueueRepository interface.
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository creates and initializes a new SQLite repository.
func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	repo := &SQLiteRepository{db: db}
	if err := repo.init(); err != nil {
		return nil, err
	}

	return repo, nil
}

// DB returns the underlying database connection.
func (r *SQLiteRepository) DB() *sql.DB {
	return r.db
}

func (r *SQLiteRepository) init() error {
	query := `
	CREATE TABLE IF NOT EXISTS queue_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS activities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		type TEXT NOT NULL,
		user TEXT NOT NULL,
		description TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL,
		profile_picture TEXT,
		role TEXT NOT NULL,
		priority_balance INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS user_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		session_date DATE NOT NULL,
		first_seen_at DATETIME NOT NULL,
		last_seen_at DATETIME NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id),
		UNIQUE(user_id, session_date)
	);

	CREATE TABLE IF NOT EXISTS priority_transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		song_id TEXT NOT NULL,
		song_title TEXT NOT NULL,
		transaction_type TEXT NOT NULL,
		amount INTEGER NOT NULL,
		balance_after INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS auto_queue_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled INTEGER NOT NULL DEFAULT 0,
		strategy TEXT NOT NULL DEFAULT 'related'
	);
	INSERT OR IGNORE INTO auto_queue_config (id, enabled, strategy) VALUES (1, 0, 'related');

	CREATE TABLE IF NOT EXISTS play_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		video_id TEXT NOT NULL,
		title TEXT NOT NULL,
		played_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_play_history_played_at ON play_history(played_at DESC);

	CREATE TRIGGER IF NOT EXISTS trg_play_history_cap
	AFTER INSERT ON play_history
	BEGIN
		DELETE FROM play_history
		WHERE id NOT IN (
			SELECT id FROM play_history ORDER BY played_at DESC LIMIT 50
		);
	END;
	`
	_, err := r.db.Exec(query)
	return err
}

// Save stores the entire queue state as a JSON blob.
func (r *SQLiteRepository) Save(ctx context.Context, queue *entity.Queue) error {
	data, err := json.Marshal(queue)
	if err != nil {
		return fmt.Errorf("failed to marshal queue: %w", err)
	}

	query := `
	INSERT INTO queue_state (id, data, updated_at) VALUES (1, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(id) DO UPDATE SET data = excluded.data, updated_at = CURRENT_TIMESTAMP
	`
	_, err = r.db.ExecContext(ctx, query, string(data))
	return err
}

// Load retrieves the queue state.
func (r *SQLiteRepository) Load(ctx context.Context) (*entity.Queue, error) {
	var data string
	err := r.db.QueryRowContext(ctx, "SELECT data FROM queue_state WHERE id = 1").Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no queue state found")
	}
	if err != nil {
		return nil, err
	}

	var queue entity.Queue
	if err := json.Unmarshal([]byte(data), &queue); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queue: %w", err)
	}

	return &queue, nil
}

// AddActivity logs an activity.
func (r *SQLiteRepository) AddActivity(ctx context.Context, activity entity.Activity) error {
	query := "INSERT INTO activities (timestamp, type, user, description) VALUES (?, ?, ?, ?)"
	_, err := r.db.ExecContext(ctx, query, activity.Timestamp, activity.Type, activity.User, activity.Description)
	return err
}

// GetActivities retrieves recent activities.
func (r *SQLiteRepository) GetActivities(ctx context.Context, limit int) ([]entity.Activity, error) {
	query := "SELECT timestamp, type, user, description FROM activities ORDER BY timestamp DESC LIMIT ?"
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []entity.Activity
	for rows.Next() {
		var a entity.Activity
		var ts time.Time
		if err := rows.Scan(&ts, &a.Type, &a.User, &a.Description); err != nil {
			return nil, err
		}
		a.Timestamp = ts
		activities = append(activities, a)
	}

	return activities, nil
}
