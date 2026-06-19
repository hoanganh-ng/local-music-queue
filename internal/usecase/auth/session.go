package auth

import (
	"context"
	"time"
)

// Clock defines an interface to retrieve the current time.
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using the standard time package.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time {
	return time.Now()
}

// SessionStore defines the interface for creating and resolving server-issued sessions.
type SessionStore interface {
	Create(ctx context.Context, userID int, ttl time.Duration) (string, time.Time, error)
	Resolve(ctx context.Context, token string) (int, error)
}
