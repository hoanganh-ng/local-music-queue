package autoqueue

import (
	"context"
	"errors"
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
