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
	lease *room.PlayerLeaseInteractor
	// archiveBroadcaster is implemented by *ws.Hub (via a thin adapter in
	// main.go) so an explicit POST /player/release can publish a
	// room_archived event without usecase/room importing delivery/ws.
	// nil is tolerated — the handler skips the broadcast rather than
	// failing the request.
	archiveBroadcaster room.RoomArchivedBroadcaster
	// memberBroadcaster is implemented by *ws.RoomWSHub via a thin
	// adapter in main.go. It fans out the targeted room_member_removed
	// envelope to the removed client BEFORE the hub closes the
	// connection, and the room_members_changed envelope to remaining
	// clients. nil is tolerated — the handler skips the broadcasts
	// rather than failing the request.
	memberBroadcaster room.RoomMembersBroadcaster
}

// NewRoomHandlers constructs RoomHandlers with an interactor and the auth
// interactor (for session resolution).
func NewRoomHandlers(inter *room.Interactor, lease *room.PlayerLeaseInteractor, a *auth.Interactor) *RoomHandlers {
	return &RoomHandlers{inter: inter, auth: a, lease: lease}
}

// SetArchiveBroadcaster wires the broadcaster used to fan out room_archived
// events on explicit release. Optional — when unset the handler still
// succeeds; it just doesn't broadcast.
func (h *RoomHandlers) SetArchiveBroadcaster(b room.RoomArchivedBroadcaster) {
	h.archiveBroadcaster = b
}

// SetMemberBroadcaster wires the broadcaster used to fan out the
// targeted room_member_removed + room_members_changed envelopes on
// a successful member removal. Optional — when unset the handler
// still succeeds; it just doesn't broadcast.
func (h *RoomHandlers) SetMemberBroadcaster(b room.RoomMembersBroadcaster) {
	h.memberBroadcaster = b
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
// list members per ADR 001. Active-membership gate is enforced by the interactor.
//
// R10c: response shape changed from a bare []entity.RoomMember to a
// {"members": [{user_id, role}]} wrapper that matches the R10b
// room_members_changed envelope. joined_at is intentionally OMITTED
// from the wire (per R10a Decision 9 — the entity has it; the wire
// does not). The active room is required (archived rooms return 409
// via the interactor's ErrArchived mapping). No mutation, no broadcast,
// no client-supplied identity fields.
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
	out := make([]roomMemberWire, 0, len(members))
	for _, m := range members {
		out = append(out, roomMemberWire{UserID: m.UserID, Role: string(m.Role)})
	}
	writeJSON(w, http.StatusOK, roomMembersListResp{Members: out})
}

// roomMemberWire is the per-member entry shape returned by
// HandleListMembers and used by the room_members_changed envelope.
// Matches the R10b ws.RoomMemberInfo field tags exactly.
type roomMemberWire struct {
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
}

// roomMembersListResp is the response body for GET /api/rooms/{slug}/members.
// The wrapper mirrors the room_members_changed data payload so the
// frontend can route both through the same applyRoomMembersChanged
// store mutator without an extra shape adapter.
type roomMembersListResp struct {
	Members []roomMemberWire `json:"members"`
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

// HandleClaimPlayer: POST /api/rooms/{slug}/player/claim — host claims the
// active player lease for the room. Returns 409 on duplicate valid claim,
// 403 on non-host caller.
func (h *RoomHandlers) HandleClaimPlayer(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	lease, err := h.lease.Claim(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

// HandleHeartbeatPlayer: POST /api/rooms/{slug}/player/heartbeat — current
// lease holder renews the lease. Returns 403 for non-holder, 410 past grace.
func (h *RoomHandlers) HandleHeartbeatPlayer(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	lease, err := h.lease.Heartbeat(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

// HandleReleasePlayer: POST /api/rooms/{slug}/player/release — host ends
// the lease and archives the room. Returns 204 on success, 404 if no
// active lease exists, 403 for non-host callers.
//
// When the release actually archives the room, a room_archived event is
// dispatched through the broadcaster (if wired). The usecase layer
// stays independent of delivery/ws; the broadcaster is the seam.
func (h *RoomHandlers) HandleReleasePlayer(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ev, err := h.lease.Release(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if ev != nil && h.archiveBroadcaster != nil {
		h.archiveBroadcaster.BroadcastRoomArchived(*ev)
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteRoom: DELETE /api/rooms/{slug} — host-requested soft
// archive. Idempotent on already-archived rooms (204, no mutation,
// no broadcast). 404 on missing room. 400 on invalid slug. 403 on
// non-host actor. Body is ignored.
//
// When this call actually transitions active → archived, a
// room_archived envelope is dispatched through the per-room hub
// with reason "host_archived". The existing global /ws archive
// broadcast (R06) is intentionally NOT invoked from this path —
// R10b uses the per-room hub only.
//
// R10a contract: error responses for invalid slug use a JSON body
// `{"error": "invalid room slug"}`. Successes return 204 (empty).
func (h *RoomHandlers) HandleDeleteRoom(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !entity.IsValidSlug(slug) {
		writeRoomJSONError(w, "invalid room slug", http.StatusBadRequest)
		return
	}
	transitioned, err := h.inter.ArchiveRoomByHost(r.Context(), slug, actorUserID)
	if err != nil {
		writeDeleteRoomError(w, err)
		return
	}
	if transitioned && h.memberBroadcaster != nil {
		// memberBroadcaster is the per-room hub adapter; its
		// BroadcastRoomArchived emits the per-room room_archived
		// envelope. We reuse the same seam so the production wiring
		// does not need to know about two distinct broadcaster
		// interfaces.
		h.memberBroadcaster.BroadcastRoomArchived(slug, "host_archived")
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteMember: DELETE /api/rooms/{slug}/members/{userId} —
// host-driven member removal. 400 on host-removes-self
// (ErrHostCannotRemoveSelf), 400 on remove-host (ErrCannotRemoveHost),
// 400 on invalid userId (handled in main.go via strconv.Atoi — the
// handler itself trusts the parsed int). 404 on target not member.
// 409 on archived room. 403 on non-host actor.
//
// On success the handler does NOT directly close the removed user's
// per-room WS connections — the interactor's broadcaster seam fans
// out the targeted room_member_removed envelope AND closes the
// removed client's connections with code 1008, in that order. The
// close-frame ordering is enforced inside usecase/room so the HTTP
// handler stays transport-only.
//
// R10a contract: error responses for invalid slug, invalid user id,
// host-removes-self, remove-host, and archived room all use JSON
// bodies of the shape `{"error": "<stable message>"}`. Successes
// return 204 (empty).
//
// body is ignored — the contract specifies no request body.
func (h *RoomHandlers) HandleDeleteMember(w http.ResponseWriter, r *http.Request, slug string, actorUserID int, targetUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !entity.IsValidSlug(slug) {
		writeRoomJSONError(w, "invalid room slug", http.StatusBadRequest)
		return
	}
	if _, err := h.inter.RemoveMemberByHost(r.Context(), slug, actorUserID, targetUserID); err != nil {
		writeDeleteMemberError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetPlayerLease: GET /api/rooms/{slug}/player/lease — any active
// member may read the current lease.
func (h *RoomHandlers) HandleGetPlayerLease(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	lease, err := h.lease.GetLease(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if lease == nil {
		http.Error(w, "no lease", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, lease)
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
	case errors.Is(err, room.ErrPlayerLeaseExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrPlayerLeaseGone):
		http.Error(w, err.Error(), http.StatusGone)
	case errors.Is(err, room.ErrPlayerLeaseNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, room.ErrNotLeaseHolder),
		errors.Is(err, room.ErrPlayerLeaseForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, room.ErrRoomNotFound),
		errors.Is(err, room.ErrInviteNotFound),
		errors.Is(err, room.ErrInviteInvalid),
		errors.Is(err, room.ErrMemberNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, room.ErrHostCannotRemoveSelf):
		http.Error(w, "host cannot remove self", http.StatusBadRequest)
	case errors.Is(err, room.ErrCannotRemoveHost):
		http.Error(w, "cannot remove host", http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeRoomJSONError writes a JSON error body `{"error": "<msg>"}`
// with the given status. Used by the R10b DELETE endpoints to
// conform to the R10a contract's documented error-body shape.
func writeRoomJSONError(w http.ResponseWriter, msg string, status int) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeDeleteRoomError maps use-case sentinel errors to JSON error
// bodies per the R10a contract for DELETE /api/rooms/{slug}.
func writeDeleteRoomError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrInvalidSlug):
		writeRoomJSONError(w, "invalid room slug", http.StatusBadRequest)
	case errors.Is(err, room.ErrRoomNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrForbidden),
		errors.Is(err, room.ErrPlayerLeaseForbidden),
		errors.Is(err, room.ErrNotLeaseHolder):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeDeleteMemberError maps use-case sentinel errors to JSON error
// bodies per the R10a contract for DELETE /api/rooms/{slug}/members/{userId}.
func writeDeleteMemberError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrInvalidSlug):
		writeRoomJSONError(w, "invalid room slug", http.StatusBadRequest)
	case errors.Is(err, room.ErrHostCannotRemoveSelf):
		writeRoomJSONError(w, "host cannot remove self", http.StatusBadRequest)
	case errors.Is(err, room.ErrCannotRemoveHost):
		writeRoomJSONError(w, "cannot remove host", http.StatusBadRequest)
	case errors.Is(err, room.ErrArchived):
		writeRoomJSONError(w, "room archived", http.StatusConflict)
	case errors.Is(err, room.ErrMemberNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrRoomNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, room.ErrForbidden),
		errors.Is(err, room.ErrPlayerLeaseForbidden),
		errors.Is(err, room.ErrNotLeaseHolder):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}