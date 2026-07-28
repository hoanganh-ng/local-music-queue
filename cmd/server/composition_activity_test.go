package main

// R09i/R14c composition regression: false-mode server composition must
// select the explicit no-op room-activity repository for roomqueue,
// roomvote, and roomautoqueue, and must NOT select
// PostgresRoomActivityRepository (true-mode selection is pinned in
// cutover_mode_test.go). The check exercises the real composition path
// (not static source-text inspection) through
// setupAppWithActivityObserver — a local observer parameter, not
// package-level state — and reads the writer actually held by each
// interactor through the narrow ActivityWriterSeam accessors. Each
// invocation can observe only its own composition.

import (
	"os"
	"testing"

	"local-music-queue/internal/infrastructure/persistence"
	usecaseRoomAutoQueue "local-music-queue/internal/usecase/roomautoqueue"
	usecaseRoomQueue "local-music-queue/internal/usecase/roomqueue"
	usecaseRoomVote "local-music-queue/internal/usecase/roomvote"
)

// activityComposition holds what a single observer invocation saw. It is
// a test-local value; production code retains nothing after setup.
type activityComposition struct {
	roomQueue     *usecaseRoomQueue.Interactor
	roomVote      *usecaseRoomVote.Interactor
	roomAutoQueue *usecaseRoomAutoQueue.Interactor
	calls         int
}

// runObservedSetup runs the real composition path once with a local
// observer and the given R14c mode, and returns the interactors
// observed by exactly that invocation, plus the roomvote interactor
// setup returned.
func runObservedSetup(t *testing.T, opts setupOptions) (activityComposition, *usecaseRoomVote.Interactor) {
	t.Helper()

	var comp activityComposition
	_, _, _, _, roomVoteInteractor, cleanup, err := setupAppWithActivityObserver(
		opts,
		func(rq *usecaseRoomQueue.Interactor, rv *usecaseRoomVote.Interactor, raq *usecaseRoomAutoQueue.Interactor) {
			comp.roomQueue = rq
			comp.roomVote = rv
			comp.roomAutoQueue = raq
			comp.calls++
		},
	)
	if err != nil {
		t.Fatalf("setupAppWithActivityObserver: %v", err)
	}
	t.Cleanup(cleanup)

	if comp.calls != 1 {
		t.Fatalf("observer expected exactly 1 call for its own invocation, got %d", comp.calls)
	}
	if comp.roomQueue == nil || comp.roomVote == nil || comp.roomAutoQueue == nil {
		t.Fatal("observer did not receive the three activity-producing interactors")
	}
	return comp, roomVoteInteractor
}

func TestSetupApp_ComposesNoopRoomActivityWriter(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	if os.Getenv("YTDLP_PATH") == "" {
		os.Setenv("YTDLP_PATH", "/bin/true")
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	defer os.Unsetenv("DATABASE_URL")

	comp, roomVoteInteractor := runObservedSetup(t, setupOptions{})
	if comp.roomVote != roomVoteInteractor {
		t.Error("observed roomvote interactor is not the one setup returned")
	}

	writers := map[string]interface{}{
		"roomqueue":     comp.roomQueue.ActivityWriterSeam(),
		"roomvote":      comp.roomVote.ActivityWriterSeam(),
		"roomautoqueue": comp.roomAutoQueue.ActivityWriterSeam(),
	}
	var noop *persistence.NoopRoomActivityRepository
	for name, w := range writers {
		if w == nil {
			t.Errorf("%s: no activity writer composed", name)
			continue
		}
		if _, isPG := w.(*persistence.PostgresRoomActivityRepository); isPG {
			t.Errorf("%s: PostgresRoomActivityRepository must not be composed pre-R14c", name)
			continue
		}
		got, ok := w.(*persistence.NoopRoomActivityRepository)
		if !ok {
			t.Errorf("%s: expected *persistence.NoopRoomActivityRepository, got %T", name, w)
			continue
		}
		// All three interactors must share the single writer instance
		// this invocation constructed.
		if noop == nil {
			noop = got
		} else if got != noop {
			t.Errorf("%s: expected the same no-op writer instance across all producers", name)
		}
	}
}

// TestSetupApp_ObserverSeesOnlyOwnInvocation proves the seam retains no
// runtime state: two sequential setup invocations each observe a fresh
// composition, and neither can see the other's interactors or writer.
func TestSetupApp_ObserverSeesOnlyOwnInvocation(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	if os.Getenv("YTDLP_PATH") == "" {
		os.Setenv("YTDLP_PATH", "/bin/true")
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	defer os.Unsetenv("DATABASE_URL")

	first, _ := runObservedSetup(t, setupOptions{})
	second, _ := runObservedSetup(t, setupOptions{})

	if first.roomQueue == second.roomQueue ||
		first.roomVote == second.roomVote ||
		first.roomAutoQueue == second.roomAutoQueue {
		t.Error("repeated setup invocations must compose fresh interactors, not reuse a prior invocation's")
	}
	if first.calls != 1 || second.calls != 1 {
		t.Errorf("each observer must fire exactly once for its own invocation; got %d and %d", first.calls, second.calls)
	}

	firstWriter, okFirst := first.roomQueue.ActivityWriterSeam().(*persistence.NoopRoomActivityRepository)
	secondWriter, okSecond := second.roomQueue.ActivityWriterSeam().(*persistence.NoopRoomActivityRepository)
	if !okFirst || !okSecond {
		t.Fatalf("expected no-op writers in both invocations, got %T and %T",
			first.roomQueue.ActivityWriterSeam(), second.roomQueue.ActivityWriterSeam())
	}
	// NoopRoomActivityRepository is a zero-size struct, so Go may give
	// distinct allocations the same address; pointer inequality across
	// invocations is not a meaningful check. The stateless writer holds
	// nothing to contaminate — non-nil-ness plus the distinct-interactor
	// checks above are the cross-invocation isolation proof.
	if firstWriter == nil || secondWriter == nil {
		t.Error("both invocations must compose a non-nil no-op writer")
	}

	// A nil observer (the production setupApp path) must not disturb a
	// previously captured composition.
	_, _, _, _, _, cleanup, err := setupApp(setupOptions{})
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()
	if second.calls != 1 {
		t.Errorf("nil-observer invocation must not fire earlier observers; got %d calls", second.calls)
	}
}
