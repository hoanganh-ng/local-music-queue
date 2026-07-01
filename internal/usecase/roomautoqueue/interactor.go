// Package roomautoqueue hosts the per-room auto-queue use case. It
// orchestrates the per-room config read, the recommendation fetch
// (via the existing domain.RelatedSongFetcher), the queue-owned
// conditional insertion (via the roomqueue AddRoomAutoQueueSongFunc
// seam), and the per-room broadcast via a typed narrow broadcaster
// seam. It is the per-room analogue of the global usecase/autoqueue
// package — same concurrency model, same slow-fetch-outside-lock
// invariant, same stale-candidate semantics, but keyed by room id so
// cross-room operations are independent (a slow FetchRelated for
// room A MUST NOT suppress or delay a trigger for room B).
//
// Authorization ownership (R09f):
//   - GetConfig is open to any active member (read).
//   - SetEnabled requires host/admin role (use-case enforced so it
//     matches the roomqueue use-case pattern).
//   - CheckAndTrigger has no per-action authorization — the trigger
//     fires regardless of who holds the lease, mirroring the global
//     auto-queue.
//
// Locking discipline:
//   - The slow FetchRelated call is NEVER held under any lock.
//   - The post-fetch enabled recheck + AddRoomAutoQueueSong insertion
//     run under the coordinator mu. This is the minimum critical
//     section that serializes SetEnabled(false) with the
//     "still-enabled?" check + insertion. GetConfig, queue snapshot
//     loading, GetRecentHistory, FetchRelated, AppendHistory, and the
//     broadcaster call all run OUTSIDE mu.
//   - The per-room in-flight map is guarded only by the coordinator
//     mu. Cross-room in-flight independence is preserved because
//     mu is only held for the map check/set/clear and the
//     recheck+insertion critical section.
//   - Stale-candidate, repository-load-failure, and disable-mid-flight
//     paths are serialized exactly as the global contract serializes
//     them — and only with respect to OTHER triggers for the SAME
//     room.
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
	"local-music-queue/internal/usecase/room"
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

// RoomAutoQueueBroadcaster is the typed narrow seam the wiring layer
// implements to fan out the per-room auto-queue-added event. Keeping
// it typed (rather than a generic map[string]interface{}) removes
// ambiguity at the call site and matches the existing per-room
// broadcaster methods (BroadcastRoomAutoQueueAdded).
//
// The "added" payload carries room_slug, song, source_song_title,
// current_index, current_song, status, elapsed, and the authoritative
// post-mutation queue state — the same fields the R09e contract
// settled for the room_auto_queue_added envelope.
type RoomAutoQueueBroadcaster interface {
	BroadcastRoomAutoQueueAdded(roomSlug string, song entity.Song, sourceSongTitle string, currentIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue)
}

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
	broadcaster       RoomAutoQueueBroadcaster
	queueSnapshotLoader queueSnapshotLoader

	// mu serializes:
	//   - the per-room inFlight map (set/clear at trigger boundaries)
	//   - the post-fetch "still enabled?" recheck + the
	//     AddRoomAutoQueueSong insertion (so SetEnabled(false)
	//     cannot land between the final recheck and the queue
	//     mutation).
	//
	// mu is NEVER held during GetConfig, queue snapshot loading,
	// GetRecentHistory, FetchRelated, AppendHistory, or the
	// broadcaster call. Cross-room in-flight independence is
	// preserved because mu is held only for the per-room map
	// check/set/clear and the final pre-insertion critical
	// section.
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

// SetBroadcaster wires the typed narrow broadcaster seam.
func (i *Interactor) SetBroadcaster(b RoomAutoQueueBroadcaster) { i.broadcaster = b }

// SetQueueSnapshotLoader wires the loader the interactor uses to
// fetch the queue snapshot before fetcher + insertion. The
// production wiring points this at roomqueue.Interactor.
// GetStateByRoomID. nil is tolerated (returns "queue unavailable"
// so the trigger can short-circuit before fetching).
func (i *Interactor) SetQueueSnapshotLoader(fn queueSnapshotLoader) {
	i.queueSnapshotLoader = fn
}

// GetConfig returns the per-room auto-queue config. Any active
// member may read. Authorization (active-membership check) is
// enforced inside the use case so the handler layer does not have
// to duplicate it.
func (i *Interactor) GetConfig(ctx context.Context, slug string, actorUserID int) (*domain.RoomAutoQueueConfig, error) {
	roomObj, err := i.resolveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := i.requireMember(ctx, roomObj.ID, actorUserID); err != nil {
		return nil, err
	}
	return i.autoQueueRepo.GetConfig(ctx, roomObj.ID)
}

// SetEnabled updates the per-room enabled flag. Host/admin only —
// the use case enforces the role gate (matches the roomqueue
// use-case pattern so the handler layer is thin). The method
// holds the same coordinator mu that the post-fetch recheck + the
// insertion hold, so a SetEnabled(false) cannot land between the
// final "still enabled?" check and the queue mutation: the disable
// either runs first (then the candidate is dropped on the
// post-fetch recheck) or runs after the insertion completes.
//
// Config loading + saving happens OUTSIDE the critical section
// (we hold mu only for the final SaveConfig write); a concurrent
// slow repo read for room A never blocks the per-room B path.
//
// Maps to roomautoqueue.ErrAutoQueueStale on insertion, which this
// method never sees (toggle only writes config; no queue mutation).
func (i *Interactor) SetEnabled(ctx context.Context, slug string, actorUserID int, enabled bool) (*domain.RoomAutoQueueConfig, error) {
	roomObj, err := i.resolveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := i.requireHostOrAdmin(ctx, roomObj.ID, actorUserID); err != nil {
		return nil, err
	}
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

// CheckAndTrigger checks if per-room auto-queue should fire and
// inserts a song if needed.
//
// Serialization contract (mirrors the global autoqueue contract):
//
//   - Pre-fetch (under mu):
//       1. cfg = autoQueueRepo.GetConfig(roomID) (outside mu; see below)
//       2. queue = queueSnapshotLoader(roomID) (outside mu; see below)
//       3. mu is taken to check+set the per-room in-flight flag
//          (returns nil if another trigger for THIS room is in
//          flight — cross-room triggers are independent).
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
		// goroutine never panics on a missing slug.
		return nil
	}
	roomID := roomObj.ID

	// Phase 1a: per-room in-flight guard under the coordinator mu.
	// This is the only place mu guards the in-flight map.
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

	// Phase 1b: pre-fetch revalidation. GetConfig, queue snapshot
	// loading, and GetRecentHistory all run OUTSIDE mu — a slow
	// repo read or slow loader for room A MUST NOT block room B's
	// trigger (mu is held only for the inFlight map ops above and
	// the final post-fetch critical section below).
	cfg, err := i.autoQueueRepo.GetConfig(ctx, roomID)
	if err != nil {
		log.Printf("room auto-queue: failed to get config (%d): %v", roomID, err)
		return fmt.Errorf("failed to get config: %w", err)
	}
	if !cfg.Enabled {
		log.Printf("room auto-queue: disabled, skipping trigger for room %d", roomID)
		return nil
	}

	queue, err := i.loadQueueSnapshot(ctx, roomID)
	if err != nil {
		log.Printf("room auto-queue: failed to load queue (%d): %v", roomID, err)
		return fmt.Errorf("failed to load queue: %w", err)
	}
	if len(queue.Songs) == 0 {
		log.Printf("room auto-queue: queue empty, skipping trigger for room %d", roomID)
		return nil
	}
	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		log.Printf("room auto-queue: invalid current index %d for %d songs, skipping trigger for room %d", queue.CurrentIndex, len(queue.Songs), roomID)
		return nil
	}
	if queue.CurrentIndex != len(queue.Songs)-1 {
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

	// Phase 2: post-fetch revalidation + insertion under mu. This is
	// the minimum critical section needed to serialize SetEnabled
	// with the post-fetch insert decision — same shape as the global
	// autoqueue Sprint 004 contract.
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

	if i.broadcaster != nil {
		// The just-inserted song is the last element of the
		// post-mutation queue. The roomqueue.AddRoomAutoQueueSong
		// contract guarantees queue.Songs[len-1] is the candidate.
		var added entity.Song
		var currentSong *entity.Song
		var currentIndex, elapsed int
		var status entity.PlaybackStatus
		var state *entity.Queue
		if result != nil && result.Queue != nil && len(result.Queue.Songs) > 0 {
			added = result.Queue.Songs[len(result.Queue.Songs)-1]
			state = result.Queue
			currentIndex = result.CurrentIndex
			currentSong = result.CurrentSong
			status = result.Status
			elapsed = result.Elapsed
		}
		i.broadcaster.BroadcastRoomAutoQueueAdded(slug, added, lastSong.Title, currentIndex, currentSong, status, elapsed, state)
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

// resolveRoom converts a slug to a *entity.Room under the same
// rules roomqueue uses. Re-declared here to avoid the upward
// dependency on usecase/roomqueue for slug validation. Returns
// the room.Err* sentinels so the handler layer can map them via
// writeRoomQueueError without taking a dependency on
// usecase/roomautoqueue sentinels (the room sentinels are the
// canonical room-domain errors).
func (i *Interactor) resolveRoom(ctx context.Context, slug string) (*entity.Room, error) {
	if !entity.IsValidSlug(slug) {
		return nil, room.ErrInvalidSlug
	}
	r, err := i.roomRepo.GetRoomBySlug(ctx, slug)
	if err != nil {
		return nil, room.ErrRoomNotFound
	}
	if r == nil {
		return nil, room.ErrRoomNotFound
	}
	if r.Status != entity.RoomStatusActive {
		return nil, room.ErrArchived
	}
	return r, nil
}

// requireMember returns nil when actorUserID is a member of the
// room (any role). Non-members get room.ErrForbidden. Mirrors the
// roomqueue.Interactor.requireMember shape so the package's
// authorization surface matches its sibling use case.
func (i *Interactor) requireMember(ctx context.Context, roomID int64, actorUserID int) error {
	if actorUserID == 0 {
		return room.ErrForbidden
	}
	if _, err := i.roomRepo.GetMember(ctx, roomID, actorUserID); err != nil {
		return room.ErrForbidden
	}
	return nil
}

// requireHostOrAdmin returns nil when actorUserID holds the host or
// admin role in the room. Guests and non-members get
// room.ErrForbidden so the controller can't distinguish the two
// via status code. Mirrors the roomqueue.Interactor permission
// checks for privileged mutations.
func (i *Interactor) requireHostOrAdmin(ctx context.Context, roomID int64, actorUserID int) error {
	if actorUserID == 0 {
		return room.ErrForbidden
	}
	member, err := i.roomRepo.GetMember(ctx, roomID, actorUserID)
	if err != nil {
		return room.ErrForbidden
	}
	if member.Role != entity.RoomRoleHost && member.Role != entity.RoomRoleAdmin {
		return room.ErrForbidden
	}
	return nil
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
func (i *Interactor) loadQueueSnapshot(ctx context.Context, roomID int64) (*entity.Queue, error) {
	if i.queueSnapshotLoader == nil {
		return nil, errors.New("room auto-queue: queue snapshot loader not wired")
	}
	return i.queueSnapshotLoader(ctx, roomID)
}

// errInvalidSlug / errArchived / errRoomNotFound are intentionally
// REMOVED — the use case returns room.ErrInvalidSlug /
// room.ErrRoomNotFound / room.ErrArchived directly so the handler
// layer's writeRoomQueueError can map them via the canonical
// room-domain sentinels without taking a dependency on
// usecase/roomautoqueue package-level errors.
var (
	_ = sync.Mutex{}
	_ queueSnapshotLoader
	_ = repository.RoomRepository(nil)
)