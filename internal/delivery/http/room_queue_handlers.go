package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomqueue"
)

// RoomQueueHandlers wires the room-scoped queue REST endpoints. Actor
// identity is supplied by the routing wrapper (main.go's roomAuth)
// which already resolved the bearer token. R07a intentionally does NOT
// broadcast WebSocket events from these handlers; per-room WS deltas
// are scoped to R08.
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
	URL      string                `json:"url"`
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
// The handler stamps Metadata.AddedBy / AddedByID from the bearer-resolved
// actor so the interactor (which currently discards its own actorUserID
// for song ownership) inherits correct attribution. The display name is
// not in scope for R07a — we stamp "user-<id>" as a placeholder.
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
	// NOTE: stamping AddedBy / AddedByID on the metadata is deferred —
	// entity.SearchResult does not carry those fields, only entity.Song
	// does. Actor identity still flows via the actorUserID argument to
	// AddSong. Logging the carry so future R07 work can attach the
	// display name lookup here without breaking the API surface.
	_ = actorUserID
	if req.Metadata == nil {
		// Slow path (yt-dlp fetch) is not exercised in R07a; the
		// interactor's metadata-supplied path is the supported one.
	}
	_, song, err := h.inter.AddSong(r.Context(), slug, actorUserID, req.URL, req.Metadata)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, song)
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
	if _, err := h.inter.RemoveSong(r.Context(), slug, actorUserID, role, req.Index); err != nil {
		writeRoomQueueError(w, err)
		return
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
	if _, err := h.inter.ClearQueue(r.Context(), slug, actorUserID, role); err != nil {
		writeRoomQueueError(w, err)
		return
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
