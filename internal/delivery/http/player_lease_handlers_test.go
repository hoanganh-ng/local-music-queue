package http

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/room"
)

// newLeaseHandlersWithDB returns RoomHandlers wired with a real
// PlayerLeaseInteractor against the per-test schema, plus the *sql.DB so
// callers can seed users in the same schema.
func newLeaseHandlersWithDB(t *testing.T) (*RoomHandlers, *sql.DB) {
	t.Helper()
	rh, _, db := newRoomHandlers(t)
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	rh.lease = room.NewPlayerLeaseInteractor(leaseRepo, rh.inter.Repo(), db, 60*time.Second, 30*time.Second)
	return rh, db
}

func TestPlayerLeaseHandler_Claim_409OnDuplicate(t *testing.T) {
	rh, db := newLeaseHandlersWithDB(t)
	hostID := seedUser(t, db, "host-claim@example.com", entity.RoleHost)
	if _, err := rh.inter.CreateRoom(context.Background(), "dup-claim", "DupClaim", hostID); err != nil {
		t.Fatalf("create room: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/dup-claim/player/claim", nil)
	rh.HandleClaimPlayer(rr, req, "dup-claim", hostID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first claim expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/rooms/dup-claim/player/claim", nil)
	rh.HandleClaimPlayer(rr2, req2, "dup-claim", hostID)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("duplicate claim expected 409, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestPlayerLeaseHandler_Heartbeat_404WhenNoLease(t *testing.T) {
	rh, db := newLeaseHandlersWithDB(t)
	hostID := seedUser(t, db, "host-hb@example.com", entity.RoleHost)
	if _, err := rh.inter.CreateRoom(context.Background(), "hb-room", "HB", hostID); err != nil {
		t.Fatalf("create room: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/hb-room/player/heartbeat", nil)
	rh.HandleHeartbeatPlayer(rr, req, "hb-room", hostID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("heartbeat with no lease expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlayerLeaseHandler_Release_204AndRoomArchived(t *testing.T) {
	rh, db := newLeaseHandlersWithDB(t)
	hostID := seedUser(t, db, "host-rel@example.com", entity.RoleHost)
	roomObj, err := rh.inter.CreateRoom(context.Background(), "rel-room", "Rel", hostID)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if _, err := rh.lease.Claim(context.Background(), "rel-room", hostID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rel-room/player/release", nil)
	rh.HandleReleasePlayer(rr, req, "rel-room", hostID)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("release expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	updated, err := rh.inter.Repo().GetRoomByID(context.Background(), roomObj.ID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if updated.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived after release, got %s", updated.Status)
	}
}

func TestPlayerLeaseHandler_GetLease_404WhenNone(t *testing.T) {
	rh, db := newLeaseHandlersWithDB(t)
	hostID := seedUser(t, db, "viewer-gl@example.com", entity.RoleHost)
	if _, err := rh.inter.CreateRoom(context.Background(), "view-room", "View", hostID); err != nil {
		t.Fatalf("create room: %v", err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/view-room/player/lease", nil)
	rh.HandleGetPlayerLease(rr, req, "view-room", hostID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get lease with none expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}