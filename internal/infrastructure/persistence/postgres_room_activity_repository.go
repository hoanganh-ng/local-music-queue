package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
)

// PostgresRoomActivityRepository implements repository.RoomActivityRepository
// against PostgreSQL. Schema: see 0009_room_cutover_support.up.sql.
//
// One row per activity, keyed by room_id. The read path orders newest
// first with a stable (timestamp DESC, id DESC) tail so activities that
// share a timestamp still have a total order.
type PostgresRoomActivityRepository struct {
	db *sql.DB
}

// NewPostgresRoomActivityRepository wraps an existing *sql.DB. The DB MUST
// be migrated to schema version 9.
func NewPostgresRoomActivityRepository(db *sql.DB) *PostgresRoomActivityRepository {
	return &PostgresRoomActivityRepository{db: db}
}

// AddActivity appends a single activity to roomID's feed. The row id is
// assigned by the BIGSERIAL sequence; the entity's fields are stored
// verbatim.
func (r *PostgresRoomActivityRepository) AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error {
	const query = `
		INSERT INTO room_activities (room_id, "timestamp", type, "user", description)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := r.db.ExecContext(ctx, query,
		roomID,
		activity.Timestamp,
		string(activity.Type),
		activity.User,
		activity.Description,
	); err != nil {
		return fmt.Errorf("failed to add room activity: %w", err)
	}
	return nil
}

// GetActivities returns up to limit activities for roomID, newest first.
// A non-positive limit short-circuits to an empty slice without querying.
func (r *PostgresRoomActivityRepository) GetActivities(ctx context.Context, roomID int64, limit int) ([]entity.Activity, error) {
	if limit <= 0 {
		return []entity.Activity{}, nil
	}
	const query = `
		SELECT "timestamp", type, "user", description
		FROM room_activities
		WHERE room_id = $1
		ORDER BY "timestamp" DESC, id DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, roomID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get room activities: %w", err)
	}
	defer rows.Close()

	activities := make([]entity.Activity, 0, limit)
	for rows.Next() {
		var (
			ts          time.Time
			actType     string
			user        string
			description string
		)
		if err := rows.Scan(&ts, &actType, &user, &description); err != nil {
			return nil, fmt.Errorf("failed to scan room activity: %w", err)
		}
		activities = append(activities, entity.Activity{
			Timestamp:   ts,
			Type:        entity.ActivityType(actType),
			User:        user,
			Description: description,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return activities, nil
}
