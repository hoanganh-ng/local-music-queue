package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
)

// TestRoomHandler_DeleteRoom_204OnSuccess: host actor → 204; room
// transitions to archived; broadcaster fires once with reason "host_archived".
func TestRoomHandler_DeleteRoom_204OnSuccess(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	rec := &recordingMembersBroadcaster{}
	rh.SetMemberBroadcaster(rec)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge", nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteRoom(rr, req, "lounge", host)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	if len(rec.archived) != 1 || rec.archived[0] != "lounge|host_archived" {
		t.Errorf("expected one archive broadcast reason=host_archived, got %+v", rec.archived)
	}
}

// TestRoomHandler_DeleteRoom_204OnAlreadyArchived: idempotent — no
// broadcast, no error.
func TestRoomHandler_DeleteRoom_204OnAlreadyArchived(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	rec := &recordingMembersBroadcaster{}
	rh.SetMemberBroadcaster(rec)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	// Pre-archive via the interactor so we have a known state.
	if _, err := rh.inter.ArchiveRoomByHost(context.Background(), "lounge", host); err != nil {
		t.Fatalf("pre-archive: %v", err)
	}
	rec.archived = nil

	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge", nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteRoom(rr, req, "lounge", host)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on idempotent re-archive, got %d", rr.Code)
	}
	if len(rec.archived) != 0 {
		t.Errorf("expected NO archive broadcast on idempotent re-archive, got %+v", rec.archived)
	}
}

// TestRoomHandler_DeleteRoom_403OnNonHost: non-host actor → 403.
func TestRoomHandler_DeleteRoom_403OnNonHost(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	admin := seedUser(t, db, "a@example.com", entity.RoleAdmin)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge", nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteRoom(rr, req, "lounge", admin)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 on non-host archive, got %d", rr.Code)
	}
}

// TestRoomHandler_DeleteRoom_404OnMissingRoom.
func TestRoomHandler_DeleteRoom_404OnMissingRoom(t *testing.T) {
	rh, _, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/nope", nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteRoom(rr, req, "nope", 1)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 on missing room, got %d", rr.Code)
	}
}

// TestRoomHandler_DeleteRoom_400OnInvalidSlug.
func TestRoomHandler_DeleteRoom_400OnInvalidSlug(t *testing.T) {
	rh, _, _ := newRoomHandlers(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/BadSlug", nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteRoom(rr, req, "BadSlug", 1)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid slug, got %d", rr.Code)
	}
}

// TestRoomHandler_DeleteMember_204OnSuccess: host removes a guest.
//
// Note (post-a7025c0): CloseRemovedClient + BroadcastRoomMemberRemoved +
// BroadcastRoomMembersChanged are now INTERACTOR-level seams. After the
// handler returns 204, the recording broadcaster wired at the handler
// level must be empty for all three slices — the handler itself only
// invokes the interactor and writes the status code. The interactor
// side is covered by TestRoom_RemoveMemberByHost_* in the usecase
// package. The primary purpose of this test is therefore the 204
// status, plus a negative assertion that no broadcaster calls leak
// from the handler transport layer.
func TestRoomHandler_DeleteMember_204OnSuccess(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	rec := &recordingMembersBroadcaster{}
	rh.SetMemberBroadcaster(rec)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	guest := seedUser(t, db, "g@example.com", entity.RoleGuest)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	roomObj, _ := rh.inter.Repo().GetRoomBySlug(context.Background(), "lounge")
	if err := rh.inter.Repo().AddMember(context.Background(), roomObj.ID, guest, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember guest: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge/members/"+strconv.Itoa(guest), nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteMember(rr, req, "lounge", host, guest)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	// Handler is transport-only: no broadcaster must fire from this layer.
	if len(rec.memberRemoved) != 0 {
		t.Errorf("expected NO handler-level member-removed broadcast (interactor's seam), got %+v", rec.memberRemoved)
	}
	if len(rec.membersChanged) != 0 {
		t.Errorf("expected NO handler-level members-changed broadcast (interactor's seam), got %d", len(rec.membersChanged))
	}
	if len(rec.closed) != 0 {
		t.Errorf("expected NO handler-level close call (interactor's seam since a7025c0), got %+v", rec.closed)
	}
}

// TestRoomHandler_DeleteMember_400OnHostRemovesSelf.
func TestRoomHandler_DeleteMember_400OnHostRemovesSelf(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge/members/"+strconv.Itoa(host), nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteMember(rr, req, "lounge", host, host)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "host cannot remove self\n" {
		t.Errorf("unexpected body: %s", rr.Body.String())
	}
}

// TestRoomHandler_DeleteMember_404OnTargetNotMember.
func TestRoomHandler_DeleteMember_404OnTargetNotMember(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge/members/999", nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteMember(rr, req, "lounge", host, 999)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// TestRoomHandler_DeleteMember_403OnNonHost.
func TestRoomHandler_DeleteMember_403OnNonHost(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	admin := seedUser(t, db, "a@example.com", entity.RoleAdmin)
	guest := seedUser(t, db, "g@example.com", entity.RoleGuest)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	roomObj, _ := rh.inter.Repo().GetRoomBySlug(context.Background(), "lounge")
	if err := rh.inter.Repo().AddMember(context.Background(), roomObj.ID, guest, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge/members/"+strconv.Itoa(guest), nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteMember(rr, req, "lounge", admin /*actor*/, guest /*target*/)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// TestRoomHandler_DeleteMember_409OnArchivedRoom.
func TestRoomHandler_DeleteMember_409OnArchivedRoom(t *testing.T) {
	rh, _, db := newRoomHandlers(t)
	host := seedUser(t, db, "h@example.com", entity.RoleHost)
	guest := seedUser(t, db, "g@example.com", entity.RoleGuest)
	if _, err := rh.inter.CreateRoom(context.Background(), "lounge", "Lounge", host); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	roomObj, _ := rh.inter.Repo().GetRoomBySlug(context.Background(), "lounge")
	if err := rh.inter.Repo().AddMember(context.Background(), roomObj.ID, guest, entity.RoomRoleGuest, time.Now()); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := rh.inter.Repo().ArchiveRoom(context.Background(), roomObj.ID, time.Now()); err != nil {
		t.Fatalf("ArchiveRoom: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/lounge/members/"+strconv.Itoa(guest), nil)
	rr := httptest.NewRecorder()
	rh.HandleDeleteMember(rr, req, "lounge", host, guest)
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

// TestRoomHandler_DeleteMember_400OnInvalidUserId is covered at the
// route registration in main.go (strconv.Atoi + <=0 gate returns 400
// before the handler runs), so there is nothing handler-level to test.
func TestRoomHandler_DeleteMember_400OnInvalidUserId(t *testing.T) {
	t.Skip("covered at route registration in main.go (strconv.Atoi + <=0 gate)")
}

// recordingMembersBroadcaster is a thread-safe stub for
// room.RoomMembersBroadcaster used by the handler tests.
type recordingMembersBroadcaster struct {
	archived       []string
	memberRemoved  []bcMemberRemoved
	membersChanged []bcMembersChanged
	closed         []bcMemberRemoved
}

type bcMemberRemoved struct {
	RoomSlug string
	Target   int
	Reason   string
}

type bcMembersChanged struct {
	RoomSlug string
	Members  []entity.RoomMember
}

func (r *recordingMembersBroadcaster) BroadcastRoomArchived(roomSlug, reason string) {
	r.archived = append(r.archived, roomSlug+"|"+reason)
}
func (r *recordingMembersBroadcaster) BroadcastRoomMemberRemoved(roomSlug string, target int, reason string) {
	r.memberRemoved = append(r.memberRemoved, bcMemberRemoved{roomSlug, target, reason})
}
func (r *recordingMembersBroadcaster) BroadcastRoomMembersChanged(roomSlug string, members []entity.RoomMember) {
	cp := make([]entity.RoomMember, len(members))
	copy(cp, members)
	r.membersChanged = append(r.membersChanged, bcMembersChanged{roomSlug, cp})
}
func (r *recordingMembersBroadcaster) CloseRemovedClient(roomSlug string, target int) {
	r.closed = append(r.closed, bcMemberRemoved{roomSlug, target, ""})
}

