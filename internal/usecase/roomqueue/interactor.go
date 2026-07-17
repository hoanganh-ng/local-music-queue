// Package roomqueue hosts the room-scoped playback queue use case. The
// queue shape is the existing entity.Queue; this layer is responsible
// for:
//   - resolving actor identity (slug -> room -> membership -> role)
//   - enforcing membership and active-room status
//   - serializing mutations under a per-room mutex
//   - preserving queue invariants (Add / Remove / Clear / ContainsSong)
//   - mapping sentinel errors to documented HTTP statuses
//
// Broadcasts: the package exposes a Broadcaster seam that the delivery
// layer wires to the per-room WebSocket hub (R07b). The interactor itself
// does NOT broadcast — mutations return the post-state and the handler
// fans it out so the usecase package stays independent of delivery/ws.
package roomqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/domain/service"
	"local-music-queue/internal/usecase/room"
)

// Sentinel errors mapped to HTTP statuses by the handler layer.
var (
	// ErrInvalidIndex mirrors usecase/queue.ErrInvalidIndex; reproduced
	// here so the room queue handler can switch on a local sentinel
	// without importing the global queue package.
	ErrInvalidIndex     = errors.New("invalid song index")
	ErrNotSongOwner     = errors.New("user does not own the song")
	ErrCannotRemoveSong = errors.New("guest cannot remove current or already-played song")
	// ErrCannotPrioritizeCurrent mirrors the entity-layer rejection so
	// the handler can map it to 400 without switching on the entity
	// package. Prioritizing the currently-playing song is a client bug.
	ErrCannotPrioritizeCurrent = errors.New("cannot prioritize the currently playing song")
	// R09a playback sentinels. The lease sentinels are re-declared as
	// thin aliases of the room-package sentinels (so handlers can
	// switch on local symbols without taking a transitive dep on
	// usecase/room.Err* — they already do for the prior queue ops).
	// ErrNoCurrentSong and ErrNoNextSong mirror the entity-layer
	// sentinels for the same reason.
	ErrNoCurrentSong      = errors.New("no current song")
	ErrNoNextSong         = errors.New("no next song in queue")
	ErrInvalidStatus      = errors.New("invalid playback status")
	ErrInvalidElapsed     = errors.New("invalid elapsed value")
	ErrPlaybackForbidden  = errors.New("not lease holder")
	ErrPlaybackLeaseGone  = errors.New("player lease gone (past grace)")
	ErrPlaybackLeaseLost  = errors.New("player lease not found")
	// R09b: SkipVote is called by the vote interactor only after the
	// vote session for the current song has been won. The expected
	// current-song ID comes from the vote session's stored song at
	// creation time. If the queue has advanced under it (e.g. the lease
	// holder already skipped, or another path advanced), SkipVote returns
	// this sentinel WITHOUT mutating state. The interactor translates the
	// absence of an error into a successful resolution. No broadcast is
	// emitted on this path; the caller (roomvote) is responsible for
	// dispatching room_vote_resolved + room_playback_song_advanced only
	// when SkipVote succeeds.
	ErrStaleSkipVote = errors.New("queue advanced under the vote session")
	// R09c: ErrInvalidDirection is returned by ChangePlaybackVolume when
	// the supplied direction is neither "up" nor "down" (case-sensitive).
	// The handler maps this to HTTP 400. The interactor validates the
	// value before resolving the room so malformed input is rejected
	// without any DB / lease work.
	ErrInvalidDirection = errors.New("invalid playback direction")
	// R09d: ErrNoPreviousSong is returned by PrevPlayback when the queue
	// is already on the first song (CurrentIndex == 0). Mirrors
	// ErrNoNextSong so the handler maps it to HTTP 400 without relying on
	// string matching against the legacy entity.Queue.Prev() error. The
	// interactor validates the move BEFORE any mutation so a failed
	// PrevPlayback leaves the persisted queue byte-for-byte unchanged.
	ErrNoPreviousSong = errors.New("no previous song in queue")
	// R09f: ErrRoomAutoQueueStale is returned by AddRoomAutoQueueSong
	// when the candidate fails any of the stale-revalidation
	// predicates against a successfully-loaded queue (source song no
	// longer current, an upcoming song now exists, or the candidate
	// is already queued). Mirrors the global queue.ErrAutoQueueStale
	// so the future roomautoqueue use case can map it to its own
	// ErrAutoQueueStale sentinel at the wiring boundary without
	// string matching the error text. The interactor surfaces this
	// sentinel with zero side effects — no queue save, no history
	// append, no broadcast.
	ErrRoomAutoQueueStale = errors.New("room auto-queue candidate is stale")
	// R09h: PrioritizeVote is called by the vote interactor only after a
	// prioritize vote session for a specific upcoming song has been won.
	// The (expectedSongID, expectedIndex) pair is snapshotted at session
	// creation. If the stored snapshot index no longer identifies the
	// same non-current target song (removed, moved, became current, or
	// the same ID now appears more than once), PrioritizeVote returns
	// this sentinel WITHOUT mutating state. Mirrors ErrStaleSkipVote:
	// the caller (roomvote) maps it to HTTP 409 and does NOT broadcast a
	// resolved event. No priority balances are touched on any path.
	ErrStalePrioritizeVote = errors.New("prioritize target moved under the vote session")
)

// Interactor owns the room-scoped queue use cases. The mutex serializes
// every mutation per Interactor instance (single-process; matches the
// ADR 001 single-process scope and the existing global queue interactor).
type Interactor struct {
	roomRepo  repository.RoomRepository
	queueRepo repository.RoomQueueRepository
	youtube   service.YouTubeService
	mu        sync.Mutex
	// broadcaster is the seam used by the delivery layer to fan out room
	// queue events. The interactor does NOT broadcast itself — the seam
	// exists so handlers can invoke Broadcast* after a successful mutation
	// without the usecase package importing delivery/ws. nil is tolerated.
	broadcaster Broadcaster
	// leaseAuthorizer is the R09a seam used to verify the caller is
	// the active lease holder for direct playback mutations
	// (status / sync / skip / ended). nil is tolerated for the prior
	// queue operations; the playback methods return ErrPlaybackLeaseLost
	// when the seam is unset so the handler can map that to a
	// misconfigured-server 500 instead of silently dropping auth.
	leaseAuthorizer room.PlaybackLeaseAuthorizer
	// autoQueueUC is the R09f roomautoqueue use-case seam. nil is
	// tolerated (no per-room triggers fire when unset — mirrors the
	// global queue.AutoQueueTrigger seam).
	autoQueueUC RoomAutoQueueTrigger
}

// NewInteractor constructs a room queue interactor. youtube may be nil
// in tests that only exercise metadata-supplied Add paths.
func NewInteractor(roomRepo repository.RoomRepository, queueRepo repository.RoomQueueRepository, youtube service.YouTubeService) *Interactor {
	return &Interactor{
		roomRepo:  roomRepo,
		queueRepo: queueRepo,
		youtube:   youtube,
	}
}

// SetYouTube attaches a metadata fetcher after DI setup. Used in tests
// that construct the interactor without a real YouTube service.
func (i *Interactor) SetYouTube(y service.YouTubeService) { i.youtube = y }

// Broadcaster is the seam the delivery layer implements to fan out
// room queue events. Implementations are expected to be non-blocking
// from the caller's perspective.
type Broadcaster interface {
	BroadcastRoomQueueSync(roomSlug string, state *entity.Queue)
	BroadcastRoomQueueSongAdded(roomSlug string, song entity.Song, position int, state *entity.Queue)
	BroadcastRoomQueueSongRemoved(roomSlug string, removedIndex int, state *entity.Queue)
	BroadcastRoomQueueCleared(roomSlug string, state *entity.Queue)
	BroadcastRoomQueueSongPrioritized(roomSlug string, fromIndex, toIndex int, song entity.Song, state *entity.Queue)
	// R09a playback deltas. Additive on top of R07b/R07d; the per-room
	// hub satisfies the interface implicitly (the method names match).
	BroadcastRoomPlaybackStatusChanged(roomSlug string, status entity.PlaybackStatus, elapsed int, state *entity.Queue)
	BroadcastRoomPlaybackElapsedSync(roomSlug string, elapsed int, state *entity.Queue)
	BroadcastRoomPlaybackSongAdvanced(roomSlug, reason string, previousIndex, newIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue)
	// R09b vote deltas. Additive on top of R07b/R07d/R09a; the per-room
	// hub satisfies the interface implicitly (the method names match).
	BroadcastRoomVoteUpdated(roomSlug string, session *entity.VoteSession, actorUserID int, state *entity.Queue)
	BroadcastRoomVoteResolved(roomSlug string, sessionID, outcome string, state *entity.Queue)
	// R09c: the lease-holder-only volume command fires a single
	// per-room WebSocket event with no associated queue state. The
	// interactor does NOT call this; the handler invokes it after a
	// successful ChangePlaybackVolume.
	BroadcastRoomPlaybackVolumeChanged(roomSlug string, direction string)
	// R09d: the lease-holder-only prev command fires a single additive
	// per-room WebSocket delta carrying the prev/next indexes, the new
	// current song, and the post-mutation queue state. The interactor
	// does NOT call this; the handler invokes it after a successful
	// PrevPlayback.
	BroadcastRoomPlaybackSongPrevious(roomSlug string, previousIndex, newIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue)
	// R09f: the per-room auto-queue trigger fires these two events
	// on /ws/rooms/{slug} only. The global /ws 16-event inventory is
	// unchanged; the trigger path does NOT call the global
	// auto_queue_added / auto_queue_config_changed broadcasters. The
	// roomautoqueue use case invokes these via the seam after a
	// successful AddRoomAutoQueueSong / SetEnabled. The handler
	// for toggle also invokes BroadcastRoomAutoQueueConfigChanged
	// after a successful SetEnabled so the per-room WebSocket
	// subscribers observe the new config without a follow-up fetch.
	BroadcastRoomAutoQueueAdded(roomSlug string, song entity.Song, sourceSongTitle string, currentIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue)
	BroadcastRoomAutoQueueConfigChanged(roomSlug string, enabled bool, strategy string)
}

// RoomAutoQueueTrigger is the seam the future roomautoqueue package
// implements. CheckAndTrigger runs asynchronously on the use-case
// goroutine; the interactor fires it as a non-blocking goroutine so
// slow yt-dlp fetches NEVER stall a successful queue mutation.
//
// nil is tolerated: when no trigger is wired the post-mutation
// goroutine short-circuits silently. This mirrors the global
// queue.AutoQueueTrigger seam.
type RoomAutoQueueTrigger interface {
	CheckAndTrigger(ctx context.Context, slug string) error
}

// SetBroadcaster wires the broadcaster used by the delivery layer to
// publish room queue events after successful mutations. nil disables
// broadcasting (handler skips the call).
func (i *Interactor) SetBroadcaster(b Broadcaster) { i.broadcaster = b }

// Broadcaster returns the broadcaster wired via SetBroadcaster, or nil
// when none is wired. The delivery layer uses this to invoke broadcasts
// after successful mutations.
func (i *Interactor) Broadcaster() Broadcaster { return i.broadcaster }

// resolveActiveRoom fetches the room by slug, ensures it is active, and
// returns it. Slug validation mirrors usecase/room.
func (i *Interactor) resolveActiveRoom(ctx context.Context, slug string) (*entity.Room, error) {
	if !entity.IsValidSlug(slug) {
		return nil, room.ErrInvalidSlug
	}
	roomObj, err := i.roomRepo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, room.ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	if roomObj.Status != entity.RoomStatusActive {
		return nil, room.ErrArchived
	}
	return roomObj, nil
}

// requireMember returns nil when actorUserID is a member of the room
// (any role). Non-members get room.ErrForbidden. Membership here is
// intentionally broader than the per-action permission checks below.
func (i *Interactor) requireMember(ctx context.Context, roomID int64, actorUserID int) error {
	if _, err := i.roomRepo.GetMember(ctx, roomID, actorUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return room.ErrForbidden
		}
		return fmt.Errorf("get member: %w", err)
	}
	return nil
}

// loadQueue loads the persisted queue for the room, or seeds an empty
// queue when no row exists yet. Caller must hold i.mu.
func (i *Interactor) loadQueue(ctx context.Context, roomID int64) (*entity.Queue, error) {
	q, err := i.queueRepo.Load(ctx, roomID)
	if err != nil {
		if errors.Is(err, repository.ErrRoomQueueNotFound) {
			return entity.NewQueue(), nil
		}
		return nil, fmt.Errorf("load room queue: %w", err)
	}
	return q, nil
}

// GetState returns the current queue for an active room. Any active
// member may read.
func (i *Interactor) GetState(ctx context.Context, slug string, actorUserID int) (*entity.Queue, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := i.requireMember(ctx, roomObj.ID, actorUserID); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.loadQueue(ctx, roomObj.ID)
}

// AddSong fetches metadata (when not supplied), applies the same
// duplicate-check + auto-start rules as the global interactor, persists
// the new queue, and returns the resulting queue plus the inserted
// song. Any active member may add.
//
// actorUserID and actorDisplayName are server-resolved from the bearer
// token by the delivery layer; request-body identity fields are ignored.
// The interactor stamps the constructed Song's AddedBy / AddedByID so
// the URL-only branch (no metadata body) also carries correct
// attribution.
func (i *Interactor) AddSong(ctx context.Context, slug string, actorUserID int, actorDisplayName string, url string, metadata *entity.SearchResult) (*entity.Queue, *entity.Song, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, nil, err
	}
	if err := i.requireMember(ctx, roomObj.ID, actorUserID); err != nil {
		return nil, nil, err
	}

	var song *entity.Song
	if metadata != nil {
		song = &entity.Song{
			ID:        metadata.ID,
			Title:     metadata.Title,
			Artist:    metadata.Artist,
			Duration:  metadata.Duration,
			Thumbnail: metadata.Thumbnail,
			URL:       metadata.URL,
			AddedBy:   metadata.AddedBy,
			AddedByID: metadata.AddedByID,
		}
	} else {
		if i.youtube == nil {
			return nil, nil, fmt.Errorf("metadata fetch unavailable: youtube service not configured")
		}
		fetched, err := i.youtube.FetchMetadata(ctx, url)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch metadata: %w", err)
		}
		song = fetched
	}
	// Server-resolved attribution is authoritative. Overwrite any
	// metadata-supplied AddedBy / AddedByID so a client cannot spoof a
	// host's add via the request body.
	song.AddedBy = actorDisplayName
	song.AddedByID = actorUserID

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, nil, err
	}
	if queue.ContainsSong(song.ID) {
		return nil, nil, entity.ErrSongAlreadyInQueue
	}
	queue.Add(*song)
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, nil, fmt.Errorf("save room queue: %w", err)
	}
	return queue, song, nil
}

// RemoveSong removes a song at the given index. Host/admin may remove
// any song; guests may remove only their own upcoming songs (CurrentIndex
// < index, AddedByID == actorUserID). Mirrors the global queue's
// permission rules so behavior is consistent.
func (i *Interactor) RemoveSong(ctx context.Context, slug string, actorUserID int, actorRoomRole entity.RoomMemberRole, index int) (*entity.Queue, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := i.requireMember(ctx, roomObj.ID, actorUserID); err != nil {
		return nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(queue.Songs) {
		return nil, ErrInvalidIndex
	}

	if actorRoomRole != entity.RoomRoleHost && actorRoomRole != entity.RoomRoleAdmin {
		if actorRoomRole == entity.RoomRoleGuest {
			song := queue.Songs[index]
			if song.AddedByID == 0 || song.AddedByID != actorUserID {
				return nil, ErrNotSongOwner
			}
			if index <= queue.CurrentIndex {
				return nil, ErrCannotRemoveSong
			}
		} else {
			return nil, ErrNotSongOwner
		}
	}

	if err := queue.Remove(index); err != nil {
		return nil, err
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, fmt.Errorf("save room queue: %w", err)
	}
	// R09f: trigger auto-queue when the removal leaves current==last.
	defer i.maybeCheckRoomAutoQueue(ctx, slug, queue)
	return queue, nil
}

// ClearQueue keeps the currently playing song and drops every upcoming
// song. Host/admin only.
func (i *Interactor) ClearQueue(ctx context.Context, slug string, actorUserID int, actorRoomRole entity.RoomMemberRole) (*entity.Queue, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := i.requireMember(ctx, roomObj.ID, actorUserID); err != nil {
		return nil, err
	}
	if actorRoomRole != entity.RoomRoleHost && actorRoomRole != entity.RoomRoleAdmin {
		return nil, room.ErrForbidden
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, err
	}
	queue.Clear()
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, fmt.Errorf("save room queue: %w", err)
	}
	// R09f: trigger auto-queue when the clear leaves current==last
	// (only-one-song-remaining case).
	defer i.maybeCheckRoomAutoQueue(ctx, slug, queue)
	return queue, nil
}

// PrioritizeSong moves a non-current song to the slot immediately after
// the currently-playing song. Host/admin only. Reuses entity.Queue.Prioritize
// for the underlying invariant (IsPrioritized stamp, current-index
// compensation). The caller (handler) is responsible for mapping errors to
// HTTP statuses; the use case returns the post-mutation queue plus the
// (fromIndex, toIndex, song) tuple the broadcaster needs to fan out the
// room_queue_song_prioritized event.
//
// Returns ErrInvalidIndex for out-of-range indexes, ErrCannotPrioritizeCurrent
// when the index matches the current song, and room.ErrForbidden for guests
// or non-privileged members. Caller must hold i.mu via the interactor — this
// method acquires it.
func (i *Interactor) PrioritizeSong(ctx context.Context, slug string, actorUserID int, actorRoomRole entity.RoomMemberRole, songIndex int) (*entity.Queue, int, int, entity.Song, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, 0, 0, entity.Song{}, err
	}
	if err := i.requireMember(ctx, roomObj.ID, actorUserID); err != nil {
		return nil, 0, 0, entity.Song{}, err
	}
	if actorRoomRole != entity.RoomRoleHost && actorRoomRole != entity.RoomRoleAdmin {
		return nil, 0, 0, entity.Song{}, room.ErrForbidden
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, 0, 0, entity.Song{}, err
	}
	if songIndex < 0 || songIndex >= len(queue.Songs) {
		return nil, 0, 0, entity.Song{}, ErrInvalidIndex
	}
	if songIndex == queue.CurrentIndex {
		return nil, 0, 0, entity.Song{}, ErrCannotPrioritizeCurrent
	}

	// Snapshot the song BEFORE the mutation only for the from-index
	// lookup; the broadcast payload must come from the post-mutation
	// slot so IsPrioritized=true is observed by subscribers.
	if err := queue.Prioritize(songIndex); err != nil {
		return nil, 0, 0, entity.Song{}, err
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, 0, 0, entity.Song{}, fmt.Errorf("save room queue: %w", err)
	}
	// The destination is always "immediately after the current song" per
	// the entity invariant. We recompute the post-mutation slot rather
	// than relying on a value the entity doesn't expose, so the
	// broadcaster's to_index is exact.
	toIndex := queue.CurrentIndex + 1
	// Defensive clamp: if CurrentIndex advanced past the last song during
	// the mutation, the slot is the last position in the array.
	if toIndex >= len(queue.Songs) {
		toIndex = len(queue.Songs) - 1
	}
	// R07d: return the post-mutation song (from queue.Songs[toIndex])
	// so the broadcaster payload carries IsPrioritized=true. The
	// pre-mutation snapshot used to leak the un-stamped copy and
	// tripped the test that asserts the broadcast song has
	// IsPrioritized=true.
	return queue, songIndex, toIndex, queue.Songs[toIndex], nil
}

// MemberRole returns the actor's room-scoped role. Returns ("", nil)
// when the actor is not a member (handlers map that to ErrForbidden).
func (i *Interactor) MemberRole(ctx context.Context, slug string, actorUserID int) (entity.RoomMemberRole, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return "", err
	}
	member, err := i.roomRepo.GetMember(ctx, roomObj.ID, actorUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get member: %w", err)
	}
	return member.Role, nil
}

// RoomBySlug resolves the slug to a room, returning the same sentinels
// as the rest of the package (ErrInvalidSlug / ErrRoomNotFound /
// ErrArchived). Exposed so external packages (e.g. roomvote) can
// resolve a slug to a room id without importing repository.
func (i *Interactor) RoomBySlug(ctx context.Context, slug string) (*entity.Room, error) {
	return i.resolveActiveRoom(ctx, slug)
}

// IsMember reports whether actorUserID is an active member of roomID.
// Exposed for external packages (roomvote). Returns false on any
// repository error other than the not-found sentinel.
func (i *Interactor) IsMember(ctx context.Context, roomID int64, actorUserID int) bool {
	_, err := i.roomRepo.GetMember(ctx, roomID, actorUserID)
	return err == nil
}

// GetStateByRoomID returns the persisted queue for an internal caller
// (no membership check). Used by the room WS hub's initial sync path;
// auth/authorization is enforced at the hub upgrade gate, not here.
func (i *Interactor) GetStateByRoomID(ctx context.Context, roomID int64) (*entity.Queue, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.loadQueue(ctx, roomID)
}

// SetLeaseAuthorizer wires the room.PlaybackLeaseAuthorizer used by
// the R09a playback mutations (SetPlaybackStatus / SyncPlaybackElapsed
// / SkipPlayback / PlaybackEnded). The concrete implementation lives
// in usecase/room.PlayerLeaseInteractor; cmd/server wires it via this
// setter. nil disables playback mutations — the methods return
// ErrPlaybackLeaseLost to make misconfiguration visible.
func (i *Interactor) SetLeaseAuthorizer(a room.PlaybackLeaseAuthorizer) { i.leaseAuthorizer = a }

// LeaseAuthorizer returns the wired lease authorizer, or nil when
// unset. Exposed for tests that need to seed / inspect the seam.
func (i *Interactor) LeaseAuthorizer() room.PlaybackLeaseAuthorizer { return i.leaseAuthorizer }

// SetRoomAutoQueueTrigger wires the per-room auto-queue trigger
// invoked after successful room queue mutations that leave
// "current is last" (R09f). nil disables the trigger path.
func (i *Interactor) SetRoomAutoQueueTrigger(t RoomAutoQueueTrigger) { i.autoQueueUC = t }

// maybeCheckRoomAutoQueue fires a non-blocking CheckAndTrigger when
// the post-mutation queue ends up at "current is last" and the seam
// is wired. Used after PlaybackEnded / SkipPlayback / SkipVote /
// RemoveSong / ClearQueue per the R09f trigger-point contract.
//
// The goroutine is intentionally fire-and-forget so the slow
// yt-dlp fetch inside CheckAndTrigger NEVER stalls the queue
// mutation's return path. Errors are logged at the trigger seam.
func (i *Interactor) maybeCheckRoomAutoQueue(ctx context.Context, slug string, q *entity.Queue) {
	if i.autoQueueUC == nil {
		return
	}
	if q == nil {
		return
	}
	if q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
		return
	}
	if q.CurrentIndex != len(q.Songs)-1 {
		return
	}
	// Snapshot slug defensively in case the caller's context carries a
	// tenant that differs from the wire slug we were given.
	go func() {
		if err := i.autoQueueUC.CheckAndTrigger(context.Background(), slug); err != nil {
			log.Printf("room auto-queue: CheckAndTrigger(%s): %v", slug, err)
		}
	}()
	_ = ctx
}

// requirePlaybackLease calls the lease authorizer seam and translates
// the room-package sentinels to the local roomqueue sentinels so the
// handler layer can switch on local symbols without a transitive
// usecase/room.Err* dependency. Returns nil when the caller is the
// active lease holder; otherwise returns a mapped error.
//
// Mapping:
//   room.ErrInvalidSlug        → room.ErrInvalidSlug     (handler → 400)
//   room.ErrRoomNotFound       → room.ErrRoomNotFound    (handler → 404)
//   room.ErrArchived           → room.ErrArchived        (handler → 409)
//   room.ErrPlayerLeaseNotFound → ErrPlaybackLeaseLost   (handler → 404)
//   room.ErrNotLeaseHolder     → ErrPlaybackForbidden    (handler → 403)
//   room.ErrPlayerLeaseGone    → ErrPlaybackLeaseGone    (handler → 410)
//   any other                  → wrapped error           (handler → 500)
func (i *Interactor) requirePlaybackLease(ctx context.Context, slug string, actorUserID int) error {
	if i.leaseAuthorizer == nil {
		return ErrPlaybackLeaseLost
	}
	if err := i.leaseAuthorizer.RequireActiveLeaseHolder(ctx, slug, actorUserID); err != nil {
		switch {
		case errors.Is(err, room.ErrPlayerLeaseNotFound):
			return ErrPlaybackLeaseLost
		case errors.Is(err, room.ErrNotLeaseHolder):
			return ErrPlaybackForbidden
		case errors.Is(err, room.ErrPlayerLeaseGone):
			return ErrPlaybackLeaseGone
		case errors.Is(err, room.ErrInvalidSlug),
			errors.Is(err, room.ErrRoomNotFound),
			errors.Is(err, room.ErrArchived):
			return err
		default:
			return fmt.Errorf("lease check: %w", err)
		}
	}
	return nil
}

// --- R09a playback use cases ---

// SetPlaybackStatus mutates the room-scoped queue's status field.
// Lease-holder only. The returned queue is the post-mutation snapshot
// used by the handler to broadcast room_playback_status_changed.
//
// Errors:
//   ErrInvalidStatus        → status not playing|paused (handler → 400)
//   ErrNoCurrentSong        → queue has no current song (handler → 400/404 per spec)
//   ErrInvalidSlug          → malformed slug (handler → 400)
//   ErrRoomNotFound         → unknown room (handler → 404)
//   ErrArchived             → room archived (handler → 409)
//   ErrPlaybackLeaseLost    → no active lease for the room (handler → 404)
//   ErrPlaybackForbidden    → lease held by another user (handler → 403)
//   ErrPlaybackLeaseGone    → lease past grace (handler → 410)
func (i *Interactor) SetPlaybackStatus(ctx context.Context, slug string, actorUserID int, status entity.PlaybackStatus) (*entity.Queue, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := i.requirePlaybackLease(ctx, slug, actorUserID); err != nil {
		return nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, err
	}
	if err := queue.SetStatus(status); err != nil {
		// Translate entity.ErrNoCurrentSong / entity.ErrInvalidStatus
		// to the local sentinels for handler-side mapping.
		if errors.Is(err, entity.ErrNoCurrentSong) {
			return nil, ErrNoCurrentSong
		}
		if errors.Is(err, entity.ErrInvalidStatus) {
			return nil, ErrInvalidStatus
		}
		return nil, err
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, fmt.Errorf("save room queue: %w", err)
	}
	return queue, nil
}

// SyncPlaybackElapsed mutates the room-scoped queue's elapsed field.
// Lease-holder only. The returned queue is the post-mutation snapshot
// used by the handler to broadcast room_playback_elapsed_sync.
//
// Errors:
//   ErrInvalidElapsed     → elapsed < 0 (handler → 400)
//   ErrNoCurrentSong      → queue has no current song (handler → 400/404 per spec)
//   ... lease sentinels (see SetPlaybackStatus)
func (i *Interactor) SyncPlaybackElapsed(ctx context.Context, slug string, actorUserID int, elapsed int) (*entity.Queue, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := i.requirePlaybackLease(ctx, slug, actorUserID); err != nil {
		return nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, err
	}
	if err := queue.SetElapsed(elapsed); err != nil {
		if errors.Is(err, entity.ErrInvalidElapsed) {
			return nil, ErrInvalidElapsed
		}
		if errors.Is(err, entity.ErrNoCurrentSong) {
			return nil, ErrNoCurrentSong
		}
		return nil, err
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, fmt.Errorf("save room queue: %w", err)
	}
	return queue, nil
}

// SkipPlayback advances the room-scoped queue to the next song.
// Lease-holder only. Returns the post-mutation queue plus the previous
// and new indexes; the handler fans these out as
// room_playback_song_advanced with reason="skip". On no-next-song
// returns ErrNoNextSong WITHOUT mutating state (the entity helper
// guards against partial mutation).
//
// Errors:
//   ErrNoNextSong         → no upcoming song (handler → 400/404 per spec)
//   ErrNoCurrentSong      → empty queue (handler → 400/404 per spec)
//   ... lease sentinels (see SetPlaybackStatus)
func (i *Interactor) SkipPlayback(ctx context.Context, slug string, actorUserID int) (*entity.Queue, int, int, *entity.Song, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	if err := i.requirePlaybackLease(ctx, slug, actorUserID); err != nil {
		return nil, 0, 0, nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	prevIdx, newSong, err := queue.AdvanceToNext()
	if err != nil {
		if errors.Is(err, entity.ErrNoNextSong) {
			return nil, 0, 0, nil, ErrNoNextSong
		}
		if errors.Is(err, entity.ErrNoCurrentSong) {
			return nil, 0, 0, nil, ErrNoCurrentSong
		}
		return nil, 0, 0, nil, err
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, 0, 0, nil, fmt.Errorf("save room queue: %w", err)
	}
	// R09f: trigger auto-queue only when the advance leaves current==last.
	defer i.maybeCheckRoomAutoQueue(ctx, slug, queue)
	return queue, prevIdx, queue.CurrentIndex, newSong, nil
}

// PlaybackEnded is the dual of SkipPlayback invoked when the current
// song finished naturally. It mirrors the global queue.ended path:
//   - if a next song exists, advance and broadcast
//     room_playback_song_advanced with reason="ended".
//   - if no next song exists, persist a paused end-of-queue state
//     (status=paused, elapsed=0) and broadcast
//     room_playback_status_changed. The previousIndex in the latter
//     case is queue.CurrentIndex (no advance happened); newIndex ==
//     previousIndex; currentSong is the final song's entity.
//
// Returns (queue, prevIndex, newIndex, currentSong, advanced, error)
// where advanced is true when a song advance happened and false when
// the queue was paused at end-of-queue.
//
// Errors:
//   ErrNoCurrentSong      → empty queue (handler → 400/404 per spec)
//   ... lease sentinels (see SetPlaybackStatus)
func (i *Interactor) PlaybackEnded(ctx context.Context, slug string, actorUserID int) (*entity.Queue, int, int, *entity.Song, bool, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, 0, 0, nil, false, err
	}
	if err := i.requirePlaybackLease(ctx, slug, actorUserID); err != nil {
		return nil, 0, 0, nil, false, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, 0, 0, nil, false, err
	}

	// Guard against no-current-song before any mutation (mirrors the
	// "queue not mutated" invariant from SkipPlayback).
	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		return nil, 0, 0, nil, false, ErrNoCurrentSong
	}

	// Capture the (final) current song before any potential mutation so
	// the end-of-queue broadcast payload can include it.
	currentSong := queue.Songs[queue.CurrentIndex]

	prevIdx, newSong, advanceErr := queue.AdvanceToNext()
	if advanceErr == nil {
		// Advanced — persist and report the advance.
		if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
			return nil, 0, 0, nil, false, fmt.Errorf("save room queue: %w", err)
		}
		// R09f: trigger auto-queue when the advance leaves current==last.
		defer i.maybeCheckRoomAutoQueue(ctx, slug, queue)
		return queue, prevIdx, queue.CurrentIndex, newSong, true, nil
	}
	if !errors.Is(advanceErr, entity.ErrNoNextSong) {
		// Translate entity sentinels to local sentinels.
		if errors.Is(advanceErr, entity.ErrNoCurrentSong) {
			return nil, 0, 0, nil, false, ErrNoCurrentSong
		}
		return nil, 0, 0, nil, false, advanceErr
	}

	// No next song. Pause at end-of-queue without mutating CurrentIndex.
	queue.Status = entity.StatusPaused
	queue.Elapsed = 0
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, 0, 0, nil, false, fmt.Errorf("save room queue: %w", err)
	}
	// R09f: end-of-queue pause leaves current==last; trigger auto-queue.
	defer i.maybeCheckRoomAutoQueue(ctx, slug, queue)
	return queue, queue.CurrentIndex, queue.CurrentIndex, &currentSong, false, nil
}

// --- R09b vote-driven skip ---

// SkipVote advances the room-scoped queue to the next song on behalf

// SkipVote advances the room-scoped queue to the next song on behalf
// of a winning vote session. Lease-bypassing — vote-to-skip is
// intentionally democratic. The caller (roomvote.Interactor) supplies
// the expectedSongID, which was captured at the moment the vote
// session was created. SkipVote refuses to mutate state if the
// current song has changed (lease-holder skip, PlaybackEnded, etc.),
// returning ErrStaleSkipVote WITHOUT mutating anything.
//
// Returns the post-mutation queue plus the previous and new indexes
// so the caller can dispatch room_playback_song_advanced with
// reason="skip".
//
// On no-next-song returns ErrNoNextSong without mutation (entity
// invariant). On no-current-song returns ErrNoCurrentSong without
// mutation.
func (i *Interactor) SkipVote(ctx context.Context, slug, expectedSongID string) (*entity.Queue, int, int, *entity.Song, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	// No-partial-mutation invariant #1: refuse to advance from an empty
	// queue. Translate the entity sentinel to the local one.
	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		return nil, 0, 0, nil, ErrNoCurrentSong
	}

	// No-partial-mutation invariant #2: refuse to advance if the
	// current song does not match what the vote session expected. This
	// prevents a stale session (lease-holder skipped or ended during
	// voting) from triggering a second advance.
	if queue.Songs[queue.CurrentIndex].ID != expectedSongID {
		return nil, 0, 0, nil, ErrStaleSkipVote
	}

	prevIdx, newSong, err := queue.AdvanceToNext()
	if err != nil {
		if errors.Is(err, entity.ErrNoNextSong) {
			return nil, 0, 0, nil, ErrNoNextSong
		}
		if errors.Is(err, entity.ErrNoCurrentSong) {
			return nil, 0, 0, nil, ErrNoCurrentSong
		}
		return nil, 0, 0, nil, err
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, 0, 0, nil, fmt.Errorf("save room queue: %w", err)
	}
	// R09f: trigger auto-queue when the vote-driven advance leaves
	// current==last.
	defer i.maybeCheckRoomAutoQueue(ctx, slug, queue)
	return queue, prevIdx, queue.CurrentIndex, newSong, nil
}

// --- R09h vote-driven prioritize ---

// PrioritizeVote moves a non-current upcoming song to the slot
// immediately after the currently-playing song on behalf of a winning
// prioritize vote session. Lease-bypassing and role-bypassing —
// vote-to-prioritize is intentionally democratic, mirroring vote-to-
// skip. The caller (roomvote.Interactor) supplies the
// (expectedSongID, expectedIndex) pair snapshotted when the vote
// session was created.
//
// It re-resolves the active room, acquires the queue mutation mutex,
// loads fresh state, and verifies that the stored snapshot index still
// identifies the same non-current target song. It rejects
// removed/moved/ambiguous/now-current targets as stale, returning
// ErrStalePrioritizeVote WITHOUT mutating anything. On success it calls
// the existing entity Queue.Prioritize, saves exactly once, and returns
// the authoritative (queue, fromIndex, toIndex, song) tuple the
// room_queue_song_prioritized broadcast needs. It does NOT broadcast
// and does NOT touch priority balances.
func (i *Interactor) PrioritizeVote(ctx context.Context, slug, expectedSongID string, expectedIndex int) (*entity.Queue, int, int, entity.Song, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, 0, 0, entity.Song{}, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, 0, 0, entity.Song{}, err
	}

	// Stale #1: no current song to prioritize relative to.
	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		return nil, 0, 0, entity.Song{}, ErrStalePrioritizeVote
	}
	// Stale #2: snapshot index out of range (song removed / queue shrank).
	if expectedIndex < 0 || expectedIndex >= len(queue.Songs) {
		return nil, 0, 0, entity.Song{}, ErrStalePrioritizeVote
	}
	// Stale #3: the song at the snapshot index is no longer the target
	// (moved / replaced).
	if queue.Songs[expectedIndex].ID != expectedSongID {
		return nil, 0, 0, entity.Song{}, ErrStalePrioritizeVote
	}
	// Stale #4: the target became the currently-playing song.
	if expectedIndex == queue.CurrentIndex {
		return nil, 0, 0, entity.Song{}, ErrStalePrioritizeVote
	}
	// Stale #5: ambiguous — the same song ID now appears more than once,
	// so the snapshot index no longer uniquely identifies the target.
	count := 0
	for idx := range queue.Songs {
		if queue.Songs[idx].ID == expectedSongID {
			count++
		}
	}
	if count != 1 {
		return nil, 0, 0, entity.Song{}, ErrStalePrioritizeVote
	}

	if err := queue.Prioritize(expectedIndex); err != nil {
		// The guards above already cover the entity rejections; translate
		// any residual index/current error to the stale sentinel so a race
		// never surfaces a raw entity error to the vote caller.
		return nil, 0, 0, entity.Song{}, ErrStalePrioritizeVote
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, 0, 0, entity.Song{}, fmt.Errorf("save room queue: %w", err)
	}
	// The destination is always "immediately after the current song" per
	// the entity invariant. Recompute the post-mutation slot so to_index
	// is exact and the payload carries IsPrioritized=true.
	toIndex := queue.CurrentIndex + 1
	if toIndex >= len(queue.Songs) {
		toIndex = len(queue.Songs) - 1
	}
	return queue, expectedIndex, toIndex, queue.Songs[toIndex], nil
}

// --- R09f per-room auto-queue ---

// AddRoomAutoQueueSong atomically revalidates an auto-queue candidate
// and inserts it only when:
//   - The current song's ID matches expectedSourceSongID (the song
//     whose play triggered the per-room radio fetch), AND
//   - No upcoming song exists (current is still the last in the
//     queue), AND
//   - The candidate is not already in the upcoming queue.
//
// When any of those predicates fails against a successfully-loaded
// queue, ErrRoomAutoQueueStale is returned with zero side effects
// (no save, no history append, no broadcast, no config touch). The
// caller (roomautoqueue.CheckAndTrigger) maps the sentinel to its
// own ErrAutoQueueStale at the wiring boundary.
//
// Repository load failures are surfaced as wrapped operational errors
// (NOT the stale sentinel) so the future roomautoqueue use case can
// distinguish a stale-but-valid state from a database failure.
//
// Mirrors the global queue.AddAutoQueueSong contract byte-for-byte
// (R04 / Sprint 004). Called by the roomautoqueue use case's
// AddRoomAutoQueueSongFunc seam so the insertion takes place under
// the roomqueue mutex — the slow yt-dlp fetch happens upstream
// OUTSIDE this lock.
//
// Returns the post-mutation snapshot tuple (queue, currentIndex,
// currentSong, status, elapsed, activity) — the broadcaster
// constructs the room_auto_queue_added event from this snapshot
// OUTSIDE the roomqueue mutex.
func (i *Interactor) AddRoomAutoQueueSong(ctx context.Context, slug string, song *entity.Song, expectedSourceSongID string) (*entity.Queue, int, *entity.Song, entity.PlaybackStatus, int, error) {
	if !entity.IsValidSlug(slug) {
		return nil, 0, nil, "", 0, room.ErrInvalidSlug
	}
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, 0, nil, "", 0, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		// Operational failure (DB down, schema corruption, etc.) —
		// surface it to the caller. Do NOT report as
		// ErrRoomAutoQueueStale, which is reserved for queue states
		// that loaded successfully but failed the staleness predicates.
		return nil, 0, nil, "", 0, fmt.Errorf("failed to load room queue for auto-queue revalidation: %w", err)
	}

	if queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		return nil, 0, nil, "", 0, ErrRoomAutoQueueStale
	}
	if queue.Songs[queue.CurrentIndex].ID != expectedSourceSongID {
		return nil, 0, nil, "", 0, ErrRoomAutoQueueStale
	}
	if queue.CurrentIndex != len(queue.Songs)-1 {
		return nil, 0, nil, "", 0, ErrRoomAutoQueueStale
	}
	if queue.ContainsSong(song.ID) {
		return nil, 0, nil, "", 0, ErrRoomAutoQueueStale
	}

	queue.Add(*song)
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, 0, nil, "", 0, fmt.Errorf("failed to save room queue after auto-queue insert: %w", err)
	}

	var currentSong *entity.Song
	if queue.CurrentIndex >= 0 && queue.CurrentIndex < len(queue.Songs) {
		s := queue.Songs[queue.CurrentIndex]
		currentSong = &s
	}
	return queue, queue.CurrentIndex, currentSong, queue.Status, queue.Elapsed, nil
}

// --- R09c volume command ---

// ChangePlaybackVolume is a lease-holder-only command that fires a
// single additive per-room WebSocket event without persisting any
// state. Direction must be exactly "up" or "down" (case-sensitive);
// any other value returns ErrInvalidDirection, which the handler
// maps to 400. The method enforces:
//   - active room (else room.ErrArchived / room.ErrRoomNotFound)
//   - active player-lease holder (else lease sentinels per R09a)
// It does NOT load or save the queue and does NOT require a current
// song. The returned error is nil on success so the handler can
// broadcast unconditionally.
//
// Errors:
//   ErrInvalidDirection  → direction != "up" and != "down" (handler → 400)
//   ErrInvalidSlug       → malformed slug (handler → 400)
//   ErrRoomNotFound      → unknown room (handler → 404)
//   ErrArchived          → archived room (handler → 409)
//   ErrPlaybackLeaseLost → no active lease (handler → 404)
//   ErrPlaybackForbidden → lease held by another user (handler → 403)
//   ErrPlaybackLeaseGone → lease past grace (handler → 410)
func (i *Interactor) ChangePlaybackVolume(ctx context.Context, slug string, actorUserID int, direction string) error {
	if direction != "up" && direction != "down" {
		return ErrInvalidDirection
	}
	// resolveActiveRoom exists purely to enforce active-room status; its
	// returned *entity.Room is unused here. The discard form
	// `_, err :=` makes the dependency obvious without a blank-identifier
	// assignment after the fact.
	if _, err := i.resolveActiveRoom(ctx, slug); err != nil {
		return err
	}
	if err := i.requirePlaybackLease(ctx, slug, actorUserID); err != nil {
		return err
	}
	return nil
}

// --- R09d prev command ---

// PrevPlayback moves the room-scoped queue from the current song to
// the previous song. Lease-holder only. Returns the post-mutation
// queue plus the previous and new indexes; the handler fans these out
// as room_playback_song_previous.
//
// The entity helper (Queue.PrevToPrevious) refuses to mutate state
// when the move is impossible, so PrevPlayback translates the entity
// sentinels into the local roomqueue ones for handler-side mapping.
//
// Errors:
//   ErrNoPreviousSong    → already on the first song (handler → 400)
//   ErrNoCurrentSong     → empty queue (handler → 400)
//   ErrInvalidSlug       → malformed slug (handler → 400)
//   ErrRoomNotFound      → unknown room (handler → 404)
//   ErrArchived          → archived room (handler → 409)
//   ErrPlaybackLeaseLost → no active lease (handler → 404)
//   ErrPlaybackForbidden → lease held by another user (handler → 403)
//   ErrPlaybackLeaseGone → lease past grace (handler → 410)
func (i *Interactor) PrevPlayback(ctx context.Context, slug string, actorUserID int) (*entity.Queue, int, int, *entity.Song, error) {
	roomObj, err := i.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	if err := i.requirePlaybackLease(ctx, slug, actorUserID); err != nil {
		return nil, 0, 0, nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	prevIdx, newSong, err := queue.PrevToPrevious()
	if err != nil {
		if errors.Is(err, entity.ErrNoPreviousSong) {
			return nil, 0, 0, nil, ErrNoPreviousSong
		}
		if errors.Is(err, entity.ErrNoCurrentSong) {
			return nil, 0, 0, nil, ErrNoCurrentSong
		}
		return nil, 0, 0, nil, err
	}
	if err := i.queueRepo.Save(ctx, roomObj.ID, queue); err != nil {
		return nil, 0, 0, nil, fmt.Errorf("save room queue: %w", err)
	}
	return queue, prevIdx, queue.CurrentIndex, newSong, nil
}
