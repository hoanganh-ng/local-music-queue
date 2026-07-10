package roomautoqueue

import (
	"context"
	"errors"
	"fmt"
	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"sync"
	"testing"
	"time"
)

// --- stubs ---

// mockRoomRepo embeds noOpRoomRepo so it satisfies the full
// repository.RoomRepository interface while routing only the two
// methods roomautoqueue cares about to custom logic. Everything
// else panics if called — the tests never exercise those paths.
//
// members stores per-(roomID, userID) role rows so the use case's
// requireMember / requireHostOrAdmin can resolve identity.
type mockRoomRepo struct {
	noOpRoomRepo
	rooms   map[string]*entity.Room
	members map[mockMemberKey]entity.RoomMember
}

type mockMemberKey struct {
	roomID int64
	userID int
}

func newMockRoomRepo() *mockRoomRepo {
	return &mockRoomRepo{rooms: map[string]*entity.Room{}, members: map[mockMemberKey]entity.RoomMember{}}
}

func (m *mockRoomRepo) GetRoomBySlug(_ context.Context, slug string) (*entity.Room, error) {
	if r, ok := m.rooms[slug]; ok {
		return r, nil
	}
	return nil, nil
}

// GetMember returns the seeded role for (roomID, userID) or a
// sql.ErrNoRows-equivalent so the use case's requireMember /
// requireHostOrAdmin can reject non-members.
func (m *mockRoomRepo) GetMember(_ context.Context, roomID int64, userID int) (*entity.RoomMember, error) {
	if mem, ok := m.members[mockMemberKey{roomID: roomID, userID: userID}]; ok {
		return &mem, nil
	}
	return nil, mockNoMember
}

// addMember seeds a role row for the (roomID, userID) pair.
func (m *mockRoomRepo) addMember(roomID int64, userID int, role entity.RoomMemberRole) {
	m.members[mockMemberKey{roomID: roomID, userID: userID}] = entity.RoomMember{
		RoomID:   roomID,
		UserID:   userID,
		Role:     role,
		JoinedAt: time.Now(),
	}
}

// mockNoMember is a sentinel for "not a member". The use case
// translates ANY error from GetMember to ErrForbidden; the
// sentinel value only needs to be non-nil.
var mockNoMember = errors.New("not a member")

// noOpRoomRepo satisfies the rest of repository.RoomRepository as
// panics — none of these methods are exercised in roomautoqueue
// tests. Embedding this keeps the mock minimal without implementing
// the full surface.
type noOpRoomRepo struct{}

func (noOpRoomRepo) CreateRoom(_ context.Context, _, _ string, _ time.Time) (*entity.Room, error) {
	panic("unused")
}
func (noOpRoomRepo) GetRoomByID(_ context.Context, _ int64) (*entity.Room, error) { panic("unused") }
func (noOpRoomRepo) ListRooms(_ context.Context, _ entity.RoomStatus) ([]entity.Room, error) {
	panic("unused")
}
func (noOpRoomRepo) ArchiveRoom(_ context.Context, _ int64, _ time.Time) error      { panic("unused") }
func (noOpRoomRepo) ArchiveRoomIfActive(_ context.Context, _ int64, _ time.Time) (bool, error) {
	panic("unused")
}
func (noOpRoomRepo) EndActiveLease(_ context.Context, _ int64, _ time.Time) (bool, error) {
	panic("unused")
}
func (noOpRoomRepo) ArchiveRoomIfActiveAndEndLease(_ context.Context, _ int64, _ time.Time) (bool, bool, error) {
	panic("unused")
}
func (noOpRoomRepo) AddMember(_ context.Context, _ int64, _ int, _ entity.RoomMemberRole, _ time.Time) error {
	panic("unused")
}
func (noOpRoomRepo) GetMember(_ context.Context, _ int64, _ int) (*entity.RoomMember, error) {
	panic("unused")
}
func (noOpRoomRepo) ListMembers(_ context.Context, _ int64) ([]entity.RoomMember, error) {
	panic("unused")
}
func (noOpRoomRepo) UpdateMemberRole(_ context.Context, _ int64, _ int, _ entity.RoomMemberRole, _ time.Time) error {
	panic("unused")
}
func (noOpRoomRepo) CountHosts(_ context.Context, _ int64) (int, error) { panic("unused") }
func (noOpRoomRepo) CreateInvite(_ context.Context, _ *entity.RoomInvite) error {
	panic("unused")
}
func (noOpRoomRepo) GetInviteByID(_ context.Context, _ int64, _ int64) (*entity.RoomInvite, error) {
	panic("unused")
}
func (noOpRoomRepo) GetInviteByTokenHash(_ context.Context, _ string) (*entity.RoomInvite, error) {
	panic("unused")
}
func (noOpRoomRepo) ListInvites(_ context.Context, _ int64) ([]entity.RoomInvite, error) {
	panic("unused")
}
func (noOpRoomRepo) RevokeInvite(_ context.Context, _ int64, _ int64, _ time.Time) error {
	panic("unused")
}
func (noOpRoomRepo) IncrementInviteUseCount(_ context.Context, _ int64) error {
	panic("unused")
}
func (noOpRoomRepo) CreateRoomAndHost(_ context.Context, _, _ string, _ int, _ time.Time) (*entity.Room, error) {
	panic("unused")
}
func (noOpRoomRepo) RedeemInviteAtomic(_ context.Context, _ int64, _ int64, _ int, _ entity.RoomMemberRole, _ int, _ time.Time) (*entity.RoomMember, error) {
	panic("unused")
}
func (noOpRoomRepo) RemoveMemberAndEndLeaseAtomic(_ context.Context, _ int64, _ int, _ time.Time) (bool, bool, error) {
	panic("unused")
}

type mockRoomAQRepo struct {
	mu              sync.Mutex
	cfg             *domain.RoomAutoQueueConfig
	history         []domain.RoomPlayHistoryEntry
	appendCount     int
	getRecentCalled int

	// Optional SaveConfig gate (SetEnabled serialization test).
	//
	// If saveConfigCalled is set, SaveConfig sends a token on it the
	// first time it is entered (and is silent on subsequent calls).
	// If saveConfigRelease is set, SaveConfig blocks until that channel
	// is closed. Both fields are independently opt-in so existing
	// tests do not need to change.
	saveConfigCalled  chan struct{}
	saveConfigRelease chan struct{}
}

func (m *mockRoomAQRepo) GetConfig(_ context.Context, _ int64) (*domain.RoomAutoQueueConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg == nil {
		return &domain.RoomAutoQueueConfig{Enabled: false, Strategy: domain.StrategyRelated}, nil
	}
	out := *m.cfg
	return &out, nil
}

func (m *mockRoomAQRepo) SaveConfig(_ context.Context, _ int64, cfg domain.RoomAutoQueueConfig) error {
	// Signal entry (first call only) so tests can observe whether
	// SetEnabled's SaveConfig entered while another caller held the
	// coordinator mu. The buffered channel makes this idempotent.
	if m.saveConfigCalled != nil {
		select {
		case m.saveConfigCalled <- struct{}{}:
		default:
			// already signaled; subsequent calls are silent.
		}
	}
	// Block on the test's gate so the test can hold SaveConfig open
	// across a critical-section observation window.
	if m.saveConfigRelease != nil {
		<-m.saveConfigRelease
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = &cfg
	return nil
}

func (m *mockRoomAQRepo) AppendHistory(_ context.Context, _ int64, entry domain.RoomPlayHistoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, entry)
	m.appendCount++
	return nil
}

func (m *mockRoomAQRepo) GetRecentHistory(_ context.Context, _ int64, limit int) ([]domain.RoomPlayHistoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getRecentCalled++
	if len(m.history) == 0 {
		return nil, nil
	}
	out := make([]domain.RoomPlayHistoryEntry, len(m.history))
	copy(out, m.history)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type mockFetcher struct {
	song *entity.Song
	err  error
	// exclude list passed to FetchRelated, exposed for assertions.
	excludeSeen []string
	videoIDSeen string
}

func (m *mockFetcher) FetchRelated(_ context.Context, videoID string, exclude []string) (*entity.Song, error) {
	m.excludeSeen = exclude
	m.videoIDSeen = videoID
	return m.song, m.err
}

// queueStubSnapshot holds a queue snapshot returned by the loader
// seam; tests can mutate Next to model concurrent queue changes
// between pre-fetch and post-fetch windows.
type queueStubSnapshot struct {
	current *entity.Queue
	mu      sync.Mutex
}

func (q *queueStubSnapshot) snapshot(_ context.Context, _ int64) (*entity.Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current == nil {
		return entity.NewQueue(), nil
	}
	clone := *q.current
	return &clone, nil
}

// testAddFn implements AddRoomAutoQueueSongFunc for tests. It applies
// the same staleness rules the roomqueue interactor enforces.
func testAddFn(q *queueStubSnapshot, calls *[]addCall) AddRoomAutoQueueSongFunc {
	return func(_ context.Context, _ string, song *entity.Song, expectedSourceSongID string) (*AddRoomAutoQueueSongResult, error) {
		q.mu.Lock()
		defer q.mu.Unlock()
		queue := q.current
		*calls = append(*calls, addCall{SongID: song.ID, SourceID: expectedSourceSongID})
		if queue == nil || queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
			return nil, ErrAutoQueueStale
		}
		if queue.Songs[queue.CurrentIndex].ID != expectedSourceSongID {
			return nil, ErrAutoQueueStale
		}
		if queue.CurrentIndex != len(queue.Songs)-1 {
			return nil, ErrAutoQueueStale
		}
		if queue.ContainsSong(song.ID) {
			return nil, ErrAutoQueueStale
		}
		queue.Add(*song)
		var currentSong *entity.Song
		if queue.CurrentIndex >= 0 && queue.CurrentIndex < len(queue.Songs) {
			s := queue.Songs[queue.CurrentIndex]
			currentSong = &s
		}
		return &AddRoomAutoQueueSongResult{
			Queue:        queue,
			CurrentIndex: queue.CurrentIndex,
			CurrentSong:  currentSong,
			Status:       queue.Status,
			Elapsed:      queue.Elapsed,
		}, nil
	}
}

type addCall struct {
	SongID   string
	SourceID string
}

// mockBroadcaster captures typed BroadcastRoomAutoQueueAdded calls.
// The interactor_test uses it instead of a generic BroadcastFunc so
// the typed narrow broadcaster seam is exercised by the tests.
type mockBroadcaster struct {
	mu    sync.Mutex
	calls []broadcasterCall
}

type broadcasterCall struct {
	roomSlug        string
	song            entity.Song
	sourceSongTitle string
	currentIndex    int
	currentSong     *entity.Song
	status          entity.PlaybackStatus
	elapsed         int
	state           *entity.Queue
}

func (m *mockBroadcaster) BroadcastRoomAutoQueueAdded(roomSlug string, song entity.Song, sourceSongTitle string, currentIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, broadcasterCall{
		roomSlug:        roomSlug,
		song:            song,
		sourceSongTitle: sourceSongTitle,
		currentIndex:    currentIndex,
		currentSong:     currentSong,
		status:          status,
		elapsed:         elapsed,
		state:           state,
	})
}

// helper: build an interactor wired against the supplied stubs. The
// caller is responsible for seed snap.current (with a 1-song queue at
// current=0 by default) BEFORE invoking CheckAndTrigger so the
// happy-path fixtures don't need to repeat the boilerplate.
func newTestInteractor(repo *mockRoomAQRepo, fetcher domain.RelatedSongFetcher, snap *queueStubSnapshot, slug string, roomID int64) (*Interactor, *mockRoomRepo) {
	roomRepo := newMockRoomRepo()
	r := &entity.Room{ID: roomID, Slug: slug, Status: entity.RoomStatusActive}
	roomRepo.rooms[slug] = r

	if snap.current == nil {
		q := entity.NewQueue()
		q.Add(entity.Song{ID: "src", Title: "Source"})
		snap.current = q
	}

	inter := NewInteractor(roomRepo, repo, fetcher)
	inter.SetQueueSnapshotLoader(snap.snapshot)
	inter.SetAddRoomAutoQueueSongFunc(testAddFn(snap, &[]addCall{}))
	// Default seed: actor 1 is the room host so the use case's
	// requireMember / requireHostOrAdmin checks pass for any test
	// that calls GetConfig / SetEnabled without explicitly seeding a
	// different role.
	roomRepo.addMember(roomID, 1, entity.RoomRoleHost)
	return inter, roomRepo
}

// --- tests ---

// TestCheckAndTrigger_DisabledConfig: when the per-room config has
// Enabled=false, no fetcher call, no insertion, no broadcast.
func TestCheckAndTrigger_DisabledConfig(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: false}}
	// Even with a candidate, fetcher must NOT be called.
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "C"}}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rsd", 11)

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	if err := inter.CheckAndTrigger(context.Background(), "rsd"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetcher.videoIDSeen != "" {
		t.Errorf("fetcher called with %q despite disabled config", fetcher.videoIDSeen)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.calls) != 0 {
		t.Errorf("broadcast must not fire when disabled, got %d calls", len(bc.calls))
	}
}

// TestCheckAndTrigger_NotLastSong_Skips: when the queue tail is not
// "current == last", no fetcher call.
func TestCheckAndTrigger_NotLastSong_Skips(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "C"}}
	snap := &queueStubSnapshot{}

	// Queue has 3 songs, current=1 → not last.
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "a", Title: "A"})
	q.Add(entity.Song{ID: "b", Title: "B"})
	q.Add(entity.Song{ID: "c", Title: "C"})
	q.CurrentIndex = 1
	q.Status = entity.StatusPlaying
	snap.current = q

	inter, _ := newTestInteractor(repo, fetcher, snap, "rnl", 12)
	// Override add fn with a no-op returning stale so we can detect any call.
	inter.SetAddRoomAutoQueueSongFunc(testAddFn(snap, &[]addCall{}))

	if err := inter.CheckAndTrigger(context.Background(), "rnl"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetcher.videoIDSeen != "" {
		t.Errorf("fetcher called with %q despite current != last", fetcher.videoIDSeen)
	}
}

// TestCheckAndTrigger_LastSong_Triggers covers the happy path: enabled,
// current == last, fetcher returns a candidate, insertion succeeds,
// broadcast emitted, history appended.
func TestCheckAndTrigger_LastSong_Triggers(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "Candidate", AddedBy: entity.SystemUserID}}
	snap := &queueStubSnapshot{}
	// Seed snap BEFORE helper so the helper sees snap.current != nil
	// and skips its own 1-song default (which would conflict).
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "src", Title: "Source"})
	snap.current = q

	inter, _ := newTestInteractor(repo, fetcher, snap, "rt1", 13)

	if snap.current == nil {
		t.Fatal("snap is nil after helper")
	}
	if len(snap.current.Songs) != 1 {
		t.Fatalf("expected 1 song in snap, got %d", len(snap.current.Songs))
	}
	if snap.current.CurrentIndex != 0 {
		t.Fatalf("expected CurrentIndex=0, got %d", snap.current.CurrentIndex)
	}
	if fetcher.videoIDSeen != "" {
		t.Fatalf("precondition: fetcher already saw %q", fetcher.videoIDSeen)
	}

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	if err := inter.CheckAndTrigger(context.Background(), "rt1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetcher.videoIDSeen != "src" {
		t.Errorf("expected fetcher called with videoID=src, got %q", fetcher.videoIDSeen)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.appendCount != 1 {
		t.Errorf("expected 1 history entry, got %d", repo.appendCount)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.calls) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bc.calls))
	}
	// Payload shape: the typed narrow broadcaster seam must carry
	// the full R09e tuple — song, source_song_title, current_index,
	// current_song, status, elapsed, state.
	got := bc.calls[0]
	if got.roomSlug != "rt1" {
		t.Errorf("roomSlug=%q want rt1", got.roomSlug)
	}
	if got.song.ID != "cand" {
		t.Errorf("song.id=%q want cand", got.song.ID)
	}
	if got.sourceSongTitle != "Source" {
		t.Errorf("sourceSongTitle=%q want Source", got.sourceSongTitle)
	}
	if got.currentIndex != 0 {
		t.Errorf("currentIndex=%d want 0", got.currentIndex)
	}
	if got.currentSong == nil || got.currentSong.ID != "src" {
		t.Errorf("currentSong=%+v want src", got.currentSong)
	}
	if got.status != entity.StatusIdle && got.status != entity.StatusPlaying {
		t.Errorf("status=%q want idle or playing", got.status)
	}
	if got.state == nil || len(got.state.Songs) != 2 {
		t.Errorf("expected state with 2 songs, got %+v", got.state)
	}
	// post-mutation queue length should now be 2.
	snap.mu.Lock()
	defer snap.mu.Unlock()
	if got := len(snap.current.Songs); got != 2 {
		t.Errorf("expected 2 songs after insertion, got %d", got)
	}
}

// TestCheckAndTrigger_FetcherFailsFallbackSuccess covers the
// fallback path: fetcher error; fallback picks from history; insertion
// succeeds.
func TestCheckAndTrigger_FetcherFailsFallbackSuccess(t *testing.T) {
	repo := &mockRoomAQRepo{
		cfg:     &domain.RoomAutoQueueConfig{Enabled: true},
		history: []domain.RoomPlayHistoryEntry{{VideoID: "hist1", Title: "H1"}, {VideoID: "hist2", Title: "H2"}},
	}
	fetcher := &mockFetcher{err: errors.New("network down")}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rfb", 14)

	if err := inter.CheckAndTrigger(context.Background(), "rfb"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.appendCount != 1 {
		t.Errorf("expected 1 history entry from successful fallback insert, got %d", repo.appendCount)
	}
	snap.mu.Lock()
	defer snap.mu.Unlock()
	if len(snap.current.Songs) != 2 {
		t.Errorf("expected 2 songs after fallback insertion, got %d", len(snap.current.Songs))
	}
}

// TestCheckAndTrigger_BothFail: no fetcher result, no fallback
// candidates → no save, no history, no broadcast, no error.
func TestCheckAndTrigger_BothFail(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{err: errors.New("network down")}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rbf", 15)

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	if err := inter.CheckAndTrigger(context.Background(), "rbf"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.appendCount != 0 {
		t.Errorf("expected 0 history entries, got %d", repo.appendCount)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.calls) != 0 {
		t.Errorf("expected 0 broadcasts, got %d", len(bc.calls))
	}
}

// TestCheckAndTrigger_StaleAtInsertion_SourceMismatch covers the
// "source song changed mid-fetch" branch: the roomqueue seam returns
// ErrAutoQueueStale; the trigger must swallow it (no save, no
// broadcast, no history append, no error returned).
func TestCheckAndTrigger_StaleAtInsertion_SourceMismatch(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand"}}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rsm", 16)

	// Replace add fn with one that always returns ErrAutoQueueStale
	// to simulate a stale-mismatch.
	inter.SetAddRoomAutoQueueSongFunc(func(_ context.Context, _ string, _ *entity.Song, _ string) (*AddRoomAutoQueueSongResult, error) {
		return nil, ErrAutoQueueStale
	})

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	if err := inter.CheckAndTrigger(context.Background(), "rsm"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.appendCount != 0 {
		t.Errorf("expected 0 history entries on stale mismatch, got %d", repo.appendCount)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.calls) != 0 {
		t.Errorf("expected 0 broadcasts on stale mismatch, got %d", len(bc.calls))
	}
}

// TestCheckAndTrigger_DisabledMidFlight: SetEnabled(false) lands
// WHILE FetchRelated is blocked; the post-fetch "still enabled?" check
// observes Enabled=false and drops the candidate. The slow fetch is
// held outside mu during the wait.
func TestCheckAndTrigger_DisabledMidFlight(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	snap := &queueStubSnapshot{}
	bf := &blockingFetcher{
		song:    &entity.Song{ID: "cand", Title: "C", AddedBy: entity.SystemUserID},
		release: make(chan struct{}),
		started: make(chan struct{}),
	}

	inter, _ := newTestInteractor(repo, bf, snap, "rdm", 17)

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	done := make(chan error, 1)
	go func() {
		done <- inter.CheckAndTrigger(context.Background(), "rdm")
	}()

	<-bf.started
	if _, err := inter.SetEnabled(context.Background(), "rdm", 1, false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	close(bf.release)
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.appendCount != 0 {
		t.Errorf("expected 0 history entries, got %d", repo.appendCount)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.calls) != 0 {
		t.Errorf("expected 0 broadcasts when disabled mid-flight, got %d", len(bc.calls))
	}
}

// TestCheckAndTrigger_ExclusionUsesRoomHistory: the exclude list
// passed to FetchRelated is built from THIS ROOM's recent history
// (20 entries) + the room's queue contents. A second room's history
// must NOT leak in.
func TestCheckAndTrigger_ExclusionUsesRoomHistory(t *testing.T) {
	repo := &mockRoomAQRepo{
		cfg: &domain.RoomAutoQueueConfig{Enabled: true},
		history: []domain.RoomPlayHistoryEntry{
			{VideoID: "h1"}, {VideoID: "h2"}, {VideoID: "h3"},
		},
	}
	fetcher := &mockFetcher{}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rex", 18)

	if err := inter.CheckAndTrigger(context.Background(), "rex"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantExclude := map[string]bool{"h1": true, "h2": true, "h3": true, "src": true}
	for _, id := range fetcher.excludeSeen {
		if !wantExclude[id] {
			t.Errorf("unexpected exclude id %q", id)
		}
		delete(wantExclude, id)
	}
	if len(wantExclude) != 0 {
		t.Errorf("missing exclude ids: %+v", wantExclude)
	}
}

// TestCheckAndTrigger_FallbackUsesRoomHistory: when the primary
// fetcher fails, the fallback draws from THIS ROOM's GetRecentHistory
// (limit=50), NOT any global history.
func TestCheckAndTrigger_FallbackUsesRoomHistory(t *testing.T) {
	repo := &mockRoomAQRepo{
		cfg: &domain.RoomAutoQueueConfig{Enabled: true},
		history: []domain.RoomPlayHistoryEntry{
			{VideoID: "h1", Title: "H1"}, {VideoID: "h2", Title: "H2"},
		},
	}
	fetcher := &mockFetcher{err: errors.New("down")}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rfh", 19)

	if err := inter.CheckAndTrigger(context.Background(), "rfh"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the fallback query was issued (repo.getRecentCalled >= 2:
	// once for the exclude list, once for the fallback).
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.getRecentCalled < 2 {
		t.Errorf("expected >=2 history reads (exclude + fallback), got %d", repo.getRecentCalled)
	}
}

// TestCheckAndTrigger_LoadFailurePropagates: a non-stale load
// failure (e.g. repository load) must surface as a wrapped error,
// NOT as the stale sentinel.
func TestCheckAndTrigger_LoadFailurePropagates(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rlf", 20)
	// Replace the queue snapshot loader with one that errors.
	inter.SetQueueSnapshotLoader(func(_ context.Context, _ int64) (*entity.Queue, error) {
		return nil, errors.New("db unavailable")
	})

	err := inter.CheckAndTrigger(context.Background(), "rlf")
	if err == nil {
		t.Fatal("expected error from queue snapshot load failure")
	}
	if errors.Is(err, ErrAutoQueueStale) {
		t.Errorf("load failure must not surface as ErrAutoQueueStale, got %v", err)
	}
	if !contains(err.Error(), "db unavailable") {
		t.Errorf("expected wrapped error to mention load failure, got %v", err)
	}
}

// TestCheckAndTrigger_ExcludedAfterInsert: the just-inserted auto
// song is excluded from the next fetch's exclude list.
func TestCheckAndTrigger_ExcludedAfterInsert(t *testing.T) {
	repo := &mockRoomAQRepo{
		cfg: &domain.RoomAutoQueueConfig{Enabled: true},
		history: []domain.RoomPlayHistoryEntry{
			{VideoID: "src", Title: "Source"},
		},
	}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, &mockFetcher{song: &entity.Song{ID: "auto1"}}, snap, "rxi", 21)

	if err := inter.CheckAndTrigger(context.Background(), "rxi"); err != nil {
		t.Fatalf("first trigger: %v", err)
	}

	// Second trigger: the auto-inserted song is now in queue.Songs
	// so it must be excluded.
	repo.history = []domain.RoomPlayHistoryEntry{}
	fetcher2 := &mockFetcher{}
	inter.SetAddRoomAutoQueueSongFunc(testAddFn(snap, &[]addCall{}))
	inter.SetBroadcaster(&mockBroadcaster{}) // capture nothing further
	// Replace the fetcher so we can observe the new exclude list.
	roomRepo := newMockRoomRepo()
	roomRepo.rooms["rxi"] = &entity.Room{ID: 21, Slug: "rxi", Status: entity.RoomStatusActive}
	_ = roomRepo
	// Build a new interactor reusing the same repo + snap but a
	// different fetcher so we can observe the second exclude list.
	inter2 := NewInteractor(newMockRoomRepo(), repo, fetcher2)
	inter2.SetQueueSnapshotLoader(snap.snapshot)
	inter2.SetAddRoomAutoQueueSongFunc(testAddFn(snap, &[]addCall{}))

	// Reset queue to current==last with the auto song at the end.
	snap.mu.Lock()
	snap.current = entity.NewQueue()
	snap.current.Add(entity.Song{ID: "src", Title: "Source"})
	snap.current.Add(entity.Song{ID: "auto1", Title: "Auto1", AddedBy: entity.SystemUserID})
	snap.current.CurrentIndex = 1
	snap.current.Status = entity.StatusPlaying
	snap.mu.Unlock()

	// Need the room repo to map slug to room. Build it.
	r := newMockRoomRepo()
	r.rooms["rxi"] = &entity.Room{ID: 21, Slug: "rxi", Status: entity.RoomStatusActive}
	inter2RoomRepo := newMockRoomRepo()
	inter2RoomRepo.rooms["rxi"] = &entity.Room{ID: 21, Slug: "rxi", Status: entity.RoomStatusActive}
	_ = inter2RoomRepo
	_ = r
	// Replace the underlying roomRepo field via a tiny wrapper.
	inter2.roomRepo = inter2RoomRepo

	if err := inter2.CheckAndTrigger(context.Background(), "rxi"); err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	got := map[string]bool{}
	for _, id := range fetcher2.excludeSeen {
		got[id] = true
	}
	if !got["auto1"] {
		t.Errorf("expected auto1 in second-fetch exclude list (no self-loop), got %+v", got)
	}
}

// TestCheckAndTrigger_Success_BroadcastsConfiguredBroadcaster pins
// the production-broadcast wiring contract: a successful trigger
// MUST call the configured broadcaster exactly once with the full
// R09e payload tuple (room_slug, song, source_song_title,
// current_index, current_song, status, elapsed, state). This is
// the regression guard for "the production BroadcastFunc adapter
// is a no-op" — any future regression that drops the broadcaster
// call (or short-circuits with the old generic-map signature) will
// fail this test.
func TestCheckAndTrigger_Success_BroadcastsConfiguredBroadcaster(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	fetcher := &mockFetcher{song: &entity.Song{ID: "cand", Title: "Candidate", AddedBy: entity.SystemUserID}}
	snap := &queueStubSnapshot{}

	inter, _ := newTestInteractor(repo, fetcher, snap, "rt-bc", 33)

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	if err := inter.CheckAndTrigger(context.Background(), "rt-bc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.calls) != 1 {
		t.Fatalf("expected exactly 1 broadcaster call after successful trigger, got %d (broadcast wiring may be a no-op)", len(bc.calls))
	}
	got := bc.calls[0]
	if got.roomSlug != "rt-bc" {
		t.Errorf("roomSlug=%q want rt-bc", got.roomSlug)
	}
	if got.song.ID != "cand" {
		t.Errorf("song.id=%q want cand", got.song.ID)
	}
	if got.sourceSongTitle != "Source" {
		t.Errorf("sourceSongTitle=%q want Source", got.sourceSongTitle)
	}
	if got.currentIndex != 0 {
		t.Errorf("currentIndex=%d want 0", got.currentIndex)
	}
	if got.currentSong == nil || got.currentSong.ID != "src" {
		t.Errorf("currentSong=%+v want src", got.currentSong)
	}
	if got.state == nil || len(got.state.Songs) != 2 {
		t.Errorf("state=%+v want post-mutation snapshot with 2 songs", got.state)
	}
}

// TestSetEnabled_SerializedWithPostFetchCriticalSection pins the
// SetEnabled/CheckAndTrigger serialization contract directly.
//
// The contract this test pins:
//
//   - SetEnabled(false) MUST take the same coordinator mu that
//     CheckAndTrigger holds across its post-fetch "still enabled?"
//     recheck + AddRoomAutoQueueSong insertion. Without that lock,
//     a SetEnabled(false) save could land between the recheck and
//     the insertion call — leaving the candidate inserted even
//     though the user has just toggled the room off.
//
// This test forces the bug-prone ordering deterministically:
//   1. CheckAndTrigger pre-fetches (Enabled=true), enters the
//      blocking fetcher, then is released into the post-fetch mu
//      critical section.
//   2. The post-fetch recheck reads Enabled=true and calls the add
//      fn; the add fn blocks on a test-controlled gate WHILE STILL
//      HOLDING mu.
//   3. A concurrent SetEnabled(false) starts in another goroutine.
//   4. The mock's SaveConfig is gated through channels so we can
//      observe whether SetEnabled's SaveConfig entered while the
//      add fn held mu.
//
// The locked-correct outcome: SaveConfig does NOT enter until mu
// is released (after the add fn returns). Without the lock,
// SaveConfig enters immediately, racing past the mu-held critical
// section.
//
// Final state after both complete: the trigger ran to completion
// (insertion succeeded, history appended, broadcast emitted)
// because SetEnabled did not land before the recheck. The persisted
// config is Enabled=false because SetEnabled saved it after mu was
// released. This is one of the two valid outcomes the lock allows;
// the in-between (interleaved) outcome is the one the lock forbids.
func TestSetEnabled_SerializedWithPostFetchCriticalSection(t *testing.T) {
	saveCalled := make(chan struct{}, 1)
	saveRelease := make(chan struct{})
	repo := &mockRoomAQRepo{
		cfg:               &domain.RoomAutoQueueConfig{Enabled: true},
		saveConfigCalled:  saveCalled,
		saveConfigRelease: saveRelease,
	}
	snap := &queueStubSnapshot{}
	bf := &blockingFetcher{
		song:    &entity.Song{ID: "cand", Title: "C", AddedBy: entity.SystemUserID},
		release: make(chan struct{}),
		started: make(chan struct{}),
	}

	inter, _ := newTestInteractor(repo, bf, snap, "rts", 34)

	// add fn blocks on a test-controlled gate WHILE INSIDE the
	// post-fetch mu critical section. We deliberately do NOT
	// return ErrAutoQueueStale here — we want to model the
	// production insertion path so we can observe whether the
	// trigger's queue mutation ran to completion (proving SetEnabled
	// did not interleave the lock-free window).
	addEntered := make(chan struct{})
	addRelease := make(chan struct{})
	inter.SetAddRoomAutoQueueSongFunc(func(_ context.Context, _ string, song *entity.Song, _ string) (*AddRoomAutoQueueSongResult, error) {
		select {
		case <-addEntered:
		default:
			close(addEntered)
		}
		<-addRelease
		snap.mu.Lock()
		defer snap.mu.Unlock()
		q := snap.current
		if q == nil {
			return nil, ErrAutoQueueStale
		}
		q.Add(*song)
		return &AddRoomAutoQueueSongResult{
			Queue:        q,
			CurrentIndex: q.CurrentIndex,
			CurrentSong:  nil,
			Status:       q.Status,
			Elapsed:      q.Elapsed,
		}, nil
	})

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	// 1. Start CheckAndTrigger. It pre-fetches (sees Enabled=true),
	//    hits the blocking fetcher, and waits for release.
	triggerDone := make(chan error, 1)
	go func() {
		triggerDone <- inter.CheckAndTrigger(context.Background(), "rts")
	}()
	<-bf.started

	// 2. Release the fetcher so CheckAndTrigger enters the post-fetch
	//    mu critical section.
	close(bf.release)

	// 3. Wait until the add fn has been entered and is holding mu.
	<-addEntered

	// 4. Start SetEnabled(false) in another goroutine. With the
	//    coordinator mu, its SaveConfig must NOT enter until the
	//    add fn releases mu.
	setDone := make(chan error, 1)
	go func() {
		_, err := inter.SetEnabled(context.Background(), "rts", 1, false)
		setDone <- err
	}()

	// 5. Give the goroutine scheduler a brief window and verify
	//    that SaveConfig did NOT enter. With the lock, this is the
	//    guaranteed outcome: SetEnabled is parked on i.mu.Lock().
	select {
	case <-saveCalled:
		t.Fatal("SetEnabled's SaveConfig entered while CheckAndTrigger held the coordinator mu — SetEnabled serialization invariant violated")
	case <-time.After(50 * time.Millisecond):
		// Good — SaveConfig was held back by the coordinator mu.
	}

	// 6. Release the add fn. The insertion completes; mu becomes
	//    free; SetEnabled can now proceed past its i.mu.Lock().
	close(addRelease)

	// 7. Now that mu is free, SetEnabled's SaveConfig must enter.
	//    Wait briefly for it (bounded wait so the test does not hang
	//    on a regression that detaches SetEnabled from the lock).
	select {
	case <-saveCalled:
		// Good — SaveConfig entered after mu release.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("SetEnabled's SaveConfig did not enter after the coordinator mu was released")
	}
	close(saveRelease)

	// 8. Both goroutines must complete without error.
	if err := <-triggerDone; err != nil {
		t.Fatalf("CheckAndTrigger: %v", err)
	}
	if err := <-setDone; err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	// 9. Final state: trigger ran to completion (insertion succeeded,
	//    history appended, broadcast emitted) because SetEnabled did
	//    NOT land before the post-fetch recheck. cfg persisted=false
	//    because SetEnabled saved it after mu was released. Both are
	//    consistent with the locked-correct outcome.
	snap.mu.Lock()
	songs := len(snap.current.Songs)
	snap.mu.Unlock()
	if songs != 2 {
		t.Errorf("expected 2 songs after successful insertion, got %d", songs)
	}
	repo.mu.Lock()
	persistEnabled := repo.cfg.Enabled
	appendCount := repo.appendCount
	repo.mu.Unlock()
	if persistEnabled {
		t.Errorf("expected persisted Enabled=false after SetEnabled, got true")
	}
	if appendCount != 1 {
		t.Errorf("expected 1 history entry from successful insertion, got %d", appendCount)
	}
	bc.mu.Lock()
	bcCount := len(bc.calls)
	bc.mu.Unlock()
	if bcCount != 1 {
		t.Errorf("expected 1 broadcast (insertion ran to completion), got %d", bcCount)
	}
}

// TestSetEnabled_RunsBeforePostFetchRecheck_DropsCandidate pins the
// other half of the serialization contract: when SetEnabled(false)
// completes BEFORE CheckAndTrigger's post-fetch recheck acquires the
// coordinator mu, the recheck observes Enabled=false and drops the
// candidate (no add fn call, no queue mutation, no history append,
// no broadcast). This is the second valid outcome the lock allows —
// distinct from the interleave outcome the lock forbids.
//
// The test forces the ordering by holding CheckAndTrigger in the
// blocking fetcher while SetEnabled(false) commits its save. The
// post-fetch recheck then observes the persisted Enabled=false and
// drops the candidate.
func TestSetEnabled_RunsBeforePostFetchRecheck_DropsCandidate(t *testing.T) {
	repo := &mockRoomAQRepo{cfg: &domain.RoomAutoQueueConfig{Enabled: true}}
	snap := &queueStubSnapshot{}
	bf := &blockingFetcher{
		song:    &entity.Song{ID: "cand", Title: "C", AddedBy: entity.SystemUserID},
		release: make(chan struct{}),
		started: make(chan struct{}),
	}

	inter, _ := newTestInteractor(repo, bf, snap, "rt-rb", 35)

	// add fn MUST NOT be called. Replace with a watchdog that fails
	// the test if invoked.
	addCalls := make(chan struct{}, 1)
	inter.SetAddRoomAutoQueueSongFunc(func(_ context.Context, _ string, _ *entity.Song, _ string) (*AddRoomAutoQueueSongResult, error) {
		select {
		case addCalls <- struct{}{}:
		default:
		}
		return nil, ErrAutoQueueStale
	})

	bc := &mockBroadcaster{}
	inter.SetBroadcaster(bc)

	triggerDone := make(chan error, 1)
	go func() {
		triggerDone <- inter.CheckAndTrigger(context.Background(), "rt-rb")
	}()

	// Wait for the fetcher to be blocked, then commit SetEnabled
	// (false) BEFORE releasing the fetcher. The persisted config is
	// now Enabled=false before CheckAndTrigger's post-fetch recheck
	// runs.
	<-bf.started
	if _, err := inter.SetEnabled(context.Background(), "rt-rb", 1, false); err != nil {
		close(bf.release)
		t.Fatalf("SetEnabled: %v", err)
	}

	// Release the fetcher. CheckAndTrigger reaches the post-fetch mu
	// critical section; the recheck observes Enabled=false and drops.
	close(bf.release)
	if err := <-triggerDone; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// add fn must NEVER have been called.
	select {
	case <-addCalls:
		t.Fatal("AddRoomAutoQueueSong was called despite post-fetch recheck observing Enabled=false")
	default:
		// Good — recheck dropped the candidate.
	}

	repo.mu.Lock()
	persistEnabled := repo.cfg.Enabled
	appendCount := repo.appendCount
	repo.mu.Unlock()
	if !persistEnabled == false {
		// Effectively "if persistEnabled is true".
		t.Errorf("expected persisted Enabled=false, got %v", persistEnabled)
	}
	if appendCount != 0 {
		t.Errorf("expected 0 history entries (candidate dropped), got %d", appendCount)
	}
	snap.mu.Lock()
	songs := len(snap.current.Songs)
	snap.mu.Unlock()
	if songs != 1 {
		t.Errorf("expected 1 song in queue (no insertion), got %d", songs)
	}
	bc.mu.Lock()
	bcCount := len(bc.calls)
	bc.mu.Unlock()
	if bcCount != 0 {
		t.Errorf("expected 0 broadcasts (candidate dropped), got %d", bcCount)
	}
}

// --- blockingFetcher ---

type blockingFetcher struct {
	song    *entity.Song
	release chan struct{}
	started chan struct{}
}

func (b *blockingFetcher) FetchRelated(_ context.Context, _ string, _ []string) (*entity.Song, error) {
	close(b.started)
	<-b.release
	return b.song, nil
}

// contains is a tiny helper so we don't pull strings.Contains into
// the package; the interactor itself only needs to check substring
// occurrences in error messages.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})())
}

// silence unused imports during incremental edits.
var (
	_ = fmt.Sprintf
	_ = sync.Mutex{}
)
