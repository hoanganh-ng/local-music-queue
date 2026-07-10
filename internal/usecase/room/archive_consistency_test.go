package room

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

// These tests pin the R10b narrow fix for host archive + active lease
// consistency. The contract:
//
//   - active room + active lease → host archive transitions the room
//     and ends the lease, atomically. transitioned=true, leaseEnded=true.
//   - active room + no lease → host archive transitions the room without
//     error. transitioned=true, leaseEnded=false.
//   - already-archived room + no lease → idempotent no-op.
//     transitioned=false, leaseEnded=false.
//   - already-archived room + stale active lease → idempotent no-op;
//     the lease is NOT mutated by the archive call.
//
// The archive transition and the lease end run inside one DB
// transaction owned by the repository, so the lease end is atomic
// with the room transition for leases visible to the archive
// transaction. The race-test below is informational only — it does
// NOT assert the no-active-lease postcondition, because the lease
// Claim path is not tightened against concurrent archive commits in
// this R10b patch (full archive-vs-claim serialization is deferred
// to a future lease-hardening sprint).

func TestRoom_ArchiveRoomByHost_ActiveRoom_ActiveLease_TransitionsAndEndsLeaseAtomically(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}
	// Host claims the lease so we have an active lease on the active room.
	if _, err := leaseRepo.Claim(ctx, room.ID, 1, time.Now(), 60*time.Second); err != nil {
		t.Fatalf("seed lease claim: %v", err)
	}

	transitioned, err := inter.ArchiveRoomByHost(ctx, "lounge", 1)
	if err != nil {
		t.Fatalf("ArchiveRoomByHost: %v", err)
	}
	if !transitioned {
		t.Errorf("expected transitioned=true on active room, got false")
	}

	// After a successful archive, no active lease may remain. The atomic
	// operation must have ended the lease in the same transaction as the
	// room transition.
	if _, err := leaseRepo.GetByRoom(ctx, room.ID); !errors.Is(err, errIsNotFound()) {
		t.Errorf("expected active lease ended (GetByRoom returns ErrNoRows), got err=%v", err)
	}
}

func TestRoom_ArchiveRoomByHost_ActiveRoom_NoLease_TransitionsWithoutError(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	transitioned, err := inter.ArchiveRoomByHost(ctx, "lounge", 1)
	if err != nil {
		t.Fatalf("ArchiveRoomByHost (no lease): %v", err)
	}
	if !transitioned {
		t.Errorf("expected transitioned=true on active room with no lease, got false")
	}
	r, _ := inter.Repo().GetRoomBySlug(ctx, "lounge")
	if r.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived, got %s", r.Status)
	}
}

func TestRoom_ArchiveRoomByHost_AlreadyArchived_NoLease_IdempotentNoOp(t *testing.T) {
	inter, cleanup := pgInter(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	// Pre-archive so the second call is the "already archived" case.
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

func TestRoom_ArchiveRoomByHost_AlreadyArchived_StaleActiveLease_NoLeaseMutation(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	// Pre-archive the room with no lease so the room is archived before
	// we add the "stale" lease.
	if _, err := inter.ArchiveRoomByHost(ctx, "lounge", 1); err != nil {
		t.Fatalf("pre-archive: %v", err)
	}
	// Now insert a stale active lease directly into the DB so we can
	// pin the idempotent contract: the archive call must NOT mutate it.
	now := time.Now()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO player_leases (room_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at)
		 VALUES ($1, $2, $3, $3, $4)`,
		room.ID, 1, now, now.Add(60*time.Second)); err != nil {
		t.Fatalf("seed stale lease: %v", err)
	}
	before, err := leaseRepo.GetByRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("seed lease lookup: %v", err)
	}

	transitioned, err := inter.ArchiveRoomByHost(ctx, "lounge", 1)
	if err != nil {
		t.Fatalf("ArchiveRoomByHost (archived room, stale lease): %v", err)
	}
	if transitioned {
		t.Errorf("expected transitioned=false on already-archived room, got true")
	}

	// Idempotent contract: no lease mutation when the room is already
	// archived. ended_at must still be NULL on the stale lease row.
	after, err := leaseRepo.GetByRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("after lookup: %v", err)
	}
	if after.EndedAt != nil {
		t.Errorf("expected lease still active (no mutation on archived-room path), got ended_at=%v", after.EndedAt)
	}
	if before.ClaimedAt != after.ClaimedAt {
		t.Errorf("lease claimed_at must be unchanged")
	}
}

// TestRoom_ArchiveIfActiveAndEndLease_RepositorySerializationCoverage
// pins the R10b atomicity contract at the repository level. The
// contract is: when ArchiveRoomIfActiveAndEndLease commits, the room
// transition and the active-lease end are observed together by any
// reader after commit. The atomic operation runs both writes in one
// transaction, so a lease that was active when the archive tx
// started is ended by the time the tx commits.
//
// This is the repository-level serialization coverage the user
// explicitly accepted as an alternative to a flaky timing-dependent
// race test: it deterministically exercises the archive operation
// after a claim has landed, asserts the atomic-op return values,
// and confirms no active lease remains for THIS room (no concurrent
// claim goroutine is racing; this is a single-threaded test of the
// atomic seam).
func TestRoom_ArchiveIfActiveAndEndLease_RepositorySerializationCoverage(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}

	// Land a claim first so there IS an active lease to be ended.
	if _, err := leaseRepo.Claim(ctx, room.ID, 1, time.Now(), 60*time.Second); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	// Now invoke the atomic archive. The claim row must be ended
	// inside the same tx as the room transition.
	transitioned, leaseEnded, err := inter.Repo().ArchiveRoomIfActiveAndEndLease(ctx, room.ID, time.Now())
	if err != nil {
		t.Fatalf("ArchiveRoomIfActiveAndEndLease: %v", err)
	}
	if !transitioned {
		t.Errorf("expected transitioned=true on active room, got false")
	}
	if !leaseEnded {
		t.Errorf("expected leaseEnded=true when an active lease was present, got false")
	}
	// Post-condition: no active lease remains.
	if _, err := leaseRepo.GetByRoom(ctx, room.ID); !errors.Is(err, errIsNotFound()) {
		t.Errorf("expected no active lease after atomic archive, got err=%v", err)
	}

	// Re-call: already-archived, no mutation. The stale-active-lease
	// test above covers that case; here we just confirm idempotence.
	transitioned, leaseEnded, err = inter.Repo().ArchiveRoomIfActiveAndEndLease(ctx, room.ID, time.Now())
	if err != nil {
		t.Fatalf("ArchiveRoomIfActiveAndEndLease (idempotent): %v", err)
	}
	if transitioned {
		t.Errorf("expected transitioned=false on already-archived room, got true")
	}
	if leaseEnded {
		t.Errorf("expected leaseEnded=false on already-archived room, got true")
	}
}

// TestRoom_ArchiveRoomByHost_RaceVsClaim_NoOpCrash is an
// informational sanity test for concurrent archive + claim
// goroutines. It runs the paths under -race so any data race in the
// concurrent interleaving surfaces. It only asserts the post-state
// the R10b patch is responsible for: the room ends archived.
// Full archive-vs-claim serialization (no active lease on the
// archived room) is NOT asserted here — that invariant is deferred
// to a future lease-hardening sprint. The behavior-pinning tests
// above (atomic transition + lease end; idempotent no-op on
// already-archived + stale lease) remain authoritative for the
// scope of this R10b patch.
func TestRoom_ArchiveRoomByHost_RaceVsClaim_NoOpCrash(t *testing.T) {
	inter, db, cleanup := pgInterWithDB(t)
	defer cleanup()
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	ctx := context.Background()

	room, err := inter.CreateRoom(ctx, "lounge", "Lounge", 1)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := inter.Repo().AddMember(ctx, room.ID, 2, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}

	const N = 16
	var wg sync.WaitGroup
	wg.Add(2 * N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, _ = inter.ArchiveRoomByHost(ctx, "lounge", 1)
		}()
		go func() {
			defer wg.Done()
			pi := NewPlayerLeaseInteractor(leaseRepo, inter.Repo(), db, 60*time.Second, 30*time.Second)
			_, _ = pi.Claim(ctx, "lounge", 1)
		}()
	}
	wg.Wait()

	// The R10b patch guarantees: the room ends archived (every
	// concurrent archive that lands converges the room to archived
	// status). Per-call outcomes of the interleaved Claim calls are
	// not asserted — Claim may succeed or fail depending on timing,
	// and the lease-row post-state is intentionally not asserted.
	r, err := inter.Repo().GetRoomBySlug(ctx, "lounge")
	if err != nil {
		t.Fatalf("GetRoomBySlug: %v", err)
	}
	if r.Status != entity.RoomStatusArchived {
		t.Errorf("expected room archived after concurrent archives, got %s", r.Status)
	}
	_ = leaseRepo
}