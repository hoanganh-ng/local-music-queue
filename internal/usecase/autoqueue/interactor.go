package autoqueue

import (
	"context"
	"errors"
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

// AddAutoQueueSongFunc atomically revalidates an auto-queue candidate and
// inserts it under the queue lock. Returns ErrAutoQueueStale (defined by the
// queue interactor) when the source song is no longer current or an upcoming
// song now exists. The Result carries the authoritative post-mutation snapshot
// (current_index, current_song, status, elapsed, activity) so the broadcaster
// can publish it without a follow-up state reload.
type AddAutoQueueSongFunc func(ctx context.Context, song *entity.Song, expectedSourceSongID string) (*AddSongResult, error)

// AddSongResult mirrors queue.AddSongResult shape. Re-declared here as a small
// transport struct so the auto-queue package does not import usecase/queue
// (avoiding an upward dependency from a leaf usecase).
type AddSongResult struct {
	Song                 entity.Song
	Position             int
	PreviousCurrentIndex int
	CurrentIndex         int
	CurrentSong          *entity.Song
	Status               entity.PlaybackStatus
	Elapsed              int
	PlaybackAdvanced     bool
	Activity             entity.Activity
}

// ErrAutoQueueStale is the sentinel returned by AddAutoQueueSongFunc when the
// candidate is no longer valid. The wiring layer adapts queue.ErrAutoQueueStale
// to this sentinel so the autoqueue package stays independent of usecase/queue.
var ErrAutoQueueStale = errors.New("auto-queue candidate is stale")

// Interactor handles auto-queue business logic.
type Interactor struct {
	autoQueueRepo    domain.AutoQueueRepository
	queueRepo        repository.QueueRepository
	fetcher          domain.RelatedSongFetcher
	broadcaster      BroadcastFunc
	addAutoQueueSong AddAutoQueueSongFunc
	// mu serializes the single-flight `triggering` flag, the pre-fetch and
	// post-fetch `cfg.Enabled` checks, AND the queue-owned conditional
	// insertion (`addAutoQueueSong`) against `SetEnabled`. Holding mu
	// through `addAutoQueueSong` means a SetEnabled(false) cannot land
	// between the final "still enabled?" check and the queue mutation —
	// the disable will either run first (then the candidate is dropped at
	// the post-fetch re-check) or run after the candidate is committed.
	// The slow `FetchRelated` call is intentionally held outside this
	// lock — only the pre-fetch check, the post-fetch check, and the
	// insertion call take it.
	mu         sync.Mutex
	triggering bool
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

// SetAddAutoQueueSongFunc sets the callback that revalidates and inserts the
// auto-queue candidate atomically. This callback is mandatory; without it
// CheckAndTrigger will refuse to perform a candidate insertion.
func (i *Interactor) SetAddAutoQueueSongFunc(fn AddAutoQueueSongFunc) {
	i.addAutoQueueSong = fn
}

// CheckAndTrigger checks if auto-queue should fire and adds a song if needed.
//
// Serialization contract:
//   - Pre-fetch: mu is taken to read config + queue snapshot, then
//     released before FetchRelated runs (so the slow yt-dlp call is
//     NOT held under any lock).
//   - Post-fetch: mu is re-acquired to re-validate that auto-queue is
//     still enabled and the queue tail is unchanged.
//   - Insertion: if the post-fetch check passes, the `addAutoQueueSong`
//     callback is invoked WHILE STILL HOLDING mu (so a concurrent
//     `SetEnabled(false)` is forced to wait for the insertion to
//     complete). The callback takes its own queue lock; the
//     auto-queue interactor's mu does NOT block on the queue
//     mutation itself, only on serializing the auto-queue decision
//     against SetEnabled.
//   - mu is RELEASED IMMEDIATELY after the insertion call returns.
//     The insertion error is inspected outside the lock, then the
//     history append and broadcaster call run WITHOUT holding mu so
//     they cannot block a concurrent `SetEnabled(false)`.
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

	// --- Phase 1: pre-fetch revalidation ---
	i.mu.Lock()
	cfg, err := i.autoQueueRepo.GetConfig(ctx)
	if err != nil {
		i.mu.Unlock()
		log.Printf("auto-queue: failed to get config: %v", err)
		return fmt.Errorf("failed to get config: %w", err)
	}
	if !cfg.Enabled {
		i.mu.Unlock()
		log.Printf("auto-queue: disabled, skipping trigger")
		return nil
	}

	queue, err := i.queueRepo.Load(ctx)
	if err != nil {
		i.mu.Unlock()
		log.Printf("auto-queue: failed to load queue: %v", err)
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if len(queue.Songs) == 0 {
		i.mu.Unlock()
		log.Printf("auto-queue: queue empty, skipping trigger")
		return nil
	}
	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		i.mu.Unlock()
		log.Printf("auto-queue: invalid current index %d for %d songs, skipping trigger", queue.CurrentIndex, len(queue.Songs))
		return nil
	}
	if queue.CurrentIndex != len(queue.Songs)-1 {
		i.mu.Unlock()
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
	i.mu.Unlock()
	// --- End Phase 1: mu released before slow FetchRelated ---

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

	// --- Phase 2: post-fetch revalidation under mu + serialized with
	// SetEnabled through the queue-owned insertion. ---
	if i.addAutoQueueSong == nil {
		// No insertion callback wired; refuse to mutate queue state directly to
		// avoid the race conditions the Sprint 004 stale-candidate fix targets.
		log.Printf("auto-queue: add-song callback not configured, skipping insertion")
		return nil
	}

	i.mu.Lock()
	cfg, err = i.autoQueueRepo.GetConfig(ctx)
	if err != nil {
		i.mu.Unlock()
		log.Printf("auto-queue: failed to re-get config: %v", err)
		return fmt.Errorf("failed to get config: %w", err)
	}
	if !cfg.Enabled {
		i.mu.Unlock()
		log.Printf("auto-queue: disabled mid-flight, dropping candidate %q (no save, no history, no activity, no broadcast)", song.ID)
		return nil
	}

	// Call the queue-owned conditional insertion WHILE STILL HOLDING mu.
	// The callback takes its own queue lock; we are not blocking on the
	// queue mutation itself, only serializing the auto-queue decision
	// against SetEnabled.
	result, insertErr := i.addAutoQueueSong(ctx, song, lastSong.ID)
	// Sprint 004: release mu IMMEDIATELY after the insertion call returns.
	// History append and broadcaster call run OUTSIDE the auto-queue mutex
	// so a concurrent `SetEnabled(false)` is not blocked on those slow
	// downstream operations.
	i.mu.Unlock()

	if insertErr != nil {
		if errors.Is(insertErr, ErrAutoQueueStale) {
			log.Printf("auto-queue: candidate %q stale at insertion, dropping (no save, no broadcast)", song.ID)
			return nil
		}
		return fmt.Errorf("failed to add song via callback: %w", insertErr)
	}

	// --- mu released: downstream work runs outside the auto-queue mutex ---

	historyEntry := domain.PlayHistoryEntry{
		VideoID:  lastSong.ID,
		Title:    lastSong.Title,
		PlayedAt: time.Now(),
	}
	if err := i.autoQueueRepo.AppendHistory(ctx, historyEntry); err != nil {
		log.Printf("auto-queue: failed to append history: %v", err)
	}

	// Broadcast auto-queue event to connected clients with the authoritative
	// snapshot captured by the locked insertion. No second state reload.
	if i.broadcaster != nil {
		i.broadcaster("auto_queue_added", map[string]interface{}{
			"song":              result.Song,
			"source_song_title": lastSong.Title,
			"activity":          result.Activity,
			"current_index":     result.CurrentIndex,
			"current_song":      result.CurrentSong,
			"status":            result.Status,
			"elapsed":           result.Elapsed,
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

// SetEnabled updates the enabled flag for auto-queue. Holds the same mutex
// as the post-fetch config revalidation in CheckAndTrigger, so a SetEnabled
// that completes while a slow FetchRelated was in flight will be observed
// before the candidate is inserted — the candidate is dropped with no save,
// history, activity, or broadcast.
func (i *Interactor) SetEnabled(ctx context.Context, enabled bool) error {
	i.mu.Lock()
	defer i.mu.Unlock()

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
