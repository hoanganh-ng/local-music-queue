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

// roomQueuePrioritizeReq is the body of POST /api/rooms/{slug}/queue/prioritize.
// R07d: server-resolves identity from the bearer token; identity fields
// (user_id, requested_by, added_by, etc.) are not honored when present.
//
// SongIndex is a pointer so the handler can distinguish "field omitted"
// (nil) from "field present with zero" (e.g. {"song_index":0}). The
// raw int form silently coerced missing fields to 0, which used to be
// treated as a valid index and mutate the first song.
type roomQueuePrioritizeReq struct {
	SongIndex *int `json:"song_index"`
}

// roomQueuePlaybackStatusReq is the body of POST
// /api/rooms/{slug}/playback/status. R09a: Status must be "playing" or
// "paused"; "idle" is rejected at the handler layer with 400 so the
// client cannot drive the queue into the idle state.
type roomQueuePlaybackStatusReq struct {
	Status string `json:"status"`
}

// roomQueuePlaybackElapsedReq is the body of POST
// /api/rooms/{slug}/playback/sync. Elapsed is *int so the handler can
// distinguish "field omitted" (nil) from "field present with zero"
// (e.g. {"elapsed":0}). The raw int form silently coerced missing
// fields to 0, which used to be treated as a valid elapsed value.
type roomQueuePlaybackElapsedReq struct {
	Elapsed *int `json:"elapsed"`
}

// roomQueuePlaybackVolumeReq is the body of POST
// /api/rooms/{slug}/playback/volume. R09c: Direction must be exactly
// "up" or "down"; any other value (including empty/missing) returns
// 400 at the handler layer.
type roomQueuePlaybackVolumeReq struct {
	Direction string `json:"direction"`
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

// HandlePrioritizeRoomSong: POST /api/rooms/{slug}/queue/prioritize —
// host/admin only. Moves a non-current song to the slot immediately
// after the currently-playing song. Reuses entity.Queue.Prioritize.
//
// Identity is server-resolved from the bearer token by roomAuth; the
// body MUST carry only song_index. Body-supplied identity fields
// (user_id, requested_by, etc.) are ignored if present.
//
// Status mapping:
//   - 204 No Content on success
//   - 400 Bad Request on malformed JSON, missing/invalid song_index, or
//     attempting to prioritize the current song
//   - 401 Unauthorized when the actor is not authenticated
//     (defense-in-depth: roomAuth already rejects 401, but the handler
//     double-checks actorUserID before touching the interactor)
//   - 403 Forbidden for guests / non-privileged members
//   - 404 Not Found when the room does not exist
//   - 409 Conflict for archived rooms
func (h *RoomQueueHandlers) HandlePrioritizeRoomSong(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roomQueuePrioritizeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// R07d: SongIndex is *int so we can reject a missing field
	// (body {} → SongIndex == nil) as 400. The pre-fix int form silently
	// defaulted missing to 0, which mutated song at index 0 — see
	// TestRoomQueue_PrioritizeSong_MissingBodyReturns400.
	if req.SongIndex == nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if *req.SongIndex < 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	role, err := h.roleOf(r, slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	queue, fromIndex, toIndex, song, err := h.inter.PrioritizeSong(r.Context(), slug, actorUserID, role, *req.SongIndex)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomQueueSongPrioritized(slug, fromIndex, toIndex, song, queue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// roleOf fetches the actor's role in the room for handlers that need
// it (remove / clear). It returns "" when the actor is not a member;
// the interactor translates that into ErrForbidden downstream.
func (h *RoomQueueHandlers) roleOf(r *http.Request, slug string, actorUserID int) (entity.RoomMemberRole, error) {
	return h.inter.MemberRole(r.Context(), slug, actorUserID)
}

// --- R09a playback handlers ---

// HandleSetRoomPlaybackStatus: POST /api/rooms/{slug}/playback/status
// — lease-holder only. Body: {"status":"playing"} or
// {"status":"paused"}. Rejects "idle" with 400. Returns 204 on success
// and broadcasts room_playback_status_changed.
func (h *RoomQueueHandlers) HandleSetRoomPlaybackStatus(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roomQueuePlaybackStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var status entity.PlaybackStatus
	switch req.Status {
	case "playing":
		status = entity.StatusPlaying
	case "paused":
		status = entity.StatusPaused
	case "idle", "":
		// Clients cannot drive the queue into idle; that is reserved
		// for natural end-of-queue paths (PlayBackEnded).
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	default:
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	queue, err := h.inter.SetPlaybackStatus(r.Context(), slug, actorUserID, status)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomPlaybackStatusChanged(slug, queue.Status, queue.Elapsed, queue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleSyncRoomPlayback: POST /api/rooms/{slug}/playback/sync —
// lease-holder only. Body: {"elapsed": <non-negative int>}. A missing
// field (nil) is rejected with 400 so the wire shape distinguishes
// missing from zero. Returns 204 on success and broadcasts
// room_playback_elapsed_sync.
func (h *RoomQueueHandlers) HandleSyncRoomPlayback(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roomQueuePlaybackElapsedReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Elapsed == nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if *req.Elapsed < 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	queue, err := h.inter.SyncPlaybackElapsed(r.Context(), slug, actorUserID, *req.Elapsed)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomPlaybackElapsedSync(slug, queue.Elapsed, queue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleSkipRoomPlayback: POST /api/rooms/{slug}/playback/skip —
// lease-holder only. Empty body. Requires a current song and a next
// song; returns 400 when there is no next song. Returns 204 on success
// and broadcasts room_playback_song_advanced with reason="skip".
func (h *RoomQueueHandlers) HandleSkipRoomPlayback(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	queue, prevIndex, newIndex, song, err := h.inter.SkipPlayback(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomPlaybackSongAdvanced(slug, "skip", prevIndex, newIndex, song, queue.Status, queue.Elapsed, queue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleRoomSongEnded: POST /api/rooms/{slug}/playback/ended —
// lease-holder only. Empty body. If a next song exists, advance and
// broadcast room_playback_song_advanced with reason="ended". Otherwise
// pause at end-of-queue and broadcast room_playback_status_changed.
// Returns 204 on success.
func (h *RoomQueueHandlers) HandleRoomSongEnded(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	queue, prevIndex, newIndex, song, advanced, err := h.inter.PlaybackEnded(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		if advanced {
			bc.BroadcastRoomPlaybackSongAdvanced(slug, "ended", prevIndex, newIndex, song, queue.Status, queue.Elapsed, queue)
		} else {
			bc.BroadcastRoomPlaybackStatusChanged(slug, queue.Status, queue.Elapsed, queue)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleChangeRoomPlaybackVolume: POST /api/rooms/{slug}/playback/volume
// — lease-holder only. Body: {"direction":"up"} or {"direction":"down"}.
// Returns 204 on success and broadcasts room_playback_volume_changed.
// The volume state is intentionally NOT persisted: there is no
// entity.Queue.Volume field, no room_queue_state column, and the
// command does not call queueRepo.Save. The event rides
// /ws/rooms/{slug} only; the global /ws contract is unchanged.
func (h *RoomQueueHandlers) HandleChangeRoomPlaybackVolume(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roomQueuePlaybackVolumeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Direction != "up" && req.Direction != "down" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := h.inter.ChangePlaybackVolume(r.Context(), slug, actorUserID, req.Direction); err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomPlaybackVolumeChanged(slug, req.Direction)
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleChangeRoomPlaybackPrevious: POST /api/rooms/{slug}/playback/prev
// — lease-holder only. Empty body. Moves the room queue from the
// current song to the previous one, decrements CurrentIndex, resets
// Elapsed to 0, and sets Status = StatusPlaying. Requires a valid
// current song and CurrentIndex > 0; otherwise returns 400 without
// mutation, save, or broadcast. The global /ws 16-event inventory is
// unchanged; the matching room_playback_song_previous event rides
// /ws/rooms/{slug} only.
//
// Status mapping:
//   - 204 No Content on success
//   - 400 Bad Request for no current song or already-on-first
//   - 401 Unauthorized when actor is missing (defense-in-depth)
//   - 403 Forbidden when the lease is held by another user
//   - 404 Not Found when the room or active lease does not exist
//   - 409 Conflict for archived rooms
//   - 410 Gone when the lease is past grace
func (h *RoomQueueHandlers) HandleChangeRoomPlaybackPrevious(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	queue, prevIndex, newIndex, song, err := h.inter.PrevPlayback(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomQueueError(w, err)
		return
	}
	if bc := h.inter.Broadcaster(); bc != nil {
		bc.BroadcastRoomPlaybackSongPrevious(slug, prevIndex, newIndex, song, queue.Status, queue.Elapsed, queue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- error mapping ---

// writeRoomQueueError maps use-case sentinel errors to the documented
// status codes. Mirrors the surface used by the global queue handlers
// so clients see consistent error semantics across the migration.
func writeRoomQueueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, roomqueue.ErrInvalidIndex),
		errors.Is(err, roomqueue.ErrCannotPrioritizeCurrent),
		errors.Is(err, roomqueue.ErrInvalidStatus),
		errors.Is(err, roomqueue.ErrInvalidElapsed),
		errors.Is(err, roomqueue.ErrInvalidDirection),
		errors.Is(err, roomqueue.ErrNoCurrentSong),
		errors.Is(err, roomqueue.ErrNoNextSong),
		errors.Is(err, roomqueue.ErrNoPreviousSong):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, roomqueue.ErrNotSongOwner),
		errors.Is(err, roomqueue.ErrCannotRemoveSong),
		errors.Is(err, roomqueue.ErrPlaybackForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, entity.ErrSongAlreadyInQueue):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrInvalidSlug):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, room.ErrRoomNotFound),
		errors.Is(err, roomqueue.ErrPlaybackLeaseLost):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrArchived):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, roomqueue.ErrPlaybackLeaseGone):
		http.Error(w, err.Error(), http.StatusGone)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
