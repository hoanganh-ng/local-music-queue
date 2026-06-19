package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"local-music-queue/internal/usecase/auth"
	"sync"
	"time"
)



type sessionData struct {
	userID    int
	expiresAt time.Time
}

// InMemoryStore implements the auth.SessionStore interface.
type InMemoryStore struct {
	mu       sync.Mutex
	sessions map[string]sessionData
	clock    auth.Clock
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore(clock auth.Clock) *InMemoryStore {
	return &InMemoryStore{
		sessions: make(map[string]sessionData),
		clock:    clock,
	}
}

// Create generates a cryptographically random token, registers it for the user ID, and returns the token and expiry time.
func (s *InMemoryStore) Create(ctx context.Context, userID int, ttl time.Duration) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Lazy prune expired entries during create
	s.pruneExpired()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token := base64.URLEncoding.EncodeToString(b)
	expiresAt := s.clock.Now().Add(ttl)

	s.sessions[token] = sessionData{
		userID:    userID,
		expiresAt: expiresAt,
	}

	return token, expiresAt, nil
}

// Resolve validates the token and returns the user ID if the session is valid and not expired.
func (s *InMemoryStore) Resolve(ctx context.Context, token string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, ok := s.sessions[token]

	// Lazy prune expired entries during resolve
	s.pruneExpired()

	if !ok {
		return 0, auth.ErrSessionInvalid
	}

	if !s.clock.Now().Before(data.expiresAt) {
		return 0, auth.ErrSessionExpired
	}

	return data.userID, nil
}

// pruneExpired removes all expired sessions from the map.
// This must be called while holding the lock.
func (s *InMemoryStore) pruneExpired() {
	now := s.clock.Now()
	for token, data := range s.sessions {
		if !now.Before(data.expiresAt) {
			delete(s.sessions, token)
		}
	}
}
