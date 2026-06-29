package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomqueue"
)

// RoomQueueHandlers wires the room-scoped queue REST endpoints. Actor
// identity is supplied by the routing wrapper (main.go's roomAuth)
// which already resolved the bearer token. R07b wires these handlers to
// the per-room WebSocket hub (via the interactor's Broadcaster seam) so
// successful add/remove/clear mutations fan out room_queue_song_added,
// room_queue_song_removed, and room_queue_cleared to /ws/rooms/{slug}
// subscribers.
type RoomQueueHandlers struct {
	inter *roomqueue.Interactor
	auth  *auth.Interactor
}

// NewRoomQueueHandlers constructs the handlers. auth is unused here
// because the actor user id is injected by the routing wrapper; the
// parameter is kept for symmetry with RoomHandlers and to make future
// token re-resolution straightforward without a constructor change.
func NewRoomQueueHandlers(inter *roomqueue.Interactor, a *auth.Interactor) *RoomQueueHandlers {
	return &RoomQueueHandlers{inter: inter, auth: a}
}

// --- Request/response shapes ---

type roomQueueAddReq struct {
	URL      string               `json:"url"`
	Metadata *entity.SearchResult `json:"metadata,omitempty"`
}

type roomQueueRemoveReq struct {
	Index int `json:"index"`
}

// --- Handlers ---

// HandleGetRoomQueue: GET /api/rooms/{slug}/queue — returns the current
// room queue state. Any active member may read.
func (h *RoomQueueHandlers) HandleGetRoomQueue(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	state, err := h.inter.GetState(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// HandleAddRoomSong: POST /api/rooms/{slug}/queue/add — server-resolves
// identity from the bearer token; request-body identity fields are
// ignored.
//
// The handler resolves the actor's display name via the existing
// auth/UserRepository path used by the rest of the app and forwards it
// to the interactor. The interactor stamps the constructed Song's
// AddedBy / AddedByID for both the metadata-supplied and the bare-URL
// branches so the persisted queue always records the resolved actor.
func (h *RoomQueueHandlers) HandleAddRoomSong(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roomQueueAddReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Resolve the actor's display name through the existing
	// auth/UserRepository path. Body-supplied display_name /
	// added_by / user_id are ignored.
	displayName, err := h.resolveActorDisplayName(r.Context(), actorUserID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	queue, song, err := h.inter.AddSong(r.Context(), slug, actorUserID, displayName, req.URL, req.Metadata)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		pos := 0
		if queue != nil {
			pos = len(queue.Songs) - 1
		}
		bc.BroadcastRoomQueueSongAdded(slug, *song, pos, queue)
	}
	writeJSON(w, http.StatusOK, song)
}

// resolveActorDisplayName loads the actor's display name through the
// auth interactor. Falls back to a generic placeholder if the user row
// is missing a display name; the actorUserID itself is the
// authoritative attribution key.
func (h *RoomQueueHandlers) resolveActorDisplayName(ctx context.Context, actorUserID int) (string, error) {
	user, err := h.auth.GetUserByID(ctx, actorUserID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", auth.ErrInvalidToken
	}
	if user.DisplayName != "" {
		return user.DisplayName, nil
	}
	if user.Email != "" {
		return user.Email, nil
	}
	return fmt.Sprintf("user-%d", actorUserID), nil
}

// HandleRemoveRoomSong: POST /api/rooms/{slug}/queue/remove — host/admin
// may remove any song; guest may remove only their own upcoming song.
// The handler resolves the actor's room role and forwards it to the
// interactor, which enforces the actual permission rules.
func (h *RoomQueueHandlers) HandleRemoveRoomSong(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roomQueueRemoveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	role, err := h.roleOf(r, slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	queue, err := h.inter.RemoveSong(r.Context(), slug, actorUserID, role, req.Index)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomQueueSongRemoved(slug, req.Index, queue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleClearRoomQueue: POST /api/rooms/{slug}/queue/clear — host/admin
// only.
func (h *RoomQueueHandlers) HandleClearRoomQueue(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, err := h.roleOf(r, slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	queue, err := h.inter.ClearQueue(r.Context(), slug, actorUserID, role)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomQueueCleared(slug, queue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// roleOf fetches the actor's role in the room for handlers that need
// it (remove / clear). It returns "" when the actor is not a member;
// the interactor translates that into ErrForbidden downstream.
func (h *RoomQueueHandlers) roleOf(r *http.Request, slug string, actorUserID int) (entity.RoomMemberRole, error) {
	return h.inter.MemberRole(r.Context(), slug, actorUserID)
}

// --- error mapping ---

// writeRoomQueueError maps use-case sentinel errors to the documented
// status codes. Mirrors the surface used by the global queue handlers
// so clients see consistent error semantics across the migration.
func writeRoomQueueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, roomqueue.ErrInvalidIndex):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, roomqueue.ErrNotSongOwner),
		errors.Is(err, roomqueue.ErrCannotRemoveSong):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, entity.ErrSongAlreadyInQueue):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrInvalidSlug):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, room.ErrRoomNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrArchived):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
