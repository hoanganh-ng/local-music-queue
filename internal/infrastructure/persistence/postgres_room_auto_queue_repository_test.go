package persistence

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain"
)

// pgRoomAQSeed returns the wired repo + the raw *sql.DB so the test
// can both exercise the repo surface and seed rooms via direct SQL.
// Uses the per-test PG schema created by NewRoomTestDB.
func pgRoomAQSeed(t *testing.T) (domain.RoomAutoQueueRepository, *sql.DB, func()) {
	t.Helper()
	db, cleanup := NewRoomTestDB(t)
	repo := NewPostgresRoomAutoQueueRepository(db)
	return repo, db, cleanup
}

// seedRoom inserts a host room keyed by slug and returns its numeric
// id. Uses the real *sql.DB so the seed matches the rest of the
// room-queue test suite's seeding convention. The rooms table has no
// host_user_id column (host role is recorded via room_members); the
// per-room auto-queue config + history tables only FK on room_id,
// so a base rooms insert is sufficient.
func seedRoom(t *testing.T, db *sql.DB, slug string) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', NOW(), NOW())`,
		slug, slug); err != nil {
		t.Fatalf("seed room %q: %v", slug, err)
	}
	var id int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM rooms WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("lookup room %q: %v", slug, err)
	}
	return id
}

// TestRoomAutoQueueRepo_GetConfig_DefaultDisabled pins the
// missing-row default: a fresh room with no config row returns
// (Enabled=false, Strategy=StrategyRelated). Mirrors the global
// auto_queue_config behavior on a fresh install.
func TestRoomAutoQueueRepo_GetConfig_DefaultDisabled(t *testing.T) {
	repo, db, cleanup := pgRoomAQSeed(t)
	defer cleanup()
	roomID := seedRoom(t, db, "rq-aq-def")

	cfg, err := repo.GetConfig(context.Background(), roomID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg.Enabled {
		t.Error("expected default Enabled=false, got true")
	}
	if cfg.Strategy != domain.StrategyRelated {
		t.Errorf("expected default Strategy=related, got %q", cfg.Strategy)
	}
}

// TestRoomAutoQueueRepo_SaveConfig_RoundTrip pins the upsert + read
// contract: SaveConfig persists a config row, GetConfig reads it
// back, second SaveConfig with a different config updates the same
// row (not a duplicate).
func TestRoomAutoQueueRepo_SaveConfig_RoundTrip(t *testing.T) {
	repo, db, cleanup := pgRoomAQSeed(t)
	defer cleanup()
	ctx := context.Background()
	roomID := seedRoom(t, db, "rq-aq-save")

	if err := repo.SaveConfig(ctx, roomID, domain.RoomAutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated}); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	cfg, err := repo.GetConfig(ctx, roomID)
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}
	if !cfg.Enabled {
		t.Errorf("expected Enabled=true after first save, got false")
	}
	if cfg.Strategy != domain.StrategyRelated {
		t.Errorf("expected Strategy=related, got %q", cfg.Strategy)
	}

	// UPDATE on conflict — only one row remains.
	if err := repo.SaveConfig(ctx, roomID, domain.RoomAutoQueueConfig{Enabled: false, Strategy: domain.StrategyRelated}); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	cfg, err = repo.GetConfig(ctx, roomID)
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("expected Enabled=false after second save, got true")
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_auto_queue_config WHERE room_id = $1`, roomID).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("expected exactly 1 config row, got %d", rowCount)
	}
}

// TestRoomAutoQueueRepo_AppendHistory_CapEnforced pins the per-room
// 50-row cap: a 51st append deletes the oldest excess row for the
// SAME room only — other rooms' history is untouched.
func TestRoomAutoQueueRepo_AppendHistory_CapEnforced(t *testing.T) {
	repo, db, cleanup := pgRoomAQSeed(t)
	defer cleanup()
	ctx := context.Background()

	roomA := seedRoom(t, db, "rq-aq-cap-a")
	roomB := seedRoom(t, db, "rq-aq-cap-b")

	now := time.Now().UTC()
	for i := 0; i < 60; i++ {
		entry := domain.RoomPlayHistoryEntry{
			VideoID:  "vA",
			Title:    "A",
			PlayedAt: now.Add(time.Duration(-i) * time.Second), // i=0 is newest
		}
		if err := repo.AppendHistory(ctx, roomA, entry); err != nil {
			t.Fatalf("append A %d: %v", i, err)
		}
	}
	for i := 0; i < 5; i++ {
		entry := domain.RoomPlayHistoryEntry{
			VideoID:  "vB",
			Title:    "B",
			PlayedAt: now.Add(time.Duration(-i) * time.Second),
		}
		if err := repo.AppendHistory(ctx, roomB, entry); err != nil {
			t.Fatalf("append B %d: %v", i, err)
		}
	}

	var countA, countB int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_play_history WHERE room_id = $1`, roomA).Scan(&countA); err != nil {
		t.Fatalf("count A: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_play_history WHERE room_id = $1`, roomB).Scan(&countB); err != nil {
		t.Fatalf("count B: %v", err)
	}
	if countA != 50 {
		t.Errorf("expected 50 history rows for room A after 60 appends, got %d", countA)
	}
	if countB != 5 {
		t.Errorf("expected 5 history rows for room B (cap not triggered), got %d", countB)
	}
}

// TestRoomAutoQueueRepo_GetRecentHistory_OrderedDesc pins the
// newest-first ordering + the limit contract.
func TestRoomAutoQueueRepo_GetRecentHistory_OrderedDesc(t *testing.T) {
	repo, db, cleanup := pgRoomAQSeed(t)
	defer cleanup()
	ctx := context.Background()
	roomID := seedRoom(t, db, "rq-aq-ord")

	base := time.Now().UTC()
	for i := 0; i < 10; i++ {
		if err := repo.AppendHistory(ctx, roomID, domain.RoomPlayHistoryEntry{
			VideoID:  "v" + string(rune('a'+i)),
			Title:    "T",
			PlayedAt: base.Add(time.Duration(-i) * time.Minute),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	got, err := repo.GetRecentHistory(ctx, roomID, 3)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (limit=3), got %d", len(got))
	}
	if got[0].VideoID != "va" {
		t.Errorf("expected first entry VideoID=va (newest), got %q", got[0].VideoID)
	}
	if !got[0].PlayedAt.After(got[1].PlayedAt) || !got[1].PlayedAt.After(got[2].PlayedAt) {
		t.Errorf("expected PlayedAt ordering to be strictly newest-first, got %v %v %v", got[0].PlayedAt, got[1].PlayedAt, got[2].PlayedAt)
	}
}

// TestRoomAutoQueueRepo_PerRoomIsolation pins the cross-room read
// isolation: each room's config + history is independent.
func TestRoomAutoQueueRepo_PerRoomIsolation(t *testing.T) {
	repo, db, cleanup := pgRoomAQSeed(t)
	defer cleanup()
	ctx := context.Background()

	roomA := seedRoom(t, db, "rq-aq-iso-a")
	roomB := seedRoom(t, db, "rq-aq-iso-b")

	if err := repo.SaveConfig(ctx, roomA, domain.RoomAutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated}); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := repo.AppendHistory(ctx, roomA, domain.RoomPlayHistoryEntry{VideoID: "va", Title: "A", PlayedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("append A: %v", err)
	}

	cfg, err := repo.GetConfig(ctx, roomB)
	if err != nil {
		t.Fatalf("get B config: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("expected room B Enabled=false, got true (cross-room leak)")
	}
	if cfg.Strategy != domain.StrategyRelated {
		t.Errorf("expected room B Strategy=related (default), got %q", cfg.Strategy)
	}
	hist, err := repo.GetRecentHistory(ctx, roomB, 50)
	if err != nil {
		t.Fatalf("get B history: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("expected room B history to be empty (cross-room leak), got %d entries", len(hist))
	}

	cfgA, err := repo.GetConfig(ctx, roomA)
	if err != nil {
		t.Fatalf("get A config: %v", err)
	}
	if !cfgA.Enabled {
		t.Errorf("expected room A Enabled=true, got false")
	}
	histA, err := repo.GetRecentHistory(ctx, roomA, 50)
	if err != nil {
		t.Fatalf("get A history: %v", err)
	}
	if len(histA) != 1 || histA[0].VideoID != "va" {
		t.Errorf("expected room A history with va, got %+v", histA)
	}
}
