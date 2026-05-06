package persistence

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer db.Close()

	repo := &SQLiteRepository{db: db}
	if err := repo.init(); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	t.Run("auto_queue_config table exists", func(t *testing.T) {
		var enabled int
		var strategy string
		err := db.QueryRow("SELECT enabled, strategy FROM auto_queue_config WHERE id = 1").Scan(&enabled, &strategy)
		if err != nil {
			t.Fatalf("auto_queue_config table not found or default row missing: %v", err)
		}
		if enabled != 0 {
			t.Errorf("expected enabled=0, got %d", enabled)
		}
		if strategy != "related" {
			t.Errorf("expected strategy='related', got %s", strategy)
		}
	})

	t.Run("play_history table exists", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO play_history (video_id, title) VALUES (?, ?)", "test_id", "Test Song")
		if err != nil {
			t.Fatalf("play_history table not found: %v", err)
		}

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM play_history").Scan(&count)
		if err != nil {
			t.Fatalf("failed to count play_history rows: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 row, got %d", count)
		}
	})

	t.Run("play_history trigger caps at 50 rows", func(t *testing.T) {
		ctx := context.Background()
		for i := 0; i < 51; i++ {
			_, err := db.ExecContext(ctx, "INSERT INTO play_history (video_id, title) VALUES (?, ?)",
				"video_"+string(rune(i)), "Song "+string(rune(i)))
			if err != nil {
				t.Fatalf("failed to insert row %d: %v", i, err)
			}
		}

		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM play_history").Scan(&count)
		if err != nil {
			t.Fatalf("failed to count rows: %v", err)
		}
		if count != 50 {
			t.Errorf("expected exactly 50 rows after trigger, got %d", count)
		}
	})
}
