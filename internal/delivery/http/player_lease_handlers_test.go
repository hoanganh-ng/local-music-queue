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

	// Wire a recorder broadcaster to assert the handler invokes the
	// room_archived path on a real release (R06 fix #2).
	rec := &recordingBroadcaster{}
	rh.SetArchiveBroadcaster(rec)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rel-room/player/release", nil)
	rh.HandleReleasePlayer(rr, req, "rel-room", hostID)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("release expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	if len(rec.events) != 1 {
		t.Fatalf("expected broadcaster to receive 1 archive event, got %d", len(rec.events))
	}
	if rec.events[0].Reason != string(entity.PlayerLeaseExplicit) {
		t.Errorf("expected reason %q, got %q", entity.PlayerLeaseExplicit, rec.events[0].Reason)
	}
	if rec.events[0].RoomID != roomObj.ID {
		t.Errorf("expected RoomID %d, got %d", roomObj.ID, rec.events[0].RoomID)
	}

	updated, err := rh.inter.Repo().GetRoomByID(context.Background(), roomObj.ID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if updated.Status != entity.RoomStatusArchived {
		t.Errorf("expected archived after release, got %s", updated.Status)
	}
}

// TestPlayerLeaseHandler_Release_NoLease_404LeavesRoomActive covers R06
// fix #3: POST /player/release on a room with no active lease must
// return 404 and must NOT touch the room (no archive, no broadcaster
// invocation).
func TestPlayerLeaseHandler_Release_NoLease_404LeavesRoomActive(t *testing.T) {
	rh, db := newLeaseHandlersWithDB(t)
	hostID := seedUser(t, db, "host-rel-nolease@example.com", entity.RoleHost)
	roomObj, err := rh.inter.CreateRoom(context.Background(), "rel-nolease-room", "RelNoLease", hostID)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	rec := &recordingBroadcaster{}
	rh.SetArchiveBroadcaster(rec)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rel-nolease-room/player/release", nil)
	rh.HandleReleasePlayer(rr, req, "rel-nolease-room", hostID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("release with no lease expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}

	if len(rec.events) != 0 {
		t.Errorf("broadcaster must NOT be invoked when there is no lease, got %d events", len(rec.events))
	}

	updated, err := rh.inter.Repo().GetRoomByID(context.Background(), roomObj.ID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if updated.Status != entity.RoomStatusActive {
		t.Errorf("expected room to remain active when no lease to release, got %s", updated.Status)
	}
}

// recordingBroadcaster captures every archive event handed to it so
// handler-level tests can assert the room_archived path was exercised.
type recordingBroadcaster struct {
	events []room.RoomArchivedEvent
}

func (r *recordingBroadcaster) BroadcastRoomArchived(ev room.RoomArchivedEvent) {
	r.events = append(r.events, ev)
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