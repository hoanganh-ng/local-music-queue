package room

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// PlayerLeaseInteractor owns claim/heartbeat/release/get + the expiry
// sweeper. The lease state machine runs in-process against expires_at;
// SweepExpired is called periodically by the hub ticker.
type PlayerLeaseInteractor struct {
	leaseRepo     repository.PlayerLeaseRepository
	roomRepo      repository.RoomRepository
	db            *sql.DB
	leaseDuration time.Duration
	grace         time.Duration
	now           func() time.Time
}

// NewPlayerLeaseInteractor constructs the interactor. db is currently
// unused by the impl but kept on the struct for symmetry with R04 and to
// give tests a direct handle.
func NewPlayerLeaseInteractor(leaseRepo repository.PlayerLeaseRepository, roomRepo repository.RoomRepository, db *sql.DB, leaseDuration, grace time.Duration) *PlayerLeaseInteractor {
	return &PlayerLeaseInteractor{
		leaseRepo:     leaseRepo,
		roomRepo:      roomRepo,
		db:            db,
		leaseDuration: leaseDuration,
		grace:         grace,
		now:           time.Now,
	}
}

// SetClock swaps the time source (tests only).
func (p *PlayerLeaseInteractor) SetClock(now func() time.Time) { p.now = now }

// Claim creates or re-creates the active lease for a room.
func (p *PlayerLeaseInteractor) Claim(ctx context.Context, slug string, actorUserID int) (*entity.PlayerLease, error) {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := p.requireHost(ctx, room.ID, actorUserID); err != nil {
		return nil, ErrPlayerLeaseForbidden
	}
	lease, err := p.leaseRepo.Claim(ctx, room.ID, actorUserID, p.now(), p.leaseDuration)
	if err != nil {
		if errors.Is(err, repository.ErrPlayerLeaseExists) {
			return nil, ErrPlayerLeaseExists
		}
		return nil, fmt.Errorf("claim lease: %w", err)
	}
	return lease, nil
}

// Heartbeat renews the lease when called by the current holder.
func (p *PlayerLeaseInteractor) Heartbeat(ctx context.Context, slug string, actorUserID int) (*entity.PlayerLease, error) {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	now := p.now()
	current, err := p.leaseRepo.GetByRoom(ctx, room.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseNotFound
		}
		return nil, fmt.Errorf("get lease: %w", err)
	}
	if current.ClaimedByUserID != actorUserID {
		return nil, ErrNotLeaseHolder
	}
	if !current.IsWithinGrace(now, p.grace) {
		return nil, ErrPlayerLeaseGone
	}
	renewed, err := p.leaseRepo.HeartbeatByHolder(ctx, room.ID, actorUserID, now, p.leaseDuration)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseNotFound
		}
		return nil, fmt.Errorf("heartbeat: %w", err)
	}
	return renewed, nil
}

// Release ends the active lease and archives the room exactly once.
// If there is no active lease for the room, it returns ErrPlayerLeaseNotFound
// without touching the room state (no archive, no event). When the release
// actually archives a room, a *RoomArchivedEvent is returned so the caller
// (delivery/http) can map it to the ws room_archived broadcast.
//
// The interactor itself does NOT broadcast; it returns the event. Wiring
// it through a broadcaster keeps usecase/room independent of delivery/ws.
func (p *PlayerLeaseInteractor) Release(ctx context.Context, slug string, actorUserID int) (*RoomArchivedEvent, error) {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := p.requireHost(ctx, room.ID, actorUserID); err != nil {
		return nil, ErrPlayerLeaseForbidden
	}
	now := p.now()
	if _, err := p.leaseRepo.EndLease(ctx, room.ID, now); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No active lease — nothing to release, nothing to archive.
			return nil, ErrPlayerLeaseNotFound
		}
		return nil, fmt.Errorf("end lease: %w", err)
	}
	archived, err := p.roomRepo.ArchiveRoomIfActive(ctx, room.ID, now)
	if err != nil {
		return nil, fmt.Errorf("archive room: %w", err)
	}
	if !archived {
		// Lease ended but the room was already archived by some other path
		// (e.g. sweeper). No event to broadcast.
		return nil, nil
	}
	return &RoomArchivedEvent{
		RoomID:     room.ID,
		Reason:     string(entity.PlayerLeaseExplicit),
		ArchivedAt: now,
	}, nil
}

// GetLease returns the active lease for a room; any active member may read.
func (p *PlayerLeaseInteractor) GetLease(ctx context.Context, slug string, actorUserID int) (*entity.PlayerLease, error) {
	room, err := p.resolveActiveRoom(ctx, slug)
	if err != nil {
		return nil, err
	}
	if _, err := p.roomRepo.GetMember(ctx, room.ID, actorUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseForbidden
		}
		return nil, fmt.Errorf("get member: %w", err)
	}
	l, err := p.leaseRepo.GetByRoom(ctx, room.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlayerLeaseNotFound
		}
		return nil, fmt.Errorf("get lease: %w", err)
	}
	return l, nil
}

// SweepExpired ends leases past grace, archives their rooms exactly once,
// and returns the resulting archive events for the hub to broadcast.
func (p *PlayerLeaseInteractor) SweepExpired(ctx context.Context) []RoomArchivedEvent {
	now := p.now()
	leases, err := p.leaseRepo.ListActive(ctx, now)
	if err != nil {
		return nil
	}
	var out []RoomArchivedEvent
	for _, l := range leases {
		if l.IsWithinGrace(now, p.grace) {
			continue
		}
		// End the lease first; the partial unique index allows re-claim after end.
		if _, err := p.leaseRepo.EndLease(ctx, l.RoomID, now); err != nil {
			continue
		}
		archived, err := p.roomRepo.ArchiveRoomIfActive(ctx, l.RoomID, now)
		if err != nil || !archived {
			continue
		}
		out = append(out, RoomArchivedEvent{
			RoomID:     l.RoomID,
			Reason:     string(entity.PlayerLeaseExpired),
			ArchivedAt: now,
		})
	}
	return out
}

// resolveActiveRoom validates slug + room existence + active status.
func (p *PlayerLeaseInteractor) resolveActiveRoom(ctx context.Context, slug string) (*entity.Room, error) {
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	room, err := p.roomRepo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}
	return room, nil
}

// requireHost ensures the actor is the host member of the room.
func (p *PlayerLeaseInteractor) requireHost(ctx context.Context, roomID int64, actorUserID int) error {
	member, err := p.roomRepo.GetMember(ctx, roomID, actorUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPlayerLeaseForbidden
		}
		return fmt.Errorf("get member: %w", err)
	}
	if member.Role != entity.RoomRoleHost {
		return ErrPlayerLeaseForbidden
	}
	return nil
}