package persistence

import (
	"context"
	"errors"
	"testing"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

func TestPostgresRoomQueue_Load_ReturnsNotFound_OnEmptyRoom(t *testing.T) {
	db, _ := NewRoomTestDB(t)
	roomID := seedRoomAndUser(t, db, 700)
	repo := NewPostgresRoomQueueRepository(db)
	ctx := context.Background()

	if _, err := repo.Load(ctx, roomID); !errors.Is(err, repository.ErrRoomQueueNotFound) {
		t.Fatalf("expected ErrRoomQueueNotFound, got %v", err)
	}
}

func TestPostgresRoomQueue_SaveThenLoad_RoundTripsQueueState(t *testing.T) {
	db, _ := NewRoomTestDB(t)
	roomID := seedRoomAndUser(t, db, 701)
	repo := NewPostgresRoomQueueRepository(db)
	ctx := context.Background()

	in := entity.NewQueue()
	in.Add(entity.Song{ID: "vid-1", Title: "Track One", URL: "https://example/1"})
	in.Add(entity.Song{ID: "vid-2", Title: "Track Two", URL: "https://example/2"})

	if err := repo.Save(ctx, roomID, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := repo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out.Songs) != 2 {
		t.Fatalf("expected 2 songs, got %d", len(out.Songs))
	}
	if out.CurrentIndex != 0 {
		t.Errorf("expected CurrentIndex 0, got %d", out.CurrentIndex)
	}
	if out.Songs[0].ID != "vid-1" || out.Songs[1].ID != "vid-2" {
		t.Errorf("round-tripped songs out of order: %+v", out.Songs)
	}
}

func TestPostgresRoomQueue_Save_UpsertsExistingRow(t *testing.T) {
	db, _ := NewRoomTestDB(t)
	roomID := seedRoomAndUser(t, db, 702)
	repo := NewPostgresRoomQueueRepository(db)
	ctx := context.Background()

	q1 := entity.NewQueue()
	q1.Add(entity.Song{ID: "vid-A", Title: "A", URL: "https://example/a"})
	if err := repo.Save(ctx, roomID, q1); err != nil {
		t.Fatalf("save 1: %v", err)
	}

	q2 := entity.NewQueue()
	q2.Add(entity.Song{ID: "vid-B", Title: "B", URL: "https://example/b"})
	q2.Add(entity.Song{ID: "vid-C", Title: "C", URL: "https://example/c"})
	if err := repo.Save(ctx, roomID, q2); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	out, err := repo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out.Songs) != 2 {
		t.Fatalf("expected 2 songs after upsert, got %d", len(out.Songs))
	}
	if out.Songs[1].ID != "vid-C" {
		t.Errorf("expected last song vid-C, got %s", out.Songs[1].ID)
	}
}
