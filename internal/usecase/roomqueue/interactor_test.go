package roomqueue

import (
	"context"
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
	return inter, queueRepo, cleanup
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
