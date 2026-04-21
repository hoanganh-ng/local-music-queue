package queue

import (
	"context"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/domain/service"
)

// Interactor handles queue-related business logic.
type Interactor struct {
	repo    repository.QueueRepository
	youtube service.YouTubeService
}

// NewInteractor creates a new Queue Interactor.
func NewInteractor(repo repository.QueueRepository, youtube service.YouTubeService) *Interactor {
	return &Interactor{
		repo:    repo,
		youtube: youtube,
	}
}

// AddSong fetches metadata for a URL and adds it to the queue.
func (i *Interactor) AddSong(ctx context.Context, url string, addedBy string) (*entity.Song, error) {
	song, err := i.youtube.FetchMetadata(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	song.AddedBy = addedBy

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
