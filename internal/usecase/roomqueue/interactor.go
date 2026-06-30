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

// GetStateByRoomID returns the persisted queue for an internal caller
// (no membership check). Used by the room WS hub's initial sync path;
// auth/authorization is enforced at the hub upgrade gate, not here.
func (i *Interactor) GetStateByRoomID(ctx context.Context, roomID int64) (*entity.Queue, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.loadQueue(ctx, roomID)
}
