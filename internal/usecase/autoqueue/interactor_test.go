package autoqueue

import (
	"context"
	"errors"
	"fmt"
	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"sync"
	"testing"
	"time"
)

var ErrFetchFailed = errors.New("fetch failed")

// Mock implementations
type mockAutoQueueRepo struct {
	config  *domain.AutoQueueConfig
	history []domain.PlayHistoryEntry
}

func (m *mockAutoQueueRepo) GetConfig(ctx context.Context) (*domain.AutoQueueConfig, error) {
	return m.config, nil
}

func (m *mockAutoQueueRepo) SaveConfig(ctx context.Context, cfg domain.AutoQueueConfig) error {
	m.config = &cfg
	return nil
}

func (m *mockAutoQueueRepo) AppendHistory(ctx context.Context, entry domain.PlayHistoryEntry) error {
	m.history = append(m.history, entry)
	return nil
}

func (m *mockAutoQueueRepo) GetRecentHistory(ctx context.Context, limit int) ([]domain.PlayHistoryEntry, error) {
	result := m.history
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type mockQueueRepo struct {
	queue      *entity.Queue
	activities []entity.Activity
}

func (m *mockQueueRepo) Load(ctx context.Context) (*entity.Queue, error) {
	return m.queue, nil
}

func (m *mockQueueRepo) Save(ctx context.Context, queue *entity.Queue) error {
	m.queue = queue
	return nil
}

func (m *mockQueueRepo) AddActivity(ctx context.Context, activity entity.Activity) error {
	m.activities = append(m.activities, activity)
	return nil
}

func (m *mockQueueRepo) GetActivities(ctx context.Context, limit int) ([]entity.Activity, error) {
	return m.activities, nil
}

type mockFetcher struct {
	song *entity.Song
	err  error
}

func (m *mockFetcher) FetchRelated(ctx context.Context, videoID string, exclude []string) (*entity.Song, error) {
	return m.song, m.err
}

// testAddAutoQueueSong is the test stand-in for queue.AddAutoQueueSong. It
// applies the same staleness rules the real interactor enforces, returning
// ErrAutoQueueStale on any violation and otherwise mutating the shared
// mockQueueRepo to mirror the locked insertion.
func testAddAutoQueueSong(repo *mockQueueRepo) AddAutoQueueSongFunc {
	return func(ctx context.Context, song *entity.Song, expectedSourceSongID string) (*AddSongResult, error) {
		q := repo.queue
		if q == nil || q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
			return nil, ErrAutoQueueStale
		}
		if q.Songs[q.CurrentIndex].ID != expectedSourceSongID {
			return nil, ErrAutoQueueStale
		}
		if q.CurrentIndex != len(q.Songs)-1 {
			return nil, ErrAutoQueueStale
		}
		if q.ContainsSong(song.ID) {
			return nil, ErrAutoQueueStale
		}

		previousCurrentIndex := q.CurrentIndex
		q.Add(*song)
		advanced := q.CurrentIndex != previousCurrentIndex
		activity := entity.NewActivity(entity.ActivitySongAdded, song.AddedBy, "added")
		repo.activities = append(repo.activities, activity)

		var currentSong *entity.Song
		if q.CurrentIndex >= 0 && q.CurrentIndex < len(q.Songs) {
			s := q.Songs[q.CurrentIndex]
			currentSong = &s
		}
		return &AddSongResult{
			Song:                 *song,
			Position:             len(q.Songs) - 1,
			PreviousCurrentIndex: previousCurrentIndex,
			CurrentIndex:         q.CurrentIndex,
			CurrentSong:          currentSong,
			Status:               q.Status,
			Elapsed:              q.Elapsed,
			PlaybackAdvanced:     advanced,
			Activity:             activity,
		}, nil
	}
}

func TestCheckAndTrigger_DisabledConfig(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config: &domain.AutoQueueConfig{Enabled: false, Strategy: domain.StrategyRelated},
	}
	queueRepo := &mockQueueRepo{queue: entity.NewQueue()}
	fetcher := &mockFetcher{}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)

	err := interactor.CheckAndTrigger(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(queueRepo.queue.Songs) != 0 {
		t.Error("expected no songs added when disabled")
	}
}

func TestCheckAndTrigger_NotLastSong_Skips(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config: &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
	}
	queue := entity.NewQueue()
	queue.Add(entity.Song{ID: "song1", Title: "Song 1"})
	queue.Add(entity.Song{ID: "song2", Title: "Song 2"})
	queue.Add(entity.Song{ID: "song3", Title: "Song 3"})
	queue.CurrentIndex = 1
	queueRepo := &mockQueueRepo{queue: queue}
	fetcher := &mockFetcher{}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)

	err := interactor.CheckAndTrigger(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(queueRepo.queue.Songs) != 3 {
		t.Errorf("expected 3 songs (no add), got %d", len(queueRepo.queue.Songs))
	}
}

func TestCheckAndTrigger_LastSong_Triggers(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	queue := entity.NewQueue()
	queue.Add(entity.Song{ID: "song1", Title: "Song 1"})
	queue.Add(entity.Song{ID: "song2", Title: "Song 2"})
	queue.Add(entity.Song{ID: "song3", Title: "Song 3"})
	queue.CurrentIndex = 2
	queueRepo := &mockQueueRepo{queue: queue}
	fetcher := &mockFetcher{
		song: &entity.Song{
			ID:        "fetched_song",
			Title:     "Fetched Song",
			AddedBy:   entity.SystemUserID,
			AddedByID: 0,
		},
	}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	err := interactor.CheckAndTrigger(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(queueRepo.queue.Songs) != 4 {
		t.Fatalf("expected 4 songs after add, got %d", len(queueRepo.queue.Songs))
	}
	if queueRepo.queue.Songs[3].ID != "fetched_song" {
		t.Errorf("expected fetched_song, got %s", queueRepo.queue.Songs[3].ID)
	}
}

func TestCheckAndTrigger_FetcherSuccess(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	queue := entity.NewQueue()
	queue.Add(entity.Song{ID: "song1", Title: "Song 1"})
	queueRepo := &mockQueueRepo{queue: queue}
	fetcher := &mockFetcher{
		song: &entity.Song{
			ID:        "fetched_song",
			Title:     "Fetched Song",
			AddedBy:   entity.SystemUserID,
			AddedByID: 0,
		},
	}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	err := interactor.CheckAndTrigger(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(queueRepo.queue.Songs) != 2 {
		t.Fatalf("expected 2 songs after add, got %d", len(queueRepo.queue.Songs))
	}
	if queueRepo.queue.Songs[1].ID != "fetched_song" {
		t.Errorf("expected fetched_song, got %s", queueRepo.queue.Songs[1].ID)
	}
	if queueRepo.queue.Songs[1].AddedBy != entity.SystemUserID {
		t.Errorf("expected AddedBy=%s, got %s", entity.SystemUserID, queueRepo.queue.Songs[1].AddedBy)
	}

	if len(autoQueueRepo.history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(autoQueueRepo.history))
	}
	if len(queueRepo.activities) != 1 {
		t.Errorf("expected 1 activity, got %d", len(queueRepo.activities))
	}
}

func TestCheckAndTrigger_FetcherFailsFallbackSuccess(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config: &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{
			{VideoID: "history1", Title: "History Song 1", PlayedAt: time.Now()},
			{VideoID: "history2", Title: "History Song 2", PlayedAt: time.Now()},
			{VideoID: "history3", Title: "History Song 3", PlayedAt: time.Now()},
		},
	}
	queue := entity.NewQueue()
	queue.Add(entity.Song{ID: "song1", Title: "Song 1"})
	queueRepo := &mockQueueRepo{queue: queue}
	fetcher := &mockFetcher{err: ErrFetchFailed}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	err := interactor.CheckAndTrigger(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Fallback should pick from history
	if len(queueRepo.queue.Songs) != 2 {
		t.Fatalf("expected 2 songs after fallback, got %d", len(queueRepo.queue.Songs))
	}
	addedSong := queueRepo.queue.Songs[1]
	// Should be from history
	validIDs := map[string]bool{"history1": true, "history2": true, "history3": true}
	if !validIDs[addedSong.ID] {
		t.Errorf("expected song from history, got %s", addedSong.ID)
	}
	if addedSong.AddedBy != entity.SystemUserID {
		t.Errorf("expected AddedBy=%s, got %s", entity.SystemUserID, addedSong.AddedBy)
	}
}

func TestCheckAndTrigger_BothFail(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	queue := entity.NewQueue()
	queue.Add(entity.Song{ID: "song1", Title: "Song 1"})
	queueRepo := &mockQueueRepo{queue: queue}
	fetcher := &mockFetcher{err: ErrFetchFailed}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	err := interactor.CheckAndTrigger(context.Background())
	if err != nil {
		t.Fatalf("expected no error (silent fail), got %v", err)
	}

	if len(queueRepo.queue.Songs) != 1 {
		t.Errorf("expected 1 song (no add), got %d", len(queueRepo.queue.Songs))
	}
}

func TestCheckAndTrigger_Concurrency(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	queue := entity.NewQueue()
	queue.Add(entity.Song{ID: "song1", Title: "Song 1"})
	queueRepo := &mockQueueRepo{queue: queue}
	fetcher := &mockFetcher{
		song: &entity.Song{
			ID:        "fetched_song",
			Title:     "Fetched Song",
			AddedBy:   entity.SystemUserID,
			AddedByID: 0,
		},
	}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = interactor.CheckAndTrigger(context.Background())
		}()
	}
	wg.Wait()

	if len(queueRepo.queue.Songs) > 2 {
		t.Errorf("expected at most 2 songs (debounce), got %d", len(queueRepo.queue.Songs))
	}
}

func TestSetEnabled(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config: &domain.AutoQueueConfig{Enabled: false, Strategy: domain.StrategyRelated},
	}
	queueRepo := &mockQueueRepo{queue: entity.NewQueue()}
	fetcher := &mockFetcher{}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)

	err := interactor.SetEnabled(context.Background(), true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	cfg, _ := interactor.GetConfig(context.Background())
	if !cfg.Enabled {
		t.Error("expected Enabled=true after SetEnabled(true)")
	}
}

// TestCheckAndTrigger_DisabledMidFlight blocks the fetcher and verifies that
// a SetEnabled(false) call landing while FetchRelated is in flight prevents
// any queue insertion, history append, activity, or broadcast. The slow fetch
// itself is never held under the interactor's mutex.
func TestCheckAndTrigger_DisabledMidFlight(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source"})
	queueRepo := &mockQueueRepo{queue: q}

	fetcher := &blockingFetcher{
		song:    &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: entity.SystemUserID},
		release: make(chan struct{}),
		started: make(chan struct{}),
	}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	var broadcasts []string
	var broadcastMu sync.Mutex
	interactor.SetBroadcaster(func(eventType string, _ interface{}) {
		broadcastMu.Lock()
		defer broadcastMu.Unlock()
		broadcasts = append(broadcasts, eventType)
	})

	done := make(chan error, 1)
	go func() {
		done <- interactor.CheckAndTrigger(context.Background())
	}()

	<-fetcher.started

	// Disable auto-queue while the fetcher is blocked. SetEnabled now takes
	// the same mutex as the post-fetch revalidation, so the post-fetch
	// re-check will observe Enabled=false.
	if err := interactor.SetEnabled(context.Background(), false); err != nil {
		t.Fatalf("SetEnabled(false) failed: %v", err)
	}

	close(fetcher.release)
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Candidate must NOT be inserted.
	if got := len(queueRepo.queue.Songs); got != 1 {
		t.Errorf("queue mutated by disabled-mid-flight candidate: %d songs (want 1)", got)
	}
	for _, s := range queueRepo.queue.Songs {
		if s.ID == "candidate" {
			t.Errorf("disabled-mid-flight candidate %q inserted into queue", s.ID)
		}
	}
	// No history entry written.
	if len(autoQueueRepo.history) != 0 {
		t.Errorf("expected 0 history entries, got %d", len(autoQueueRepo.history))
	}
	// No queue activity recorded (the stand-in only writes activity on success).
	if len(queueRepo.activities) != 0 {
		t.Errorf("expected 0 activities, got %d", len(queueRepo.activities))
	}
	// No broadcast emitted.
	broadcastMu.Lock()
	defer broadcastMu.Unlock()
	for _, e := range broadcasts {
		if e == "auto_queue_added" {
			t.Errorf("auto_queue_added broadcast must not fire when disabled mid-flight")
		}
	}
}

// TestCheckAndTrigger_LoadFailurePropagates ensures that a load failure
// observed by the AddAutoQueueSong callback surfaces as a non-nil error from
// CheckAndTrigger. The error is NOT ErrAutoQueueStale (which is reserved for
// stale predicates against a successfully-loaded queue) — it is the wrapped
// operational error the callback returns.
func TestCheckAndTrigger_LoadFailurePropagates(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source"})
	queueRepo := &mockQueueRepo{queue: q}

	fetcher := &mockFetcher{
		song: &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: entity.SystemUserID},
	}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)

	// Inject a stand-in that simulates a repository load failure. The error
	// is a wrapped operational failure — NOT ErrAutoQueueStale — so we can
	// assert CheckAndTrigger surfaces it as such.
	loadErr := errors.New("db unavailable")
	interactor.SetAddAutoQueueSongFunc(func(ctx context.Context, song *entity.Song, expectedSourceSongID string) (*AddSongResult, error) {
		return nil, fmt.Errorf("failed to load queue for auto-queue revalidation: %w", loadErr)
	})

	var broadcasts []string
	var broadcastMu sync.Mutex
	interactor.SetBroadcaster(func(eventType string, _ interface{}) {
		broadcastMu.Lock()
		defer broadcastMu.Unlock()
		broadcasts = append(broadcasts, eventType)
	})

	err := interactor.CheckAndTrigger(context.Background())
	if err == nil {
		t.Fatal("expected error from CheckAndTrigger when load fails")
	}
	if errors.Is(err, ErrAutoQueueStale) {
		t.Errorf("load failure must not surface as ErrAutoQueueStale, got %v", err)
	}
	if !errors.Is(err, loadErr) {
		t.Errorf("expected wrapped load error to reach CheckAndTrigger, got %v", err)
	}
	if len(autoQueueRepo.history) != 0 {
		t.Errorf("expected 0 history entries, got %d", len(autoQueueRepo.history))
	}
	broadcastMu.Lock()
	defer broadcastMu.Unlock()
	for _, e := range broadcasts {
		if e == "auto_queue_added" {
			t.Errorf("auto_queue_added broadcast must not fire when load fails")
		}
	}
}

// --- Sprint 004 regressions for Issue #8 ---

// blockingFetcher pauses on Wait until released, so a test can simulate a
// concurrent queue mutation landing while the FetchRelated call is in flight.
type blockingFetcher struct {
	song    *entity.Song
	release chan struct{}
	started chan struct{}
}

func (b *blockingFetcher) FetchRelated(ctx context.Context, videoID string, exclude []string) (*entity.Song, error) {
	close(b.started)
	<-b.release
	return b.song, nil
}

// TestCheckAndTrigger_StaleDuringBlockedFetch covers Issue #8 case 3:
// while FetchRelated is blocked, the queue advances such that the source song
// is no longer current. The Sprint 004 contract requires the candidate be
// dropped atomically — no queue save, no play_history append, no activity,
// no auto_queue_added broadcast.
func TestCheckAndTrigger_StaleDuringBlockedFetch(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source"})
	queueRepo := &mockQueueRepo{queue: q}

	fetcher := &blockingFetcher{
		song:    &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: entity.SystemUserID},
		release: make(chan struct{}),
		started: make(chan struct{}),
	}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	var broadcasts []string
	var broadcastMu sync.Mutex
	interactor.SetBroadcaster(func(eventType string, _ interface{}) {
		broadcastMu.Lock()
		defer broadcastMu.Unlock()
		broadcasts = append(broadcasts, eventType)
	})

	done := make(chan error, 1)
	go func() {
		done <- interactor.CheckAndTrigger(context.Background())
	}()

	// Wait for fetcher to enter its blocked section.
	<-fetcher.started

	// Simulate a concurrent mutation: another song was manually added so the
	// source song is no longer last (and source is no longer the "tail" the
	// auto-queue interactor expected).
	queueRepo.queue.Add(entity.Song{ID: "manual-add", Title: "Manual"})

	// Release the fetcher; CheckAndTrigger will now reach AddAutoQueueSong,
	// which must reject the candidate as stale.
	close(fetcher.release)

	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Queue must still have exactly source + manual-add (2 songs); no candidate.
	if got := len(queueRepo.queue.Songs); got != 2 {
		t.Errorf("queue mutated by stale candidate: %d songs (want 2)", got)
	}
	for _, s := range queueRepo.queue.Songs {
		if s.ID == "candidate" {
			t.Errorf("stale candidate %q inserted into queue", s.ID)
		}
	}
	// No play_history entry written.
	if len(autoQueueRepo.history) != 0 {
		t.Errorf("expected 0 history entries on stale candidate, got %d", len(autoQueueRepo.history))
	}
	// No queue activity recorded (the stand-in only writes activity on success).
	if len(queueRepo.activities) != 0 {
		t.Errorf("expected 0 activities on stale candidate, got %d", len(queueRepo.activities))
	}
	// No broadcast emitted.
	broadcastMu.Lock()
	defer broadcastMu.Unlock()
	for _, e := range broadcasts {
		if e == "auto_queue_added" {
			t.Errorf("auto_queue_added broadcast must not fire for stale candidate")
		}
	}
}

// TestCheckAndTrigger_SuccessCarriesAuthoritativeBroadcast asserts the
// auto_queue_added broadcast payload carries the post-mutation snapshot
// (current_index, current_song, status, elapsed) per Sprint 004.
func TestCheckAndTrigger_SuccessCarriesAuthoritativeBroadcast(t *testing.T) {
	autoQueueRepo := &mockAutoQueueRepo{
		config:  &domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated},
		history: []domain.PlayHistoryEntry{},
	}
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source"})
	queueRepo := &mockQueueRepo{queue: q}
	fetcher := &mockFetcher{
		song: &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: entity.SystemUserID},
	}

	interactor := NewInteractor(autoQueueRepo, queueRepo, fetcher)
	interactor.SetAddAutoQueueSongFunc(testAddAutoQueueSong(queueRepo))

	var capturedType string
	var captured map[string]interface{}
	interactor.SetBroadcaster(func(eventType string, data interface{}) {
		capturedType = eventType
		if m, ok := data.(map[string]interface{}); ok {
			captured = m
		}
	})

	if err := interactor.CheckAndTrigger(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedType != "auto_queue_added" {
		t.Fatalf("expected auto_queue_added broadcast, got %q", capturedType)
	}
	if captured == nil {
		t.Fatal("expected non-nil payload")
	}
	for _, k := range []string{"song", "source_song_title", "activity", "current_index", "current_song", "status", "elapsed"} {
		if _, ok := captured[k]; !ok {
			t.Errorf("broadcast payload missing %q", k)
		}
	}
	if got, _ := captured["source_song_title"].(string); got != "Source" {
		t.Errorf("expected source_song_title 'Source', got %q", got)
	}
}
