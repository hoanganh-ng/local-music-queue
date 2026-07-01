package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomautoqueue"
	"local-music-queue/internal/usecase/roomqueue"
)

// RoomAutoQueueHandlers wires the room-scoped auto-queue REST
// endpoints added in R09f. The handlers are intentionally thin so
// the use case (usecase/roomautoqueue) owns the per-room scope +
// concurrency model.
//
// Two endpoints are exposed:
//   - GET  /api/rooms/{slug}/autoqueue/status  (any active member)
//   - POST /api/rooms/{slug}/autoqueue/toggle  (host or admin only)
//
// Both endpoints ride the existing roomAuth middleware (so actor
// identity is server-resolved from the bearer token) and broadcast
// via the roomqueue.Broadcaster seam — never on the global /ws
// endpoint, which is unchanged.
type RoomAutoQueueHandlers struct {
	inter          *roomautoqueue.Interactor
	roomRepo       repository.RoomRepository
	auth           *auth.Interactor
	broadcaster    roomqueue.Broadcaster
}

// NewRoomAutoQueueHandlers constructs the handlers. broadcaster is
// used only by the toggle handler (after a successful SetEnabled it
// fires room_auto_queue_config_changed).
func NewRoomAutoQueueHandlers(inter *roomautoqueue.Interactor, roomRepo repository.RoomRepository, a *auth.Interactor, broadcaster roomqueue.Broadcaster) *RoomAutoQueueHandlers {
	return &RoomAutoQueueHandlers{
		inter:       inter,
		roomRepo:    roomRepo,
		auth:        a,
		broadcaster: broadcaster,
	}
}

// roomAutoQueueToggleReq is the body of POST /api/rooms/{slug}/autoqueue/toggle.
// Required field: enabled (bool). Any other shape (missing field,
// wrong type) returns 400.
type roomAutoQueueToggleReq struct {
	Enabled *bool `json:"enabled"`
}

// HandleGetRoomAutoQueueStatus: GET /api/rooms/{slug}/autoqueue/status
// — returns {"enabled": bool, "strategy": "related"} for the room.
// Any active member may read (roomAuth-resolved membership check
// happens at the use-case layer via the room repository).
//
// Status mapping:
//   - 200 OK with the config
//   - 401 Unauthorized on missing actor (defense-in-depth)
//   - 403 Forbidden for non-members
//   - 404 Not Found when the room does not exist
//   - 409 Conflict when the room is archived
func (h *RoomAutoQueueHandlers) HandleGetRoomAutoQueueStatus(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.requireMember(r.Context(), slug, actorUserID); err != nil {
		writeRoomQueueError(w, err)
		return
	}
	cfg, err := h.inter.GetConfig(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	// The status payload intentionally mirrors the global
	// /api/autoqueue/status shape (enabled + strategy) so clients
	// only need one rendering code path for both surfaces.
	writeJSON(w, http.StatusOK, &roomAutoQueueStatusPayload{
		Enabled:  cfg.Enabled,
		Strategy: string(cfg.Strategy),
	})
}

// HandleRoomAutoQueueToggle: POST /api/rooms/{slug}/autoqueue/toggle
// — host/admin only. Body: {"enabled": bool}. Missing field returns
// 400; only true/false is accepted. Returns 200 with the new config
// on success and broadcasts room_auto_queue_config_changed.
//
// Status mapping:
//   - 200 OK with the new config on success
//   - 400 Bad Request on missing/invalid body shape
//   - 401 Unauthorized on missing actor
//   - 403 Forbidden for guests / non-privileged members
//   - 404 Not Found when the room does not exist
//   - 409 Conflict when the room is archived
func (h *RoomAutoQueueHandlers) HandleRoomAutoQueueToggle(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roomAutoQueueToggleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// R09f: missing field returns 400 (caller-supplied toggles MUST
	// specify the new value).
	if req.Enabled == nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Role gate: only host/admin may toggle. The handler-layer role
	// check is defensive; the use case rejects non-host/admin in
	// SetEnabled via the persisted role row.
	if actorUserID > 0 {
		if err := h.requireHostOrAdmin(r.Context(), slug, actorUserID); err != nil {
			writeRoomQueueError(w, err)
			return
		}
	}

	cfg, err := h.inter.SetEnabled(r.Context(), slug, actorUserID, *req.Enabled)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}

	// Fan out the per-room config change on /ws/rooms/{slug}. The
	// global /ws 16-event inventory is unchanged.
	if h.broadcaster != nil {
		h.broadcaster.BroadcastRoomAutoQueueConfigChanged(slug, cfg.Enabled, string(cfg.Strategy))
	}

	writeJSON(w, http.StatusOK, &roomAutoQueueStatusPayload{
		Enabled:  cfg.Enabled,
		Strategy: string(cfg.Strategy),
	})
}

// requireMember rejects non-members. Mirrors the roomqueue.Interactor.
// requireMember check; the handler layer preserves the existing 403
// shape so non-roommembers never observe the per-room config.
func (h *RoomAutoQueueHandlers) requireMember(ctx context.Context, slug string, actorUserID int) error {
	if !entity.IsValidSlug(slug) {
		return room.ErrInvalidSlug
	}
	roomObj, err := h.roomRepo.GetRoomBySlug(ctx, slug)
	if err != nil {
		return room.ErrRoomNotFound
	}
	if roomObj.Status != entity.RoomStatusActive {
		return room.ErrArchived
	}
	if _, err := h.roomRepo.GetMember(ctx, roomObj.ID, actorUserID); err != nil {
		return room.ErrForbidden
	}
	return nil
}

// requireHostOrAdmin rejects non-host/admin members. Reads the
// actor's room-member role via the same helper the queue handlers
// use to keep the role check aligned with roomqueue's membership
// model. Guests get ErrForbidden; missing members also get
// ErrForbidden so the controller can't distinguish the two via
// status code.
func (h *RoomAutoQueueHandlers) requireHostOrAdmin(ctx context.Context, slug string, actorUserID int) error {
	if !entity.IsValidSlug(slug) {
		return room.ErrInvalidSlug
	}
	roomObj, err := h.roomRepo.GetRoomBySlug(ctx, slug)
	if err != nil {
		return room.ErrRoomNotFound
	}
	if roomObj.Status != entity.RoomStatusActive {
		return room.ErrArchived
	}
	member, err := h.roomRepo.GetMember(ctx, roomObj.ID, actorUserID)
	if err != nil {
		return room.ErrForbidden
	}
	if member.Role != entity.RoomRoleHost && member.Role != entity.RoomRoleAdmin {
		return room.ErrForbidden
	}
	return nil
}

// roomAutoQueueStatusPayload is the response shape for both the
// status and toggle handlers. Mirrors the global
// /api/autoqueue/status payload so clients have one rendering
// code path. Strategy is serialized as the raw string ("related"
// today; future strategies add new values).
type roomAutoQueueStatusPayload struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
}

// Compile-time guard: ensure the package-level sentinels referenced
// from roomqueue are reachable (so future drift in the error
// surfaces lights up at compile time).
var (
	_ = room.ErrInvalidSlug
	_ = room.ErrRoomNotFound
	_ = room.ErrArchived
	_ = room.ErrForbidden
	_ = errors.New
	_ = fmt.Errorf
	_ = domain.RoomAutoQueueConfig{}
	_ = roomautoqueue.ErrAutoQueueStale
)
