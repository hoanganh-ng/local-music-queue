package persistence

import (
	"context"
	"database/sql"
	"local-music-queue/internal/domain"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestAutoQueueRepository(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer db.Close()

	repo := &SQLiteRepository{db: db}
	if err := repo.init(); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	autoQueueRepo := NewSQLiteAutoQueueRepository(db)
	ctx := context.Background()

	t.Run("GetConfig returns default values", func(t *testing.T) {
		cfg, err := autoQueueRepo.GetConfig(ctx)
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if cfg.Enabled {
			t.Error("expected Enabled=false by default")
		}
		if cfg.Strategy != domain.StrategyRelated {
			t.Errorf("expected Strategy='related', got %s", cfg.Strategy)
		}
	})

	t.Run("SaveConfig updates enabled flag", func(t *testing.T) {
		err := autoQueueRepo.SaveConfig(ctx, domain.AutoQueueConfig{
			Enabled:  true,
			Strategy: domain.StrategyRelated,
		})
		if err != nil {
			t.Fatalf("SaveConfig failed: %v", err)
		}

		cfg, err := autoQueueRepo.GetConfig(ctx)
		if err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}
		if !cfg.Enabled {
			t.Error("expected Enabled=true after save")
		}
	})

	t.Run("AppendHistory adds a row", func(t *testing.T) {
		entry := domain.PlayHistoryEntry{
			VideoID:  "test_video_1",
			Title:    "Test Song 1",
			PlayedAt: time.Now(),
		}
		err := autoQueueRepo.AppendHistory(ctx, entry)
		if err != nil {
			t.Fatalf("AppendHistory failed: %v", err)
		}

		history, err := autoQueueRepo.GetRecentHistory(ctx, 1)
		if err != nil {
			t.Fatalf("GetRecentHistory failed: %v", err)
		}
		if len(history) != 1 {
			t.Fatalf("expected 1 history entry, got %d", len(history))
		}
		if history[0].VideoID != "test_video_1" {
			t.Errorf("expected VideoID='test_video_1', got %s", history[0].VideoID)
		}
	})

	t.Run("GetRecentHistory returns newest first", func(t *testing.T) {
		entry2 := domain.PlayHistoryEntry{
			VideoID:  "test_video_2",
			Title:    "Test Song 2",
			PlayedAt: time.Now().Add(1 * time.Second),
		}
		err := autoQueueRepo.AppendHistory(ctx, entry2)
		if err != nil {
			t.Fatalf("AppendHistory failed: %v", err)
		}

		history, err := autoQueueRepo.GetRecentHistory(ctx, 10)
		if err != nil {
			t.Fatalf("GetRecentHistory failed: %v", err)
		}
		if len(history) < 2 {
			t.Fatalf("expected at least 2 history entries, got %d", len(history))
		}
		if history[0].VideoID != "test_video_2" {
			t.Errorf("expected newest entry first, got %s", history[0].VideoID)
		}
	})

	t.Run("History cap at 50 entries", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			entry := domain.PlayHistoryEntry{
				VideoID:  "bulk_video",
				Title:    "Bulk Song",
				PlayedAt: time.Now(),
			}
			_ = autoQueueRepo.AppendHistory(ctx, entry)
		}

		history, err := autoQueueRepo.GetRecentHistory(ctx, 100)
		if err != nil {
			t.Fatalf("GetRecentHistory failed: %v", err)
		}
		if len(history) != 50 {
			t.Errorf("expected exactly 50 entries after trigger, got %d", len(history))
		}
	})
}
