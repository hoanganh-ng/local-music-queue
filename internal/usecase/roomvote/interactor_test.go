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

	prioritizeCount int
	prioritizeCalls []prioritizeCall

	// staleOnNextSkip, when true, makes the next SkipVote call advance
	// the queue by 1 BEFORE checking expectedSongID. This simulates the
	// race where the lease-holder skipped the queue between the vote
	// interactor's GetStateByRoomID and SkipVote calls.
	staleOnNextSkip bool

	// staleOnNextPrioritize, when true, makes the next PrioritizeVote
	// call return ErrStalePrioritizeVote WITHOUT mutating. Simulates the
	// race where the target song was removed/moved between the vote
	// interactor's GetStateByRoomID and PrioritizeVote calls.
	staleOnNextPrioritize bool

	// currentOnNextPrioritize, when true, makes the next PrioritizeVote
	// call return entity.ErrVoteOnCurrentSong WITHOUT mutating. Simulates
	// the race where the target became the currently-playing song between
	// the vote interactor's GetStateByRoomID and PrioritizeVote calls; the
	// queue owner surfaces the current-song sentinel (HTTP 400), NOT a
	// moved/removed stale conflict (409).
	currentOnNextPrioritize bool
}

type skipCall struct {
	Slug         string
	ExpectedSong string
}

type prioritizeCall struct {
	Slug          string
	ExpectedSong  string
	ExpectedIndex int
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

// PrioritizeVote mirrors the real (*roomqueue.Interactor).PrioritizeVote
// contract: it re-validates the (expectedSongID, expectedIndex) snapshot
// against fresh state and refuses removed/moved/ambiguous/now-current
// targets with ErrStalePrioritizeVote (no mutation). On success it moves
// the target immediately after the current song via entity.Prioritize.
func (a queueAdapter) PrioritizeVote(_ context.Context, slug, expectedSongID string, expectedIndex int) (*entity.Queue, int, int, entity.Song, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.prioritizeCount++
	a.f.prioritizeCalls = append(a.f.prioritizeCalls, prioritizeCall{Slug: slug, ExpectedSong: expectedSongID, ExpectedIndex: expectedIndex})
	r, ok := a.f.rooms[slug]
	if !ok {
		return nil, 0, 0, entity.Song{}, room.ErrRoomNotFound
	}
	q, ok := a.f.queues[r.ID]
	if !ok {
		return nil, 0, 0, entity.Song{}, roomqueue.ErrStalePrioritizeVote
	}
	// Simulated race: target moved/removed under the vote.
	if a.f.staleOnNextPrioritize {
		a.f.staleOnNextPrioritize = false
		return nil, 0, 0, entity.Song{}, roomqueue.ErrStalePrioritizeVote
	}
	// Simulated race: target became the currently-playing song under the
	// vote. Current-song rejection, NOT a stale conflict.
	if a.f.currentOnNextPrioritize {
		a.f.currentOnNextPrioritize = false
		return nil, 0, 0, entity.Song{}, entity.ErrVoteOnCurrentSong
	}
	if q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
		return nil, 0, 0, entity.Song{}, roomqueue.ErrStalePrioritizeVote
	}
	if expectedIndex < 0 || expectedIndex >= len(q.Songs) {
		return nil, 0, 0, entity.Song{}, roomqueue.ErrStalePrioritizeVote
	}
	if q.Songs[expectedIndex].ID != expectedSongID {
		return nil, 0, 0, entity.Song{}, roomqueue.ErrStalePrioritizeVote
	}
	if expectedIndex == q.CurrentIndex {
		return nil, 0, 0, entity.Song{}, entity.ErrVoteOnCurrentSong
	}
	count := 0
	for idx := range q.Songs {
		if q.Songs[idx].ID == expectedSongID {
			count++
		}
	}
	if count != 1 {
		return nil, 0, 0, entity.Song{}, roomqueue.ErrStalePrioritizeVote
	}
	if err := q.Prioritize(expectedIndex); err != nil {
		return nil, 0, 0, entity.Song{}, roomqueue.ErrStalePrioritizeVote
	}
	toIndex := q.CurrentIndex + 1
	if toIndex >= len(q.Songs) {
		toIndex = len(q.Songs) - 1
	}
	out := *q
	out.Songs = append([]entity.Song(nil), q.Songs...)
	return &out, expectedIndex, toIndex, out.Songs[toIndex], nil
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	out, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, nil)

	// First vote — threshold = max(2, 2/2 + 1) = 2; only 1 vote so not passed.
	out1, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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
	out2, err := inter.CastSkipVote(context.Background(), "alpha", 43, "")
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, nil)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, ""); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	_, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
	if !errors.Is(err, entity.ErrAlreadyVoted) {
		t.Fatalf("expected ErrAlreadyVoted, got %v", err)
	}
}

func TestRoomVote_StaleSessionReturnsErrStaleAndNoMutation(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 43: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, nil)

	// First vote from 42 — not passed yet (threshold 2 of 2).
	out1, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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
	out2, err := inter.CastSkipVote(context.Background(), "alpha", 43, "")
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)
	inter.SetClock(func() time.Time { return clock })

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, ""); err != nil {
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
	out, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1, "beta": 1}}, 30*time.Second, nil)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, ""); err != nil {
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	_, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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

	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)
	_, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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

	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)
	_, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)
	inter.SetClock(func() time.Time { return clock })

	// Cast at T0 — creates session skip:alpha:song1.
	out1, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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
	out2, err := inter.CastSkipVote(context.Background(), "alpha", 42, "")
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
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1, "beta": 1}}, 30*time.Second, nil)
	inter.SetClock(func() time.Time { return clock })

	// Cast in alpha → seed session skip:alpha:song1.
	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, ""); err != nil {
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
	if _, err := inter.CastSkipVote(context.Background(), "beta", 42, ""); err != nil {
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

// TestRoomVote_ExpireSessionsPrioritizeUsesSessionID pins the R09h-fix
// identifier contract for the ticker-driven expiry sweep: an expired
// PRIORITIZE session must report ExpiredOutcome.SessionID equal to the
// session.ID ("prioritize:{songID}") the client saw on
// room_vote_updated — NOT the internal map key
// ("prioritize:{slug}:{songID}") — so a client can correlate the ticker
// expiry with the session it was tracking. The coexisting SKIP session
// keeps the internal map key to preserve the accepted R09b contract.
func TestRoomVote_ExpireSessionsPrioritizeUsesSessionID(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithTwoSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	clockVal, _ := nowClock()
	clock := clockVal
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)
	inter.SetClock(func() time.Time { return clock })

	// Seed a skip session (current song song1) and a prioritize session
	// (upcoming song song2 at index 1).
	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, ""); err != nil {
		t.Fatalf("skip cast: %v", err)
	}
	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 1, 42, ""); err != nil {
		t.Fatalf("prioritize cast: %v", err)
	}

	// Advance past expiry and sweep.
	clock = clockVal.Add(31 * time.Second)
	out, err := inter.ExpireSessions(context.Background())
	if err != nil {
		t.Fatalf("ExpireSessions: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 expired outcomes, got %d (%+v)", len(out), out)
	}

	byType := map[entity.VoteType]ExpiredOutcome{}
	for _, e := range out {
		if e.Session == nil {
			t.Fatalf("nil Session in expired outcome: %+v", e)
		}
		byType[e.Session.Type] = e
	}

	prio, ok := byType[entity.VoteTypePrioritize]
	if !ok {
		t.Fatalf("no prioritize outcome in %+v", out)
	}
	if prio.SessionID != "prioritize:song2" {
		t.Errorf("prioritize expiry SessionID = %q, want %q (session.ID, not the map key)", prio.SessionID, "prioritize:song2")
	}
	if prio.SessionID != prio.Session.ID {
		t.Errorf("prioritize expiry SessionID %q must equal session.ID %q", prio.SessionID, prio.Session.ID)
	}

	skip, ok := byType[entity.VoteTypeSkip]
	if !ok {
		t.Fatalf("no skip outcome in %+v", out)
	}
	if skip.SessionID != "skip:alpha:song1" {
		t.Errorf("skip expiry SessionID = %q, want %q (accepted R09b map-key contract preserved)", skip.SessionID, "skip:alpha:song1")
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

// --- R09h prioritize vote tests ---

// seedRoomWithThreeSongs puts an active room + a 3-song queue at index
// 0 in the fake, so indexes 1 and 2 are distinct non-current targets.
func seedRoomWithThreeSongs(t *testing.T, fq *fakeRoomQueue, slug string, roomID int64) {
	t.Helper()
	fq.mu.Lock()
	defer fq.mu.Unlock()
	fq.rooms[slug] = &entity.Room{ID: roomID, Slug: slug, Status: entity.RoomStatusActive}
	fq.queues[roomID] = &entity.Queue{
		Songs: []entity.Song{
			{ID: "song1", Title: "S1", URL: "u", AddedBy: "Host", AddedByID: 42},
			{ID: "song2", Title: "S2", URL: "u", AddedBy: "Host", AddedByID: 42},
			{ID: "song3", Title: "S3", URL: "u", AddedBy: "Host", AddedByID: 42},
		},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      0,
		History:      []entity.Activity{},
	}
}

func TestRoomVotePrioritize_NonMemberForbidden(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 1, 999, ""); !errors.Is(err, room.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for non-member, got %v", err)
	}
}

func TestRoomVotePrioritize_InvalidIndexRejected(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 99, 42, ""); !errors.Is(err, roomqueue.ErrInvalidIndex) {
		t.Fatalf("expected ErrInvalidIndex for out-of-range index, got %v", err)
	}
}

func TestRoomVotePrioritize_CurrentSongRejected(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 0, 42, ""); !errors.Is(err, entity.ErrVoteOnCurrentSong) {
		t.Fatalf("expected ErrVoteOnCurrentSong for current index, got %v", err)
	}
}

func TestRoomVotePrioritize_FirstVoteCreatesSessionSnapshot(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	out, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, "")
	if err != nil {
		t.Fatalf("cast: %v", err)
	}
	if out.Session == nil {
		t.Fatalf("expected session populated")
	}
	if out.Passed || out.Resolution != "" {
		t.Fatalf("expected not passed (1 vote < threshold 2), got passed=%v res=%q", out.Passed, out.Resolution)
	}
	if out.Session.Type != entity.VoteTypePrioritize {
		t.Errorf("expected type prioritize, got %q", out.Session.Type)
	}
	if out.Session.SongID != "song3" || out.Session.SongIndex != 2 {
		t.Errorf("expected snapshot song3@2, got %s@%d", out.Session.SongID, out.Session.SongIndex)
	}
	if got := inter.ActivePrioritizeSession("alpha", "song3"); got == nil {
		t.Errorf("expected active prioritize session for song3")
	}
	// A prioritize vote must NOT create a skip session for the same room.
	if got := inter.ActiveSession("alpha", "song1"); got != nil {
		t.Errorf("prioritize vote must not create a skip session")
	}
}

func TestRoomVotePrioritize_DuplicateBallotRejected(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 1, 42, ""); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 1, 42, ""); !errors.Is(err, entity.ErrAlreadyVoted) {
		t.Fatalf("expected ErrAlreadyVoted on duplicate ballot, got %v", err)
	}
}

func TestRoomVotePrioritize_PassMovesTargetOnce(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 99: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	// Threshold is max(2, 1/2+1) = 2. Two distinct voters pass it.
	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	out, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 99, "")
	if err != nil {
		t.Fatalf("second cast: %v", err)
	}
	if !out.Passed || out.Resolution != "passed" {
		t.Fatalf("expected passed resolution, got passed=%v res=%q", out.Passed, out.Resolution)
	}
	if out.PrioritizeQueue == nil {
		t.Fatalf("expected PrioritizeQueue populated on pass")
	}
	// song3 moves to slot immediately after current (index 1).
	if out.ToIndex != 1 {
		t.Errorf("expected ToIndex=1, got %d", out.ToIndex)
	}
	if out.PrioritizeSong.ID != "song3" {
		t.Errorf("expected moved song3, got %s", out.PrioritizeSong.ID)
	}
	if !out.PrioritizeSong.IsPrioritized {
		t.Errorf("expected IsPrioritized=true on broadcast song")
	}
	// The snapshot index (2) was passed to PrioritizeVote, not a live
	// recompute, and it must have been called exactly once.
	if fq.prioritizeCount != 1 {
		t.Errorf("expected exactly 1 PrioritizeVote call, got %d", fq.prioritizeCount)
	}
	if len(fq.prioritizeCalls) != 1 || fq.prioritizeCalls[0].ExpectedIndex != 2 || fq.prioritizeCalls[0].ExpectedSong != "song3" {
		t.Errorf("expected PrioritizeVote(song3, idx2), got %+v", fq.prioritizeCalls)
	}
	// Session cleared after pass.
	if got := inter.ActivePrioritizeSession("alpha", "song3"); got != nil {
		t.Errorf("expected session cleared after pass")
	}
}

func TestRoomVotePrioritize_StaleTargetSurfacesConflict(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 99: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	// The target moves/was removed between the passing vote's load and
	// the PrioritizeVote resolution.
	fq.mu.Lock()
	fq.staleOnNextPrioritize = true
	fq.mu.Unlock()
	out, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 99, "")
	if !errors.Is(err, ErrStalePrioritizeSession) {
		t.Fatalf("expected ErrStalePrioritizeSession, got %v", err)
	}
	if out == nil || !out.StaleSession || out.Resolution != "" {
		t.Fatalf("expected StaleSession outcome with empty resolution, got %+v", out)
	}
}

func TestRoomVotePrioritize_DistinctTargetsAreIndependentSessions(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 1, 42, ""); err != nil {
		t.Fatalf("cast song2: %v", err)
	}
	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("cast song3: %v", err)
	}
	if inter.ActivePrioritizeSession("alpha", "song2") == nil {
		t.Errorf("expected independent session for song2")
	}
	if inter.ActivePrioritizeSession("alpha", "song3") == nil {
		t.Errorf("expected independent session for song3")
	}
}

func TestRoomVotePrioritize_CrossRoomIsolation(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	seedRoomWithThreeSongs(t, fq, "beta", 2)
	fq.members[1] = map[int]bool{42: true}
	fq.members[2] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1, "beta": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 1, 42, ""); err != nil {
		t.Fatalf("cast alpha: %v", err)
	}
	if inter.ActivePrioritizeSession("alpha", "song2") == nil {
		t.Errorf("expected alpha session")
	}
	if inter.ActivePrioritizeSession("beta", "song2") != nil {
		t.Errorf("beta must not share alpha's prioritize session")
	}
}

func TestRoomVotePrioritize_ExpirySweepEvictsSession(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)
	t0, clock := nowClock()
	inter.SetClock(clock)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("cast: %v", err)
	}
	// Advance the clock past expiry and sweep.
	inter.SetClock(func() time.Time { return t0.Add(31 * time.Second) })
	out, err := inter.ExpireSessions(context.Background())
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 expired prioritize outcome, got %d", len(out))
	}
	if out[0].RoomSlug != "alpha" {
		t.Errorf("expected slug alpha recovered from prioritize key, got %q", out[0].RoomSlug)
	}
	if out[0].Session == nil || out[0].Session.Type != entity.VoteTypePrioritize {
		t.Errorf("expected evicted prioritize session, got %+v", out[0].Session)
	}
	if got := inter.ActivePrioritizeSession("alpha", "song3"); got != nil {
		t.Errorf("expected prioritize session evicted after sweep")
	}
}

// TestRoomVotePrioritize_SkipSessionUntouched pins that casting a
// prioritize vote never disturbs a concurrent skip session for the same
// room (they share the map but keys never collide across types).
func TestRoomVotePrioritize_SkipSessionUntouched(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, ""); err != nil {
		t.Fatalf("skip cast: %v", err)
	}
	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("prioritize cast: %v", err)
	}
	// Both sessions coexist.
	if inter.ActiveSession("alpha", "song1") == nil {
		t.Errorf("skip session for current song must survive a prioritize cast")
	}
	if inter.ActivePrioritizeSession("alpha", "song3") == nil {
		t.Errorf("prioritize session must exist alongside skip session")
	}
}

// ---- skip-behavior regression (session-key generalization) ----

// TestSplitSessionKey_ParsesSkipPrioritizeAndDegrades pins that the
// shared session-key parser recovers the slug (and songID) for both
// skip and prioritize keys, and degrades gracefully on malformed keys.
// The skip cases are the regression guard: generalizing the parser for
// prioritize keys must not change how existing skip keys are decoded.
func TestSplitSessionKey_ParsesSkipPrioritizeAndDegrades(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		wantSlug   string
		wantSongID string
	}{
		{"skip key", "skip:alpha:song1", "alpha", "song1"},
		{"prioritize key", "prioritize:alpha:song3", "alpha", "song3"},
		{"skip songID with colon-free id", "skip:beta:s-42", "beta", "s-42"},
		{"prioritize songID retains later colons", "prioritize:beta:a:b", "beta", "a:b"},
		{"malformed skip missing song", "skip:alpha", "alpha", ""},
		{"malformed prioritize missing song", "prioritize:alpha", "alpha", ""},
		{"unprefixed degrades", "alpha", "alpha", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slug, songID := splitSessionKey(tc.key)
			if slug != tc.wantSlug || songID != tc.wantSongID {
				t.Errorf("splitSessionKey(%q) = (%q, %q), want (%q, %q)",
					tc.key, slug, songID, tc.wantSlug, tc.wantSongID)
			}
		})
	}
}

// TestHasSlugPrefix_MatchesSkipNotPrioritize pins that the skip
// eviction-on-entry predicate matches only "skip:{slug}:" keys and
// never prioritize keys for the same slug, nor prefix-only slug
// collisions. This is the invariant that keeps a skip cast from
// evicting a coexisting prioritize session.
func TestHasSlugPrefix_MatchesSkipNotPrioritize(t *testing.T) {
	cases := []struct {
		name string
		key  string
		slug string
		want bool
	}{
		{"skip key for slug", "skip:alpha:song1", "alpha", true},
		{"prioritize key for slug", "prioritize:alpha:song3", "alpha", false},
		{"skip key other slug", "skip:beta:song1", "alpha", false},
		{"prefix-only slug collision", "skip:alphabet:song1", "alpha", false},
		{"skip key missing song segment", "skip:alpha", "alpha", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasSlugPrefix(tc.key, tc.slug); got != tc.want {
				t.Errorf("hasSlugPrefix(%q, %q) = %v, want %v", tc.key, tc.slug, got, tc.want)
			}
		})
	}
}

// TestRoomVoteSkip_EvictionOnEntry_LeavesPrioritizeSession is the
// behavioral reverse of SkipSessionUntouched: a skip cast that runs its
// eviction-on-entry sweep must evict an expired *skip* session for the
// slug (existing R09b behavior) while leaving a coexisting expired
// prioritize session in the map untouched (only the shared
// ExpireSessions sweep may reap it).
func TestRoomVoteSkip_EvictionOnEntry_LeavesPrioritizeSession(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 7: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 2}}, 30*time.Second, nil)
	t0, clock := nowClock()
	inter.SetClock(clock)

	// Seed a skip session (current song1) and a prioritize session
	// (song3) at t0.
	if _, err := inter.CastSkipVote(context.Background(), "alpha", 42, ""); err != nil {
		t.Fatalf("skip cast: %v", err)
	}
	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("prioritize cast: %v", err)
	}

	// Advance past expiry so both sessions are expired, then cast skip
	// again. Eviction-on-entry must reap the expired skip session
	// (surfacing an "expired" resolution) and rebuild a fresh one.
	inter.SetClock(func() time.Time { return t0.Add(31 * time.Second) })
	out, err := inter.CastSkipVote(context.Background(), "alpha", 7, "")
	if err != nil {
		t.Fatalf("second skip cast: %v", err)
	}
	if out == nil || out.ExpiredID != sessionKey("alpha", "song1") {
		t.Errorf("expected eviction-on-entry to reap expired skip session, got %+v", out)
	}

	// The expired prioritize session must remain in the map: the skip
	// eviction loop only touches skip-prefixed keys.
	if inter.ActivePrioritizeSession("alpha", "song3") == nil {
		t.Errorf("skip eviction-on-entry must not reap a prioritize session")
	}
}

// ---- R09h correction: prioritize expired-restart + conflict guard ----

// TestRoomVotePrioritize_ExpiredMatchingSessionRestartsWithExpiredOutcome
// pins the Issue #17 blocking fix: a ballot for a target whose prior
// session has expired must evict + snapshot the exact expired session
// (surfacing Resolution=="expired" so the handler broadcasts one
// room_vote_resolved{"expired"} + one fresh room_vote_updated and
// replies 200), then start and cast a fresh session. It must NOT
// silently restart with a 204/no resolution.
func TestRoomVotePrioritize_ExpiredMatchingSessionRestartsWithExpiredOutcome(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 7: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 3}}, 30*time.Second, nil)
	t0, clock := nowClock()
	inter.SetClock(clock)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	first := inter.ActivePrioritizeSession("alpha", "song3")
	if first == nil {
		t.Fatalf("expected first session")
	}

	// Advance past expiry; the next ballot for the same target evicts the
	// expired session and restarts a fresh one.
	inter.SetClock(func() time.Time { return t0.Add(31 * time.Second) })
	out, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 7, "")
	if err != nil {
		t.Fatalf("second cast: %v", err)
	}
	if out.Resolution != "expired" {
		t.Fatalf("expected expired resolution, got %q", out.Resolution)
	}
	if out.ExpiredSession != first || out.ExpiredID != first.ID {
		t.Errorf("expected snapshot of the exact expired session, got id=%q sess=%p want=%p", out.ExpiredID, out.ExpiredSession, first)
	}
	if out.ExpiredQueue == nil {
		t.Errorf("expected ExpiredQueue populated for the expired broadcast")
	}
	if out.Session == nil || out.Session == first {
		t.Fatalf("expected a distinct fresh session, got %p", out.Session)
	}
	if out.Session.VoteCount() != 1 || !out.Session.HasVoted(7) {
		t.Errorf("fresh session must carry only the new ballot, got %+v", out.Session)
	}
	if out.Session.HasVoted(42) {
		t.Errorf("fresh session must not inherit the expired session's voter")
	}
	if out.Passed || out.PrioritizeQueue != nil {
		t.Errorf("expired restart must not pass or carry a prioritize result")
	}
	live := inter.ActivePrioritizeSession("alpha", "song3")
	if live != out.Session {
		t.Errorf("map must hold the fresh session")
	}
	if fq.prioritizeCount != 0 {
		t.Errorf("expired restart must not call queue PrioritizeVote, got %d", fq.prioritizeCount)
	}
}

// TestRoomVotePrioritize_SameSongIDDifferentIndexRejectedWithoutBallotChange
// pins the Issue #17 blocking fix: when a live session already exists
// for a target song ID, a second ballot referencing the same song ID at
// a DIFFERENT snapshot index (a distinct queue entry that happens to
// share the video ID) is rejected as a stale conflict immediately,
// WITHOUT recording the ballot on the existing session.
func TestRoomVotePrioritize_SameSongIDDifferentIndexRejectedWithoutBallotChange(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 7: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 3}}, 30*time.Second, nil)

	// First ballot: song3 at index 2 -> session SongIndex=2, one vote.
	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	before := inter.ActivePrioritizeSession("alpha", "song3")
	if before == nil || before.VoteCount() != 1 {
		t.Fatalf("expected 1-ballot session for song3, got %+v", before)
	}

	// The queue reorders so song3 now sits at index 1 (a different entry
	// position). A ballot referencing index 1 targets the same song ID at
	// a different snapshot index than the live session's SongIndex (2).
	fq.mu.Lock()
	fq.queues[1].Songs = []entity.Song{
		{ID: "song1", Title: "S1", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "song3", Title: "S3", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "song2", Title: "S2", URL: "u", AddedBy: "Host", AddedByID: 42},
	}
	fq.mu.Unlock()

	out, err := inter.CastPrioritizeVote(context.Background(), "alpha", 1, 7, "")
	if !errors.Is(err, ErrStalePrioritizeSession) {
		t.Fatalf("expected ErrStalePrioritizeSession for same-id/different-index, got %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil outcome on conflict, got %+v", out)
	}
	// The existing ballot map must be untouched: still exactly one vote,
	// still snapshot index 2, and user 7 never recorded.
	after := inter.ActivePrioritizeSession("alpha", "song3")
	if after == nil || after.VoteCount() != 1 || after.SongIndex != 2 {
		t.Fatalf("existing session must be unchanged, got %+v", after)
	}
	if after.HasVoted(7) {
		t.Errorf("conflicting ballot must not be recorded in the existing session")
	}
	if fq.prioritizeCount != 0 {
		t.Errorf("conflict must reject before any PrioritizeVote call, got %d", fq.prioritizeCount)
	}
}

// TestRoomVotePrioritize_BecameCurrentUnderVotePassesThroughCurrentSong
// pins the Issue #17 important fix: when the queue owner rejects a
// passing vote because the target became the currently-playing song, the
// vote interactor passes the current-song sentinel through (HTTP 400)
// rather than converting it into ErrStalePrioritizeSession (409).
func TestRoomVotePrioritize_BecameCurrentUnderVotePassesThroughCurrentSong(t *testing.T) {
	fq := newFakeRoomQueue()
	seedRoomWithThreeSongs(t, fq, "alpha", 1)
	fq.members[1] = map[int]bool{42: true, 7: true}
	inter := NewInteractor(queueAdapter{f: fq}, stubResolver{counts: map[string]int{"alpha": 1}}, 30*time.Second, nil)

	if _, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 42, ""); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	// The target becomes current between the passing vote's load and the
	// queue-owned resolution.
	fq.mu.Lock()
	fq.currentOnNextPrioritize = true
	fq.mu.Unlock()

	out, err := inter.CastPrioritizeVote(context.Background(), "alpha", 2, 7, "")
	if !errors.Is(err, entity.ErrVoteOnCurrentSong) {
		t.Fatalf("expected entity.ErrVoteOnCurrentSong passed through, got %v (out=%+v)", err, out)
	}
	if errors.Is(err, ErrStalePrioritizeSession) {
		t.Fatalf("became-current must NOT be converted to ErrStalePrioritizeSession")
	}
}
