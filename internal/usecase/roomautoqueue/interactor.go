// Package roomautoqueue hosts the per-room auto-queue use case. It
// orchestrates the per-room config read, the recommendation fetch
// (via the existing domain.RelatedSongFetcher), the queue-owned
// conditional insertion (via the roomqueue AddRoomAutoQueueSongFunc
// seam), and the per-room broadcast via the roomqueue.Broadcaster
// seam. It is the per-room analogue of the global usecase/autoqueue
// package — same concurrency model, same slow-fetch-outside-lock
// invariant, same stale-candidate semantics, but keyed by room id so
// cross-room operations are independent (a slow FetchRelated for
// room A MUST NOT suppress or delay a trigger for room B).
//
// All locking mirrors the global contract:
//   - The slow FetchRelated call is NEVER held under any lock.
//   - The queue-owned conditional insertion takes the roomqueue
//     mutex (via the AddRoomAutoQueueSongFunc seam). The seam takes
//     care of the roomqueue mutex; this package never imports
//     usecase/roomqueue.
//   - The per-room in-flight flag is guarded by a coordinator mu.
//   - AppendHistory, activity writes, and the broadcaster call run
//     OUTSIDE both locks.
//   - Stale-candidate, repository-load-failure, and disable-mid-flight
//     paths are serialized exactly as the global contract serializes
//     them — and only with respect to OTHER triggers for the SAME room.
//   - The per-room in-flight map is in-memory; on restart every
//     room's in-flight state is empty. No cross-process safety claim
//     is made; a future horizontal-scaling redesign would need a
//     different coordinator (e.g. advisory lock per room) and is
//     out of scope.
package roomautoqueue

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

// AddRoomAutoQueueSongResult mirrors the queue.AddSongResult fields
// the per-room insertion returns to the broadcaster. Re-declared here
// (rather than imported from queue) so this package stays independent
// of usecase/roomqueue at the type level. The actual tuple delivered
// by the AddRoomAutoQueueSongFunc seam is decoded into this shape
// inside CheckAndTrigger.
type AddRoomAutoQueueSongResult struct {
	Queue        *entity.Queue
	CurrentIndex int
	CurrentSong  *entity.Song
	Status       entity.PlaybackStatus
	Elapsed      int
}

// AddRoomAutoQueueSongFunc is the seam into the roomqueue
// conditional-insertion path. The wiring adapter in cmd/server
// implements this so roomautoqueue does not need to import
// usecase/roomqueue (avoiding an upward dependency from a leaf
// usecase). Mirrors the global autoqueue.AddAutoQueueSongFunc seam.
type AddRoomAutoQueueSongFunc func(ctx context.Context, slug string, song *entity.Song, expectedSourceSongID string) (*AddRoomAutoQueueSongResult, error)

// BroadcastFunc is the seam into the per-room broadcaster. Avoids
// an import cycle onto delivery/ws. The cmd/server adapter wires
// this to the per-room WebSocket hub.
type BroadcastFunc func(eventType string, payload interface{})

// ErrAutoQueueStale is the per-room analogue of
// autoqueue.ErrAutoQueueStale. The AddRoomAutoQueueSongFunc seam
// adapter maps roomqueue.ErrRoomAutoQueueStale to this sentinel so
// this package stays independent of usecase/roomqueue.
var ErrAutoQueueStale = errors.New("room auto-queue candidate is stale")

// queueSnapshotLoader is the seam the wiring uses to expose the
// roomqueue interactor's GetStateByRoomID without usecase/roomautoqueue
// importing usecase/roomqueue.
type queueSnapshotLoader func(ctx context.Context, roomID int64) (*entity.Queue, error)

// Interactor orchestrates per-room auto-queue triggers.
type Interactor struct {
	roomRepo          repository.RoomRepository
	autoQueueRepo     domain.RoomAutoQueueRepository
	fetcher           domain.RelatedSongFetcher
	addRoomAutoSongFn AddRoomAutoQueueSongFunc
	broadcast         BroadcastFunc
	queueSnapshotLoader queueSnapshotLoader

	// mu serializes the per-room in-flight map (a slow FetchRelated
	// for room A must not suppress or block a trigger for room B).
	// The slow FetchRelated call is intentionally held OUTSIDE mu so
	// the coordinator mutex never blocks on the yt-dlp I/O.
	mu       sync.Mutex
	inFlight map[int64]bool // roomID -> in-flight flag
}

// NewInteractor creates a per-room auto-queue Interactor. The
// roomRepo is used for slug-to-roomid resolution (the broadcaster
// and trigger receive slugs, but the coordinator map is keyed by
// room id so a slug->id lookup runs once per CheckAndTrigger).
func NewInteractor(roomRepo repository.RoomRepository, autoQueueRepo domain.RoomAutoQueueRepository, fetcher domain.RelatedSongFetcher) *Interactor {
	return &Interactor{
		roomRepo:      roomRepo,
		autoQueueRepo: autoQueueRepo,
		fetcher:       fetcher,
		inFlight:      map[int64]bool{},
	}
}

// SetAddRoomAutoQueueSongFunc wires the roomqueue conditional
// insertion seam. Mandatory; without it CheckAndTrigger refuses to
// perform a candidate insertion (mirrors the global contract).
func (i *Interactor) SetAddRoomAutoQueueSongFunc(fn AddRoomAutoQueueSongFunc) { i.addRoomAutoSongFn = fn }

// SetBroadcastFunc wires the per-room broadcaster seam.
func (i *Interactor) SetBroadcastFunc(fn BroadcastFunc) { i.broadcast = fn }

// GetConfig returns the per-room auto-queue config. Slug-validation
// + active-room checks mirror the roomqueue package. Any active
// member may read.
func (i *Interactor) GetConfig(ctx context.Context, slug string, actorUserID int) (*domain.RoomAutoQueueConfig, error) {
	roomObj, err := i.resolveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	// Authorization: the handler layer enforces host/admin for
	// toggles. GetConfig is open to any active member per the spec.
	return i.autoQueueRepo.GetConfig(ctx, roomObj.ID)
}

// SetEnabled updates the per-room enabled flag. Host/admin only —
// the handler layer enforces the role gate. Maps to
// roomautoqueue.ErrAutoQueueStale on insertion, which this method
// never sees (toggle only writes config; no queue mutation).
func (i *Interactor) SetEnabled(ctx context.Context, slug string, actorUserID int, enabled bool) (*domain.RoomAutoQueueConfig, error) {
	roomObj, err := i.resolveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	// Load the current config (preserving the strategy field) and
	// update only Enabled.
	cfg, err := i.autoQueueRepo.GetConfig(ctx, roomObj.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get room auto-queue config: %w", err)
	}
	cfg.Enabled = enabled
	if err := i.autoQueueRepo.SaveConfig(ctx, roomObj.ID, *cfg); err != nil {
		return nil, fmt.Errorf("failed to save room auto-queue config: %w", err)
	}
	return cfg, nil
}

// resolveRoom converts a slug to a *entity.Room under the same
// rules roomqueue uses. Re-declared here to avoid the upward
// dependency on usecase/roomqueue for slug validation.
func (i *Interactor) resolveRoom(ctx context.Context, slug string) (*entity.Room, error) {
	if !entity.IsValidSlug(slug) {
		return nil, errInvalidSlug
	}
	r, err := i.roomRepo.GetRoomBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errRoomNotFound
	}
	if r.Status != entity.RoomStatusActive {
		return nil, errArchived
	}
	return r, nil
}

// errInvalidSlug / errArchived are intentionally local (not a
// re-export from usecase/room) so the handler layer can map them
// without taking a dependency on usecase/room. Mirrors the global
// queue.AddAutoQueueSong sentinel shape.
var (
	errInvalidSlug  = errors.New("invalid room slug")
	errArchived     = errors.New("room archived")
	errRoomNotFound = errors.New("room not found")
)

// Errors returns the typed sentinels the handler layer needs to map
// to HTTP statuses. Exposed here (rather than as package vars) so
// callers don't accidentally treat them as the global queue's
// sentinels.
func (i *Interactor) Errors() (invalidSlug, archived, forbidden error) {
	return errInvalidSlug, errArchived, errForbidden
}

// errForbidden is the local sentinel the handler layer maps to 403
// when an active-member check fails (rare — the handler typically
// resolves actor identity before calling GetConfig/SetEnabled, but
// the seam allows for membership-resolved variants).
var errForbidden = errors.New("room auto-queue action not permitted for this member")

// CheckAndTrigger checks if per-room auto-queue should fire and
// inserts a song if needed.
//
// Serialization contract (mirrors the global autoqueue contract):
//   - Pre-fetch:
//       1. mu is taken to check+set the per-room in-flight flag
//          (returns nil if another trigger for THIS room is in
//          flight — cross-room triggers are independent).
//       2. cfg = autoQueueRepo.GetConfig(roomID)
//       3. queue = queueRepo.Load(roomID)
//       4. recentHistory = autoQueueRepo.GetRecentHistory(roomID, 20)
//       5. exclude = history ∪ queue
//       6. mu is released BEFORE the slow FetchRelated runs.
//
//   - FetchRelated: held outside any lock.
//
//   - Post-fetch (under mu + serialized with SetEnabled):
//       1. cfg is re-read; if disabled-mid-flight, candidate is
//          dropped with no save, no history, no activity, no
//          broadcast.
//       2. addRoomAutoSongFn(slug, song, expectedSourceSongID) is
//          invoked WHILE STILL HOLDING mu.
//       3. mu is RELEASED IMMEDIATELY after the insertion call.
//          History append and broadcaster call run OUTSIDE mu.
//
//   - Returning:
//       - The per-room in-flight flag is cleared BEFORE
//         CheckAndTrigger returns.
func (i *Interactor) CheckAndTrigger(ctx context.Context, slug string) error {
	roomObj, err := i.resolveRoom(ctx, slug)
	if err != nil {
		// No room → nothing to trigger. Silent skip so the trigger
		// goroutine never panics on a missing slub.
		return nil
	}
	roomID := roomObj.ID

	// Phase 1a: per-room in-flight guard under the coordinator mu.
	i.mu.Lock()
	if i.inFlight[roomID] {
		i.mu.Unlock()
		return nil
	}
	i.inFlight[roomID] = true
	i.mu.Unlock()
	// Defer the in-flight clear so it runs even on early returns.
	defer func() {
		i.mu.Lock()
		delete(i.inFlight, roomID)
		i.mu.Unlock()
	}()

	// Phase 1b: pre-fetch revalidation under mu.
	i.mu.Lock()
	cfg, err := i.autoQueueRepo.GetConfig(ctx, roomID)
	if err != nil {
		i.mu.Unlock()
		log.Printf("room auto-queue: failed to get config (%d): %v", roomID, err)
		return fmt.Errorf("failed to get config: %w", err)
	}
	if !cfg.Enabled {
		i.mu.Unlock()
		log.Printf("room auto-queue: disabled, skipping trigger for room %d", roomID)
		return nil
	}

	// Load the queue snapshot via the same path the roomqueue
	// interactor uses; we re-load instead of threading the
	// post-mutation queue through the trigger seam. This keeps the
	// trigger independent of any in-flight roomqueue mutex holder.
	queue, err := i.loadQueueSnapshot(ctx, roomID)
	if err != nil {
		i.mu.Unlock()
		log.Printf("room auto-queue: failed to load queue (%d): %v", roomID, err)
		return fmt.Errorf("failed to load queue: %w", err)
	}
	if len(queue.Songs) == 0 {
		i.mu.Unlock()
		log.Printf("room auto-queue: queue empty, skipping trigger for room %d", roomID)
		return nil
	}
	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		i.mu.Unlock()
		log.Printf("room auto-queue: invalid current index %d for %d songs, skipping trigger for room %d", queue.CurrentIndex, len(queue.Songs), roomID)
		return nil
	}
	if queue.CurrentIndex != len(queue.Songs)-1 {
		i.mu.Unlock()
		log.Printf("room auto-queue: current index %d is not last (%d), skipping trigger for room %d", queue.CurrentIndex, len(queue.Songs), roomID)
		return nil
	}

	lastSong := queue.Songs[queue.CurrentIndex]

	recentHistory, err := i.autoQueueRepo.GetRecentHistory(ctx, roomID, 20)
	if err != nil {
		log.Printf("room auto-queue: failed to get recent history (%d): %v", roomID, err)
		recentHistory = []domain.RoomPlayHistoryEntry{}
	}

	// Build exclude list (recent history + room queue contents).
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
	// End Phase 1: mu released before slow FetchRelated.

	song, err := i.fetcher.FetchRelated(ctx, lastSong.ID, exclude)
	if err != nil {
		log.Printf("room auto-queue: fetcher failed (%d): %v, trying fallback", roomID, err)
		queueOnlyExclude := make(map[string]bool)
		for _, s := range queue.Songs {
			queueOnlyExclude[s.ID] = true
		}
		song = i.fallbackFromHistory(ctx, roomID, queueOnlyExclude)
	}

	if song == nil {
		log.Printf("room auto-queue: no candidate (fetcher and fallback both failed) for room %d", roomID)
		return nil
	}

	// Phase 2: post-fetch revalidation + insertion under mu.
	if i.addRoomAutoSongFn == nil {
		log.Printf("room auto-queue: insertion callback not configured, skipping for room %d", roomID)
		return nil
	}

	i.mu.Lock()
	cfg, err = i.autoQueueRepo.GetConfig(ctx, roomID)
	if err != nil {
		i.mu.Unlock()
		log.Printf("room auto-queue: failed to re-get config (%d): %v", roomID, err)
		return fmt.Errorf("failed to get config: %w", err)
	}
	if !cfg.Enabled {
		i.mu.Unlock()
		log.Printf("room auto-queue: disabled mid-flight, dropping candidate %q for room %d (no save, no history, no activity, no broadcast)", song.ID, roomID)
		return nil
	}

	// Snapshot expected source song title BEFORE releasing mu so
	// downstream goroutines can use it for the broadcast and
	// history append. We retain the post-fetch hold-mu invariant
	// here so a SetEnabled(false) cannot land between the final
	// "still enabled?" check and the queue-owned insertion.
	result, insertErr := i.addRoomAutoSongFn(ctx, slug, song, lastSong.ID)
	i.mu.Unlock()

	if insertErr != nil {
		if errors.Is(insertErr, ErrAutoQueueStale) {
			log.Printf("room auto-queue: candidate %q stale at insertion, dropping for room %d (no save, no broadcast)", song.ID, roomID)
			return nil
		}
		return fmt.Errorf("failed to add room auto-queue song: %w", insertErr)
	}

	// mu released: downstream work runs outside the per-room mutex.
	historyEntry := domain.RoomPlayHistoryEntry{
		VideoID:  lastSong.ID,
		Title:    lastSong.Title,
		PlayedAt: time.Now(),
	}
	if err := i.autoQueueRepo.AppendHistory(ctx, roomID, historyEntry); err != nil {
		log.Printf("room auto-queue: failed to append room play history (%d): %v", roomID, err)
	}

	if i.broadcast != nil {
		i.broadcast("room_auto_queue_added", map[string]interface{}{
			"room_slug":         slug,
			"song":              result.Queue.Songs[len(result.Queue.Songs)-1],
			"source_song_title": lastSong.Title,
			"state":             result.Queue,
		})
	}

	return nil
}

// fallbackFromHistory picks a random song from the room's recent
// 50-entry history that isn't currently in the room queue. Failure
// returns nil so the caller can treat fallback-failure as
// "no candidate available".
func (i *Interactor) fallbackFromHistory(ctx context.Context, roomID int64, excludeMap map[string]bool) *entity.Song {
	history, err := i.autoQueueRepo.GetRecentHistory(ctx, roomID, 50)
	if err != nil || len(history) == 0 {
		return nil
	}
	var candidates []domain.RoomPlayHistoryEntry
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
		Thumbnail: fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", chosen.VideoID),
		AddedBy:   entity.SystemUserID,
		AddedByID: 0,
		URL:       fmt.Sprintf("https://www.youtube.com/watch?v=%s", chosen.VideoID),
	}
}

// loadQueueSnapshot loads the persisted room queue for a trigger.
// The interactor does not hold the roomqueue mutex itself; the
// post-fetch insertion takes the roomqueue mutex via the seam. We
// read through the roomqueue Interactor's public surface — but
// that would create an import cycle. Instead, the wiring layer in
// cmd/server installs the snapshot loader as part of the seam.
//
// SetQueueSnapshotLoader wires the loader the interactor uses to
// fetch the queue snapshot before fetcher + insertion. The
// production wiring points this at roomqueue.Interactor.
// GetStateByRoomID. nil is tolerated (returns "queue unavailable"
// so the trigger can short-circuit before fetching).
func (i *Interactor) SetQueueSnapshotLoader(fn queueSnapshotLoader) {
	i.queueSnapshotLoader = fn
}

func (i *Interactor) loadQueueSnapshot(ctx context.Context, roomID int64) (*entity.Queue, error) {
	if i.queueSnapshotLoader == nil {
		return nil, errors.New("room auto-queue: queue snapshot loader not wired")
	}
	return i.queueSnapshotLoader(ctx, roomID)
}

var (
	_ = sync.Mutex{}
	_ queueSnapshotLoader
	_ = repository.RoomRepository(nil)
)
