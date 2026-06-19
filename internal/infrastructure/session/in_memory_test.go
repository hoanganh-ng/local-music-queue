package session

import (
	"context"
	"errors"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/auth"
	"sync"
	"testing"
	"time"
)

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	return c.now
}

func TestInMemoryStore_CreateAndResolve(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)}
	store := NewInMemoryStore(clock)
	ctx := context.Background()

	// 1. Token creation uses cryptographic source (URL-safe base64, length > 32)
	token, expiresAt, err := store.Create(ctx, 42, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if len(token) < 32 {
		t.Errorf("token looks too short: length %d", len(token))
	}
	if !expiresAt.Equal(clock.now.Add(10 * time.Minute)) {
		t.Errorf("expected expiry %v, got %v", clock.now.Add(10*time.Minute), expiresAt)
	}

	// 2. Token resolves to correct user ID.
	userID, err := store.Resolve(ctx, token)
	if err != nil {
		t.Fatalf("failed to resolve token: %v", err)
	}
	if userID != 42 {
		t.Errorf("expected user ID 42, got %d", userID)
	}

	// 4. Expired token is rejected (equal to expiresAt is treated as expired)
	clock.now = clock.now.Add(10 * time.Minute)
	_, err = store.Resolve(ctx, token)
	if err == nil || !errors.Is(err, auth.ErrSessionExpired) {
		t.Errorf("expected expired error at expiresAt boundary, got %v", err)
	}

	// 6. Unknown token is rejected.
	_, err = store.Resolve(ctx, "unknown-token")
	if err == nil || !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("expected invalid session error, got %v", err)
	}
}

func TestInMemoryStore_LazyPruning(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)}
	store := NewInMemoryStore(clock)
	ctx := context.Background()

	// Create three sessions
	t1, _, _ := store.Create(ctx, 1, 5*time.Minute)
	t2, _, _ := store.Create(ctx, 2, 15*time.Minute)

	// Advance clock so t1 is expired
	clock.now = clock.now.Add(10 * time.Minute)

	// Create another session t3, which should trigger lazy prune of t1
	t3, _, _ := store.Create(ctx, 3, 5*time.Minute)

	// Verify t1 is deleted (pruned)
	store.mu.Lock()
	_, exists1 := store.sessions[t1]
	_, exists2 := store.sessions[t2]
	_, exists3 := store.sessions[t3]
	store.mu.Unlock()

	if exists1 {
		t.Error("expected expired session t1 to be pruned")
	}
	if !exists2 {
		t.Error("expected active session t2 to be preserved")
	}
	if !exists3 {
		t.Error("expected new session t3 to exist")
	}
}

func TestInMemoryStore_Concurrency(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)}
	store := NewInMemoryStore(clock)
	ctx := context.Background()

	var wg sync.WaitGroup
	numWorkers := 100

	// Concurrent creation and resolution
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			token, _, err := store.Create(ctx, id, time.Hour)
			if err != nil {
				t.Errorf("failed to create session: %v", err)
				return
			}
			resolvedID, err := store.Resolve(ctx, token)
			if err != nil {
				t.Errorf("failed to resolve session: %v", err)
				return
			}
			if resolvedID != id {
				t.Errorf("expected user ID %d, got %d", id, resolvedID)
			}
		}(i)
	}
	wg.Wait()

	store.mu.Lock()
	size := len(store.sessions)
	store.mu.Unlock()

	if size != numWorkers {
		t.Errorf("expected %d sessions, got %d", numWorkers, size)
	}
}

type mockUserRepoForConcurrency struct {
	mu   sync.Mutex
	user *entity.User
}

func (m *mockUserRepoForConcurrency) CreateUser(ctx context.Context, user *entity.User) error {
	return nil
}

func (m *mockUserRepoForConcurrency) GetUserByEmail(ctx context.Context, email string) (*entity.User, error) {
	return m.user, nil
}

func (m *mockUserRepoForConcurrency) GetUserByID(ctx context.Context, id int) (*entity.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.user, nil
}

func (m *mockUserRepoForConcurrency) UpdateUser(ctx context.Context, user *entity.User) error {
	return nil
}

func (m *mockUserRepoForConcurrency) DecrementPriority(ctx context.Context, userID int) error {
	return nil
}

func (m *mockUserRepoForConcurrency) IncrementPriority(ctx context.Context, userID int) error {
	return nil
}

func (m *mockUserRepoForConcurrency) RecordSession(ctx context.Context, userID int, sessionDate time.Time) error {
	return nil
}

func (m *mockUserRepoForConcurrency) GetLastSessionDate(ctx context.Context, userID int) (*time.Time, error) {
	return nil, nil
}

func (m *mockUserRepoForConcurrency) LogPriorityTransaction(ctx context.Context, userID int, songID, songTitle, txType string, amount, balanceAfter int) error {
	return nil
}

func TestResolveSession_ConcreteConcurrency(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)}
	store := NewInMemoryStore(clock)

	user := &entity.User{
		ID:          42,
		Email:       "test@example.com",
		DisplayName: "Test User",
		Role:        entity.RoleGuest,
	}
	userRepo := &mockUserRepoForConcurrency{user: user}

	interactor := auth.NewInteractor(userRepo, "client-id", nil, nil, store, clock)
	ctx := context.Background()
	var wg sync.WaitGroup
	numWorkers := 100

	// Concurrently call CreateSession and ResolveSession
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			token, _, err := interactor.CreateSession(ctx, user.ID)
			if err != nil {
				t.Errorf("failed to create session: %v", err)
				return
			}
			resolved, err := interactor.ResolveSession(ctx, token)
			if err != nil {
				t.Errorf("failed to resolve session: %v", err)
				return
			}
			if resolved.ID != user.ID {
				t.Errorf("expected user ID %d, got %d", user.ID, resolved.ID)
			}
		}(i)
	}
	wg.Wait()
}
