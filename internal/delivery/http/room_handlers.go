package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/room"
)

// RoomHandlers wires the room REST endpoints. Actor identity is supplied by
// the routing wrapper (main.go) which resolves the bearer token via
// auth.Interactor.ResolveSession. A 0 actor means no session, and the handler
// returns 401 for endpoints that require an actor.
//
// Per ADR 001 §6, the URL-safe slug is the external room identifier. Numeric
// DB ids are internal only and never exposed in routes.
//
// All handlers map interactor sentinel errors to documented status codes;
// no handler reads role strings from request bodies.
type RoomHandlers struct {
	inter *room.Interactor
	auth  *auth.Interactor
}

// NewRoomHandlers constructs RoomHandlers with an interactor and the auth
// interactor (for session resolution).
func NewRoomHandlers(inter *room.Interactor, a *auth.Interactor) *RoomHandlers {
	return &RoomHandlers{inter: inter, auth: a}
}

// --- Request/response shapes ---

type createRoomReq struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type inviteReq struct {
	MaxUses   int       `json:"max_uses"`
	ExpiresAt time.Time `json:"expires_at"`
}

type inviteResp struct {
	ID        int64     `json:"id"`
	RoomID    int64     `json:"room_id"`
	Token     string    `json:"token,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	MaxUses   int       `json:"max_uses"`
	UseCount  int       `json:"use_count"`
}

// --- Handlers ---

// HandleCreateRoom: POST /api/rooms — creates a room and seeds the caller
// as the host member.
func (h *RoomHandlers) HandleCreateRoom(w http.ResponseWriter, r *http.Request, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req createRoomReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Slug == "" || req.Name == "" {
		http.Error(w, "slug and name are required", http.StatusBadRequest)
		return
	}
	roomObj, err := h.inter.CreateRoom(r.Context(), req.Slug, req.Name, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, roomObj)
}

// HandleListRooms: GET /api/rooms?status=active — returns rooms filtered
// by status (no filter returns all).
func (h *RoomHandlers) HandleListRooms(w http.ResponseWriter, r *http.Request, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	status := entity.RoomStatus(r.URL.Query().Get("status"))
	if status != "" && !status.IsValid() {
		http.Error(w, "invalid status filter", http.StatusBadRequest)
		return
	}
	rooms, err := h.inter.ListRooms(r.Context(), status)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if rooms == nil {
		rooms = []entity.Room{}
	}
	writeJSON(w, http.StatusOK, rooms)
}

// HandleGetRoom: GET /api/rooms/{slug} — returns a single room by slug.
func (h *RoomHandlers) HandleGetRoom(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !entity.IsValidSlug(slug) {
		http.Error(w, "invalid room slug", http.StatusBadRequest)
		return
	}
	roomObj, err := h.inter.GetRoomBySlug(r.Context(), slug)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roomObj)
}

// HandleListMembers: GET /api/rooms/{slug}/members — any active member may
// list members per ADR 001. Host/admin gate is enforced by the interactor.
func (h *RoomHandlers) HandleListMembers(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	members, err := h.inter.ListMembers(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if members == nil {
		members = []entity.RoomMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

// HandlePromoteMember: POST /api/rooms/{slug}/members/{userId}/promote —
// host promotes a guest to admin. The new role is derived from the route
// suffix ("promote" / "demote") and never trusted from the request body.
func (h *RoomHandlers) HandlePromoteMember(w http.ResponseWriter, r *http.Request, slug string, actorUserID int, targetUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.inter.PromoteMember(r.Context(), slug, actorUserID, targetUserID, "admin"); err != nil {
		writeRoomError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleDemoteMember: POST /api/rooms/{slug}/members/{userId}/demote —
// host demotes an admin to guest.
func (h *RoomHandlers) HandleDemoteMember(w http.ResponseWriter, r *http.Request, slug string, actorUserID int, targetUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.inter.DemoteMember(r.Context(), slug, actorUserID, targetUserID, "guest"); err != nil {
		writeRoomError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleCreateInvite: POST /api/rooms/{slug}/invites — host/admin mints a
// new invite. Role check is enforced by the interactor.
func (h *RoomHandlers) HandleCreateInvite(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req inviteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		// io.EOF = empty body; defaults will be applied.
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = time.Time{}
	}
	plaintext, inv, err := h.inter.CreateInvite(r.Context(), slug, actorUserID, req.MaxUses, req.ExpiresAt)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inviteResp{
		ID:        inv.ID,
		RoomID:    inv.RoomID,
		Token:     plaintext,
		CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt,
		MaxUses:   inv.MaxUses,
		UseCount:  inv.UseCount,
	})
}

// HandleListInvites: GET /api/rooms/{slug}/invites — host/admin listing
// of invites for a room. The token hash is never serialized.
func (h *RoomHandlers) HandleListInvites(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	invites, err := h.inter.ListInvites(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if invites == nil {
		invites = []entity.RoomInvite{}
	}
	type pubInvite struct {
		ID        int64      `json:"id"`
		RoomID    int64      `json:"room_id"`
		CreatedAt time.Time  `json:"created_at"`
		ExpiresAt time.Time  `json:"expires_at"`
		RevokedAt *time.Time `json:"revoked_at,omitempty"`
		MaxUses   int        `json:"max_uses"`
		UseCount  int        `json:"use_count"`
	}
	out := make([]pubInvite, 0, len(invites))
	for _, inv := range invites {
		out = append(out, pubInvite{
			ID:        inv.ID,
			RoomID:    inv.RoomID,
			CreatedAt: inv.CreatedAt,
			ExpiresAt: inv.ExpiresAt,
			RevokedAt: inv.RevokedAt,
			MaxUses:   inv.MaxUses,
			UseCount:  inv.UseCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// HandleRevokeInvite: POST /api/rooms/{slug}/invites/{inviteId}/revoke —
// host/admin revokes an invite.
func (h *RoomHandlers) HandleRevokeInvite(w http.ResponseWriter, r *http.Request, slug string, inviteID int64, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := h.inter.RevokeInvite(r.Context(), slug, actorUserID, inviteID); err != nil {
		writeRoomError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleRedeemInvite: POST /api/invites/{token}/redeem — actor redeems
// an invite token, joining the room as a guest.
func (h *RoomHandlers) HandleRedeemInvite(w http.ResponseWriter, r *http.Request, token string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	member, err := h.inter.RedeemInvite(r.Context(), token, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

// writeRoomError maps use-case sentinel errors to the documented status codes.
func writeRoomError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrInvalidSlug), errors.Is(err, room.ErrReservedSlug):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, room.ErrDuplicateSlug):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrArchived):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrInviteExhausted):
		http.Error(w, err.Error(), http.StatusGone)
	case errors.Is(err, room.ErrRoomNotFound),
		errors.Is(err, room.ErrInviteNotFound),
		errors.Is(err, room.ErrInviteInvalid),
		errors.Is(err, room.ErrMemberNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}