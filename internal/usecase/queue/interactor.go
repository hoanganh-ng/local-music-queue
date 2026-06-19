package queue

import (
	"context"
	"errors"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/domain/service"
	"log"
	"sync"
)

// Interactor handles queue-related business logic.
var (
	ErrInvalidIndex     = errors.New("invalid song index")
	ErrNotSongOwner     = errors.New("user does not own the song")
	ErrCannotRemoveSong = errors.New("guest cannot remove current or already-played song")
)

type RemoveSongResult struct {
	RemovedIndex    int
	ResultingIndex  int
	ResultingStatus entity.PlaybackStatus
	Activity        entity.Activity
	RemovedSong     entity.Song
}

type Interactor struct {
	repo        repository.QueueRepository
	youtube     service.YouTubeService
	autoQueueUC AutoQueueTrigger
	mu          sync.RWMutex
}

// AutoQueueTrigger is the interface for triggering auto-queue checks.
type AutoQueueTrigger interface {
	CheckAndTrigger(ctx context.Context) error
}

// NewInteractor creates a new Queue Interactor.
func NewInteractor(repo repository.QueueRepository, youtube service.YouTubeService) *Interactor {
	return &Interactor{
		repo:    repo,
		youtube: youtube,
	}
}

// SetAutoQueueTrigger sets the auto-queue trigger (called after DI setup to avoid circular dependency).
func (i *Interactor) SetAutoQueueTrigger(trigger AutoQueueTrigger) {
	i.autoQueueUC = trigger
}

// AddSong adds a song to the queue. If metadata is provided (e.g. from a prior
// search result) it is used directly, skipping the yt-dlp metadata fetch.
func (i *Interactor) AddSong(ctx context.Context, url string, addedBy string, addedByID int, metadata *entity.SearchResult) (*entity.Song, error) {
	var song *entity.Song

	if metadata != nil {
		// Fast path: search result already contains everything we need.
		song = &entity.Song{
			ID:        metadata.ID,
			Title:     metadata.Title,
			Artist:    metadata.Artist,
			Duration:  metadata.Duration,
			Thumbnail: metadata.Thumbnail,
			URL:       metadata.URL,
			AddedBy:   addedBy,
			AddedByID: addedByID,
		}
	} else {
		// Slow path: bare URL — must fetch metadata via yt-dlp.
		fetched, err := i.youtube.FetchMetadata(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch metadata: %w", err)
		}
		fetched.AddedBy = addedBy
		fetched.AddedByID = addedByID
		song = fetched
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		queue = entity.NewQueue()
	}

	// Check for duplicate
	if queue.ContainsSong(song.ID) {
		return nil, entity.ErrSongAlreadyInQueue
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

// AddSongDirect adds a pre-built song to the queue without fetching metadata.
// Used by auto-queue and other internal systems that already have full song data.
func (i *Interactor) AddSongDirect(ctx context.Context, song *entity.Song) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		queue = entity.NewQueue()
	}

	// Check for duplicate
	if queue.ContainsSong(song.ID) {
		return entity.ErrSongAlreadyInQueue
	}

	queue.Add(*song)

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivitySongAdded, song.AddedBy, fmt.Sprintf("added \"%s\"", song.Title))
	_ = i.repo.AddActivity(ctx, activity)

	return nil
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

	// Trigger auto-queue check
	go func() {
		if i.autoQueueUC != nil {
			if err := i.autoQueueUC.CheckAndTrigger(context.Background()); err != nil {
				log.Printf("auto-queue: %v", err)
			}
		}
	}()

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
		if errors.Is(err, entity.ErrNoNextSong) {
			queue.Status = entity.StatusPaused
			if saveErr := i.repo.Save(ctx, queue); saveErr != nil {
				return fmt.Errorf("failed to save queue after reaching end: %w", saveErr)
			}

			// Log activity for queue ending
			activity := entity.NewActivity(entity.ActivityPlayback, "System", "queue finished playing")
			_ = i.repo.AddActivity(ctx, activity)

			// Trigger auto-queue check — the queue is at its last song,
			// so auto-queue should fire to add more songs for endless radio.
			go func() {
				if i.autoQueueUC != nil {
					if err := i.autoQueueUC.CheckAndTrigger(context.Background()); err != nil {
						log.Printf("auto-queue: %v", err)
					}
				}
			}()

			return nil
		}
		return err
	}

	err = i.repo.Save(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	activity := entity.NewActivity(entity.ActivityPlayback, "System", "song finished playing")
	_ = i.repo.AddActivity(ctx, activity)

	// Trigger auto-queue check
	go func() {
		if i.autoQueueUC != nil {
			if err := i.autoQueueUC.CheckAndTrigger(context.Background()); err != nil {
				log.Printf("auto-queue: %v", err)
			}
		}
	}()

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

// RemoveSong removes a song at the specified index, enforcing permissions.
func (i *Interactor) RemoveSong(ctx context.Context, user *entity.User, index int) (*RemoveSongResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.repo.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load queue: %w", err)
	}

	// 1. Validate index
	if index < 0 || index >= len(queue.Songs) {
		return nil, ErrInvalidIndex
	}

	// 2. Check actor role and ownership
	if user.Role != entity.RoleHost && user.Role != entity.RoleAdmin {
		if user.Role == entity.RoleGuest {
			// Guest:
			// - index must be greater than queue.CurrentIndex
			// - song.AddedByID must equal the guest's ID
			// - AddedByID must not be zero
			if index <= queue.CurrentIndex {
				return nil, ErrCannotRemoveSong
			}
			song := queue.Songs[index]
			if song.AddedByID == 0 || song.AddedByID != user.ID {
				return nil, ErrNotSongOwner
			}
		} else {
			// Unknown/unauthorized role
			return nil, ErrNotSongOwner
		}
	}

	// Capture removed-song information
	removedSong := queue.Songs[index]

	// Call the existing queue entity mutation
	err = queue.Remove(index)
	if err != nil {
		return nil, err
	}

	// Save queue
	err = i.repo.Save(ctx, queue)
	if err != nil {
		return nil, fmt.Errorf("failed to save queue: %w", err)
	}

	// Add successful activity
	activity := entity.NewActivity(entity.ActivityPlayback, user.DisplayName, fmt.Sprintf("removed \"%s\" from queue", removedSong.Title))
	_ = i.repo.AddActivity(ctx, activity)

	return &RemoveSongResult{
		RemovedIndex:    index,
		ResultingIndex:  queue.CurrentIndex,
		ResultingStatus: queue.Status,
		Activity:        activity,
		RemovedSong:     removedSong,
	}, nil
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

// ChangeVolume broadcasts a volume change event.
func (i *Interactor) ChangeVolume(ctx context.Context, direction string) error {
	if direction != "up" && direction != "down" {
		return fmt.Errorf("invalid direction: must be 'up' or 'down'")
	}

	return nil
}
