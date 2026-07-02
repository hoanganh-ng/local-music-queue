package room

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

// sqlErrNoRows is a package-level handle for sql.ErrNoRows so tests can
// compare against it via errors.Is without importing database/sql in
// every helper.
var sqlErrNoRows = sql.ErrNoRows

// recordingBroadcaster is a thread-safe stub for RoomMembersBroadcaster.
// It captures every broadcast call so tests can assert on per-call
// arguments (target user, reason, members snapshot, etc.).
type recordingBroadcaster struct {
	mu             sync.Mutex
	archived       []string
	memberRemoved  []memberRemovedCall
	membersChanged []membersChangedCall
	closed         []memberRemovedCall
}

type memberRemovedCall struct {
	RoomSlug    string
	TargetUserID int
	Reason      string
}

type membersChangedCall struct {
	RoomSlug string
	Members  []entity.RoomMember
}

func (r *recordingBroadcaster) BroadcastRoomArchived(slug, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.archived = append(r.archived, slug+"|"+reason)
}

func (r *recordingBroadcaster) BroadcastRoomMemberRemoved(slug string, target int, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memberRemoved = append(r.memberRemoved, memberRemovedCall{slug, target, reason})
}

func (r *recordingBroadcaster) BroadcastRoomMembersChanged(slug string, members []entity.RoomMember) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]entity.RoomMember, len(members))
	copy(cp, members)
	r.membersChanged = append(r.membersChanged, membersChangedCall{slug, cp})
}

func (r *recordingBroadcaster) CloseRemovedClient(slug string, target int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = append(r.closed, memberRemovedCall{slug, target, ""})
}

// errIsNotFound returns sql.ErrNoRows so the tests can use errors.Is
// without importing database/sql at the top level.
func errIsNotFound() error {
	return sqlErrNoRows
}

func TestRoom_ArchiveRoomByHost_ActiveRoom_ReturnsTransitioned(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	transitioned, err := inter.ArchiveRoomByHost(ctx, "lounge", 1)
	if err != nil {
		t.Fatalf("ArchiveRoomByHost: %v", err)
	}
	if !transitioned {
		t.Errorf("expected transitioned=true on active room, got false")
	}
	r, _ := inter.Repo().GetRoomBySlug(ctx, "lounge")
	if r.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived, got %s", r.Status)
	}
	if room == nil {
		t.Errorf("expected non-nil room from CreateRoom, got nil")
	}
}

func TestRoom_ArchiveRoomByHost_AlreadyArchived_IdempotentNoTransition(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := inter.ArchiveRoomByHost(ctx, "lounge", 1); err != nil {
		t.Fatalf("first archive: %v", err)
	}
	transitioned, err := inter.ArchiveRoomByHost(ctx, "lounge", 1)
	if err != nil {
		t.Fatalf("second archive: %v", err)
	}
	if transitioned {
		t.Errorf("expected transitioned=false on already-archived room, got true")
	}
}

func TestRoom_ArchiveRoomByHost_NonHost_Forbidden(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := inter.ArchiveRoomByHost(ctx, "lounge", 2); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-host archive: expected ErrForbidden, got %v", err)
	}
}

func TestRoom_ArchiveRoomByHost_NotFound(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	if _, err := inter.ArchiveRoomByHost(context.Background(), "nope", 1); !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("missing room archive: expected ErrRoomNotFound, got %v", err)
	}
}

func TestRoom_ArchiveRoomByHost_InvalidSlug(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	if _, err := inter.ArchiveRoomByHost(context.Background(), "BAD", 1); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("invalid slug archive: expected ErrInvalidSlug, got %v", err)
	}
}

func TestRoom_RemoveMemberByHost_RemovesGuest(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	rec := &recordingBroadcaster{}
	inter.SetMembersBroadcaster(rec)
	ctx := context.Background()
	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}
	leaseEnded, err := inter.RemoveMemberByHost(ctx, "lounge", 1 /*actor=host*/, 2 /*target=guest*/)
	if err != nil {
		t.Fatalf("RemoveMemberByHost: %v", err)
	}
	if leaseEnded {
		t.Errorf("expected leaseEnded=false (guest was not lease holder), got true")
	}
	if _, err := inter.Repo().GetMember(ctx, room.ID, 2); !errors.Is(err, errIsNotFound()) {
		t.Errorf("expected guest member row deleted, got err=%v", err)
	}
	if len(rec.memberRemoved) != 1 || rec.memberRemoved[0].TargetUserID != 2 {
		t.Errorf("expected exactly one member-removed broadcast for target=2, got %+v", rec.memberRemoved)
	}
	if len(rec.membersChanged) != 1 {
		t.Errorf("expected exactly one members-changed broadcast, got %d", len(rec.membersChanged))
	}
	if got := len(rec.membersChanged[0].Members); got != 1 {
		t.Errorf("expected post-mutation snapshot to have 1 member (host), got %d", got)
	} else if rec.membersChanged[0].Members[0].UserID != 1 {
		t.Errorf("expected surviving member to be host (user 1), got %d", rec.membersChanged[0].Members[0].UserID)
	}
	if len(rec.closed) != 1 || rec.closed[0].TargetUserID != 2 {
		t.Errorf("expected exactly one CloseRemovedClient call for target=2, got %+v", rec.closed)
	}
}

func TestRoom_RemoveMemberByHost_RemovesAdmin(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleAdmin, time.Now()); err != nil {
		t.Fatalf("AddMember admin: %v", err)
	}
	if _, err := inter.RemoveMemberByHost(ctx, "lounge", 1, 2); err != nil {
		t.Fatalf("RemoveMemberByHost admin: %v", err)
	}
	if _, err := inter.Repo().GetMember(ctx, room.ID, 2); !errors.Is(err, errIsNotFound()) {
		t.Errorf("expected admin member row deleted, got err=%v", err)
	}
}

func TestRoom_RemoveMemberByHost_HostCannotRemoveSelf(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := inter.RemoveMemberByHost(ctx, "lounge", 1 /*actor*/, 1 /*target=self*/); !errors.Is(err, ErrHostCannotRemoveSelf) {
		t.Errorf("host-removes-self: expected ErrHostCannotRemoveSelf, got %v", err)
	}
}

func TestRoom_RemoveMemberByHost_CannotRemoveHost(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	// The DB enforces exactly-one-host per room (partial unique index),
	// so we cannot seed two hosts in one room. The ErrCannotRemoveHost
	// sentinel is the host-actor branch when target.Role == host AND
	// target != actor — which the invariant prevents in a single room.
	//
	// Pragmatic coverage: pin the non-host-actor branch (admin attempts
	// to remove the host → ErrForbidden) and rely on the production
	// code path for the sentinel itself.
	if room == nil {
		t.Fatalf("expected non-nil room from CreateRoom, got nil")
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleAdmin, time.Now()); err != nil {
		t.Fatalf("AddMember admin: %v", err)
	}
	if _, err := inter.RemoveMemberByHost(ctx, "lounge", 2 /*actor=admin*/, 1 /*target=host*/); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-host actor removing host: expected ErrForbidden, got %v", err)
	}
}

func TestRoom_RemoveMemberByHost_TargetNotMember_NotFound(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := inter.RemoveMemberByHost(ctx, "lounge", 1, 999); !errors.Is(err, ErrMemberNotFound) {
		t.Errorf("non-member target: expected ErrMemberNotFound, got %v", err)
	}
}

func TestRoom_RemoveMemberByHost_ArchivedRoom_ReturnsArchived(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}
	if err := inter.Repo().ArchiveRoom(ctx, room.ID, time.Now()); err != nil {
		t.Fatalf("ArchiveRoom: %v", err)
	}
	if _, err := inter.RemoveMemberByHost(ctx, "lounge", 1, 2); !errors.Is(err, ErrArchived) {
		t.Errorf("archived room removal: expected ErrArchived, got %v", err)
	}
}

func TestRoom_RemoveMemberByHost_EndsActiveLease_WhenTargetIsHolder(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, db, 60*time.Second, 30*time.Second)
	rec := &recordingBroadcaster{}
	inter.SetMembersBroadcaster(rec)
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}
	// User 2 claims the lease.
	if _, err := pi.leaseRepo.Claim(ctx, room.ID, 2, time.Now(), 60*time.Second); err != nil {
		t.Fatalf("seed lease claim: %v", err)
	}

	leaseEnded, err := inter.RemoveMemberByHost(ctx, "lounge", 1 /*actor=host*/, 2 /*target=lease-holder*/)
	if err != nil {
		t.Fatalf("RemoveMemberByHost: %v", err)
	}
	if !leaseEnded {
		t.Errorf("expected leaseEnded=true when target is lease holder, got false")
	}
	// Verify the lease row was actually ended (GetByRoom returns
	// sql.ErrNoRows for ended leases).
	if _, err := pi.leaseRepo.GetByRoom(ctx, room.ID); !errors.Is(err, errIsNotFound()) {
		t.Errorf("expected lease ended (GetByRoom returns ErrNoRows), got err=%v", err)
	}
}

func TestRoom_RemoveMemberByHost_TargetNotLeaseHolder_NoLeaseMutation(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	pi := NewPlayerLeaseInteractor(leaseRepo, inter.repo, db, 60*time.Second, 30*time.Second)
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}
	// User 1 (the host) holds the lease; user 2 (target) does not.
	if _, err := pi.leaseRepo.Claim(ctx, room.ID, 1, time.Now(), 60*time.Second); err != nil {
		t.Fatalf("seed lease claim: %v", err)
	}

	leaseEnded, err := inter.RemoveMemberByHost(ctx, "lounge", 1, 2)
	if err != nil {
		t.Fatalf("RemoveMemberByHost: %v", err)
	}
	if leaseEnded {
		t.Errorf("expected leaseEnded=false (target is not the lease holder), got true")
	}
	// Verify the lease row is still active.
	if _, err := pi.leaseRepo.GetByRoom(ctx, room.ID); err != nil {
		t.Errorf("expected lease still active after non-holder removal, got err=%v", err)
	}
}

// TestRoom_RemoveMember_ConcurrentDuplicateRemove_OneWins pins the
// race-sensitive contract: two concurrent removes on the same target
// produce exactly one success and one ErrMemberNotFound.
func TestRoom_RemoveMember_ConcurrentDuplicateRemove_OneWins(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()
	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := inter.RemoveMemberByHost(ctx, "lounge", 1, 2)
			results[idx] = err
		}(i)
	}
	wg.Wait()

	var success, notFound int
	for _, e := range results {
		switch {
		case e == nil:
			success++
		case errors.Is(e, ErrMemberNotFound):
			notFound++
		default:
			t.Errorf("unexpected error: %v", e)
		}
	}
	if success != 1 || notFound != 1 {
		t.Errorf("expected exactly one success and one ErrMemberNotFound, got success=%d notFound=%d", success, notFound)
	}
}