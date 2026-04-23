package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"time"
)

// SQLiteUserRepository implements the UserRepository interface.
type SQLiteUserRepository struct {
	db *sql.DB
}

// NewSQLiteUserRepository creates a new SQLite user repository.
func NewSQLiteUserRepository(db *sql.DB) *SQLiteUserRepository {
	return &SQLiteUserRepository{db: db}
}

// CreateUser creates a new user in the database.
func (r *SQLiteUserRepository) CreateUser(ctx context.Context, user *entity.User) error {
	query := `
		INSERT INTO users (email, display_name, profile_picture, role, priority_balance, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	result, err := r.db.ExecContext(ctx, query,
		user.Email,
		user.DisplayName,
		user.ProfilePicture,
		user.Role,
		user.PriorityBalance,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get user ID: %w", err)
	}
	user.ID = int(id)

	return nil
}

// GetUserByEmail retrieves a user by email.
func (r *SQLiteUserRepository) GetUserByEmail(ctx context.Context, email string) (*entity.User, error) {
	query := `
		SELECT id, email, display_name, profile_picture, role, priority_balance, created_at, updated_at
		FROM users WHERE email = ?
	`
	var user entity.User
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
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

	return &user, nil
}

// GetUserByID retrieves a user by ID.
func (r *SQLiteUserRepository) GetUserByID(ctx context.Context, id int) (*entity.User, error) {
	query := `
		SELECT id, email, display_name, profile_picture, role, priority_balance, created_at, updated_at
		FROM users WHERE id = ?
	`
	var user entity.User
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
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

	return &user, nil
}

// UpdateUser updates an existing user.
func (r *SQLiteUserRepository) UpdateUser(ctx context.Context, user *entity.User) error {
	query := `
		UPDATE users
		SET display_name = ?, profile_picture = ?, role = ?, priority_balance = ?, updated_at = ?
		WHERE id = ?
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
func (r *SQLiteUserRepository) DecrementPriority(ctx context.Context, userID int) error {
	query := `UPDATE users SET priority_balance = priority_balance - 1 WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to decrement priority: %w", err)
	}
	return nil
}

// IncrementPriority increments the user's priority balance by 1.
func (r *SQLiteUserRepository) IncrementPriority(ctx context.Context, userID int) error {
	query := `UPDATE users SET priority_balance = priority_balance + 1 WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to increment priority: %w", err)
	}
	return nil
}

// RecordSession records a user session for a specific date.
func (r *SQLiteUserRepository) RecordSession(ctx context.Context, userID int, sessionDate time.Time) error {
	query := `
		INSERT INTO user_sessions (user_id, session_date, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?)
	`
	now := time.Now()
	dateOnly := sessionDate.Format("2006-01-02")
	_, err := r.db.ExecContext(ctx, query, userID, dateOnly, now, now)
	if err != nil {
		return fmt.Errorf("failed to record session: %w", err)
	}
	return nil
}

// GetLastSessionDate retrieves the last session date for a user.
func (r *SQLiteUserRepository) GetLastSessionDate(ctx context.Context, userID int) (*time.Time, error) {
	query := `
		SELECT session_date FROM user_sessions
		WHERE user_id = ?
		ORDER BY session_date DESC
		LIMIT 1
	`
	var dateStr string
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&dateStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last session date: %w", err)
	}

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse session date: %w", err)
	}

	return &date, nil
}

// LogPriorityTransaction logs a priority transaction.
func (r *SQLiteUserRepository) LogPriorityTransaction(ctx context.Context, userID int, songID, songTitle, txType string, amount, balanceAfter int) error {
	query := `
		INSERT INTO priority_transactions (user_id, song_id, song_title, transaction_type, amount, balance_after, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query, userID, songID, songTitle, txType, amount, balanceAfter, time.Now())
	if err != nil {
		return fmt.Errorf("failed to log priority transaction: %w", err)
	}
	return nil
}
