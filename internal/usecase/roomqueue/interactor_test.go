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
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/room"
)

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
	q, returned, err := inter.AddSong(ctx, "rq-add", 42, "", &entity.SearchResult{ID: song.ID, Title: song.Title, URL: song.URL})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if returned.ID != "vid-1" {
		t.Errorf("expected returned song id vid-1, got %s", returned.ID)
	}
	if len(q.Songs) != 1 || q.CurrentIndex != 0 || q.Status != entity.StatusPlaying {
		t.Errorf("expected 1 song at index 0 playing, got %+v", q)
	}
}

func TestRoomQueue_AddSong_RejectsDuplicate(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-dup", "DupRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	s := &entity.SearchResult{ID: "dup-1", Title: "Dup", URL: "https://example/dup"}
	if _, _, err := inter.AddSong(ctx, "rq-dup", 42, "", s); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, _, err := inter.AddSong(ctx, "rq-dup", 42, "", s); !errors.Is(err, entity.ErrSongAlreadyInQueue) {
		t.Fatalf("expected ErrSongAlreadyInQueue, got %v", err)
	}
}

func TestRoomQueue_RemoveSong_GuestCannotRemoveOthers(t *testing.T) {
	inter, _, cleanup := pgRoomQueue(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := inter.roomRepo.CreateRoomAndHost(ctx, "rq-rm", "RmRoom", 42, testTime()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	// Host adds two songs; mark the second as owned by user 200.
	if _, _, err := inter.AddSong(ctx, "rq-rm", 42, "", &entity.SearchResult{ID: "h-1", Title: "H1", URL: "https://example/h1"}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	// Add the second song with AddedByID set so we can simulate "owned
	// by user 200". The handler normally sets this; here we reach into
	// the persisted queue to flip the field.
	if err := setAddedByID(t, ctx, inter, "rq-rm", "h-1", 0, 200); err != nil {
		t.Fatalf("flip ownership: %v", err)
	}
	// Now an admin-guest (user 200 as guest role) tries to remove a song
	// they don't own: index 0 (host-owned).
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
// specific user" without re-running the metadata path.
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
)
