package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/room"
)

// seedUser inserts a user row with the given role and returns the new id.
// Uses the per-test scoped *sql.DB returned from newRoomHandlers so the user
// is created in the SAME schema the room repo reads from.
func seedUser(t *testing.T, db *sql.DB, email string, role entity.Role) int {
	t.Helper()
	var id int
	err := db.QueryRow(`INSERT INTO users (email, display_name, role, created_at, updated_at)
		VALUES ($1, $1, $2, NOW(), NOW()) RETURNING id`, email, role).Scan(&id)
	if err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	return id
}

// newRoomHandlers builds a RoomHandlers plus a base *Handlers and the
// *sql.DB scoped to the per-test schema. Tests use the returned db
// directly to seed users so they live in the same schema as the room
// repo. The schema is dropped and the db closed on test cleanup.
//
// PG connectivity is optional: when LMQ_TEST_DATABASE_URL is unreachable
// the test is skipped rather than failed.
func newRoomHandlers(t *testing.T) (*RoomHandlers, *Handlers, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}

	// Probe for reachability first; skip (not fail) when the DB is absent.
	probe, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("postgres unavailable (open): %v", err)
	}
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 3*time.Second)
	pingErr := probe.PingContext(pingCtx)
	cancelPing()
	_ = probe.Close()
	if pingErr != nil {
		t.Skipf("postgres unavailable (ping): %v", pingErr)
	}

	schema := fmt.Sprintf("lmq_roomhandler_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())

	// Open the scoped connection first so search_path is applied from the
	// very first query (including CREATE SCHEMA bookkeeping). This mirrors
	// NewRoomTestDB.
	scoped, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		t.Fatalf("open scoped: %v", err)
	}
	if err := scoped.Ping(); err != nil {
		_ = scoped.Close()
		t.Skipf("postgres unavailable (ping scoped): %v", err)
	}

	if _, err := scoped.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = scoped.Close()
		t.Fatalf("create schema: %v", err)
	}

	if err := persistence.RunEmbeddedMigrationsUp(scoped); err != nil {
		_ = scoped.Close()
		t.Fatalf("migrate up: %v", err)
	}

	t.Cleanup(func() {
		drop, derr := sql.Open("pgx", dsn)
		if derr == nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
		_ = scoped.Close()
	})

	// Build a *Handlers via newTestHandlers so unrelated dependencies
	// (auth interactor, hub, etc.) are wired. newTestHandlers opens its
	// OWN per-test schema — that's fine for this test, since these tests
	// do not exercise base handler endpoints.
	base := newTestHandlers(t)

	repo := persistence.NewPostgresRoomRepository(scoped)
	inter := room.NewInteractor(repo)
	return NewRoomHandlers(inter, base.auth), base, scoped
}

func TestRoomHandler_CreateRoom_Success(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	creatorID := seedUser(t, db, "host@example.com", entity.RoleHost)

	body, _ := json.Marshal(map[string]string{"slug": "lounge", "name": "Lounge"})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rh.HandleCreateRoom(rr, req, creatorID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got entity.Room
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Slug != "lounge" || got.Status != entity.RoomStatusActive {
		t.Errorf("unexpected room: %+v", got)
	}
}

func TestRoomHandler_CreateRoom_ReservedSlug400(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	creatorID := seedUser(t, db, "host@example.com", entity.RoleHost)
	body, _ := json.Marshal(map[string]string{"slug": "api", "name": "X"})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rh.HandleCreateRoom(rr, req, creatorID)
	// Reserved slugs map to 400 per the spec mapping (writeRoomError).
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for reserved slug, got %d", rr.Code)
	}
}

func TestRoomHandler_CreateRoom_InvalidSlug(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	creatorID := seedUser(t, db, "host@example.com", entity.RoleHost)
	body, _ := json.Marshal(map[string]string{"slug": "Bad", "name": "X"})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rh.HandleCreateRoom(rr, req, creatorID)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRoomHandler_ListRooms(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	_ = seedUser(t, db, "host@example.com", entity.RoleHost)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms", nil)
	rr := httptest.NewRecorder()
	rh.HandleListRooms(rr, req, 1)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRoomHandler_GetRoom_NotFound404(t *testing.T) {
	rh, _, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/missing-room", nil)
	rr := httptest.NewRecorder()
	rh.HandleGetRoom(rr, req, "missing-room", 1)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestRoomHandler_GetRoom_InvalidSlug400(t *testing.T) {
	rh, _, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/BadSlug", nil)
	rr := httptest.NewRecorder()
	rh.HandleGetRoom(rr, req, "BadSlug", 1)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid slug, got %d", rr.Code)
	}
}

func TestRoomHandler_ListMembers_Unauthorized401(t *testing.T) {
	rh, _, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/lounge/members", nil)
	rr := httptest.NewRecorder()
	rh.HandleListMembers(rr, req, "lounge", 0) // actor=0 (unauthenticated)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRoomHandler_ListMembers_NonMember403(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	outsider := seedUser(t, db, "o@example.com", entity.RoleGuest)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/lounge/members", nil)
	rr := httptest.NewRecorder()
	rh.HandleListMembers(rr, req, "lounge", outsider)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-member, got %d", rr.Code)
	}
}

func TestRoomHandler_PromoteNonHostForbidden(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	admin := seedUser(t, db, "a@example.com", entity.RoleAdmin)
	guest := seedUser(t, db, "g@example.com", entity.RoleGuest)

	// Create room and seed members directly via the repo.
	ctx := context.Background()
	if _, err := rh.inter.CreateRoom(ctx, "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	// Look up the room ID via slug so we can seed members under the same ID.
	room1, err := rh.inter.Repo().GetRoomBySlug(ctx, "lounge")
	if err != nil {
		t.Fatalf("GetRoomBySlug: %v", err)
	}
	if err := rh.inter.Repo().AddMember(ctx, room1.ID, admin, entity.RoomRoleAdmin, time.Now()); err != nil {
		t.Fatalf("AddMember admin: %v", err)
	}
	if err := rh.inter.Repo().AddMember(ctx, room1.ID, guest, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/members/%d/promote", "lounge", guest), nil)
	rr := httptest.NewRecorder()
	rh.HandlePromoteMember(rr, req, "lounge", guest /*actor is non-host*/, guest)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRoomHandler_CreateInvite_NonHostForbidden(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	outsider := seedUser(t, db, "o@example.com", entity.RoleGuest)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	body, _ := json.Marshal(map[string]int{"max_uses": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/lounge/invites", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rh.HandleCreateInvite(rr, req, "lounge", outsider)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-member creating invite, got %d", rr.Code)
	}
}

func TestRoomHandler_InviteRedeem_Generic404(t *testing.T) {
	rh, _, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/api/invites/garbage/redeem", nil)
	rr := httptest.NewRecorder()
	rh.HandleRedeemInvite(rr, req, "garbage", 1)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown token, got %d", rr.Code)
	}
}
