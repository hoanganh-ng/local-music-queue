package roomqueue

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
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
	volumeN       int
	autoQueueAddedN        int
	autoQueueConfigChangedN int
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
func (r *recordingBroadcaster) BroadcastRoomPlaybackVolumeChanged(_ string, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.volumeN++
}
func (r *recordingBroadcaster) BroadcastRoomPlaybackSongPrevious(_ string, _, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {}
func (r *recordingBroadcaster) BroadcastRoomAutoQueueAdded(_ string, _ entity.Song, _ string, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoQueueAddedN++
}
func (r *recordingBroadcaster) BroadcastRoomAutoQueueConfigChanged(_ string, _ bool, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoQueueConfigChangedN++
}

// --- R09f AddRoomAutoQueueSong tests ---
//
// Pins the queue-owned conditional insertion contract:
//   - happy path: candidate inserted when predicates pass
//   - source mismatch: ErrRoomAutoQueueStale, no save, no broadcast
//   - upcoming song already exists: ErrRoomAutoQueueStale, no save
//   - duplicate-of-queued candidate: ErrRoomAutoQueueStale, no save

// seedQueueWithThree returns a room + queue with one current song
// (id="cur") and one upcoming song (id="up"). Used as a base for
// the stale-revalidation tests.
func seedQueueWithThree(t *testing.T, slug string) (*Interactor, *persistence.PostgresRoomQueueRepository, int64) {
	t.Helper()
	inter, db, _, cleanup := pgRoomQueueWithDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, slug, "TR-"+slug, 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomID(t, inter, slug)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "cur", Title: "Current", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "up", Title: "Upcoming", URL: "u", AddedBy: "Host", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := inter.queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	return inter, queueRepo, roomID
}

// seedQueueLastSong returns a room + queue with a single song (current
// is also the last). Used for the happy-path AddRoomAutoQueueSong
// test so the post-mutation queue has 2 songs.
func seedQueueLastSong(t *testing.T, slug string) (*Interactor, *persistence.PostgresRoomQueueRepository, int64) {
	t.Helper()
	inter, queueRepo, roomID := seedQueueWithThree(t, slug) //nolint:staticcheck // uses three-slot seed
	ctx := context.Background()
	// Reset queue to one song so current == last.
	if err := queueRepo.Save(ctx, roomID, &entity.Queue{
		Songs:        []entity.Song{{ID: "src", Title: "Source", URL: "u", AddedBy: "Host", AddedByID: 42}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	}); err != nil {
		t.Fatalf("reset single song: %v", err)
	}
	return inter, queueRepo, roomID
}

// TestRoomQueue_AddRoomAutoQueueSong_HappyPath pins the success path:
// source song still current, no upcoming song, candidate is fresh.
// Returns the post-mutation snapshot; persisted queue has the new
// song appended.
func TestRoomQueue_AddRoomAutoQueueSong_HappyPath(t *testing.T) {
	inter, queueRepo, roomID := seedQueueLastSong(t, "rq-f-aq-ok")

	queue, currentIndex, currentSong, status, elapsed, err := inter.AddRoomAutoQueueSong(context.Background(), "rq-f-aq-ok",
		&entity.Song{ID: "auto1", Title: "Auto 1", URL: "u", AddedBy: entity.SystemUserID, AddedByID: 0},
		"src",
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if len(queue.Songs) != 2 {
		t.Fatalf("expected 2 songs after insert, got %d", len(queue.Songs))
	}
	if queue.Songs[1].ID != "auto1" {
		t.Errorf("expected auto1 at idx 1, got %q", queue.Songs[1].ID)
	}
	if currentIndex != 0 {
		t.Errorf("expected currentIndex=0 (unchanged), got %d", currentIndex)
	}
	if status != entity.StatusPlaying {
		t.Errorf("expected status=playing, got %q", status)
	}
	if elapsed != 0 {
		t.Errorf("expected elapsed=0, got %d", elapsed)
	}
	if currentSong == nil || currentSong.ID != "src" {
		t.Errorf("expected currentSong.id=src, got %+v", currentSong)
	}

	persisted, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if len(persisted.Songs) != 2 || persisted.Songs[1].ID != "auto1" {
		t.Errorf("expected persisted queue with auto1 at idx 1, got %+v", persisted)
	}
}

// TestRoomQueue_AddRoomAutoQueueSong_SourceMismatchStale pins the
// stale invariant: the source song's ID has changed; the insertion
// returns ErrRoomAutoQueueStale with zero side effects.
func TestRoomQueue_AddRoomAutoQueueSong_SourceMismatchStale(t *testing.T) {
	inter, queueRepo, roomID := seedQueueLastSong(t, "rq-f-aq-sm")
	pre, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("pre-load: %v", err)
	}
	preLen := len(pre.Songs)

	_, _, _, _, _, err = inter.AddRoomAutoQueueSong(context.Background(), "rq-f-aq-sm",
		&entity.Song{ID: "auto1", Title: "Auto 1", URL: "u", AddedBy: entity.SystemUserID, AddedByID: 0},
		"WRONG-SOURCE-ID",
	)
	if !errors.Is(err, ErrRoomAutoQueueStale) {
		t.Fatalf("expected ErrRoomAutoQueueStale, got %v", err)
	}

	post, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("post-load: %v", err)
	}
	if len(post.Songs) != preLen {
		t.Errorf("queue mutated on stale source mismatch: %d vs %d songs", len(post.Songs), preLen)
	}
}

// TestRoomQueue_AddRoomAutoQueueSong_UpcomingExistsStale pins the
// second stale invariant: an upcoming song already exists (queue has
// 2+ songs); insertion returns ErrRoomAutoQueueStale with no save.
func TestRoomQueue_AddRoomAutoQueueSong_UpcomingExistsStale(t *testing.T) {
	inter, queueRepo, roomID := seedQueueWithThree(t, "rq-f-aq-up")
	pre, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("pre-load: %v", err)
	}
	preLen := len(pre.Songs)

	_, _, _, _, _, err = inter.AddRoomAutoQueueSong(context.Background(), "rq-f-aq-up",
		&entity.Song{ID: "auto1", Title: "Auto 1", URL: "u", AddedBy: entity.SystemUserID, AddedByID: 0},
		pre.Songs[pre.CurrentIndex].ID,
	)
	if !errors.Is(err, ErrRoomAutoQueueStale) {
		t.Fatalf("expected ErrRoomAutoQueueStale, got %v", err)
	}

	post, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("post-load: %v", err)
	}
	if len(post.Songs) != preLen {
		t.Errorf("queue mutated when upcoming song already exists")
	}
}

// TestRoomQueue_AddRoomAutoQueueSong_DuplicateStale pins the third
// stale invariant: the candidate is already in the upcoming queue;
// insertion returns ErrRoomAutoQueueStale with no save.
func TestRoomQueue_AddRoomAutoQueueSong_DuplicateStale(t *testing.T) {
	inter, queueRepo, roomID := seedQueueLastSong(t, "rq-f-aq-dup")
	// Insert candidate as a normal future song first.
	ctx := context.Background()
	if _, _, err := inter.AddSong(ctx, "rq-f-aq-dup", 42, "Host U42", "",
		&entity.SearchResult{ID: "dup", Title: "Dup", URL: "u"}); err != nil {
		t.Fatalf("seed add: %v", err)
	}
	pre, err := queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("pre-load: %v", err)
	}
	preLen := len(pre.Songs)

	// Now try to auto-queue the SAME id — must return stale.
	_, _, _, _, _, err = inter.AddRoomAutoQueueSong(ctx, "rq-f-aq-dup",
		&entity.Song{ID: "dup", Title: "Dup", URL: "u", AddedBy: entity.SystemUserID, AddedByID: 0},
		pre.Songs[pre.CurrentIndex].ID,
	)
	if !errors.Is(err, ErrRoomAutoQueueStale) {
		t.Fatalf("expected ErrRoomAutoQueueStale on duplicate, got %v", err)
	}

	post, err := queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("post-load: %v", err)
	}
	if len(post.Songs) != preLen {
		t.Errorf("queue mutated on duplicate auto-queue: %d vs %d songs", len(post.Songs), preLen)
	}
}

// --- R09f auto-queue trigger wiring tests ---
//
// Pins the trigger ownership contract: a successful queue mutation
// that ends with current == last fires a non-blocking
// CheckAndTrigger on the wired RoomAutoQueueTrigger. Mutations that
// do not end at current == last do NOT fire.

// stubRoomAutoQueueTrigger captures CheckAndTrigger invocations and
// returns immediately. Used to assert when the roomqueue interactor
// fires the auto-queue trigger.
type stubRoomAutoQueueTrigger struct {
	mu        sync.Mutex
	calls     []string
	queueLive *entity.Queue
}

func (s *stubRoomAutoQueueTrigger) CheckAndTrigger(_ context.Context, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, slug)
	return nil
}

// TestRoomQueue_TriggerFiresAfterSuccessfulMutation_OnCurrentIsLast:
// with a stub trigger wired and the post-mutation queue at
// current == last, a non-blocking CheckAndTrigger is fired for the
// room slug. The trigger argument resolves to the expected slug.
func TestRoomQueue_TriggerFiresAfterSuccessfulMutation_OnCurrentIsLast(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-f-trig-last", 42)
	defer cleanup()
	trig := &stubRoomAutoQueueTrigger{}
	inter.SetRoomAutoQueueTrigger(trig)
	ctx := context.Background()

	// Drive a SkipPlayback so the queue advances to 1 (the only
	// remaining song), leaving current==last. SkipPlayback's
	// post-mutation check then fires the trigger.
	// pre-load: 2 songs at current=0 → not last (current != 1).
	q, err := inter.GetStateByRoomID(ctx, mustRoomID(t, inter, "rq-f-trig-last"))
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if q.CurrentIndex == len(q.Songs)-1 && len(q.Songs) >= 2 {
		// The fixture already pre-advanced — drop a song instead so
		// we still end with current==last (last==last after remove).
		// Skip the SkipPlayback branch in that case.
	} else {
		if _, _, _, _, err := inter.SkipPlayback(ctx, "rq-f-trig-last", 42); err != nil {
			t.Fatalf("seed-skip: %v", err)
		}
	}

	// Verify current == last now.
	q, err = inter.GetStateByRoomID(ctx, mustRoomID(t, inter, "rq-f-trig-last"))
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if q.CurrentIndex != len(q.Songs)-1 {
		t.Fatalf("seed-precondition: current=%d != last=%d", q.CurrentIndex, len(q.Songs)-1)
	}

	// Drive ClearQueue (host/admin path) — clear leaves current at
	// the kept song, still current==last.
	if _, err := inter.ClearQueue(ctx, "rq-f-trig-last", 42, entity.RoomRoleHost); err != nil {
		t.Fatalf("clear: %v", err)
	}

	// Wait briefly for the goroutine to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		trig.mu.Lock()
		n := len(trig.calls)
		trig.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	trig.mu.Lock()
	defer trig.mu.Unlock()
	if len(trig.calls) == 0 {
		t.Fatalf("expected CheckAndTrigger to fire after ClearQueue (current==last), got no calls")
	}
	if trig.calls[0] != "rq-f-trig-last" {
		t.Errorf("expected trigger slug=rq-f-trig-last, got %q", trig.calls[0])
	}
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

// --- R09c ChangePlaybackVolume tests ---

// TestRoomQueue_ChangePlaybackVolume_HolderReturnsNil pins the happy
// path: the holder sends "up"; the interactor returns nil without
// mutating queue state. No broadcast is asserted here — that is the
// handler layer's job.
func TestRoomQueue_ChangePlaybackVolume_HolderReturnsNil(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-c-vol-up", 42)
	defer cleanup()
	ctx := context.Background()

	roomID := mustRoomID(t, inter, "rq-c-vol-up")
	queueRepo := inter.queueRepo
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "s1", Title: "S1", URL: "u", AddedBy: "H", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	seed.Elapsed = 12
	if err := queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	pre, err := queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("pre-load: %v", err)
	}
	if err := inter.ChangePlaybackVolume(ctx, "rq-c-vol-up", 42, "up"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	post, err := queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("post-load: %v", err)
	}
	if !reflect.DeepEqual(pre.Songs, post.Songs) ||
		pre.CurrentIndex != post.CurrentIndex ||
		pre.Status != post.Status ||
		pre.Elapsed != post.Elapsed {
		t.Fatalf("queue state mutated by volume command: pre=%+v post=%+v", pre, post)
	}
}

// TestRoomQueue_ChangePlaybackVolume_NonHolderReturnsForbidden
func TestRoomQueue_ChangePlaybackVolume_NonHolderReturnsForbidden(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-c-vol-nonholder", 42)
	defer cleanup()
	err := inter.ChangePlaybackVolume(context.Background(), "rq-c-vol-nonholder", 999, "up")
	if !errors.Is(err, ErrPlaybackForbidden) {
		t.Fatalf("expected ErrPlaybackForbidden, got %v", err)
	}
}

// TestRoomQueue_ChangePlaybackVolume_MissingLeaseReturnsLeaseLost
func TestRoomQueue_ChangePlaybackVolume_MissingLeaseReturnsLeaseLost(t *testing.T) {
	inter, db, _, cleanup := pgRoomQueueWithDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-c-vol-nolease", "CV", 42, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-c-vol-nolease")
	q := entity.NewQueue()
	q.Songs = []entity.Song{{ID: "x", Title: "X"}}
	q.CurrentIndex = 0
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("seed: %v", err)
	}
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	leaseInter := room.NewPlayerLeaseInteractor(leaseRepo, inter.roomRepo, db, room.DefaultLeaseDuration, room.DefaultLeaseGrace)
	inter.SetLeaseAuthorizer(leaseInter)

	err := inter.ChangePlaybackVolume(ctx, "rq-c-vol-nolease", 42, "up")
	if !errors.Is(err, ErrPlaybackLeaseLost) {
		t.Fatalf("expected ErrPlaybackLeaseLost, got %v", err)
	}
}

// TestRoomQueue_ChangePlaybackVolume_InvalidDirectionReturnsSentinel
func TestRoomQueue_ChangePlaybackVolume_InvalidDirectionReturnsSentinel(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-c-vol-bad-dir", 42)
	defer cleanup()
	for _, d := range []string{"", "left", "UP", "Down ", "0", "increase"} {
		err := inter.ChangePlaybackVolume(context.Background(), "rq-c-vol-bad-dir", 42, d)
		if !errors.Is(err, ErrInvalidDirection) {
			t.Fatalf("direction=%q: expected ErrInvalidDirection, got %v", d, err)
		}
	}
}

// TestRoomQueue_ChangePlaybackVolume_RequiresNoCurrentSong: hold a
// lease on a fresh, empty queue and confirm the command still returns
// nil (R09c must not require a current song).
func TestRoomQueue_ChangePlaybackVolume_RequiresNoCurrentSong(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-c-vol-empty", 42)
	defer cleanup()
	ctx := context.Background()
	// Reset queue back to empty so the fixture's seed doesn't pin a song.
	roomID := mustRoomID(t, inter, "rq-c-vol-empty")
	if err := inter.queueRepo.Save(ctx, roomID, entity.NewQueue()); err != nil {
		t.Fatalf("reset empty queue: %v", err)
	}
	if err := inter.ChangePlaybackVolume(ctx, "rq-c-vol-empty", 42, "down"); err != nil {
		t.Fatalf("expected nil error on empty queue, got %v", err)
	}
	// And state must be unchanged (still empty).
	post, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("post-load: %v", err)
	}
	if len(post.Songs) != 0 || post.CurrentIndex != -1 {
		t.Fatalf("queue mutated on empty volume command: %+v", post)
	}
}

// --- R09d PrevPlayback tests ---
//
// PrevPlayback is a lease-holder-only command that moves the room
// queue from the current song to the previous one. It must:
//   - require an active lease holder (R09a seam)
//   - require a valid current song (no mutation on empty queue)
//   - refuse to mutate when already on the first song (no prev song)
//   - decrement CurrentIndex, reset Elapsed to 0, set Status to playing
//   - persist the post-mutation queue
//   - return the (prevIndex, newIndex, currentSong) tuple for the broadcast

// TestRoomPlayback_PrevPlayback_HappyPath seeds a 2-song queue at
// index 1 with the lease held by user 42. After PrevPlayback the queue
// is on the previous song at index 0, elapsed=0, status=playing.
func TestRoomPlayback_PrevPlayback_HappyPath(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-d-prev-ok", 42)
	defer cleanup()
	ctx := context.Background()

	// Fixture already seeded CurrentIndex=0; advance to index 1 so
	// "previous" has a target. Use the lease-only SkipPlayback path.
	if _, _, _, _, err := inter.SkipPlayback(ctx, "rq-d-prev-ok", 42); err != nil {
		t.Fatalf("seed-advance: %v", err)
	}

	q, prevIdx, newIdx, song, err := inter.PrevPlayback(ctx, "rq-d-prev-ok", 42)
	if err != nil {
		t.Fatalf("prev: %v", err)
	}
	if prevIdx != 1 || newIdx != 0 {
		t.Errorf("expected prev=1 new=0, got prev=%d new=%d", prevIdx, newIdx)
	}
	if song == nil || song.ID != "cur" {
		t.Errorf("expected previous song id=cur, got %+v", song)
	}
	if q.CurrentIndex != 0 || q.Status != entity.StatusPlaying || q.Elapsed != 0 {
		t.Errorf("expected current=0 playing elapsed=0, got %+v", q)
	}

	persisted, err := inter.queueRepo.Load(ctx, mustRoomID(t, inter, "rq-d-prev-ok"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if persisted.CurrentIndex != 0 || persisted.Songs[0].ID != "cur" {
		t.Errorf("expected persisted queue on cur@0, got %+v", persisted)
	}
}

// TestRoomPlayback_PrevPlayback_AlreadyOnFirstReturnsErrNoPreviousSong
// pins the no-partial-mutation invariant: fixture starts on index 0,
// PrevPlayback must return ErrNoPreviousSong without mutating state.
func TestRoomPlayback_PrevPlayback_AlreadyOnFirstReturnsErrNoPreviousSong(t *testing.T) {
	inter, _, cleanup := playbackFixture(t, "rq-d-prev-first", 42)
	defer cleanup()
	ctx := context.Background()

	// Seed Elapsed so the no-mutation invariant is observable.
	roomID := mustRoomID(t, inter, "rq-d-prev-first")
	if err := inter.queueRepo.Save(ctx, roomID, &entity.Queue{
		Songs: []entity.Song{
			{ID: "cur", Title: "Cur"},
		},
		CurrentIndex: 0,
		Status:       entity.StatusPaused,
		Elapsed:      21,
	}); err != nil {
		t.Fatalf("reseed: %v", err)
	}

	_, _, _, _, err := inter.PrevPlayback(ctx, "rq-d-prev-first", 42)
	if !errors.Is(err, ErrNoPreviousSong) {
		t.Fatalf("expected ErrNoPreviousSong, got %v", err)
	}
	persisted, _ := inter.queueRepo.Load(ctx, roomID)
	if persisted.CurrentIndex != 0 || persisted.Elapsed != 21 || persisted.Status != entity.StatusPaused {
		t.Errorf("queue mutated on no-prev: %+v", persisted)
	}
}

// TestRoomPlayback_PrevPlayback_EmptyQueueReturnsErrNoCurrentSong
// pins the empty-queue branch: PrevPlayback on a fresh room must return
// ErrNoCurrentSong without mutation or persistence.
func TestRoomPlayback_PrevPlayback_EmptyQueueReturnsErrNoCurrentSong(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-d-prev-empty", "PE", 42, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := inter.queueRepo.Save(ctx, mustRoomID(t, inter, "rq-d-prev-empty"), entity.NewQueue()); err != nil {
		t.Fatalf("reset empty queue: %v", err)
	}
	inter.SetLeaseAuthorizer(allowAllLeaseAuthorizer{})

	_, _, _, _, err := inter.PrevPlayback(ctx, "rq-d-prev-empty", 42)
	if !errors.Is(err, ErrNoCurrentSong) {
		t.Fatalf("expected ErrNoCurrentSong, got %v", err)
	}
}

// TestRoomPlayback_PrevPlayback_NonHolderReturnsErrPlaybackForbidden
// exercises the lease-authorizer seam: a guest must not be able to
// advance the room queue backward. Built manually so the host is 200
// (lease holder) and 42 is a guest member who can NOT issue the
// command.
func TestRoomPlayback_PrevPlayback_NonHolderReturnsErrPlaybackForbidden(t *testing.T) {
	inter, db, _, cleanup := pgRoomQueueWithDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-d-prev-nh", "PDNH", 200, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-d-prev-nh")
	if err := inter.roomRepo.AddMember(ctx, roomID, 42, entity.RoomRoleGuest, testTime()); err != nil {
		t.Fatalf("add guest 42: %v", err)
	}
	if err := inter.queueRepo.Save(ctx, roomID, &entity.Queue{
		Songs: []entity.Song{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B"},
		},
		CurrentIndex: 1,
		Status:       entity.StatusPlaying,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	leaseInter := room.NewPlayerLeaseInteractor(leaseRepo, inter.roomRepo, db, room.DefaultLeaseDuration, room.DefaultLeaseGrace)
	if _, err := leaseInter.Claim(ctx, "rq-d-prev-nh", 200); err != nil {
		t.Fatalf("claim as 200: %v", err)
	}
	inter.SetLeaseAuthorizer(leaseInter)

	_, _, _, _, err := inter.PrevPlayback(ctx, "rq-d-prev-nh", 42)
	if !errors.Is(err, ErrPlaybackForbidden) {
		t.Fatalf("expected ErrPlaybackForbidden, got %v", err)
	}
}

// TestRoomPlayback_PrevPlayback_MissingLeaseReturnsErrPlaybackLeaseLost
// pins the missing-lease path: the use case refuses the mutation when
// no lease exists.
func TestRoomPlayback_PrevPlayback_MissingLeaseReturnsErrPlaybackLeaseLost(t *testing.T) {
	inter, db, _, cleanup := pgRoomQueueWithDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-d-prev-nl", "PN", 42, testTime()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-d-prev-nl")
	if err := inter.queueRepo.Save(ctx, roomID, &entity.Queue{
		Songs: []entity.Song{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B"},
		},
		CurrentIndex: 1,
		Status:       entity.StatusPlaying,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	leaseInter := room.NewPlayerLeaseInteractor(leaseRepo, inter.roomRepo, db, room.DefaultLeaseDuration, room.DefaultLeaseGrace)
	inter.SetLeaseAuthorizer(leaseInter)

	_, _, _, _, err := inter.PrevPlayback(ctx, "rq-d-prev-nl", 42)
	if !errors.Is(err, ErrPlaybackLeaseLost) {
		t.Fatalf("expected ErrPlaybackLeaseLost, got %v", err)
	}
}

// --- R09h PrioritizeVote (queue-owned) tests ---
//
// PrioritizeVote is the queue-owned resolution the roomvote interactor
// calls once a prioritize vote session passes. It re-resolves the room,
// takes the queue mutation mutex, reloads fresh state, and re-validates
// the (expectedSongID, expectedIndex) snapshot captured when the vote
// session was created. It must move the target immediately after the
// current song, save exactly once, and NEVER broadcast or touch
// priority balances. Any race that removes, moves, duplicates, or
// promotes the target to current surfaces ErrStalePrioritizeVote with
// zero mutation.

func TestRoomQueue_PrioritizeVote_HappyPathMovesTargetAndPersists(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-pv-ok")
	ctx := context.Background()

	queue, fromIndex, toIndex, song, err := inter.PrioritizeVote(ctx, "rq-pv-ok", "up2", 2)
	if err != nil {
		t.Fatalf("prioritize vote: %v", err)
	}
	if fromIndex != 2 || toIndex != 1 {
		t.Errorf("expected from=2 to=1, got from=%d to=%d", fromIndex, toIndex)
	}
	if song.ID != "up2" || !song.IsPrioritized {
		t.Errorf("expected moved up2 with IsPrioritized=true, got %+v", song)
	}
	if queue.Songs[1].ID != "up2" || !queue.Songs[1].IsPrioritized {
		t.Errorf("expected up2 prioritized at idx 1, got %+v", queue.Songs[1])
	}

	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if persisted.Songs[1].ID != "up2" || !persisted.Songs[1].IsPrioritized {
		t.Errorf("persisted: expected up2 prioritized at idx 1, got %+v", persisted.Songs[1])
	}
}

func TestRoomQueue_PrioritizeVote_RemovedTargetReturnsStaleNoMutation(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-pv-removed")
	ctx := context.Background()

	// Shrink the queue so the snapshot index 2 no longer exists.
	q, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	q.Songs = q.Songs[:2] // cur, up1
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("save shrink: %v", err)
	}

	_, _, _, _, err = inter.PrioritizeVote(ctx, "rq-pv-removed", "up2", 2)
	if !errors.Is(err, ErrStalePrioritizeVote) {
		t.Fatalf("expected ErrStalePrioritizeVote, got %v", err)
	}
	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if len(persisted.Songs) != 2 || persisted.Songs[1].IsPrioritized {
		t.Errorf("expected unchanged 2-song queue with no prioritization, got %+v", persisted.Songs)
	}
}

func TestRoomQueue_PrioritizeVote_MovedTargetReturnsStaleNoMutation(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-pv-moved")
	ctx := context.Background()

	q, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Swap up1 and up2 so index 2 no longer holds the target up2.
	q.Songs[1], q.Songs[2] = q.Songs[2], q.Songs[1]
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("save swap: %v", err)
	}

	_, _, _, _, err = inter.PrioritizeVote(ctx, "rq-pv-moved", "up2", 2)
	if !errors.Is(err, ErrStalePrioritizeVote) {
		t.Fatalf("expected ErrStalePrioritizeVote for moved target, got %v", err)
	}
	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if persisted.Songs[1].ID != "up2" || persisted.Songs[2].ID != "up1" {
		t.Errorf("expected swap preserved (no mutation), got %+v", persisted.Songs)
	}
	for i := range persisted.Songs {
		if persisted.Songs[i].IsPrioritized {
			t.Errorf("expected no prioritization applied, got %+v", persisted.Songs[i])
		}
	}
}

func TestRoomQueue_PrioritizeVote_TargetBecameCurrentReturnsCurrentSong(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-pv-current")
	ctx := context.Background()

	q, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	q.CurrentIndex = 2 // up2 is now the currently-playing song
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("save advance: %v", err)
	}

	// Became-current is a current-song rejection (mapped to HTTP 400),
	// NOT a moved/removed stale target (409).
	_, _, _, _, err = inter.PrioritizeVote(ctx, "rq-pv-current", "up2", 2)
	if !errors.Is(err, entity.ErrVoteOnCurrentSong) {
		t.Fatalf("expected entity.ErrVoteOnCurrentSong when target is current, got %v", err)
	}
	if errors.Is(err, ErrStalePrioritizeVote) {
		t.Fatalf("became-current must NOT surface as ErrStalePrioritizeVote")
	}
	// Zero mutation on the current-song branch.
	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	for i := range persisted.Songs {
		if persisted.Songs[i].IsPrioritized {
			t.Errorf("expected no mutation on became-current target, got prioritized %+v", persisted.Songs[i])
		}
	}
}

func TestRoomQueue_PrioritizeVote_NoCurrentSongReturnsStale(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-pv-nocur")
	ctx := context.Background()

	// Empty queue: no current song to prioritize relative to.
	empty := entity.NewQueue()
	if err := inter.queueRepo.Save(ctx, roomID, empty); err != nil {
		t.Fatalf("save empty: %v", err)
	}

	_, _, _, _, err := inter.PrioritizeVote(ctx, "rq-pv-nocur", "up2", 2)
	if !errors.Is(err, ErrStalePrioritizeVote) {
		t.Fatalf("expected ErrStalePrioritizeVote with no current song, got %v", err)
	}
}

func TestRoomQueue_PrioritizeVote_DuplicateSongIDReturnsStaleNoMutation(t *testing.T) {
	inter, roomID := seedPrioritizeQueue(t, "rq-pv-dup")
	ctx := context.Background()

	q, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Introduce a second "up2" so the snapshot index is ambiguous.
	q.Songs = append(q.Songs, entity.Song{ID: "up2", Title: "Dup", URL: "u", AddedBy: "Host U42", AddedByID: 42})
	if err := inter.queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("save dup: %v", err)
	}

	_, _, _, _, err = inter.PrioritizeVote(ctx, "rq-pv-dup", "up2", 2)
	if !errors.Is(err, ErrStalePrioritizeVote) {
		t.Fatalf("expected ErrStalePrioritizeVote for ambiguous duplicate id, got %v", err)
	}
	persisted, err := inter.queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	for i := range persisted.Songs {
		if persisted.Songs[i].IsPrioritized {
			t.Errorf("expected no mutation on ambiguous target, got prioritized %+v", persisted.Songs[i])
		}
	}
}

func TestRoomQueue_PrioritizeVote_DoesNotBroadcast(t *testing.T) {
	inter, _ := seedPrioritizeQueue(t, "rq-pv-nobc")
	ctx := context.Background()
	bc := &recordingBroadcaster{}
	inter.SetBroadcaster(bc)

	if _, _, _, _, err := inter.PrioritizeVote(ctx, "rq-pv-nobc", "up2", 2); err != nil {
		t.Fatalf("prioritize vote: %v", err)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if bc.prioN != 0 {
		t.Errorf("PrioritizeVote must not broadcast; got prioN=%d", bc.prioN)
	}
}

func TestRoomQueue_PrioritizeVote_DoesNotDebitPriorityBalance(t *testing.T) {
	inter, db, queueRepo, cleanup := pgRoomQueueWithDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-pv-nodebit", "PR", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomID(t, inter, "rq-pv-nodebit")
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "cur", Title: "Cur", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "up1", Title: "Up1", URL: "u", AddedBy: "Host U42", AddedByID: 42},
		{ID: "up2", Title: "Up2", URL: "u", AddedBy: "Host U42", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Give the actor a non-zero balance and pin it survives the vote pass.
	if _, err := db.ExecContext(ctx, `UPDATE users SET priority_balance = 5 WHERE id = 42`); err != nil {
		t.Fatalf("set balance: %v", err)
	}

	if _, _, _, _, err := inter.PrioritizeVote(ctx, "rq-pv-nodebit", "up2", 2); err != nil {
		t.Fatalf("prioritize vote: %v", err)
	}

	var bal int
	if err := db.QueryRowContext(ctx, `SELECT priority_balance FROM users WHERE id = 42`).Scan(&bal); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if bal != 5 {
		t.Errorf("expected priority_balance unchanged at 5, got %d", bal)
	}
}
