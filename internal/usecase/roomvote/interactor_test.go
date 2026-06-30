package roomvote

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomqueue"
)

// fakeRoomQueue is the in-memory stand-in for *roomqueue.Interactor.
// queueAdapter wraps one of these to satisfy QueueSkipping.
type fakeRoomQueue struct {
	mu sync.Mutex

	rooms   map[string]*entity.Room
	members map[int64]map[int]bool
	queues  map[int64]*entity.Queue

	skipCount int
	skipCalls []skipCall

	// staleOnNextSkip, when true, makes the next SkipVote call advance
	// the queue by 1 BEFORE checking expectedSongID. This simulates the
	// race where the lease-holder skipped the queue between the vote
	// interactor's GetStateByRoomID and SkipVote calls.
	staleOnNextSkip bool
}

type skipCall struct {
	Slug         string
	ExpectedSong string
}

func newFakeRoomQueue() *fakeRoomQueue {
	return &fakeRoomQueue{
		rooms:   map[string]*entity.Room{},
		members: map[int64]map[int]bool{},
		queues:  map[int64]*entity.Queue{},
	}
}

// queueAdapter wraps a *fakeRoomQueue so it implements the QueueSkipping
// interface used by NewInteractor. SkipVote mirrors the real
// (*roomqueue.Interactor).SkipVote contract: refuses on song-id mismatch
// (no mutation) and advances via AdvanceToNext semantics.
type queueAdapter struct {
	f *fakeRoomQueue
}

var _ QueueSkipping = queueAdapter{}

func (a queueAdapter) RoomBySlug(_ context.Context, slug string) (*entity.Room, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	r, ok := a.f.rooms[slug]
	if !ok {
		return nil, room.ErrRoomNotFound
	}
	if r.Status != entity.RoomStatusActive {
		return nil, room.ErrArchived
	}
	return r, nil
}

func (a queueAdapter) IsMember(_ context.Context, roomID int64, userID int) bool {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	m, ok := a.f.members[roomID]
	if !ok {
		return false
	}
	return m[userID]
}

func (a queueAdapter) GetStateByRoomID(_ context.Context, roomID int64) (*entity.Queue, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	q, ok := a.f.queues[roomID]
	if !ok {
		return entity.NewQueue(), nil
	}
	// defensive copy so the interactor can't mutate the fake's state
	cp := *q
	cp.Songs = append([]entity.Song(nil), q.Songs...)
	return &cp, nil
}

func (a queueAdapter) SkipVote(_ context.Context, slug, expectedSongID string) (*entity.Queue, int, int, *entity.Song, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.skipCount++
	a.f.skipCalls = append(a.f.skipCalls, skipCall{Slug: slug, ExpectedSong: expectedSongID})
	r, ok := a.f.rooms[slug]
	if !ok {
		return nil, 0, 0, nil, room.ErrRoomNotFound
	}
	q, ok := a.f.queues[r.ID]
	if !ok {
		return nil, 0, 0, nil, entity.ErrNoCurrentSong
	}
	if q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
		return nil, 0, 0, nil, entity.ErrNoCurrentSong
	}
	// Simulated race: lease-holder skip between interactor's load and SkipVote.
	if a.f.staleOnNextSkip {
		a.f.staleOnNextSkip = false
		// Advance the fake's underlying queue (NOT the song-id) so the
		// current song no longer matches the vote's expectedSongID.
		if q.CurrentIndex < len(q.Songs)-1 {
			q.CurrentIndex++
			q.Elapsed = 0
			q.Status = entity.StatusPlaying
		}
	}
	if q.Songs[q.CurrentIndex].ID != expectedSongID {
		return nil, 0, 0, nil, roomqueue.ErrStaleSkipVote
	}
	prevIdx := q.CurrentIndex
	if prevIdx >= len(q.Songs)-1 {
		return nil, 0, 0, nil, entity.ErrNoNextSong
	}
	q.CurrentIndex++
	q.Elapsed = 0
	q.Status = entity.StatusPlaying
	newSong := q.Songs[q.CurrentIndex]
	out := *q
	out.Songs = append([]entity.Song(nil), q.Songs...)
	return &out, prevIdx, q.CurrentIndex, &newSong, nil
}

// stubResolver is a Resolver backed by a map.
type stubResolver struct {
	counts map[string]int
}

var _ Resolver = stubResolver{}

func (s stubResolver) UniqueConnectedUserIDs(slug string) int {
	return s.counts[slug]
}

// seedRoomWithTwoSongs puts an active room + a 2-song queue at index 0
// in the fake.
func seedRoomWithTwoSongs(t *testing.T, fq *fakeRoomQueue, slug string, roomID int64) {
	t.Helper()
	fq.mu.Lock()
	defer fq.mu.Unlock()
	fq.rooms[slug] = &entity.Room{ID: roomID, Slug: slug, Status: entity.RoomStatusActive}
	fq.queues[roomID] = &entity.Queue{
		Songs: []entity.Song{
			{ID: "song1", Title: "S1", URL: "u", AddedBy: "Host", AddedByID: 42},
			{ID: "song2", Title: "S2", URL: "u", AddedBy: "Host", AddedByID: 42},
		},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      0,
		History:      []entity.Activity{},
	}
}

// nowClock returns a real-time-anchored clock: starts at real time.Now()
// so that VoteSession.Cast's internal IsExpired (which uses real time)
// does NOT report the session as already-expired at creation.
func nowClock() (time.Time, func() time.Time) {
	t0 := time.Now()
	return t0, func() time.Time { return t0 }
}

// ---- tests ----

func TestRoomVote_FirstVoteCreatesSessionWithMinThreshold(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second)

	out, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}
	if out.Session == nil {
		t.Fatalf("expected session populated")
	}
	if out.Passed {
		t.Fatalf("expected not passed yet (1 vote < threshold 2)")
	}
	if out.Resolution != "" {
		t.Errorf("expected Resolution empty, got %q", out.Resolution)
	}
	if out.Session.Threshold != 2 {
		t.Errorf("expected threshold=2 (strict-majority minimum), got %d", out.Session.Threshold)
	}
	if out.Session.VoteCount() != 1 {
		t.Errorf("expected VoteCount==1, got %d", out.Session.VoteCount())
	}
	if fq.skipCount != 0 {
		t.Errorf("expected 0 SkipVote calls, got %d", fq.skipCount)
	}
}

func TestRoomVote_PassingVoteCallsSkipVoteAndPopulatesAdvance(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second)

	// First vote — threshold = max(2, 2/2 + 1) = 2; only 1 vote so not passed.
	out1, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if err != nil {
		t.Fatalf("cast1: %v", err)
	}
	if out1.Passed {
		t.Fatalf("first vote should not pass")
	}
	if out1.Resolution != "" {
		t.Errorf("expected empty resolution on non-pass, got %q", out1.Resolution)
	}

	// Second vote — meets threshold; resolves passed.
	out2, err := inter.CastSkipVote(context.Background(), "alpha", 43)
	if err != nil {
		t.Fatalf("cast2: %v", err)
	}
	if !out2.Passed {
		t.Fatalf("second vote should pass (threshold=2 reached)")
	}
	if out2.Resolution != "passed" {
		t.Errorf("expected resolution=passed, got %q", out2.Resolution)
	}
	if out2.AdvanceQueue == nil {
		t.Fatalf("expected AdvanceQueue populated")
	}
	if out2.AdvanceSong == nil {
		t.Fatalf("expected AdvanceSong populated")
	}
	if out2.AdvanceSong.ID != "song2" {
		t.Errorf("expected AdvanceSong.ID=song2, got %q", out2.AdvanceSong.ID)
	}
	if out2.AdvancePrev != 0 || out2.AdvanceNext != 1 {
		t.Errorf("expected prev=0 next=1, got prev=%d next=%d", out2.AdvancePrev, out2.AdvanceNext)
	}
	if len(fq.skipCalls) != 1 {
		t.Fatalf("expected 1 SkipVote call, got %d", len(fq.skipCalls))
	}
	if fq.skipCalls[0].Slug != "alpha" || fq.skipCalls[0].ExpectedSong != "song1" {
		t.Errorf("expected SkipVote(alpha, song1), got %+v", fq.skipCalls[0])
	}
	// Session was deleted on pass.
	if got := inter.ActiveSession("alpha", "song1"); got != nil {
		t.Errorf("expected session deleted on pass, got %+v", got)
	}
}

func TestRoomVote_DuplicateVoteFromSameUserReturnsErrAlreadyVoted(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	_, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if !errors.Is(err, entity.ErrAlreadyVoted) {
		t.Fatalf("expected ErrAlreadyVoted, got %v", err)
	}
}

func TestRoomVote_StaleSessionReturnsErrStaleAndNoMutation(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second)

	// First vote from 42 — not passed yet (threshold 2 of 2).
	out1, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if err != nil {
		t.Fatalf("cast1: %v", err)
	}
	if out1.Passed {
		t.Fatalf("first vote should not pass")
	}

	// Trigger the concurrent-lease-race: the next SkipVote call will
	// advance the queue by 1 BEFORE the expectedSongID check, so
	// expectedSongID="song1" mismatches q.Songs[CurrentIndex].ID="song2"
	// and the fake returns roomqueue.ErrStaleSkipVote.
	fq.mu.Lock()
	fq.staleOnNextSkip = true
	fq.mu.Unlock()

	// User 43's vote — threshold now met, SkipVote called.
	out2, err := inter.CastSkipVote(context.Background(), "alpha", 43)
	if !errors.Is(err, ErrStaleSession) {
		t.Fatalf("expected ErrStaleSession, got %v", err)
	}
	if !out2.StaleSession {
		t.Errorf("expected StaleSession=true, got false")
	}
	if out2.Resolution != "" {
		t.Errorf("expected Resolution cleared on stale path, got %q", out2.Resolution)
	}
	// Queue is on song2 — no further mutation by SkipVote (it refused).
	fq.mu.Lock()
	currentID := fq.queues[1].Songs[fq.queues[1].CurrentIndex].ID
	fq.mu.Unlock()
	if currentID != "song2" {
		t.Errorf("expected queue on song2 after stale, got %q", currentID)
	}
	if len(fq.skipCalls) != 1 {
		t.Errorf("expected exactly 1 SkipVote call, got %d", len(fq.skipCalls))
	}
	if fq.skipCalls[0].ExpectedSong != "song1" {
		t.Errorf("expected SkipVote expectedSongID=song1, got %q", fq.skipCalls[0].ExpectedSong)
	}
}

func TestRoomVote_ExpiredSessionIsRecreatedOnNextVote(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	clockVal, clockFn := nowClock()
	clock := clockVal
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second)
	inter.SetClock(func() time.Time { return clock })

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42); err != nil {
		t.Fatalf("cast: %v", err)
	}
	if got := inter.ActiveSession("alpha", "song1"); got == nil {
		t.Fatalf("expected session present after first cast")
	}
	original := inter.ActiveSession("alpha", "song1")
	if original.ExpiresAt.Sub(original.CreatedAt) != 30*time.Second {
		t.Errorf("expected 30s expiry window, got %s", original.ExpiresAt.Sub(original.CreatedAt))
	}

	// Advance the interactor's clock past expiry. The session is still
	// in the map (eviction runs lazily on the next cast).
	clock = clockVal.Add(31 * time.Second)
	if got := inter.ActiveSession("alpha", "song1"); got == nil {
		t.Fatalf("expected session still in map before next cast")
	}

	// A new cast should evict the expired session and start a fresh one.
	out, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if err != nil {
		t.Fatalf("post-expiry cast: %v", err)
	}
	if out.Session == nil {
		t.Fatalf("expected fresh session populated")
	}
	if out.Session.VoteCount() != 1 {
		t.Errorf("expected fresh session VoteCount=1, got %d", out.Session.VoteCount())
	}
	if !out.Session.CreatedAt.Equal(clock) {
		t.Errorf("expected fresh CreatedAt=clock, got %s", out.Session.CreatedAt)
	}
	// Suppress unused-but-keep helper import warning.
	_ = clockFn
}

func TestRoomVote_SessionsAreRoomScoped(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	seedRoomWithTwoSongs(t, fq, "beta", 2)
	fq.members[1] = map[int]bool{42: true}
	fq.members[2] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1, "beta": 1}}, 30*time.Second)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42); err != nil {
		t.Fatalf("alpha cast: %v", err)
	}
	if got := inter.ActiveSession("alpha", "song1"); got == nil {
		t.Fatalf("expected alpha session for song1")
	}
	if got := inter.ActiveSession("beta", "song1"); got != nil {
		t.Fatalf("expected NO beta session for song1, got %+v", got)
	}
}

func TestRoomVote_NonMemberReturnsErrForbidden(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	// user 42 not a member of room 1
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second)

	_, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if !errors.Is(err, room.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestRoomVote_NoCurrentSongReturnsErrNoCurrentSong(t *testing.T) {
	fq := newFakeRoomQueue()
	// Seed a room but NO queue songs.
	fq.mu.Lock()
	fq.rooms["alpha"] = &entity.Room{ID: 1, Slug: "alpha", Status: entity.RoomStatusActive}
	fq.queues[1] = entity.NewQueue() // empty queue, CurrentIndex == -1
	fq.members[1] = map[int]bool{42: true}
	fq.mu.Unlock()

	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second)
	_, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if !errors.Is(err, entity.ErrNoCurrentSong) {
		t.Fatalf("expected ErrNoCurrentSong, got %v", err)
	}
}

func TestRoomVote_ArchivedRoomReturnsErrArchived(t *testing.T) {
	fq := newFakeRoomQueue()
	fq.mu.Lock()
	fq.rooms["alpha"] = &entity.Room{ID: 1, Slug: "alpha", Status: entity.RoomStatusArchived}
	fq.queues[1] = &entity.Queue{
		Songs:        []entity.Song{{ID: "song1", Title: "S1", URL: "u"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	}
	fq.members[1] = map[int]bool{42: true}
	fq.mu.Unlock()

	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second)
	_, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if !errors.Is(err, room.ErrArchived) {
		t.Fatalf("expected ErrArchived, got %v", err)
	}
}

func TestRoomVote_ExpiredSessionOnEntryProducesExpiredOutcomeAlongsideNewSession(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	clockVal, _ := nowClock()
	clock := clockVal
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second)
	inter.SetClock(func() time.Time { return clock })

	// Cast at T0 — creates session skip:alpha:song1.
	out1, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if err != nil {
		t.Fatalf("cast1: %v", err)
	}
	if out1.Session == nil || out1.Resolution != "" {
		t.Fatalf("expected plain cast (session present, no resolution), got %+v", out1)
	}

	// Flip the queue's current song ID so the new current song is "songX".
	fq.mu.Lock()
	fq.queues[1].Songs[0].ID = "songX"
	fq.mu.Unlock()

	// Advance the interactor's clock past the original session's expiry.
	clock = clockVal.Add(31 * time.Second)

	// Cast by user 42 again — eviction-on-entry must fire (the original
	// skip:alpha:song1 session is now expired per interactor clock).
	out2, err := inter.CastSkipVote(context.Background(), "alpha", 42)
	if err != nil {
		t.Fatalf("cast2: %v", err)
	}
	if out2.Resolution != "expired" {
		t.Errorf("expected Resolution=expired, got %q", out2.Resolution)
	}
	if out2.Session == nil {
		t.Errorf("expected new Session populated alongside expired outcome")
	}
	if out2.ExpiredID != "skip:alpha:song1" {
		t.Errorf("expected ExpiredID=skip:alpha:song1, got %q", out2.ExpiredID)
	}
	if out2.ExpiredSession == nil {
		t.Errorf("expected ExpiredSession populated")
	}
	if out2.ExpiredQueue == nil {
		t.Errorf("expected ExpiredQueue populated")
	}
	// The new session is keyed on the new current song (songX).
	if out2.Session.SongID != "songX" {
		t.Errorf("expected new session SongID=songX, got %q", out2.Session.SongID)
	}
}

func TestRoomVote_ExpireSessionsReturnsExpiredOutcomes(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	clockVal, _ := nowClock()
	clock := clockVal
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1, "beta": 1}}, 30*time.Second)
	inter.SetClock(func() time.Time { return clock })

	// Cast in alpha → seed session skip:alpha:song1.
	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42); err != nil {
		t.Fatalf("alpha cast: %v", err)
	}
	// Set up beta room + cast → seed session skip:beta:song1.
	fq.mu.Lock()
	fq.rooms["beta"] = &entity.Room{ID: 2, Slug: "beta", Status: entity.RoomStatusActive}
	fq.queues[2] = &entity.Queue{
		Songs:        []entity.Song{{ID: "song1", Title: "B-S1", URL: "u", AddedBy: "H", AddedByID: 42}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      0,
		History:      []entity.Activity{},
	}
	fq.members[2] = map[int]bool{42: true}
	fq.mu.Unlock()
	if _, err := inter.CastSkipVote(context.Background(), "beta", 42); err != nil {
		t.Fatalf("beta cast: %v", err)
	}

	if got := inter.ActiveSession("alpha", "song1"); got == nil {
		t.Fatalf("alpha session missing before expiry")
	}
	if got := inter.ActiveSession("beta", "song1"); got == nil {
		t.Fatalf("beta session missing before expiry")
	}

	// Advance interactor's clock past expiry.
	clock = clockVal.Add(31 * time.Second)

	out, err := inter.ExpireSessions(context.Background())
	if err != nil {
		t.Fatalf("ExpireSessions: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 expired outcomes, got %d (%+v)", len(out), out)
	}
	slugs := map[string]bool{}
	for _, e := range out {
		if e.SessionID == "" {
			t.Errorf("empty SessionID in expired outcome: %+v", e)
		}
		if e.RoomSlug == "" {
			t.Errorf("empty RoomSlug in expired outcome: %+v", e)
		}
		if e.Session == nil {
			t.Errorf("nil Session in expired outcome: %+v", e)
		}
		slugs[e.RoomSlug] = true
	}
	if !slugs["alpha"] || !slugs["beta"] {
		t.Errorf("expected both alpha and beta in expired outcomes, got %+v", slugs)
	}
	// Sessions must be evicted.
	if got := inter.ActiveSession("alpha", "song1"); got != nil {
		t.Errorf("alpha session not evicted after ExpireSessions")
	}
	if got := inter.ActiveSession("beta", "song1"); got != nil {
		t.Errorf("beta session not evicted after ExpireSessions")
	}
}

// TestRoomVote_Threshold_StrictMajorityMatrix pins the strict-majority
// rule: threshold(n) = max(2, n/2 + 1) for n = 1,2,3,4,5.
//
// The original draft used max(2, n/2); that gave a tie (not a strict
// majority) and the Product Owner rejected it. The current rule is
// max(2, n/2 + 1), which is a strict majority with a 2-voter floor.
func TestRoomVote_Threshold_StrictMajorityMatrix(t *testing.T) {
	cases := []struct {
		n, want int
	}{
		{1, 2},
		{2, 2},
		{3, 2},
		{4, 3},
		{5, 3},
	}
	for _, tc := range cases {
		if got := threshold(tc.n); got != tc.want {
			t.Errorf("threshold(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}
