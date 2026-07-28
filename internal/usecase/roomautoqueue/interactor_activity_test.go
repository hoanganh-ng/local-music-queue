package roomautoqueue

// R09i focused tests: the System song_added activity is appended only
// after a successful per-room auto-queue insertion, outside the
// coordinator mu, best-effort (a write failure never fails the
// trigger), and never on stale/dropped candidates.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
)

// captureActivityWriter records AddActivity calls; err (when set) is
// returned by every call. probe runs inside AddActivity (lock check).
type captureActivityWriter struct {
	mu      sync.Mutex
	roomIDs []int64
	acts    []entity.Activity
	err     error
	probe   func()
}

func (w *captureActivityWriter) AddActivity(_ context.Context, roomID int64, act entity.Activity) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.probe != nil {
		w.probe()
	}
	if w.err != nil {
		return w.err
	}
	w.roomIDs = append(w.roomIDs, roomID)
	w.acts = append(w.acts, act)
	return nil
}

func TestRoomAutoQueueActivity_SuccessfulInsertionAppendsSystemSongAdded(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "Candidate"}}
	snap := &queueStubSnapshot{}
	inter, _ := newTestInteractor(repo, fetcher, snap, "raq-act", 21)
	w := &captureActivityWriter{}
	inter.activityWriter = w

	if err := inter.CheckAndTrigger(context.Background(), "raq-act"); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.acts) != 1 {
		t.Fatalf("expected 1 activity, got %d: %+v", len(w.acts), w.acts)
	}
	a := w.acts[0]
	if a.Type != entity.ActivitySongAdded || a.User != "System" {
		t.Errorf("expected System song_added, got %+v", a)
	}
	if a.Description != `added "Candidate"` {
		t.Errorf("unexpected description %q", a.Description)
	}
	if w.roomIDs[0] != 21 {
		t.Errorf("expected room 21, got %d", w.roomIDs[0])
	}
}

func TestRoomAutoQueueActivity_StaleCandidateProducesNoActivity(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "Candidate"}}
	snap := &queueStubSnapshot{}
	inter, _ := newTestInteractor(repo, fetcher, snap, "raq-stale", 22)
	w := &captureActivityWriter{}
	inter.activityWriter = w
	// Force the queue-owned insertion to refuse the candidate.
	inter.SetAddRoomAutoQueueSongFunc(func(_ context.Context, _ string, _ *entity.Song, _ string) (*AddRoomAutoQueueSongResult, error) {
		return nil, ErrAutoQueueStale
	})

	if err := inter.CheckAndTrigger(context.Background(), "raq-stale"); err != nil {
		t.Fatalf("stale drop must not error: %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.acts) != 0 {
		t.Errorf("stale candidate must record no activity, got %+v", w.acts)
	}
}

func TestRoomAutoQueueActivity_WriteFailureDoesNotFailTrigger(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "Candidate"}}
	snap := &queueStubSnapshot{}
	inter, _ := newTestInteractor(repo, fetcher, snap, "raq-fail", 23)
	w := &captureActivityWriter{err: errors.New("simulated activity write failure")}
	inter.activityWriter = w

	if err := inter.CheckAndTrigger(context.Background(), "raq-fail"); err != nil {
		t.Fatalf("activity failure must not fail the trigger: %v", err)
	}
	// The insertion itself still landed (queue grew to 2).
	snap.mu.Lock()
	defer snap.mu.Unlock()
	if len(snap.current.Songs) != 2 {
		t.Errorf("expected insertion despite activity failure, queue=%+v", snap.current.Songs)
	}
}

func TestRoomAutoQueueActivity_WriteRunsOutsideCoordinatorMutex(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "Candidate"}}
	snap := &queueStubSnapshot{}
	inter, _ := newTestInteractor(repo, fetcher, snap, "raq-lock", 24)
	w := &captureActivityWriter{}
	var lockedDuringWrite bool
	w.probe = func() {
		if inter.mu.TryLock() {
			inter.mu.Unlock()
		} else {
			lockedDuringWrite = true
		}
	}
	inter.activityWriter = w

	if err := inter.CheckAndTrigger(context.Background(), "raq-lock"); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if lockedDuringWrite {
		t.Error("AddActivity ran while the coordinator mutex was held")
	}
}
