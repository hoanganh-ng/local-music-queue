package persistence

import (
	"context"
	"errors"
	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"testing"
	"time"
)

// ----- QueueRepository conformance -----

func TestPostgresQueue_SaveAndLoad(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresRepository(db)
	ctx := context.Background()

	q := entity.NewQueue()
	q.Add(entity.Song{ID: "1", Title: "Song 1", URL: "url1", Artist: "Artist 1"})
	q.Add(entity.Song{ID: "2", Title: "Song 2", URL: "url2"})
	if err := repo.Save(ctx, q); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Songs) != 2 {
		t.Errorf("expected 2 songs, got %d", len(loaded.Songs))
	}
	if loaded.Songs[0].Artist != "Artist 1" {
		t.Errorf("expected artist 'Artist 1', got '%s'", loaded.Songs[0].Artist)
	}
	if loaded.CurrentIndex != 0 || loaded.Status != entity.StatusPlaying {
		t.Errorf("expected current=0 playing, got idx=%d status=%s", loaded.CurrentIndex, loaded.Status)
	}
}

func TestPostgresQueue_SaveOverwritesAndLoadMissing(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresRepository(db)
	ctx := context.Background()

	if _, err := repo.Load(ctx); err == nil {
		t.Fatal("expected error on empty queue_state")
	}

	q1 := entity.NewQueue()
	q1.Add(entity.Song{ID: "a", Title: "A", URL: "u"})
	if err := repo.Save(ctx, q1); err != nil {
		t.Fatalf("Save q1: %v", err)
	}

	q2 := entity.NewQueue()
	q2.Add(entity.Song{ID: "b", Title: "B", URL: "u"})
	q2.Add(entity.Song{ID: "c", Title: "C", URL: "u"})
	if err := repo.Save(ctx, q2); err != nil {
		t.Fatalf("Save q2: %v", err)
	}

	loaded, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Songs) != 2 || loaded.Songs[0].ID != "b" {
		t.Errorf("expected overwrite to take effect, got %+v", loaded.Songs)
	}
}

func TestPostgresActivities_AddAndGet(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	a1 := entity.Activity{Timestamp: now, Type: entity.ActivitySongAdded, User: "Alice", Description: "added"}
	a2 := entity.Activity{Timestamp: now.Add(time.Second), Type: entity.ActivitySongSkipped, User: "Bob", Description: "skipped"}
	if err := repo.AddActivity(ctx, a1); err != nil {
		t.Fatalf("AddActivity a1: %v", err)
	}
	if err := repo.AddActivity(ctx, a2); err != nil {
		t.Fatalf("AddActivity a2: %v", err)
	}

	got, err := repo.GetActivities(ctx, 10)
	if err != nil {
		t.Fatalf("GetActivities: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(got))
	}
	// Newest first.
	if got[0].User != "Bob" {
		t.Errorf("expected newest first, got %s", got[0].User)
	}

	// Limit honored.
	for i := 0; i < 3; i++ {
		_ = repo.AddActivity(ctx, entity.Activity{Timestamp: now.Add(time.Duration(i+10) * time.Second), Type: entity.ActivitySongAdded, User: "u", Description: "d"})
	}
	limited, err := repo.GetActivities(ctx, 2)
	if err != nil {
		t.Fatalf("GetActivities limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("expected 2 with limit, got %d", len(limited))
	}
}

// ----- UserRepository conformance -----

func TestPostgresUser_CreateGetUpdate(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresUserRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	u := &entity.User{
		Email:           "alice@example.com",
		DisplayName:     "Alice",
		ProfilePicture:  "pic",
		Role:            entity.RoleHost,
		PriorityBalance: 5,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("expected user ID assigned")
	}

	got, err := repo.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != u.ID || got.Role != entity.RoleHost {
		t.Errorf("roundtrip mismatch: %+v vs %+v", got, u)
	}

	gotByID, err := repo.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if gotByID.Email != "alice@example.com" {
		t.Errorf("GetUserByID mismatch: %+v", gotByID)
	}

	u.DisplayName = "Alice 2"
	u.UpdatedAt = now.Add(time.Minute)
	if err := repo.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	updated, err := repo.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID after update: %v", err)
	}
	if updated.DisplayName != "Alice 2" {
		t.Errorf("update did not stick, got %s", updated.DisplayName)
	}
}

func TestPostgresUser_GetMissing(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresUserRepository(db)
	_, err := repo.GetUserByEmail(context.Background(), "missing@example.com")
	if err == nil {
		t.Fatal("expected error for missing user")
	}
}

func TestPostgresUser_PriorityIncrementDecrement(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresUserRepository(db)
	ctx := context.Background()

	u := &entity.User{
		Email:       "p@example.com",
		DisplayName: "P",
		Role:        entity.RoleGuest,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := repo.IncrementPriority(ctx, u.ID); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if err := repo.IncrementPriority(ctx, u.ID); err != nil {
		t.Fatalf("Increment 2: %v", err)
	}
	got, _ := repo.GetUserByID(ctx, u.ID)
	if got.PriorityBalance != 2 {
		t.Errorf("expected balance=2, got %d", got.PriorityBalance)
	}

	if err := repo.DecrementPriority(ctx, u.ID); err != nil {
		t.Fatalf("Decrement: %v", err)
	}
	got, _ = repo.GetUserByID(ctx, u.ID)
	if got.PriorityBalance != 1 {
		t.Errorf("expected balance=1 after decrement, got %d", got.PriorityBalance)
	}
}

func TestPostgresUserSessions_RecordAndGet(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresUserRepository(db)
	ctx := context.Background()

	u := &entity.User{
		Email:       "s@example.com",
		DisplayName: "S",
		Role:        entity.RoleGuest,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	date := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	if err := repo.RecordSession(ctx, u.ID, date); err != nil {
		t.Fatalf("RecordSession: %v", err)
	}
	got, err := repo.GetLastSessionDate(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetLastSessionDate: %v", err)
	}
	if got == nil {
		t.Fatal("expected a session date")
	}
	if got.Year() != 2026 || got.Month() != time.June || got.Day() != 24 {
		t.Errorf("unexpected date: %s", got)
	}

	// Recording the same date again is an upsert that updates last_seen_at.
	if err := repo.RecordSession(ctx, u.ID, date); err != nil {
		t.Fatalf("RecordSession second: %v", err)
	}

	// A newer date returns the newer date.
	date2 := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	if err := repo.RecordSession(ctx, u.ID, date2); err != nil {
		t.Fatalf("RecordSession date2: %v", err)
	}
	got2, err := repo.GetLastSessionDate(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetLastSessionDate 2: %v", err)
	}
	if got2.Day() != 25 {
		t.Errorf("expected day=25 (newer), got %d", got2.Day())
	}
}

func TestPostgresPriorityTransactions_Log(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresUserRepository(db)
	ctx := context.Background()

	u := &entity.User{
		Email:       "t@example.com",
		DisplayName: "T",
		Role:        entity.RoleGuest,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := repo.LogPriorityTransaction(ctx, u.ID, "vid", "Song", "spend", 1, 0); err != nil {
		t.Fatalf("LogPriorityTransaction: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM priority_transactions WHERE user_id = $1`, u.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 priority transaction, got %d", n)
	}
}

// ----- AutoQueueRepository conformance -----

func TestPostgresAutoQueue_GetSaveConfig(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresAutoQueueRepository(db)
	ctx := context.Background()

	cfg, err := repo.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.Enabled || cfg.Strategy != domain.StrategyRelated {
		t.Errorf("expected default disabled/related, got %+v", cfg)
	}

	if err := repo.SaveConfig(ctx, domain.AutoQueueConfig{Enabled: true, Strategy: domain.StrategyHistoryRandom}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cfg, err = repo.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig 2: %v", err)
	}
	if !cfg.Enabled || cfg.Strategy != domain.StrategyHistoryRandom {
		t.Errorf("expected enabled/random after save, got %+v", cfg)
	}
}

func TestPostgresAutoQueue_AppendAndRecentHistory(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresAutoQueueRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 3; i++ {
		entry := domain.PlayHistoryEntry{
			VideoID:  "v" + string(rune('a'+i)),
			Title:    "T" + string(rune('a'+i)),
			PlayedAt: now.Add(time.Duration(i) * time.Second),
		}
		if err := repo.AppendHistory(ctx, entry); err != nil {
			t.Fatalf("AppendHistory %d: %v", i, err)
		}
	}
	got, err := repo.GetRecentHistory(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecentHistory: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	// Newest first.
	if got[0].VideoID != "vc" {
		t.Errorf("expected newest first (vc), got %s", got[0].VideoID)
	}
}

func TestPostgresAutoQueue_PlayHistory50Cap(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	repo := NewPostgresAutoQueueRepository(db)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 60; i++ {
		entry := domain.PlayHistoryEntry{
			VideoID:  "bulk",
			Title:    "bulk",
			PlayedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := repo.AppendHistory(ctx, entry); err != nil {
			t.Fatalf("AppendHistory %d: %v", i, err)
		}
	}
	got, err := repo.GetRecentHistory(ctx, 100)
	if err != nil {
		t.Fatalf("GetRecentHistory: %v", err)
	}
	if len(got) != PlayHistoryCap {
		t.Errorf("expected exactly %d rows after cap, got %d", PlayHistoryCap, len(got))
	}
	// The 50 most recent (i=10..59) must remain; the oldest 10 must be gone.
	first := got[len(got)-1]
	if !first.PlayedAt.Equal(base.Add(10 * time.Second)) {
		t.Errorf("expected oldest surviving entry at +10s, got %s", first.PlayedAt.Sub(base))
	}
}

// ----- Compile-time conformance assertion -----
//
// The Postgres repositories must satisfy the domain/repository interfaces.
// A failure here signals signature drift between SQLite and Postgres
// implementations and is treated as a build-time error.

var (
	_ = func() error {
		var q repository.QueueRepository = (*PostgresRepository)(nil)
		var u repository.UserRepository = (*PostgresUserRepository)(nil)
		var a domain.AutoQueueRepository = (*PostgresAutoQueueRepository)(nil)
		_ = q
		_ = u
		_ = a
		return nil
	}()
	_ = errors.New
)