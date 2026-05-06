package autoqueue

import (
	"context"
	"fmt"
	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"log"
	"math/rand"
	"sync"
	"time"
)

// BroadcastFunc is a callback for broadcasting events (e.g. WebSocket).
// The usecase layer uses this abstraction to avoid importing the delivery layer.
type BroadcastFunc func(eventType string, data interface{})

// AddSongFunc is a callback for adding a song to the queue with proper locking.
type AddSongFunc func(ctx context.Context, song *entity.Song) error

// Interactor handles auto-queue business logic.
type Interactor struct {
	autoQueueRepo domain.AutoQueueRepository
	queueRepo     repository.QueueRepository
	fetcher       domain.RelatedSongFetcher
	broadcaster   BroadcastFunc
	addSongFunc   AddSongFunc
	mu            sync.Mutex
	triggering    bool
}

// NewInteractor creates a new AutoQueue Interactor.
func NewInteractor(
	autoQueueRepo domain.AutoQueueRepository,
	queueRepo repository.QueueRepository,
	fetcher domain.RelatedSongFetcher,
) *Interactor {
	return &Interactor{
		autoQueueRepo: autoQueueRepo,
		queueRepo:     queueRepo,
		fetcher:       fetcher,
	}
}

// SetBroadcaster sets the broadcast callback for emitting events.
func (i *Interactor) SetBroadcaster(fn BroadcastFunc) {
	i.broadcaster = fn
}

// SetAddSongFunc sets the callback for adding songs with proper locking.
func (i *Interactor) SetAddSongFunc(fn AddSongFunc) {
	i.addSongFunc = fn
}

// CheckAndTrigger checks if auto-queue should fire and adds a song if needed.
func (i *Interactor) CheckAndTrigger(ctx context.Context) error {
	i.mu.Lock()
	if i.triggering {
		i.mu.Unlock()
		return nil
	}
	i.triggering = true
	defer func() {
		i.mu.Lock()
		i.triggering = false
		i.mu.Unlock()
	}()
	i.mu.Unlock()

	cfg, err := i.autoQueueRepo.GetConfig(ctx)
	if err != nil {
		log.Printf("auto-queue: failed to get config: %v", err)
		return fmt.Errorf("failed to get config: %w", err)
	}

	if !cfg.Enabled {
		log.Printf("auto-queue: disabled, skipping trigger")
		return nil
	}

	queue, err := i.queueRepo.Load(ctx)
	if err != nil {
		log.Printf("auto-queue: failed to load queue: %v", err)
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if len(queue.Songs) == 0 {
		log.Printf("auto-queue: queue empty, skipping trigger")
		return nil
	}
	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		log.Printf("auto-queue: invalid current index %d for %d songs, skipping trigger", queue.CurrentIndex, len(queue.Songs))
		return nil
	}
	if queue.CurrentIndex != len(queue.Songs)-1 {
		log.Printf("auto-queue: current index %d is not last (%d), skipping trigger", queue.CurrentIndex, len(queue.Songs)-1)
		return nil
	}

	lastSong := queue.Songs[queue.CurrentIndex]

	recentHistory, err := i.autoQueueRepo.GetRecentHistory(ctx, 20)
	if err != nil {
		log.Printf("auto-queue: failed to get recent history: %v", err)
		recentHistory = []domain.PlayHistoryEntry{}
	}

	// Build exclude list for fetcher (recent history + current queue)
	excludeMap := make(map[string]bool)
	for _, entry := range recentHistory {
		excludeMap[entry.VideoID] = true
	}
	for _, song := range queue.Songs {
		excludeMap[song.ID] = true
	}

	var exclude []string
	for id := range excludeMap {
		exclude = append(exclude, id)
	}

	song, err := i.fetcher.FetchRelated(ctx, lastSong.ID, exclude)
	if err != nil {
		log.Printf("auto-queue: fetcher failed: %v, trying fallback", err)
		// For fallback, only exclude songs currently in queue (not history)
		queueOnlyExclude := make(map[string]bool)
		for _, s := range queue.Songs {
			queueOnlyExclude[s.ID] = true
		}
		song = i.fallbackFromHistory(ctx, queue, queueOnlyExclude)
	}

	if song == nil {
		log.Printf("auto-queue: no song available (fetcher and fallback both failed)")
		return nil
	}

	// Use the injected AddSongFunc if available (preferred for proper locking),
	// otherwise fall back to direct repo access (legacy path).
	if i.addSongFunc != nil {
		if err := i.addSongFunc(ctx, song); err != nil {
			return fmt.Errorf("failed to add song via callback: %w", err)
		}
	} else {
		// Legacy path: direct repo access (has race condition risk)
		queue.Add(*song)
		if err := i.queueRepo.Save(ctx, queue); err != nil {
			return fmt.Errorf("failed to save queue: %w", err)
		}

		activity := entity.NewActivity(entity.ActivitySongAdded, "Auto-Queue", fmt.Sprintf("added \"%s\"", song.Title))
		_ = i.queueRepo.AddActivity(ctx, activity)
	}

	historyEntry := domain.PlayHistoryEntry{
		VideoID:  lastSong.ID,
		Title:    lastSong.Title,
		PlayedAt: time.Now(),
	}
	if err := i.autoQueueRepo.AppendHistory(ctx, historyEntry); err != nil {
		log.Printf("auto-queue: failed to append history: %v", err)
	}

	// Broadcast auto-queue event to connected clients
	if i.broadcaster != nil {
		activity := entity.NewActivity(entity.ActivitySongAdded, "Auto-Queue", fmt.Sprintf("added \"%s\"", song.Title))
		i.broadcaster("auto_queue_added", map[string]interface{}{
			"song":              *song,
			"source_song_title": lastSong.Title,
			"activity":          activity,
		})
	}

	return nil
}

// fallbackFromHistory picks a random song from history that's not in excludeMap.
func (i *Interactor) fallbackFromHistory(ctx context.Context, queue *entity.Queue, excludeMap map[string]bool) *entity.Song {
	history, err := i.autoQueueRepo.GetRecentHistory(ctx, 50)
	if err != nil || len(history) == 0 {
		return nil
	}

	var candidates []domain.PlayHistoryEntry
	for _, entry := range history {
		if !excludeMap[entry.VideoID] {
			candidates = append(candidates, entry)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	chosen := candidates[rand.Intn(len(candidates))]

	return &entity.Song{
		ID:        chosen.VideoID,
		Title:     chosen.Title,
		Artist:    "",
		Duration:  0,
		Thumbnail: fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", chosen.VideoID),
		AddedBy:   entity.SystemUserID,
		AddedByID: 0,
		URL:       fmt.Sprintf("https://www.youtube.com/watch?v=%s", chosen.VideoID),
	}
}

// GetConfig returns the current auto-queue configuration.
func (i *Interactor) GetConfig(ctx context.Context) (*domain.AutoQueueConfig, error) {
	return i.autoQueueRepo.GetConfig(ctx)
}

// SetEnabled updates the enabled flag for auto-queue.
func (i *Interactor) SetEnabled(ctx context.Context, enabled bool) error {
	cfg, err := i.autoQueueRepo.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	cfg.Enabled = enabled
	if err := i.autoQueueRepo.SaveConfig(ctx, *cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}
