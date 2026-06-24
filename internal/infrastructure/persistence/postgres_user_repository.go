package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"time"
)

// PostgresUserRepository implements the UserRepository interface on PostgreSQL.
//
// Uses BIGSERIAL for the users.id surrogate key, DATE for session_date, and
// TIMESTAMPTZ for the timestamps, per ADR 002 §10.
type PostgresUserRepository struct {
	db *sql.DB
}

// NewPostgresUserRepository creates a new PostgreSQL-backed user repository.
func NewPostgresUserRepository(db *sql.DB) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

// CreateUser creates a new user in the database.
func (r *PostgresUserRepository) CreateUser(ctx context.Context, user *entity.User) error {
	const query = `
		INSERT INTO users (email, display_name, profile_picture, role, priority_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`
	var id int64
	err := r.db.QueryRowContext(ctx, query,
		user.Email,
		user.DisplayName,
		user.ProfilePicture,
		user.Role,
		user.PriorityBalance,
		user.CreatedAt,
		user.UpdatedAt,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	user.ID = int(id)
	return nil
}

// GetUserByEmail retrieves a user by email.
func (r *PostgresUserRepository) GetUserByEmail(ctx context.Context, email string) (*entity.User, error) {
	const query = `
		SELECT id, email, display_name, profile_picture, role, priority_balance, created_at, updated_at
		FROM users WHERE email = $1
	`
	var user entity.User
	var id int64
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&id,
		&user.Email,
		&user.DisplayName,
		&user.ProfilePicture,
		&user.Role,
		&user.PriorityBalance,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	user.ID = int(id)
	return &user, nil
}

// GetUserByID retrieves a user by ID.
func (r *PostgresUserRepository) GetUserByID(ctx context.Context, id int) (*entity.User, error) {
	const query = `
		SELECT id, email, display_name, profile_picture, role, priority_balance, created_at, updated_at
		FROM users WHERE id = $1
	`
	var user entity.User
	var dbID int64
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&dbID,
		&user.Email,
		&user.DisplayName,
		&user.ProfilePicture,
		&user.Role,
		&user.PriorityBalance,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	user.ID = int(dbID)
	return &user, nil
}

// UpdateUser updates an existing user.
func (r *PostgresUserRepository) UpdateUser(ctx context.Context, user *entity.User) error {
	const query = `
		UPDATE users
		SET display_name = $1, profile_picture = $2, role = $3,
		    priority_balance = $4, updated_at = $5
		WHERE id = $6
	`
	_, err := r.db.ExecContext(ctx, query,
		user.DisplayName,
		user.ProfilePicture,
		user.Role,
		user.PriorityBalance,
		user.UpdatedAt,
		user.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

// DecrementPriority decrements the user's priority balance by 1.
func (r *PostgresUserRepository) DecrementPriority(ctx context.Context, userID int) error {
	const query = `UPDATE users SET priority_balance = priority_balance - 1 WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to decrement priority: %w", err)
	}
	return nil
}

// IncrementPriority increments the user's priority balance by 1.
func (r *PostgresUserRepository) IncrementPriority(ctx context.Context, userID int) error {
	const query = `UPDATE users SET priority_balance = priority_balance + 1 WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to increment priority: %w", err)
	}
	return nil
}

// RecordSession records a user session for a specific date.
func (r *PostgresUserRepository) RecordSession(ctx context.Context, userID int, sessionDate time.Time) error {
	const query = `
		INSERT INTO user_sessions (user_id, session_date, first_seen_at, last_seen_at)
		VALUES ($1, $2::date, $3, $4)
		ON CONFLICT (user_id, session_date) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at
	`
	now := time.Now()
	_, err := r.db.ExecContext(ctx, query, userID, sessionDate, now, now)
	if err != nil {
		return fmt.Errorf("failed to record session: %w", err)
	}
	return nil
}

// GetLastSessionDate retrieves the last session date for a user.
func (r *PostgresUserRepository) GetLastSessionDate(ctx context.Context, userID int) (*time.Time, error) {
	const query = `
		SELECT session_date
		FROM user_sessions
		WHERE user_id = $1
		ORDER BY session_date DESC
		LIMIT 1
	`
	var date time.Time
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&date)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last session date: %w", err)
	}
	return &date, nil
}

// LogPriorityTransaction logs a priority transaction.
func (r *PostgresUserRepository) LogPriorityTransaction(ctx context.Context, userID int, songID, songTitle, txType string, amount, balanceAfter int) error {
	const query = `
		INSERT INTO priority_transactions (user_id, song_id, song_title, transaction_type, amount, balance_after, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.ExecContext(ctx, query, userID, songID, songTitle, txType, amount, balanceAfter, time.Now())
	if err != nil {
		return fmt.Errorf("failed to log priority transaction: %w", err)
	}
	return nil
}