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
