package session

import (
	"context"
	"errors"
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
		t.Errorf("token looks too short: %s", token)
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

	// 4. Expired token is rejected.
	clock.now = clock.now.Add(11 * time.Minute)
	_, err = store.Resolve(ctx, token)
	if err == nil || (!errors.Is(err, ErrSessionExpired) && err.Error() != "session expired") {
		t.Errorf("expected expired error, got %v", err)
	}

	// 6. Unknown token is rejected.
	_, err = store.Resolve(ctx, "unknown-token")
	if err == nil || (!errors.Is(err, ErrSessionNotFound) && err.Error() != "session not found") {
		t.Errorf("expected not found error, got %v", err)
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

	// Concurrent creation
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, _, _ = store.Create(ctx, id, time.Hour)
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
