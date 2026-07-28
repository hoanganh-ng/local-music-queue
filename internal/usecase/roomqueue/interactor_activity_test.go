package roomqueue

// R09i focused tests: room-activity production for the roomqueue
// mutation surface. Covers the activity matrix rows owned by this
// package, the actor-name fallback, non-events, best-effort failure
// semantics, and the no-write-under-mutex lock rule. PG-backed like
// the rest of this package's tests (skips when PG is unreachable).

import (
	"context"
	"errors"
	"sync"
	"testing"

	"local-music-queue/internal/domain/entity"
)

// captureActivityWriter records AddActivity calls in order. err (when
// set) is returned by every call. probe, when set, runs inside every
// AddActivity call (used for the lock-release check).
type captureActivityWriter struct {
	mu      sync.Mutex
	roomIDs []int64
	acts    []entity.Activity
	err     error
	probe   func()
}

func (w *captureActivityWriter) AddActivity(_ context.Context, roomID int64, act entity.Activity) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.probe != nil {
		w.probe()
	}
	if w.err != nil {
		return w.err
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

func (w *captureActivityWriter) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.acts = nil
	w.roomIDs = nil
}

// requireSingleActivity asserts exactly one recorded activity and
// returns it alongside the attributed room id.
func requireSingleActivity(t *testing.T, w *captureActivityWriter) (entity.Activity, int64) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.acts) != 1 {
		t.Fatalf("expected exactly 1 activity, got %d: %+v", len(w.acts), w.acts)
	}
	return w.acts[0], w.roomIDs[0]
}

func TestRoomQueueActivity_AddSongRecordsSongAdded(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rqa-add", "ActAdd", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	w := &captureActivityWriter{}
	inter.activityWriter = w

	if _, _, err := inter.AddSong(ctx, "rqa-add", 42, "Alice", "Alice", "", &entity.SearchResult{ID: "v1", Title: "T1", URL: "https://x/1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	a, roomID := requireSingleActivity(t, w)
	if a.Type != entity.ActivitySongAdded || a.User != "Alice" {
		t.Errorf("expected song_added by Alice, got %+v", a)
	}
	if a.Description != `added "T1"` {
		t.Errorf("unexpected description %q", a.Description)
	}
	if want := mustRoomID(t, inter, "rqa-add"); roomID != want {
		t.Errorf("expected room %d, got %d", want, roomID)
	}
}

func TestRoomQueueActivity_BlankDisplayNameFallsBackToUserID(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rqa-fallback", "ActFallback", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	w := &captureActivityWriter{}
	inter.activityWriter = w

	// addedByName carries the legacy email fallback while the raw
	// activity display name is blank: the actor must be the canonical
	// "user #42", never the email-derived AddedBy string.
	q, _, err := inter.AddSong(ctx, "rqa-fallback", 42, "fallback@example.com", "   ", "", &entity.SearchResult{ID: "v1", Title: "T1", URL: "https://x/1"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.User != "user #42" {
		t.Errorf("expected actor fallback user #42, got %q", a.User)
	}
	// Song attribution keeps the legacy fallback contract untouched.
	if got := q.Songs[len(q.Songs)-1].AddedBy; got != "fallback@example.com" {
		t.Errorf("expected AddedBy to keep the legacy fallback, got %q", got)
	}
}

func TestRoomQueueActivity_RemoveSongRecordsRemoval(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rqa-remove")
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	if _, err := inter.RemoveSong(ctx, "rqa-remove", 42, "Alice", entity.RoomRoleHost, 1); err != nil {
		t.Fatalf("remove: %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.Type != entity.ActivityPlayback || a.User != "Alice" {
		t.Errorf("expected playback_changed by Alice, got %+v", a)
	}
	if a.Description != `removed "Up1" from queue` {
		t.Errorf("unexpected description %q", a.Description)
	}
}

func TestRoomQueueActivity_ClearQueueRecordsCleared(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rqa-clear")
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	if _, err := inter.ClearQueue(ctx, "rqa-clear", 42, "Alice", entity.RoomRoleHost); err != nil {
		t.Fatalf("clear: %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.Type != entity.ActivityPlayback || a.Description != "cleared the queue" {
		t.Errorf("unexpected clear activity %+v", a)
	}
}

func TestRoomQueueActivity_PrioritizeSongRecordsPrioritized(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rqa-prio")
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	if _, _, _, _, err := inter.PrioritizeSong(ctx, "rqa-prio", 42, "Alice", entity.RoomRoleHost, 2); err != nil {
		t.Fatalf("prioritize: %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.Type != entity.ActivityPlayback || a.Description != `prioritized "Up2"` {
		t.Errorf("unexpected prioritize activity %+v", a)
	}
}

func TestRoomQueueActivity_SetPlaybackStatusRecordsStatusChange(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rqa-status", 42)
	defer cleanup()
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	if _, err := inter.SetPlaybackStatus(ctx, "rqa-status", 42, "Alice", entity.StatusPaused); err != nil {
		t.Fatalf("set status: %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.Type != entity.ActivityPlayback || a.User != "Alice" {
		t.Errorf("expected playback_changed by Alice, got %+v", a)
	}
	if a.Description != "changed status to paused" {
		t.Errorf("unexpected description %q", a.Description)
	}
}

func TestRoomQueueActivity_SkipRecordsSongSkipped(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rqa-skip", 42)
	defer cleanup()
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	if _, _, _, _, err := inter.SkipPlayback(ctx, "rqa-skip", 42, "Alice"); err != nil {
		t.Fatalf("skip: %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.Type != entity.ActivitySongSkipped || a.User != "Alice" {
		t.Errorf("expected song_skipped by Alice, got %+v", a)
	}
	if a.Description != "skipped the current song" {
		t.Errorf("unexpected description %q", a.Description)
	}
}

func TestRoomQueueActivity_PrevRecordsWentToPrevious(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rqa-prev", 42)
	defer cleanup()
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	// Advance to index 1 first so prev has somewhere to go, then
	// discard the skip's own activity.
	if _, _, _, _, err := inter.SkipPlayback(ctx, "rqa-prev", 42, "Alice"); err != nil {
		t.Fatalf("skip: %v", err)
	}
	w.reset()
	if _, _, _, _, err := inter.PrevPlayback(ctx, "rqa-prev", 42, "Alice"); err != nil {
		t.Fatalf("prev: %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.Type != entity.ActivityPlayback || a.User != "Alice" {
		t.Errorf("expected playback_changed by Alice, got %+v", a)
	}
	if a.Description != "went to the previous song" {
		t.Errorf("unexpected description %q", a.Description)
	}
}

func TestRoomQueueActivity_PlaybackEndedRecordsSystemActivities(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rqa-ended", 42)
	defer cleanup()
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	// First end: advance cur→next (song finished playing).
	if _, _, _, _, _, err := inter.PlaybackEnded(ctx, "rqa-ended", 42); err != nil {
		t.Fatalf("ended (advance): %v", err)
	}
	a, _ := requireSingleActivity(t, w)
	if a.Type != entity.ActivityPlayback || a.User != "System" || a.Description != "song finished playing" {
		t.Errorf("unexpected advance activity %+v", a)
	}

	// Second end: last song done (queue finished playing).
	w.reset()
	if _, _, _, _, _, err := inter.PlaybackEnded(ctx, "rqa-ended", 42); err != nil {
		t.Fatalf("ended (end of queue): %v", err)
	}
	a, _ = requireSingleActivity(t, w)
	if a.Type != entity.ActivityPlayback || a.User != "System" || a.Description != "queue finished playing" {
		t.Errorf("unexpected end-of-queue activity %+v", a)
	}
}

func TestRoomQueueActivity_NonEventsRecordNothing(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rqa-nonevent", 42)
	defer cleanup()
	ctx := context.Background()
	w := &captureActivityWriter{}
	inter.activityWriter = w

	// Elapsed sync and volume are explicit non-events.
	if _, err := inter.SyncPlaybackElapsed(ctx, "rqa-nonevent", 42, 10); err != nil {
		t.Fatalf("sync elapsed: %v", err)
	}
	if err := inter.ChangePlaybackVolume(ctx, "rqa-nonevent", 42, "up"); err != nil {
		t.Fatalf("volume: %v", err)
	}
	// A rejected mutation (invalid index) records nothing.
	if _, err := inter.RemoveSong(ctx, "rqa-nonevent", 42, "Alice", entity.RoomRoleHost, 99); !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("expected ErrInvalidIndex, got %v", err)
	}
	if acts := w.snapshot(); len(acts) != 0 {
		t.Errorf("non-events must record no activity, got %+v", acts)
	}
}

func TestRoomQueueActivity_WriteFailureDoesNotFailMutation(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rqa-fail")
	ctx := context.Background()
	w := &captureActivityWriter{err: errors.New("simulated activity write failure")}
	inter.activityWriter = w

	q, err := inter.ClearQueue(ctx, "rqa-fail", 42, "Alice", entity.RoomRoleHost)
	if err != nil {
		t.Fatalf("activity failure must not fail the mutation: %v", err)
	}
	if len(q.Songs) != 1 {
		t.Errorf("expected cleared queue keeping current, got %+v", q.Songs)
	}
	// The mutation persisted despite the failed activity write.
	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(persisted.Songs) != 1 {
		t.Errorf("expected persisted clear despite activity failure, got %+v", persisted.Songs)
	}
}

func TestRoomQueueActivity_WriteRunsAfterMutexReleased(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rqa-lock", "ActLock", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	w := &captureActivityWriter{}
	var lockedDuringWrite bool
	w.probe = func() {
		if inter.mu.TryLock() {
			inter.mu.Unlock()
		} else {
			lockedDuringWrite = true
		}
	}
	inter.activityWriter = w

	if _, _, err := inter.AddSong(ctx, "rqa-lock", 42, "Alice", "Alice", "", &entity.SearchResult{ID: "v1", Title: "T1", URL: "https://x/1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if lockedDuringWrite {
		t.Error("AddActivity ran while the roomqueue mutex was held")
	}
}
