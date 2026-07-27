package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/roomautoqueue"
	"local-music-queue/internal/usecase/roomqueue"
)

// --- test fixture helpers ---

// pgRoomAutoQueueHandlers wires the RoomAutoQueueHandlers against a
// per-test schema (created via persistence.NewRoomTestDB), the
// real Postgres-backed room/queue/auto-queue repos, and the
// roomautoqueue use case. Skip when PG is unreachable.
func pgRoomAutoQueueHandlers(t *testing.T) (*RoomAutoQueueHandlers, *sql.DB, *persistence.PostgresRoomAutoQueueRepository, func()) {
	t.Helper()
	db, cleanup := persistence.NewRoomTestDB(t)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	authI := auth.NewInteractor(
		persistence.NewPostgresUserRepository(db),
		"", nil, nil, nil, nil,
	)
	roomAQRepo := persistence.NewPostgresRoomAutoQueueRepository(db)
	inter := roomautoqueue.NewInteractor(roomRepo, roomAQRepo, &stubFetcher{}, nil)
	// Wire the snapshot loader so CheckAndTrigger can read queue.
	inter.SetQueueSnapshotLoader(func(ctx context.Context, roomID int64) (*entity.Queue, error) {
		q, err := queueRepo.Load(ctx, roomID)
		if err != nil {
			if errors.Is(err, repository.ErrRoomQueueNotFound) {
				return entity.NewQueue(), nil
			}
			return nil, err
		}
		return q, nil
	})
	// Wire the AddRoomAutoQueueSong seam adapter.
	roomQueueInter := roomqueue.NewInteractor(roomRepo, queueRepo, nil, nil)
	inter.SetAddRoomAutoQueueSongFunc(func(ctx context.Context, slug string, song *entity.Song, expectedSourceSongID string) (*roomautoqueue.AddRoomAutoQueueSongResult, error) {
		q, ci, cs, st, el, err := roomQueueInter.AddRoomAutoQueueSong(ctx, slug, song, expectedSourceSongID)
		if err != nil {
			if errors.Is(err, roomqueue.ErrRoomAutoQueueStale) {
				return nil, roomautoqueue.ErrAutoQueueStale
			}
			return nil, err
		}
		return &roomautoqueue.AddRoomAutoQueueSongResult{
			Queue: q, CurrentIndex: ci, CurrentSong: cs, Status: st, Elapsed: el,
		}, nil
	})
	bc := &recordingRoomBroadcaster{}
	rq := NewRoomAutoQueueHandlers(inter, authI, bc)
	return rq, db, roomAQRepo, cleanup
}

// stubFetcher implements domain.RelatedSongFetcher with no behavior
// — the handler tests never trigger an auto-queue insertion.
type stubFetcher struct{}

func (*stubFetcher) FetchRelated(_ context.Context, _ string, _ []string) (*entity.Song, error) {
	return nil, errors.New("not used in handler tests")
}

func seedRoom(t *testing.T, db *sql.DB, slug, name string, hostUserID int) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', NOW(), NOW())`,
		slug, name); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	// Members hold the per-room host role (rooms table has no
	// host_user_id column; the FK is via room_members).
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO room_members (room_id, user_id, role, joined_at)
		 SELECT id, $1, 'host', NOW() FROM rooms WHERE slug = $2`,
		hostUserID, slug); err != nil {
		t.Fatalf("seed host member: %v", err)
	}
}

func seedUserAQ(t *testing.T, db *sql.DB, id int, role entity.Role) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO users (id, email, display_name, profile_picture, role, priority_balance, created_at, updated_at)
		 VALUES ($1, $2, $2, '', $3, 0, NOW(), NOW())`,
		id, "u"+time.Now().Format("150405.000000")+"@example.com", role,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func roomID(t *testing.T, db *sql.DB, slug string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM rooms WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("roomID: %v", err)
	}
	return id
}

// --- GET /api/rooms/{slug}/autoqueue/status ---

func TestRoomAutoQueue_Status_HostSucceeds(t *testing.T) {
	rqh, db, roomAQRepo, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedRoom(t, db, "rq-aq-status-h", "StatusH", 42)
	// Seed an enabled config.
	if err := roomAQRepo.SaveConfig(context.Background(), roomID(t, db, "rq-aq-status-h"),
		domain.RoomAutoQueueConfig{Enabled: true, Strategy: domain.StrategyRelated}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/rq-aq-status-h/autoqueue/status", nil)
	rr := httptest.NewRecorder()
	rqh.HandleGetRoomAutoQueueStatus(rr, req, "rq-aq-status-h", 42)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Enabled  bool   `json:"enabled"`
		Strategy string `json:"strategy"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled {
		t.Error("expected enabled=true")
	}
	if got.Strategy != "related" {
		t.Errorf("expected strategy=related, got %q", got.Strategy)
	}
}

// TestRoomAutoQueue_Status_AnyMember_Succeeds: even a non-host
// member may read the per-room config per the spec ("any active
// member may read").
func TestRoomAutoQueue_Status_AnyMember_Succeeds(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedUserAQ(t, db, 200, entity.RoleGuest)
	seedRoom(t, db, "rq-aq-status-m", "StatusM", 42)
	rID := roomID(t, db, "rq-aq-status-m")
	if err := persistence.NewPostgresRoomRepository(db).AddMember(context.Background(), rID, 200, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
		t.Fatalf("add guest: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/rq-aq-status-m/autoqueue/status", nil)
	rr := httptest.NewRecorder()
	rqh.HandleGetRoomAutoQueueStatus(rr, req, "rq-aq-status-m", 200)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomAutoQueue_Status_NonMember_403(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedRoom(t, db, "rq-aq-status-nm", "StatusNM", 42)

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/rq-aq-status-nm/autoqueue/status", nil)
	rr := httptest.NewRecorder()
	rqh.HandleGetRoomAutoQueueStatus(rr, req, "rq-aq-status-nm", 999)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomAutoQueue_Status_RoomNotFound_404(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/rq-aq-status-missing/autoqueue/status", nil)
	rr := httptest.NewRecorder()
	rqh.HandleGetRoomAutoQueueStatus(rr, req, "rq-aq-status-missing", 42)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomAutoQueue_Status_ArchivedRoom_409(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedRoom(t, db, "rq-aq-status-arch", "StatusArch", 42)
	rID := roomID(t, db, "rq-aq-status-arch")
	if err := persistence.NewPostgresRoomRepository(db).ArchiveRoom(context.Background(), rID, time.Now().UTC()); err != nil {
		t.Fatalf("archive: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/rq-aq-status-arch/autoqueue/status", nil)
	rr := httptest.NewRecorder()
	rqh.HandleGetRoomAutoQueueStatus(rr, req, "rq-aq-status-arch", 42)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// --- POST /api/rooms/{slug}/autoqueue/toggle ---

func TestRoomAutoQueue_Toggle_HostSucceeds(t *testing.T) {
	rqh, db, roomAQRepo, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedRoom(t, db, "rq-aq-tog-h", "TogH", 42)
	rID := roomID(t, db, "rq-aq-tog-h")
	if err := roomAQRepo.SaveConfig(context.Background(), rID,
		domain.RoomAutoQueueConfig{Enabled: false, Strategy: domain.StrategyRelated}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	body := []byte(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-aq-tog-h/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRoomAutoQueueToggle(rr, req, "rq-aq-tog-h", 42)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Enabled  bool   `json:"enabled"`
		Strategy string `json:"strategy"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled {
		t.Error("expected enabled=true in response")
	}
	cfg, _ := roomAQRepo.GetConfig(context.Background(), rID)
	if !cfg.Enabled {
		t.Error("expected persisted Enabled=true")
	}
	if got.Strategy != "related" {
		t.Errorf("expected strategy=related, got %q", got.Strategy)
	}
}

func TestRoomAutoQueue_Toggle_GuestForbidden(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedUserAQ(t, db, 200, entity.RoleGuest)
	seedRoom(t, db, "rq-aq-tog-g", "TogG", 42)
	rID := roomID(t, db, "rq-aq-tog-g")
	if err := persistence.NewPostgresRoomRepository(db).AddMember(context.Background(), rID, 200, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
		t.Fatalf("add guest: %v", err)
	}

	body := []byte(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-aq-tog-g/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRoomAutoQueueToggle(rr, req, "rq-aq-tog-g", 200)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomAutoQueue_Toggle_MissingBody_400(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedRoom(t, db, "rq-aq-tog-mb", "TogMB", 42)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-aq-tog-mb/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRoomAutoQueueToggle(rr, req, "rq-aq-tog-mb", 42)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomAutoQueue_Toggle_ArchivedRoom_409(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedRoom(t, db, "rq-aq-tog-arch", "TogArch", 42)
	rID := roomID(t, db, "rq-aq-tog-arch")
	if err := persistence.NewPostgresRoomRepository(db).ArchiveRoom(context.Background(), rID, time.Now().UTC()); err != nil {
		t.Fatalf("archive: %v", err)
	}

	body := []byte(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-aq-tog-arch/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRoomAutoQueueToggle(rr, req, "rq-aq-tog-arch", 42)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomAutoQueue_Toggle_RoomNotFound_404(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)

	body := []byte(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-aq-tog-missing/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRoomAutoQueueToggle(rr, req, "rq-aq-tog-missing", 42)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestRoomAutoQueue_Toggle_BroadcastsConfigChanged: a successful
// toggle fires room_auto_queue_config_changed on the broadcaster.
func TestRoomAutoQueue_Toggle_BroadcastsConfigChanged(t *testing.T) {
	rqh, db, _, cleanup := pgRoomAutoQueueHandlers(t)
	defer cleanup()
	seedUserAQ(t, db, 42, entity.RoleHost)
	seedRoom(t, db, "rq-aq-tog-bc", "TogBC", 42)

	// Replace the interactor's broadcaster so we capture the call.
	bc := &recordingRoomBroadcaster{}
	rqh.broadcaster = bc

	body := []byte(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-aq-tog-bc/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRoomAutoQueueToggle(rr, req, "rq-aq-tog-bc", 42)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.autoQueueConfigChangedCalls) != 1 {
		t.Fatalf("expected 1 config-changed broadcast, got %d", len(bc.autoQueueConfigChangedCalls))
	}
	call := bc.autoQueueConfigChangedCalls[0]
	if call.slug != "rq-aq-tog-bc" || !call.enabled || call.strategy != "related" {
		t.Errorf("unexpected config-changed call: %+v", call)
	}
}

// silence unused imports during incremental edits.
var (
	_ = errors.New
	_ = sync.Mutex{}
)
