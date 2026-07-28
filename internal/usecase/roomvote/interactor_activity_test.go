package roomvote

// R09i focused tests: room-activity production for the vote flows.
// Covers the ordered batch contract (expired → cast → passed → action),
// the rejected-ballot / stale-pass / sweep rules, best-effort failure
// semantics, and the no-write-under-mutex lock rule.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
)

// captureActivityWriter records AddActivity calls in order. failCalls
// marks 1-based call indexes that return an error (the entry is still
// counted as attempted but not recorded). probe, when set, runs inside
// every AddActivity call (used for the lock-release check).
type captureActivityWriter struct {
	mu        sync.Mutex
	roomIDs   []int64
	acts      []entity.Activity
	failCalls map[int]bool
	calls     int
	probe     func()
}

func (w *captureActivityWriter) AddActivity(_ context.Context, roomID int64, act entity.Activity) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.probe != nil {
		w.probe()
	}
	if w.failCalls[w.calls] {
		return errors.New("simulated activity write failure")
	}
	w.roomIDs = append(w.roomIDs, roomID)
	w.acts = append(w.acts, act)
	return nil
}

func (w *captureActivityWriter) snapshot() []entity.Activity {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]entity.Activity(nil), w.acts...)
}

// seedRoomWithThreeSongs (three-song variant of seedRoomWithTwoSongs)
// already lives in interactor_test.go and is reused here so
// prioritize votes have a movable target at index 2.

func TestRoomVoteActivity_CastRecordsVoteCastWithDisplayName(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 7)
	fq.members[7] = map[int]bool{42: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, w)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast: %v", err)
	}
	acts := w.snapshot()
	if len(acts) != 1 {
		t.Fatalf("expected 1 activity, got %d: %+v", len(acts), acts)
	}
	if acts[0].Type != entity.ActivityVoteCast || acts[0].User != "Alice" {
		t.Errorf("expected vote_cast by Alice, got %+v", acts[0])
	}
	if acts[0].Description != `voted to skip "S1" (1/2)` {
		t.Errorf("unexpected description %q", acts[0].Description)
	}
	if len(w.roomIDs) != 1 || w.roomIDs[0] != 7 {
		t.Errorf("expected activity attributed to room 7, got %v", w.roomIDs)
	}
}

func TestRoomVoteActivity_BlankDisplayNameFallsBackToUserID(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, w)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "   "); err != nil {
		t.Fatalf("cast: %v", err)
	}
	acts := w.snapshot()
	if len(acts) != 1 || acts[0].User != "user #42" {
		t.Fatalf("expected actor fallback user #42, got %+v", acts)
	}
}

func TestRoomVoteActivity_SkipPassOrderCastPassedAction(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, w)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast 1: %v", err)
	}
	out, err := inter.CastSkipVote(context.Background(), "alpha", 43, "Bob")
	if err != nil {
		t.Fatalf("cast 2: %v", err)
	}
	if out.Resolution != "passed" {
		t.Fatalf("expected pass, got %q", out.Resolution)
	}
	acts := w.snapshot()
	if len(acts) != 4 {
		t.Fatalf("expected 4 activities, got %d: %+v", len(acts), acts)
	}
	wantTypes := []entity.ActivityType{
		entity.ActivityVoteCast, entity.ActivityVoteCast,
		entity.ActivityVotePassed, entity.ActivitySongSkipped,
	}
	for i, wt := range wantTypes {
		if acts[i].Type != wt {
			t.Errorf("act[%d].Type=%s want %s", i, acts[i].Type, wt)
		}
	}
	if acts[1].Description != `voted to skip "S1" (2/2)` || acts[1].User != "Bob" {
		t.Errorf("unexpected decisive cast %+v", acts[1])
	}
	if acts[2].User != "System" || acts[2].Description != `vote to skip "S1" passed` {
		t.Errorf("unexpected vote_passed %+v", acts[2])
	}
	if acts[3].User != "Bob" || acts[3].Description != `vote skipped "S1"` {
		t.Errorf("unexpected skip action %+v", acts[3])
	}
}

func TestRoomVoteActivity_PrioritizePassOrderCastPassedAction(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, w)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, "Alice"); err != nil {
		t.Fatalf("cast 1: %v", err)
	}
	out, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 43, "Bob")
	if err != nil {
		t.Fatalf("cast 2: %v", err)
	}
	if out.Resolution != "passed" {
		t.Fatalf("expected pass, got %q", out.Resolution)
	}
	acts := w.snapshot()
	if len(acts) != 4 {
		t.Fatalf("expected 4 activities, got %d: %+v", len(acts), acts)
	}
	if acts[2].Type != entity.ActivityVotePassed || acts[2].User != "System" ||
		acts[2].Description != `vote to prioritize "S3" passed` {
		t.Errorf("unexpected vote_passed %+v", acts[2])
	}
	if acts[3].Type != entity.ActivityPlayback || acts[3].User != "Bob" ||
		acts[3].Description != `vote prioritized "S3"` {
		t.Errorf("unexpected prioritize action %+v", acts[3])
	}
}

func TestRoomVoteActivity_RejectedBallotProducesNoActivity(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 3}}, 30*time.Second, w)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast: %v", err)
	}
	// Duplicate ballot: rejected, no activity of any type.
	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); !errors.Is(err, entity.ErrAlreadyVoted) {
		t.Fatalf("expected ErrAlreadyVoted, got %v", err)
	}
	// Non-member ballot: rejected, no activity.
	if _, err := inter.CastSkipVote(context.Background(), "alpha", 999, "Mallory"); err == nil {
		t.Fatal("expected non-member rejection")
	}
	acts := w.snapshot()
	if len(acts) != 1 || acts[0].Type != entity.ActivityVoteCast {
		t.Fatalf("expected exactly the first vote_cast, got %+v", acts)
	}
}

func TestRoomVoteActivity_EvictionRecordsExpiredBeforeReplacementCast(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 3}}, 30*time.Second, w)
	t0, clock := nowClock()
	inter.SetClock(clock)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast 1: %v", err)
	}
	// Advance past expiry so the next ballot evicts and replaces.
	inter.SetClock(func() time.Time { return t0.Add(31 * time.Second) })
	out, err := inter.CastSkipVote(context.Background(), "alpha", 43, "Bob")
	if err != nil {
		t.Fatalf("cast 2: %v", err)
	}
	if out.Resolution != "expired" {
		t.Fatalf("expected expired resolution, got %q", out.Resolution)
	}
	acts := w.snapshot()
	if len(acts) != 3 {
		t.Fatalf("expected 3 activities, got %d: %+v", len(acts), acts)
	}
	if acts[1].Type != entity.ActivityVoteExpired || acts[1].User != "System" ||
		acts[1].Description != `vote to skip "S1" expired` {
		t.Errorf("unexpected eviction vote_expired %+v", acts[1])
	}
	if acts[2].Type != entity.ActivityVoteCast || acts[2].User != "Bob" {
		t.Errorf("expected replacement vote_cast after expired, got %+v", acts[2])
	}
}

func TestRoomVoteActivity_StalePassKeepsCastOnly(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, w)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast 1: %v", err)
	}
	fq.mu.Lock()
	fq.staleOnNextSkip = true
	fq.mu.Unlock()
	if _, err := inter.CastSkipVote(context.Background(), "alpha", 43, "Bob"); !errors.Is(err, ErrStaleSession) {
		t.Fatalf("expected ErrStaleSession, got %v", err)
	}
	acts := w.snapshot()
	if len(acts) != 2 {
		t.Fatalf("expected 2 activities (casts only), got %d: %+v", len(acts), acts)
	}
	for i, a := range acts {
		if a.Type != entity.ActivityVoteCast {
			t.Errorf("act[%d] must be vote_cast on the stale branch, got %s", i, a.Type)
		}
	}
}

func TestRoomVoteActivity_SweepEmitsOneExpiredPerSession(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	seedRoomWithTwoSongs(t, fq, "beta", 2)
	fq.members[1] = map[int]bool{42: true}
	fq.members[2] = map[int]bool{42: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 3, "beta": 3}}, 30*time.Second, w)
	t0, clock := nowClock()
	inter.SetClock(clock)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast alpha: %v", err)
	}
	if _, err := inter.CastSkipVote(context.Background(), "beta", 42, "Alice"); err != nil {
		t.Fatalf("cast beta: %v", err)
	}
	w.mu.Lock()
	w.acts = nil
	w.roomIDs = nil
	w.mu.Unlock()

	inter.SetClock(func() time.Time { return t0.Add(31 * time.Second) })
	outs, err := inter.ExpireSessions(context.Background())
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("expected 2 expired outcomes, got %d", len(outs))
	}
	acts := w.snapshot()
	if len(acts) != 2 {
		t.Fatalf("expected exactly one vote_expired per session, got %d: %+v", len(acts), acts)
	}
	rooms := map[int64]bool{}
	for i, a := range acts {
		if a.Type != entity.ActivityVoteExpired || a.User != "System" {
			t.Errorf("act[%d] must be System vote_expired, got %+v", i, a)
		}
		if a.Description != `vote to skip "S1" expired` {
			t.Errorf("act[%d] unexpected description %q", i, a.Description)
		}
		rooms[w.roomIDs[i]] = true
	}
	if !rooms[1] || !rooms[2] {
		t.Errorf("expected one expiry per room {1,2}, got rooms %v", w.roomIDs)
	}
}

func TestRoomVoteActivity_WriteFailureContinuesBatchAndVoteSucceeds(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	// The decisive cast produces [cast, passed, action] as calls 2..4;
	// fail the vote_cast write (call 2) and require the rest to land.
	w := &captureActivityWriter{failCalls: map[int]bool{2: true}}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, w)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast 1: %v", err)
	}
	out, err := inter.CastSkipVote(context.Background(), "alpha", 43, "Bob")
	if err != nil {
		t.Fatalf("primary vote flow must not fail on activity errors: %v", err)
	}
	if out.Resolution != "passed" {
		t.Fatalf("expected pass, got %q", out.Resolution)
	}
	acts := w.snapshot()
	if w.calls != 4 {
		t.Errorf("expected 4 attempted writes, got %d", w.calls)
	}
	if len(acts) != 3 {
		t.Fatalf("expected 3 recorded activities after 1 failure, got %d: %+v", len(acts), acts)
	}
	if acts[1].Type != entity.ActivityVotePassed || acts[2].Type != entity.ActivitySongSkipped {
		t.Errorf("batch must continue past the failed item in order, got %+v", acts)
	}
}

func TestRoomVoteActivity_WritesRunAfterMutexReleased(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	w := &captureActivityWriter{}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, w)
	var lockedDuringWrite bool
	w.probe = func() {
		if inter.mu.TryLock() {
			inter.mu.Unlock()
		} else {
			lockedDuringWrite = true
		}
	}

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice"); err != nil {
		t.Fatalf("cast: %v", err)
	}
	if lockedDuringWrite {
		t.Error("AddActivity ran while the roomvote mutex was held")
	}
}

// blockingActivityWriter blocks inside AddActivity for the configured
// room until release is closed. entered is signalled exactly once when
// the blocked write begins. Writes for other rooms pass straight
// through to the embedded capture writer.
type blockingActivityWriter struct {
	captureActivityWriter
	blockRoomID int64
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (w *blockingActivityWriter) AddActivity(ctx context.Context, roomID int64, act entity.Activity) error {
	if roomID == w.blockRoomID {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	return w.captureActivityWriter.AddActivity(ctx, roomID, act)
}

// TestRoomVoteActivity_BlockedAppendDoesNotStallOtherRooms is the R09i
// corrective concurrency regression: a deliberately blocked activity
// append for room 1 must not prevent a vote in room 2 from progressing.
// Deterministic sequencing: the room-1 cast provably sits inside its
// blocked AddActivity (entered closed) while the room-2 cast must run
// to completion. If the interactor held the single
// roomvote mutex across the append — or serialized batches through a
// detached goroutine — the room-2 cast would hang and the guard timeout
// would fail the test. Writes within one batch stay sequential and
// synchronous on the mutating goroutine.
func TestRoomVoteActivity_BlockedAppendDoesNotStallOtherRooms(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	seedRoomWithTwoSongs(t, fq, "beta", 2)
	fq.members[1] = map[int]bool{42: true}
	fq.members[2] = map[int]bool{43: true}
	w := &blockingActivityWriter{
		blockRoomID: 1,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 3, "beta": 3}}, 30*time.Second, w)

	room1Done := make(chan error, 1)
	go func() {
		_, err := inter.CastSkipVote(context.Background(), "alpha", 42, "Alice")
		room1Done <- err
	}()

	// Wait until the room-1 append is provably blocked inside the
	// writer (after the interactor released its mutex).
	select {
	case <-w.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("room-1 activity append never started")
	}

	// With room 1 still blocked, a room-2 vote must complete. Run it
	// on a helper goroutine purely so a regression fails via timeout
	// instead of deadlocking the whole test binary.
	room2Done := make(chan error, 1)
	go func() {
		_, err := inter.CastSkipVote(context.Background(), "beta", 43, "Bob")
		room2Done <- err
	}()
	select {
	case err := <-room2Done:
		if err != nil {
			t.Fatalf("room-2 cast: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("room-2 vote stalled behind room-1's blocked activity append")
	}

	// Unblock room 1 and confirm its cast finishes cleanly too.
	close(w.release)
	select {
	case err := <-room1Done:
		if err != nil {
			t.Fatalf("room-1 cast: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("room-1 cast did not finish after release")
	}

	acts := w.snapshot()
	if len(acts) != 2 {
		t.Fatalf("expected 2 recorded activities, got %d: %+v", len(acts), acts)
	}
	// The room-2 write completed while room 1 was blocked, so it must
	// have been recorded first.
	if len(w.roomIDs) != 2 || w.roomIDs[0] != 2 || w.roomIDs[1] != 1 {
		t.Errorf("expected room-2 write before the released room-1 write, got %v", w.roomIDs)
	}
}
