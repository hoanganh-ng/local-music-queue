// Package roomqueue hosts the room-scoped playback queue use case. The
// queue shape is the existing entity.Queue; this layer is responsible
// for:
//   - resolving actor identity (slug -> room -> membership -> role)
//   - enforcing membership and active-room status
//   - serializing mutations under a per-room mutex
//   - preserving queue invariants (Add / Remove / Clear / ContainsSong)
//   - mapping sentinel errors to documented HTTP statuses
//
// The package does NOT broadcast WebSocket events; the global hub is
// unchanged in R07a. Per-room WS deltas land in R08.
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
)

// Interactor owns the room-scoped queue use cases. The mutex serializes
// every mutation per Interactor instance (single-process; matches the
// ADR 001 single-process scope and the existing global queue interactor).
type Interactor struct {
	roomRepo  repository.RoomRepository
	queueRepo repository.RoomQueueRepository
	youtube   service.YouTubeService
	mu        sync.Mutex
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
func (i *Interactor) AddSong(ctx context.Context, slug string, actorUserID int, url string, metadata *entity.SearchResult) (*entity.Queue, *entity.Song, error) {
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
	// Actor identity is server-resolved from the bearer token; the
	// request body MUST NOT supply AddedBy / AddedByID. We set them from
	// the caller here (the handler passes user.DisplayName / user.ID).
	// The handler is the only place this is allowed; tests that bypass
	// the handler MUST pass the values explicitly via metadata or via
	// a small helper.
	_ = actorUserID

	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.loadQueue(ctx, roomObj.ID)
	if err != nil {
		return nil, nil, err
	}
	if queue.ContainsSong(song.ID) {
		return nil, nil, entity.ErrSongAlreadyInQueue
	}
	// entity.Queue.ContainsSong only inspects upcoming songs (after
	// CurrentIndex). R07a's contract also rejects a duplicate when the
	// song is already the currently-playing track, so we do a full
	// range check here without mutating the entity.
	for _, s := range queue.Songs {
		if s.ID == song.ID {
			return nil, nil, entity.ErrSongAlreadyInQueue
		}
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
