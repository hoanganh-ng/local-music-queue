package room

import (
	"context"
	"errors"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

func TestPlayerLease_Claim_RenewHeartbeat_Release_ArchivesRoom(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, db, 60*time.Second, 30*time.Second)
	ctx := context.Background()

	roomObj, err := inter.CreateRoom(ctx, "claim-room", "ClaimRoom", 42)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	l, err := pi.Claim(ctx, "claim-room", 42)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if l.ClaimedByUserID != 42 {
		t.Fatalf("expected holder 42, got %d", l.ClaimedByUserID)
	}

	// Duplicate claim within grace returns ErrPlayerLeaseExists.
	if _, err := pi.Claim(ctx, "claim-room", 42); !errors.Is(err, ErrPlayerLeaseExists) {
		t.Errorf("expected ErrPlayerLeaseExists, got %v", err)
	}

	// Admin attempt: seed an admin member (id=200). Admins cannot claim.
	_ = inter.Repo().AddMember(ctx, roomObj.ID, 200, entity.RoomRoleAdmin, time.Now())
	if _, err := pi.Claim(ctx, "claim-room", 200); !errors.Is(err, ErrPlayerLeaseForbidden) {
		t.Errorf("expected ErrPlayerLeaseForbidden for admin claim, got %v", err)
	}

	// Heartbeat by holder extends expires_at.
	pi.SetClock(func() time.Time { return time.Now().Add(10 * time.Second) })
	renewed, err := pi.Heartbeat(ctx, "claim-room", 42)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !renewed.ExpiresAt.After(l.ExpiresAt) {
		t.Errorf("expected expires_at to advance")
	}

	// Heartbeat by non-holder returns ErrNotLeaseHolder.
	if _, err := pi.Heartbeat(ctx, "claim-room", 200); !errors.Is(err, ErrNotLeaseHolder) {
		t.Errorf("expected ErrNotLeaseHolder, got %v", err)
	}

	// Release by host archives the room exactly once AND emits the
	// room_archived event with reason=explicit (R06 fix #2).
	ev, err := pi.Release(ctx, "claim-room", 42)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if ev == nil {
		t.Fatalf("expected a RoomArchivedEvent from Release, got nil")
	}
	if ev.RoomID != roomObj.ID {
		t.Errorf("expected event RoomID %d, got %d", roomObj.ID, ev.RoomID)
	}
	if ev.Reason != string(entity.PlayerLeaseExplicit) {
		t.Errorf("expected reason %q, got %q", entity.PlayerLeaseExplicit, ev.Reason)
	}
	roomAfter, _ := inter.Repo().GetRoomByID(ctx, roomObj.ID)
	if roomAfter.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived after release, got %s", roomAfter.Status)
	}
}

// TestPlayerLease_ReleaseWithoutLease_Returns404_NoArchive pins down fix
// #3: an explicit release on a room with no active lease must return
// ErrPlayerLeaseNotFound (HTTP 404) without archiving the room.
func TestPlayerLease_ReleaseWithoutLease_Returns404_NoArchive(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, db, 60*time.Second, 30*time.Second)
	ctx := context.Background()

	roomObj, err := inter.CreateRoom(ctx, "norel-room", "NoRel", 42)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	// No claim -> release must error and must NOT archive the room.
	ev, err := pi.Release(ctx, "norel-room", 42)
	if !errors.Is(err, ErrPlayerLeaseNotFound) {
		t.Fatalf("expected ErrPlayerLeaseNotFound, got err=%v ev=%v", err, ev)
	}
	if ev != nil {
		t.Errorf("expected nil event when no lease exists, got %+v", ev)
	}

	roomAfter, err := inter.Repo().GetRoomByID(ctx, roomObj.ID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if roomAfter.Status != entity.RoomStatusActive {
		t.Errorf("expected room to remain ACTIVE when no lease to release, got %s", roomAfter.Status)
	}
}

func TestPlayerLease_SweepExpired_ArchivesRoomOnce(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, db, 60*time.Second, 30*time.Second)
	ctx := context.Background()

	roomObj, _ := inter.CreateRoom(ctx, "sweep-room", "SweepRoom", 42)
	if _, err := pi.Claim(ctx, "sweep-room", 42); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Advance the clock past grace (91s > 60s lease + 30s grace).
	pi.SetClock(func() time.Time { return time.Now().Add(91 * time.Second) })
	events := pi.SweepExpired(ctx)
	if len(events) != 1 {
		t.Fatalf("expected 1 archive event, got %d", len(events))
	}
	if events[0].Reason != string(entity.PlayerLeaseExpired) {
		t.Errorf("expected player_lease_expired, got %s", events[0].Reason)
	}

	// Second sweep is idempotent (no double-archive, no duplicate event).
	events = pi.SweepExpired(ctx)
	if len(events) != 0 {
		t.Errorf("expected 0 events on second sweep, got %d", len(events))
	}

	roomAfter, _ := inter.Repo().GetRoomByID(ctx, roomObj.ID)
	if roomAfter.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived after sweep, got %s", roomAfter.Status)
	}
}