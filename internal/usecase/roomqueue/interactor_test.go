package roomqueue

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/domain/service"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/room"
)

// stubYouTube is a minimal service.YouTubeService used to drive the
// URL-only AddSong path in tests without spinning up yt-dlp.
type stubYouTube struct {
	song *entity.Song
	err  error
}

func (s *stubYouTube) FetchMetadata(ctx context.Context, url string) (*entity.Song, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.song, nil
}

func (s *stubYouTube) SearchYouTube(ctx context.Context, query string, maxResults int) ([]*entity.SearchResult, error) {
	return nil, nil
}

// lastAddedByID returns the most recently appended song in the queue
// owned by user id, or (zero, false) when none is present. Used to
// verify server-resolved attribution without scraping all entries.
func lastAddedByID(q *entity.Queue, id int) (entity.Song, bool) {
	for i := len(q.Songs) - 1; i >= 0; i-- {
		if q.Songs[i].AddedByID == id {
			return q.Songs[i], true
		}
	}
	return entity.Song{}, false
}

// pgRoomQueue builds an Interactor wired against a per-test PG schema.
// Skips when Postgres is unreachable.
func pgRoomQueue(t *testing.T) (*Interactor, *persistence.PostgresRoomQueueRepository, func()) {
	t.Helper()
	inter, _, queueRepo, cleanup := pgRoomQueueWithDB(t)
	return inter, queueRepo, cleanup
}

// pgRoomQueueWithDB is the R09a extension that also returns the
// underlying *sql.DB so callers (the playback fixture) can wire the
// player-lease repo against the SAME per-test schema as the room/queue
// repos. A separate NewRoomTestDB call would create a fresh schema
// and the lease insert would violate the rooms FK.
func pgRoomQueueWithDB(t *testing.T) (*Interactor, *sql.DB, *persistence.PostgresRoomQueueRepository, func()) {
	t.Helper()
	db, cleanup := persistence.NewRoomTestDB(t)
	now := context.Background()
	if _, err := db.ExecContext(now,
		`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
		 VALUES (42, 'u42@example.com', 'U42', 'guest', 0, NOW(), NOW())
		 ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("seed host user: %v", err)
	}
	if _, err := db.ExecContext(now,
		`INSERT INTO users (id, email, display_name, role, priority_balance, created_at, updated_at)
		 VALUES (200, 'u200@example.com', 'U200', 'guest', 0, NOW(), NOW())
		 ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("seed admin user: %v", err)
	}
	roomRepo := persistence.NewPostgresRoomRepository(db)
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	inter := NewInteractor(roomRepo, queueRepo, nil)
	return inter, db, queueRepo, cleanup
}

func TestRoomQueue_GetState_SeedsEmptyQueueForNewRoom(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-empty", "Empty", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	q, err := inter.GetState(ctx, "rq-empty", 42)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if len(q.Songs) != 0 || q.CurrentIndex != -1 {
		t.Errorf("expected empty queue, got %+v", q)
	}
}

func TestRoomQueue_AddSong_PersistsAndAdvances(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-add", "AddRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	song := &entity.Song{ID: "vid-1", Title: "T1", URL: "https://example/1"}
	q, returned, err := inter.AddSong(ctx, "rq-add", 42, "Host U42", "", &entity.SearchResult{ID: song.ID, Title: song.Title, URL: song.URL})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if returned.ID != "vid-1" {
		t.Errorf("expected returned song id vid-1, got %s", returned.ID)
	}
	if returned.AddedByID != 42 || returned.AddedBy != "Host U42" {
		t.Errorf("expected returned attribution AddedByID=42 AddedBy=Host U42, got AddedByID=%d AddedBy=%q", returned.AddedByID, returned.AddedBy)
	}
	if len(q.Songs) != 1 || q.CurrentIndex != 0 || q.Status != entity.StatusPlaying {
		t.Errorf("expected 1 song at index 0 playing, got %+v", q)
	}
	if added, ok := lastAddedByID(q, 42); !ok {
		t.Errorf("expected persisted song to carry AddedByID=42")
	} else if added.AddedBy != "Host U42" {
		t.Errorf("expected persisted song AddedBy=Host U42, got %q", added.AddedBy)
	}
}

func TestRoomQueue_AddSong_RejectsDuplicateUpcoming(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-dup", "DupRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-dup")
	// Seed: a current song at index 0 and an upcoming song at index 1.
	// entity.Queue.ContainsSong only inspects the upcoming slice, so the
	// third add (which targets the upcoming id) must be rejected.
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "cur-1", Title: "Cur1", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "dup-1", Title: "Dup", URL: "u", AddedBy: "Host U42", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := inter.queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	_, _, err := inter.AddSong(ctx, "rq-dup", 42, "Host U42", "", &entity.SearchResult{ID: "dup-1", Title: "Dup", URL: "u"})
	if !errors.Is(err, entity.ErrSongAlreadyInQueue) {
		t.Fatalf("expected ErrSongAlreadyInQueue for upcoming duplicate, got %v", err)
	}
}

func TestRoomQueue_AddSong_AllowsDuplicateOfCurrentSong(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-dup-current", "DupCurRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	// Add two songs; the first becomes the current track, the second is upcoming.
	if _, _, err := inter.AddSong(ctx, "rq-dup-current", 42, "Host U42", "", &entity.SearchResult{ID: "cur-1", Title: "Cur1", URL: "https://example/cur1"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, _, err := inter.AddSong(ctx, "rq-dup-current", 42, "Host U42", "", &entity.SearchResult{ID: "cur-2", Title: "Cur2", URL: "https://example/cur2"}); err != nil {
		t.Fatalf("second add: %v", err)
	}
	// A third song whose ID matches the current track must be allowed: the
	// global entity.Queue.ContainsSong invariant only checks upcoming songs,
	// so the current song may be requeued by another add.
	q, _, err := inter.AddSong(ctx, "rq-dup-current", 42, "Host U42", "", &entity.SearchResult{ID: "cur-1", Title: "Cur1", URL: "https://example/cur1"})
	if err != nil {
		t.Fatalf("expected current duplicate to be allowed, got %v", err)
	}
	count := 0
	for _, s := range q.Songs {
		if s.ID == "cur-1" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected duplicate of current song to be appended (count=2), got %d", count)
	}
}

func TestRoomQueue_AddSong_AllowsDuplicateOfPlayedSong(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-dup-played", "DupPlayedRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	// Seed a queue with a played song (CurrentIndex past it), a current
	// song, and an upcoming song. The replayed ID should be permitted
	// because ContainsSong only looks at upcoming.
	roomID := mustRoomID(t, inter, "rq-dup-played")
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "played-1", Title: "Played1", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "current-1", Title: "Curr1", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "upcoming-1", Title: "Up1", URL: "u", AddedBy: "Host U42", AddedByID: 42},
	}
	seed.CurrentIndex = 1
	seed.Status = entity.StatusPlaying
	if err := inter.queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	q, _, err := inter.AddSong(ctx, "rq-dup-played", 42, "Host U42", "", &entity.SearchResult{ID: "played-1", Title: "Played1", URL: "u"})
	if err != nil {
		t.Fatalf("expected replay of already-played song to be allowed, got %v", err)
	}
	count := 0
	for _, s := range q.Songs {
		if s.ID == "played-1" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected replayed ID to be persisted (count=2), got %d", count)
	}
}

// TestRoomQueue_AddSong_URLOnlyStampsServerIdentity covers task (2)
// above: when the handler drives a bare-URL add (no metadata body) and
// supplies only the server-resolved actor, the song persisted in the
// queue must carry the actor's AddedBy / AddedByID.
func TestRoomQueue_AddSong_URLOnlyStampsServerIdentity(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-url", "URLRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	stub := &stubYouTube{song: &entity.Song{
		ID: "yt-vid", Title: "YT", Artist: "Channel", Duration: 100, Thumbnail: "t", URL: "https://youtube.com/watch?v=yt-vid",
	}}
	inter.SetYouTube(stub)
	q, returned, err := inter.AddSong(ctx, "rq-url", 42, "Host U42", "https://youtube.com/watch?v=yt-vid", nil)
	if err != nil {
		t.Fatalf("url add: %v", err)
	}
	if returned.AddedByID != 42 || returned.AddedBy != "Host U42" {
		t.Errorf("expected returned song AddedByID=42 AddedBy=Host U42, got AddedByID=%d AddedBy=%q", returned.AddedByID, returned.AddedBy)
	}
	if len(q.Songs) != 1 {
		t.Fatalf("expected 1 song persisted, got %d", len(q.Songs))
	}
	if q.Songs[0].AddedByID != 42 || q.Songs[0].AddedBy != "Host U42" {
		t.Errorf("expected persisted song AddedByID=42 AddedBy=Host U42, got AddedByID=%d AddedBy=%q", q.Songs[0].AddedByID, q.Songs[0].AddedBy)
	}
}

func TestRoomQueue_RemoveSong_GuestCannotRemoveOthers(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-rm", "RmRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	if err := inter.roomRepo.AddMember(ctx, mustRoomID(t, inter, "rq-rm"), 200, entity.RoomRoleGuest, testTime()); err != nil {
		t.Fatalf("add guest: %v", err)
	}
	// Host adds a song.
	if _, _, err := inter.AddSong(ctx, "rq-rm", 42, "Host U42", "", &entity.SearchResult{ID: "h-1", Title: "H1", URL: "https://example/h1"}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	// Guest 200 tries to remove the host's current song (index 0).
	// Ownership check fires first → ErrNotSongOwner.
	_, err := inter.RemoveSong(ctx, "rq-rm", 200, entity.RoomRoleGuest, 0)
	if !errors.Is(err, ErrNotSongOwner) {
		t.Fatalf("expected ErrNotSongOwner, got %v", err)
	}
}

func TestRoomQueue_ClearQueue_ForbiddenForGuest(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-clear", "ClearRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	if err := inter.roomRepo.AddMember(ctx, mustRoomID(t, inter, "rq-clear"), 200, entity.RoomRoleGuest, testTime()); err != nil {
		t.Fatalf("seed guest: %v", err)
	}
	_, err := inter.ClearQueue(ctx, "rq-clear", 200, entity.RoomRoleGuest)
	if !errors.Is(err, room.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestRoomQueue_GetState_ForbiddenForNonMember(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-nm", "NMRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	_, err := inter.GetState(ctx, "rq-nm", 999)
	if !errors.Is(err, room.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

// TestRoomQueue_Broadcaster_SetterAcceptsStub verifies the interactor
// accepts a non-nil broadcaster seam. The seam is used by the delivery
// layer; the interactor itself does not broadcast.
func TestRoomQueue_Broadcaster_SetterAcceptsStub(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()

	stub := &recordingBroadcaster{}
	inter.SetBroadcaster(stub)
	if got := inter.Broadcaster(); got != stub {
		t.Fatalf("expected broadcaster to be wired, got %v", got)
	}
}

// recordingBroadcaster is a no-op roomqueue.Broadcaster for tests.
type recordingBroadcaster struct {
	mu      sync.Mutex
	syncN   int
	addN    int
	removeN int
	clearN  int
	prioN   int
	statusN int
	elapsedN int
	advancedN int
	voteUpdatedN  int
	voteResolvedN int
}

func (r *recordingBroadcaster) BroadcastRoomQueueSync(_ string, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncN++
}
func (r *recordingBroadcaster) BroadcastRoomQueueSongAdded(_ string, _ entity.Song, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addN++
}
func (r *recordingBroadcaster) BroadcastRoomQueueSongRemoved(_ string, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeN++
}
func (r *recordingBroadcaster) BroadcastRoomQueueCleared(_ string, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearN++
}
func (r *recordingBroadcaster) BroadcastRoomQueueSongPrioritized(_ string, _, _ int, _ entity.Song, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prioN++
}
func (r *recordingBroadcaster) BroadcastRoomPlaybackStatusChanged(_ string, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusN++
}
func (r *recordingBroadcaster) BroadcastRoomPlaybackElapsedSync(_ string, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.elapsedN++
}
func (r *recordingBroadcaster) BroadcastRoomPlaybackSongAdvanced(_ string, _ string, _, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.advancedN++
}
func (r *recordingBroadcaster) BroadcastRoomVoteUpdated(_ string, _ *entity.VoteSession, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.voteUpdatedN++
}
func (r *recordingBroadcaster) BroadcastRoomVoteResolved(_, _, _ string, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.voteResolvedN++
}

// --- helpers ---

func testTime() (t time.Time) {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func mustRoomID(t *testing.T, inter *Interactor, slug string) int64 {
	t.Helper()
	r, err := inter.roomRepo.GetRoomBySlug(context.Background(), slug)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	return r.ID
}

// setAddedByID mutates the persisted JSON to set AddedByID for the song
// whose id matches songID, so the test can simulate "song owned by a
// specific user" without re-running the metadata path. Retained for
// future tests that need ownership simulation.
func setAddedByID(t *testing.T, ctx context.Context, inter *Interactor, slug string, songID string, oldID, newID int) error {
	t.Helper()
	q, err := inter.queueRepo.Load(ctx, mustRoomID(t, inter, slug))
	if err != nil {
		return err
	}
	for i := range q.Songs {
		if q.Songs[i].ID == songID && q.Songs[i].AddedByID == oldID {
			q.Songs[i].AddedByID = newID
		}
	}
	return inter.queueRepo.Save(ctx, mustRoomID(t, inter, slug), q)
}

// guard against unused imports when tests are added incrementally.
var (
	_ = sync.Mutex{}
	_ = repository.RoomQueueRepository(nil)
	_ = repository.ErrRoomQueueNotFound
	_ = service.YouTubeService((*stubYouTube)(nil))
)

// --- R07d PrioritizeSong tests ---

// seedPrioritizeQueue creates a room with three songs: current at 0,
// upcoming at 1 and 2. Returns the room id and the interactor.
func seedPrioritizeQueue(t *testing.T, slug string) (*Interactor, int64) {
	t.Helper()
	inter, _, cleanup := pgRoomQueue(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, slug, "PR-"+slug, 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomID(t, inter, slug)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "cur", Title: "Cur", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "up1", Title: "Up1", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "up2", Title: "Up2", URL: "u", AddedBy: "Host U42", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := inter.queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	return inter, roomID
}

// seedVoteQueue creates a room with three songs: current at 0, upcoming
// at 1 and 2. Songs have IDs "vote-cur", "vote-next", "vote-future" so
// vote tests can pin expectedSongID without colliding with other
// fixtures.
func seedVoteQueue(t *testing.T, slug string) (*Interactor, int64) {
	t.Helper()
	inter, _, cleanup := pgRoomQueue(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, slug, "VR-"+slug, 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomID(t, inter, slug)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "vote-cur", Title: "Cur", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "vote-next", Title: "Next", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "vote-future", Title: "Future", URL: "u", AddedBy: "Host U42", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := inter.queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	return inter, roomID
}

func TestRoomQueue_PrioritizeSong_HostMovesUpcomingToCurrentPlusOne(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-prio-ok")
	ctx := context.Background()

	queue, fromIndex, toIndex, song, err := inter.PrioritizeSong(ctx, "rq-prio-ok", 42, entity.RoomRoleHost, 2)
	if err != nil {
		t.Fatalf("prioritize: %v", err)
	}
	if fromIndex != 2 || toIndex != 1 {
		t.Errorf("expected from=2 to=1, got from=%d to=%d", fromIndex, toIndex)
	}
	if song.ID != "up2" {
		t.Errorf("expected moved song id up2, got %q", song.ID)
	}
	if len(queue.Songs) != 3 {
		t.Fatalf("expected 3 songs, got %d", len(queue.Songs))
	}
	if queue.Songs[1].ID != "up2" {
		t.Errorf("expected up2 at index 1 after prioritize, got %q", queue.Songs[1].ID)
	}
	if !queue.Songs[1].IsPrioritized {
		t.Errorf("expected IsPrioritized=true on moved song")
	}

	// Persisted state must match.
	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if persisted.Songs[1].ID != "up2" || !persisted.Songs[1].IsPrioritized {
		t.Errorf("persisted: expected up2 prioritized at idx 1, got %+v", persisted.Songs[1])
	}
}

func TestRoomQueue_PrioritizeSong_InvalidIndexReturnsErrInvalidIndex(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rq-prio-bad-idx")
	ctx := context.Background()

	for _, idx := range []int{-1, 3, 99} {
		_, _, _, _, err := inter.PrioritizeSong(ctx, "rq-prio-bad-idx", 42, entity.RoomRoleHost, idx)
		if !errors.Is(err, ErrInvalidIndex) {
			t.Errorf("idx=%d: expected ErrInvalidIndex, got %v", idx, err)
		}
	}
}

func TestRoomQueue_PrioritizeSong_CurrentSongReturnsErrCannotPrioritizeCurrent(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rq-prio-current")
	ctx := context.Background()

	_, _, _, _, err := inter.PrioritizeSong(ctx, "rq-prio-current", 42, entity.RoomRoleHost, 0)
	if !errors.Is(err, ErrCannotPrioritizeCurrent) {
		t.Fatalf("expected ErrCannotPrioritizeCurrent, got %v", err)
	}
}

func TestRoomQueue_PrioritizeSong_GuestForbidden(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rq-prio-guest")
	ctx := context.Background()

	if err := inter.roomRepo.AddMember(ctx, mustRoomID(t, inter, "rq-prio-guest"), 200, entity.RoomRoleGuest, testTime()); err != nil {
		t.Fatalf("add guest: %v", err)
	}
	_, _, _, _, err := inter.PrioritizeSong(ctx, "rq-prio-guest", 200, entity.RoomRoleGuest, 1)
	if !errors.Is(err, room.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for guest, got %v", err)
	}
}

func TestRoomQueue_PrioritizeSong_NonMemberForbidden(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rq-prio-nm")
	ctx := context.Background()

	_, _, _, _, err := inter.PrioritizeSong(ctx, "rq-prio-nm", 999, entity.RoomRoleHost, 1)
	if !errors.Is(err, room.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for non-member, got %v", err)
	}
}

func TestRoomQueue_PrioritizeSong_ArchivedRoomReturnsErrArchived(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-prio-archived")
	ctx := context.Background()

	if err := inter.roomRepo.ArchiveRoom(ctx, roomID, testTime()); err != nil {
		t.Fatalf("archive: %v", err)
	}
	_, _, _, _, err := inter.PrioritizeSong(ctx, "rq-prio-archived", 42, entity.RoomRoleHost, 1)
	if !errors.Is(err, room.ErrArchived) {
		t.Fatalf("expected ErrArchived, got %v", err)
	}
}

func TestRoomQueue_PrioritizeSong_NoBroadcastOnError(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rq-prio-nobc")
	ctx := context.Background()

	bc := &recordingBroadcaster{}
	inter.SetBroadcaster(bc)

	// Trigger an error (current song) — broadcaster must NOT fire.
	_, _, _, _, _ = inter.PrioritizeSong(ctx, "rq-prio-nobc", 42, entity.RoomRoleHost, 0)
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if bc.prioN != 0 {
		t.Errorf("expected 0 prioritize broadcasts on error, got %d", bc.prioN)
	}
}

// TestRoomQueue_PrioritizeSong_ReturnedSongIsPrioritizedTrue covers the
// R07d important-fix invariant: the song returned by PrioritizeSong
// (which the handler forwards to the broadcaster) MUST carry
// IsPrioritized=true. The pre-fix code returned the pre-mutation
// snapshot, which leaked the un-stamped copy and broke the front-end
// visual indicator. This test pins the post-mutation behavior.
func TestRoomQueue_PrioritizeSong_ReturnedSongIsPrioritizedTrue(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rq-prio-bc-song")
	ctx := context.Background()

	_, _, _, song, err := inter.PrioritizeSong(ctx, "rq-prio-bc-song", 42, entity.RoomRoleHost, 2)
	if err != nil {
		t.Fatalf("prioritize: %v", err)
	}
	if !song.IsPrioritized {
		t.Errorf("expected returned song IsPrioritized=true, got %+v", song)
	}
	if song.ID != "up2" {
		t.Errorf("expected returned song id up2, got %q", song.ID)
	}
}

// --- R09a playback tests ---
//
// The playback fixture wires the real PlayerLeaseInteractor against
// the SAME per-test schema as the room/queue repos so the R06 lease
// invariants are exercised end-to-end. The interactor's
// SetLeaseAuthorizer seam is the under-test boundary. This matches the
// production wiring in cmd/server/main.go.

// playbackFixture creates a room + 2-song queue with current at 0 and
// claims the player lease as holderID (host user 42 by default).
// Returns the interactor, the lease interactor (for tests that need
// to re-shape the lease), and a cleanup.
func playbackFixture(t *testing.T, slug string, holderID int) (*Interactor, *room.PlayerLeaseInteractor, func()) {
	t.Helper()
	inter, db, _, cleanup := pgRoomQueueWithDB(t)
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, slug, "PB-"+slug, 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomID(t, inter, slug)
	q := entity.NewQueue()
	q.Songs = []entity.Song{
		{ID: "cur", Title: "Cur", URL: "https://x/cur"},
		{ID: "next", Title: "Next", URL: "https://x/next"},
	}
	q.CurrentIndex = 0
	q.Status = entity.StatusPlaying
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("save seed queue: %v", err)
	}
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	leaseInter := room.NewPlayerLeaseInteractor(leaseRepo, inter.roomRepo, db, room.DefaultLeaseDuration, room.DefaultLeaseGrace)
	if _, err := leaseInter.Claim(ctx, slug, holderID); err != nil {
		t.Fatalf("claim lease for holder %d: %v", holderID, err)
	}
	inter.SetLeaseAuthorizer(leaseInter)
	return inter, leaseInter, cleanup
}

// allowAllLeaseAuthorizer is a no-op PlaybackLeaseAuthorizer used by
// tests that want to exercise the post-lease code paths
// (no-current-song / no-next-song) without claiming a real lease.
type allowAllLeaseAuthorizer struct{}

func (allowAllLeaseAuthorizer) RequireActiveLeaseHolder(_ context.Context, _ string, _ int) error { return nil }

// TestRoomPlayback_SetPlaybackStatus_HappyPath
// pins the success path: holder transitions playing→paused, returned
// queue reflects the new status, persisted state mirrors.
func TestRoomPlayback_SetPlaybackStatus_HappyPath(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-pb-status", 42)
	defer cleanup()
	ctx := context.Background()

	q, err := inter.SetPlaybackStatus(ctx, "rq-pb-status", 42, entity.StatusPaused)
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if q.Status != entity.StatusPaused {
		t.Errorf("expected returned status=paused, got %q", q.Status)
	}
	persisted, err := inter.GetState(ctx, "rq-pb-status", 42)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if persisted.Status != entity.StatusPaused {
		t.Errorf("expected persisted status=paused, got %q", persisted.Status)
	}
}

// TestRoomPlayback_SetPlaybackStatus_InvalidStatusReturnsErrInvalidStatus
func TestRoomPlayback_SetPlaybackStatus_InvalidStatusReturnsErrInvalidStatus(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-pb-bad-status", 42)
	defer cleanup()
	ctx := context.Background()

	for _, status := range []entity.PlaybackStatus{entity.StatusIdle, ""} {
		_, err := inter.SetPlaybackStatus(ctx, "rq-pb-bad-status", 42, status)
		if !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("status=%q: expected ErrInvalidStatus, got %v", status, err)
		}
	}
}

// TestRoomPlayback_SetPlaybackStatus_NoCurrentSongReturnsErrNoCurrentSong
func TestRoomPlayback_SetPlaybackStatus_NoCurrentSongReturnsErrNoCurrentSong(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-pb-empty", "PB-Empty", 42, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	inter.SetLeaseAuthorizer(allowAllLeaseAuthorizer{})

	_, err := inter.SetPlaybackStatus(ctx, "rq-pb-empty", 42, entity.StatusPlaying)
	if !errors.Is(err, ErrNoCurrentSong) {
		t.Fatalf("expected ErrNoCurrentSong, got %v", err)
	}
}

// TestRoomPlayback_SetPlaybackStatus_LeaseMissingReturnsErrPlaybackLeaseLost
func TestRoomPlayback_SetPlaybackStatus_LeaseMissingReturnsErrPlaybackLeaseLost(t *testing.T) {
	inter, db, _, cleanup := pgRoomQueueWithDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-pb-nolease", "PB", 42, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-pb-nolease")
	q := entity.NewQueue()
	q.Songs = []entity.Song{{ID: "x", Title: "X"}}
	q.CurrentIndex = 0
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	leaseInter := room.NewPlayerLeaseInteractor(leaseRepo, inter.roomRepo, db, room.DefaultLeaseDuration, room.DefaultLeaseGrace)
	inter.SetLeaseAuthorizer(leaseInter)

	_, err := inter.SetPlaybackStatus(ctx, "rq-pb-nolease", 42, entity.StatusPaused)
	if !errors.Is(err, ErrPlaybackLeaseLost) {
		t.Fatalf("expected ErrPlaybackLeaseLost, got %v", err)
	}
}

// TestRoomPlayback_SetPlaybackStatus_NonHolderReturnsErrPlaybackForbidden
// builds the fixture manually so we can claim the lease as user 200
// (the room's host) and then have user 42 (a guest member, not the
// lease holder) attempt a playback mutation. The unique-host
// constraint means we create the room with 200 as the host and add 42
// as a guest afterwards.
func TestRoomPlayback_SetPlaybackStatus_NonHolderReturnsErrPlaybackForbidden(t *testing.T) {
	inter, db, _, cleanup := pgRoomQueueWithDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-pb-nonholder", "PB", 200, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-pb-nonholder")
	q := entity.NewQueue()
	q.Songs = []entity.Song{{ID: "x", Title: "X"}, {ID: "y", Title: "Y"}}
	q.CurrentIndex = 0
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Add 42 as a guest member so they have access to the room but
	// cannot hold the lease (which belongs to host 200).
	if err := inter.roomRepo.AddMember(ctx, roomID, 42, entity.RoomRoleGuest, testTime()); err != nil {
		t.Fatalf("add guest 42: %v", err)
	}
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	leaseInter := room.NewPlayerLeaseInteractor(leaseRepo, inter.roomRepo, db, room.DefaultLeaseDuration, room.DefaultLeaseGrace)
	if _, err := leaseInter.Claim(ctx, "rq-pb-nonholder", 200); err != nil {
		t.Fatalf("claim as 200: %v", err)
	}
	inter.SetLeaseAuthorizer(leaseInter)

	// 42 is the room host but NOT the lease holder; the call must be rejected.
	_, err := inter.SetPlaybackStatus(ctx, "rq-pb-nonholder", 42, entity.StatusPaused)
	if !errors.Is(err, ErrPlaybackForbidden) {
		t.Fatalf("expected ErrPlaybackForbidden, got %v", err)
	}
}

// TestRoomPlayback_SetPlaybackStatus_LeaseGoneReturnsErrPlaybackLeaseGone
func TestRoomPlayback_SetPlaybackStatus_LeaseGoneReturnsErrPlaybackLeaseGone(t *testing.T) {
	inter, leaseInter, cleanup := playbackFixture(t, "rq-pb-gone", 42)
	defer cleanup()
	ctx := context.Background()
	// Force the lease interactor's clock forward past expiry + grace.
	future := testTime().Add(room.DefaultLeaseDuration + room.DefaultLeaseGrace + time.Second)
	leaseInter.SetClock(func() time.Time { return future })

	_, err := inter.SetPlaybackStatus(ctx, "rq-pb-gone", 42, entity.StatusPaused)
	if !errors.Is(err, ErrPlaybackLeaseGone) {
		t.Fatalf("expected ErrPlaybackLeaseGone, got %v", err)
	}
}

// TestRoomPlayback_SetPlaybackStatus_ArchivedRoomReturnsErrArchived
func TestRoomPlayback_SetPlaybackStatus_ArchivedRoomReturnsErrArchived(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-pb-archived", 42)
	defer cleanup()
	ctx := context.Background()
	if err := inter.roomRepo.ArchiveRoom(ctx, mustRoomID(t, inter, "rq-pb-archived"), testTime()); err != nil {
		t.Fatalf("archive: %v", err)
	}

	_, err := inter.SetPlaybackStatus(ctx, "rq-pb-archived", 42, entity.StatusPaused)
	if !errors.Is(err, room.ErrArchived) {
		t.Fatalf("expected room.ErrArchived, got %v", err)
	}
}

// TestRoomPlayback_SyncPlaybackElapsed_HappyPath
func TestRoomPlayback_SyncPlaybackElapsed_HappyPath(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-pb-sync", 42)
	defer cleanup()
	ctx := context.Background()

	q, err := inter.SyncPlaybackElapsed(ctx, "rq-pb-sync", 42, 25)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if q.Elapsed != 25 {
		t.Errorf("expected returned elapsed=25, got %d", q.Elapsed)
	}
	persisted, _ := inter.GetState(ctx, "rq-pb-sync", 42)
	if persisted.Elapsed != 25 {
		t.Errorf("expected persisted elapsed=25, got %d", persisted.Elapsed)
	}
}

// TestRoomPlayback_SyncPlaybackElapsed_NegativeElapsedReturnsErrInvalidElapsed
func TestRoomPlayback_SyncPlaybackElapsed_NegativeElapsedReturnsErrInvalidElapsed(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-pb-neg", 42)
	defer cleanup()
	ctx := context.Background()

	_, err := inter.SyncPlaybackElapsed(ctx, "rq-pb-neg", 42, -1)
	if !errors.Is(err, ErrInvalidElapsed) {
		t.Fatalf("expected ErrInvalidElapsed, got %v", err)
	}
}

// TestRoomPlayback_SkipPlayback_HappyPath
func TestRoomPlayback_SkipPlayback_HappyPath(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-pb-skip", 42)
	defer cleanup()
	ctx := context.Background()

	q, prev, next, song, err := inter.SkipPlayback(ctx, "rq-pb-skip", 42)
	if err != nil {
		t.Fatalf("skip: %v", err)
	}
	if prev != 0 || next != 1 {
		t.Errorf("expected prev=0 next=1, got prev=%d next=%d", prev, next)
	}
	if q.CurrentIndex != 1 || q.Status != entity.StatusPlaying || q.Elapsed != 0 {
		t.Errorf("expected post-mutation current=1 playing elapsed=0, got %+v", q)
	}
	if song == nil || song.ID != "next" {
		t.Errorf("expected advanced song id=next, got %+v", song)
	}
}

// TestRoomPlayback_SkipPlayback_NoNextSongReturnsErrNoNextSong
// pins the no-partial-mutation invariant: skip with no next song must
// leave the queue unchanged.
func TestRoomPlayback_SkipPlayback_NoNextSongReturnsErrNoNextSong(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-pb-skipnone", "PB", 42, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-pb-skipnone")
	q := entity.NewQueue()
	q.Songs = []entity.Song{{ID: "only", Title: "Only"}}
	q.CurrentIndex = 0
	q.Status = entity.StatusPlaying
	q.Elapsed = 99
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("seed: %v", err)
	}
	inter.SetLeaseAuthorizer(allowAllLeaseAuthorizer{})

	_, _, _, _, err := inter.SkipPlayback(ctx, "rq-pb-skipnone", 42)
	if !errors.Is(err, ErrNoNextSong) {
		t.Fatalf("expected ErrNoNextSong, got %v", err)
	}
	persisted, _ := inter.GetState(ctx, "rq-pb-skipnone", 42)
	if persisted.Elapsed != 99 || persisted.Status != entity.StatusPlaying || persisted.CurrentIndex != 0 {
		t.Errorf("queue was partially mutated on no-next skip: %+v", persisted)
	}
}

// TestRoomPlayback_PlaybackEnded_HappyPath_AdvancesWhenNextExists
func TestRoomPlayback_PlaybackEnded_HappyPath_AdvancesWhenNextExists(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-pb-ended", 42)
	defer cleanup()
	ctx := context.Background()

	q, prev, next, song, advanced, err := inter.PlaybackEnded(ctx, "rq-pb-ended", 42)
	if err != nil {
		t.Fatalf("ended: %v", err)
	}
	if !advanced {
		t.Errorf("expected advanced=true")
	}
	if prev != 0 || next != 1 {
		t.Errorf("expected prev=0 next=1, got prev=%d next=%d", prev, next)
	}
	if q.CurrentIndex != 1 || q.Status != entity.StatusPlaying || q.Elapsed != 0 {
		t.Errorf("expected post-mutation current=1 playing elapsed=0, got %+v", q)
	}
	if song == nil || song.ID != "next" {
		t.Errorf("expected advanced song id=next, got %+v", song)
	}
}

// TestRoomPlayback_PlaybackEnded_NoNextSongPausesAndPersists
func TestRoomPlayback_PlaybackEnded_NoNextSongPausesAndPersists(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-pb-endnone", "PB", 42, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-pb-endnone")
	q := entity.NewQueue()
	q.Songs = []entity.Song{{ID: "final", Title: "Final"}}
	q.CurrentIndex = 0
	q.Status = entity.StatusPlaying
	q.Elapsed = 77
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("seed: %v", err)
	}
	inter.SetLeaseAuthorizer(allowAllLeaseAuthorizer{})

	out, prev, next, song, advanced, err := inter.PlaybackEnded(ctx, "rq-pb-endnone", 42)
	if err != nil {
		t.Fatalf("ended: %v", err)
	}
	if advanced {
		t.Errorf("expected advanced=false at end-of-queue")
	}
	if prev != 0 || next != 0 {
		t.Errorf("expected prev=0 next=0 (no advance), got prev=%d next=%d", prev, next)
	}
	if out.Status != entity.StatusPaused {
		t.Errorf("expected status=paused, got %q", out.Status)
	}
	if out.Elapsed != 0 {
		t.Errorf("expected elapsed=0 on end-of-queue pause, got %d", out.Elapsed)
	}
	if song == nil || song.ID != "final" {
		t.Errorf("expected final song in payload, got %+v", song)
	}
}

// --- R09b vote-driven SkipVote tests ---

// TestRoomQueue_SkipVote_HappyPathAdvancesAndPersists pins the lease-
// bypassing happy path: SkipVote advances 0→1 and persists.
func TestRoomQueue_SkipVote_HappyPathAdvancesAndPersists(t *testing.T) {
	inter, roomID := seedVoteQueue(t, "rq-vote-ok")
	ctx := context.Background()

	queue, prevIdx, newIdx, song, err := inter.SkipVote(ctx, "rq-vote-ok", "vote-cur")
	if err != nil {
		t.Fatalf("skip vote: %v", err)
	}
	if prevIdx != 0 || newIdx != 1 {
		t.Errorf("expected prev=0 new=1, got prev=%d new=%d", prevIdx, newIdx)
	}
	if song == nil || song.ID != "vote-next" {
		t.Errorf("expected returned song id=vote-next, got %+v", song)
	}
	if queue.CurrentIndex != 1 || queue.Songs[queue.CurrentIndex].ID != "vote-next" {
		t.Errorf("expected queue advanced to vote-next@1, got %+v", queue)
	}

	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if persisted.CurrentIndex != 1 || persisted.Songs[1].ID != "vote-next" {
		t.Errorf("expected persisted advance to vote-next@1, got %+v", persisted)
	}
}

// TestRoomQueue_SkipVote_StaleExpectedSongReturnsErrStaleSkipVoteNoMutation
// pins the no-partial-mutation invariant: when the queue's current
// song has moved past expectedSongID, SkipVote refuses without
// mutating state.
func TestRoomQueue_SkipVote_StaleExpectedSongReturnsErrStaleSkipVoteNoMutation(t *testing.T) {
	inter, _ := seedVoteQueue(t, "rq-vote-stale")
	ctx := context.Background()

	// Force-advance the queue via the lease-only SkipPlayback path so
	// the persisted current song becomes "vote-next" instead of
	// "vote-cur".
	inter.SetLeaseAuthorizer(allowAllLeaseAuthorizer{})
	if _, _, _, _, err := inter.SkipPlayback(ctx, "rq-vote-stale", 42); err != nil {
		t.Fatalf("advance via SkipPlayback: %v", err)
	}

	// Stale expectedSongID — queue is now on "vote-next".
	_, _, _, _, err := inter.SkipVote(ctx, "rq-vote-stale", "vote-cur")
	if !errors.Is(err, ErrStaleSkipVote) {
		t.Fatalf("expected ErrStaleSkipVote, got %v", err)
	}

	// Reload — queue must NOT have been mutated further.
	persisted, err := inter.queueRepo.Load(ctx, mustRoomID(t, inter, "rq-vote-stale"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if persisted.CurrentIndex != 1 || persisted.Songs[1].ID != "vote-next" {
		t.Errorf("expected queue still on vote-next@1, got current=%d song=%q",
			persisted.CurrentIndex,
			func() string {
				if persisted.CurrentIndex >= 0 && persisted.CurrentIndex < len(persisted.Songs) {
					return persisted.Songs[persisted.CurrentIndex].ID
				}
				return ""
			}())
	}
}

// TestRoomQueue_SkipVote_NoNextSongReturnsErrNoNextSong pins the no-
// partial-mutation invariant when there's no next song: SkipVote must
// return ErrNoNextSong without mutating state.
func TestRoomQueue_SkipVote_NoNextSongReturnsErrNoNextSong(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-vote-nonext", "VRNonext", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-vote-nonext")
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{{ID: "vote-only", Title: "Only", URL: "u", AddedBy: "H", AddedByID: 42}}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := inter.queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, _, _, _, err := inter.SkipVote(ctx, "rq-vote-nonext", "vote-only")
	if !errors.Is(err, ErrNoNextSong) {
		t.Fatalf("expected ErrNoNextSong, got %v", err)
	}
	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if persisted.CurrentIndex != 0 {
		t.Errorf("expected current=0 unchanged, got %d", persisted.CurrentIndex)
	}
}
