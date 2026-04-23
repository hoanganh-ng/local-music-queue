package repository

import (
	"context"
	"local-music-queue/internal/domain/entity"
	"time"
)

// UserRepository defines the interface for user persistence operations.
type UserRepository interface {
	// User CRUD
	CreateUser(ctx context.Context, user *entity.User) error
	GetUserByEmail(ctx context.Context, email string) (*entity.User, error)
	GetUserByID(ctx context.Context, id int) (*entity.User, error)
	UpdateUser(ctx context.Context, user *entity.User) error

	// Priority management
	DecrementPriority(ctx context.Context, userID int) error
	IncrementPriority(ctx context.Context, userID int) error

	// Session tracking
	RecordSession(ctx context.Context, userID int, sessionDate time.Time) error
	GetLastSessionDate(ctx context.Context, userID int) (*time.Time, error)

	// Transaction logging
	LogPriorityTransaction(ctx context.Context, userID int, songID, songTitle, txType string, amount, balanceAfter int) error
}
