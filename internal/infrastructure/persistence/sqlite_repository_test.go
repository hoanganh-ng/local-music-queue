package persistence

import (
	"context"
	"local-music-queue/internal/domain/entity"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestRepo(t *testing.T) (*SQLiteRepository, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	repo, err := NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	return repo, dbPath
}

func TestNewSQLiteRepository(t *testing.T) {
	repo, _ := newTestRepo(t)
	if repo == nil {
		t.Fatal("expected non-nil repo")
	}
}

func TestSave_And_Load(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	q := entity.NewQueue()
	q.Add(entity.Song{ID: "1", Title: "Song 1", URL: "url1", Artist: "Artist 1"})
	q.Add(entity.Song{ID: "2", Title: "Song 2", URL: "url2"})

	if err := repo.Save(ctx, q); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Songs) != 2 {
		t.Errorf("expected 2 songs, got %d", len(loaded.Songs))
	}
	if loaded.CurrentIndex != 0 {
		t.Errorf("expected index 0, got %d", loaded.CurrentIndex)
	}
	if loaded.Status != entity.StatusPlaying {
		t.Errorf("expected playing, got %s", loaded.Status)
	}
	if loaded.Songs[0].Artist != "Artist 1" {
		t.Errorf("expected artist 'Artist 1', got '%s'", loaded.Songs[0].Artist)
	}
}

func TestLoad_NoState(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.Load(ctx)
	if err == nil {
		t.Fatal("expected error when no state exists")
	}
}

func TestSave_Overwrites(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	q1 := entity.NewQueue()
	q1.Add(entity.Song{ID: "1", Title: "Song 1", URL: "url1"})
	repo.Save(ctx, q1)

	q2 := entity.NewQueue()
	q2.Add(entity.Song{ID: "2", Title: "Song 2", URL: "url2"})
	q2.Add(entity.Song{ID: "3", Title: "Song 3", URL: "url3"})
	repo.Save(ctx, q2)

	loaded, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Songs) != 2 {
		t.Errorf("expected 2 songs after overwrite, got %d", len(loaded.Songs))
	}
	if loaded.Songs[0].ID != "2" {
		t.Errorf("expected first song ID '2', got '%s'", loaded.Songs[0].ID)
	}
}

func TestAddActivity_And_GetActivities(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	a1 := entity.Activity{
		Timestamp:   time.Now(),
		Type:        entity.ActivitySongAdded,
		User:        "Alice",
		Description: "added a song",
	}
	a2 := entity.Activity{
		Timestamp:   time.Now().Add(time.Second),
		Type:        entity.ActivitySongSkipped,
		User:        "Bob",
		Description: "skipped",
	}

	if err := repo.AddActivity(ctx, a1); err != nil {
		t.Fatalf("AddActivity failed: %v", err)
	}
	if err := repo.AddActivity(ctx, a2); err != nil {
		t.Fatalf("AddActivity failed: %v", err)
	}

	activities, err := repo.GetActivities(ctx, 10)
	if err != nil {
		t.Fatalf("GetActivities failed: %v", err)
	}
	if len(activities) != 2 {
		t.Errorf("expected 2 activities, got %d", len(activities))
	}
}

func TestGetActivities_Limit(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		a := entity.Activity{
			Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
			Type:        entity.ActivitySongAdded,
			User:        "User",
			Description: "desc",
		}
		repo.AddActivity(ctx, a)
	}

	activities, err := repo.GetActivities(ctx, 3)
	if err != nil {
		t.Fatalf("GetActivities failed: %v", err)
	}
	if len(activities) != 3 {
		t.Errorf("expected 3 activities with limit, got %d", len(activities))
	}
}

func TestGetActivities_Order(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	a1 := entity.Activity{
		Timestamp:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Type:        entity.ActivitySongAdded,
		User:        "First",
		Description: "first",
	}
	a2 := entity.Activity{
		Timestamp:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Type:        entity.ActivitySongSkipped,
		User:        "Second",
		Description: "second",
	}

	repo.AddActivity(ctx, a1)
	repo.AddActivity(ctx, a2)

	activities, err := repo.GetActivities(ctx, 10)
	if err != nil {
		t.Fatalf("GetActivities failed: %v", err)
	}
	// Newest first
	if activities[0].User != "Second" {
		t.Errorf("expected newest first ('Second'), got '%s'", activities[0].User)
	}
}

func TestDataSurvivesReconnect(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")
	ctx := context.Background()

	// Create repo, save data, close
	repo1, err := NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "persist", Title: "Persistent Song", URL: "url"})
	repo1.Save(ctx, q)
	repo1.db.Close()

	// Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file should exist on disk")
	}

	// Reopen and verify data
	repo2, err := NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen repo: %v", err)
	}
	loaded, err := repo2.Load(ctx)
	if err != nil {
		t.Fatalf("Load after reconnect failed: %v", err)
	}
	if len(loaded.Songs) != 1 || loaded.Songs[0].ID != "persist" {
		t.Error("data did not survive reconnect")
	}
}
