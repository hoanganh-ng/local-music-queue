package queue

import (
	"context"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/domain/service"
	"sync"
	"time"
)

// Interactor handles queue-related business logic.
type Interactor struct {
	repo    repository.QueueRepository
	youtube service.YouTubeService
	mu      sync.RWMutex
}

// NewInteractor creates a new Queue Interactor.
func NewInteractor(repo repository.QueueRepository, youtube service.YouTubeService) *Interactor {
	return &Interactor{
		repo:    repo,
		youtube: youtube,
	}
}

// AddSong adds a song to the queue. If metadata is provided (e.g. from a prior
// search result) it is used directly, skipping the yt-dlp metadata fetch.
func (i *Interactor) AddSong(ctx context.Context, url string, addedBy string, metadata *entity.SearchResult) (*entity.Song, error) {
	var song *entity.Song

	if metadata != nil {
		// Fast path: search result already contains everything we need.
		song = &entity.Song{
			ID:        metadata.ID,
			Title:     metadata.Title,
			Artist:    metadata.Artist,
			Duration:  time.Duration(metadata.Duration) * time.Second,
			Thumbnail: metadata.Thumbnail,
			URL:       metadata.URL,
			AddedBy:   addedBy,
		}
	} else {
		// Slow path: bare URL — must fetch metadata via yt-dlp.
		fetched, err := i.youtube.FetchMetadata(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch metadata: %w", err)
		}
		fetched.AddedBy = addedBy
		song = fetched
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		queue = entity.NewQueue()
	}

	queue.Add(*song)

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return nil, fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivitySongAdded, addedBy, fmt.Sprintf("added \"%s\"", song.Title))
	_ = i.repo.AddActivity(ctx, activity)

	return song, nil
}

// SkipSong moves to the next song in the queue.
func (i *Interactor) SkipSong(ctx context.Context, requestedBy string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	err = queue.Next()
	if err != nil {
		return err
	}

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivitySongSkipped, requestedBy, "skipped the current song")
	_ = i.repo.AddActivity(ctx, activity)

	return nil
}

// GetState returns the current queue state.
func (i *Interactor) GetState(ctx context.Context) (*entity.Queue, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		queue = entity.NewQueue()
	}

	activities, err := i.repo.GetActivities(ctx, 50)
	if err == nil {
		queue.History = activities
	}

	return queue, nil
}

// SetStatus updates the playback status (Play/Pause).
func (i *Interactor) SetStatus(ctx context.Context, requestedBy string, status entity.PlaybackStatus) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if !queue.IsValidTransition(status) {
		return fmt.Errorf("invalid status transition to %s", string(status))
	}

	queue.Status = status
	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivityPlayback, requestedBy, fmt.Sprintf("changed status to %s", string(status)))
	_ = i.repo.AddActivity(ctx, activity)

	return nil
}

// SyncPlayback updates the elapsed time without generating an activity log.
func (i *Interactor) SyncPlayback(ctx context.Context, elapsed int) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	queue.Elapsed = elapsed
	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	return nil
}

// SongEnded is called when a song naturally finishes playing.
func (i *Interactor) SongEnded(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	err = queue.Next()
	if err != nil {
		return err
	}

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivityPlayback, "System", "song finished playing")
	_ = i.repo.AddActivity(ctx, activity)

	return nil
}

// PrevSong moves to the previous song in the queue.
func (i *Interactor) PrevSong(ctx context.Context, requestedBy string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	err = queue.Prev()
	if err != nil {
		return err
	}

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivityPlayback, requestedBy, "went to the previous song")
	_ = i.repo.AddActivity(ctx, activity)

	return nil
}

// RemoveSong removes a song at the specified index.
func (i *Interactor) RemoveSong(ctx context.Context, requestedBy string, index int) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if index < 0 || index >= len(queue.Songs) {
		return fmt.Errorf("invalid song index")
	}

	songTitle := queue.Songs[index].Title

	err = queue.Remove(index)
	if err != nil {
		return err
	}

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivityPlayback, requestedBy, fmt.Sprintf("removed \"%s\" from queue", songTitle))
	_ = i.repo.AddActivity(ctx, activity)

	return nil
}

// ClearQueue clears all upcoming songs.
func (i *Interactor) ClearQueue(ctx context.Context, requestedBy string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	queue.Clear()

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivityPlayback, requestedBy, "cleared the queue")
	_ = i.repo.AddActivity(ctx, activity)

	return nil
}

// SearchYouTube searches YouTube and returns search results.
func (i *Interactor) SearchYouTube(ctx context.Context, query string) ([]*entity.SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	results, err := i.youtube.SearchYouTube(ctx, query, 5)
	if err != nil {
		return nil, fmt.Errorf("failed to search YouTube: %w", err)
	}

	return results, nil
}

