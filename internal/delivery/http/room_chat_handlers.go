package http

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/roomchat"
)

// RoomChatHandlers wires the R11a room chat REST endpoints. Actor
// identity is supplied by the routing wrapper (main.go) which
// resolves the bearer token via auth.Interactor.ResolveSession. A 0
// actor means no session, and the handler returns 401 for endpoints
// that require an actor.
//
// Per ADR 001 §6, the URL-safe slug is the external room identifier.
// Numeric DB ids are internal only and never exposed in routes.
//
// All handlers map interactor sentinel errors to documented status
// codes (400/401/403/404/409); no handler reads identity fields from
// the request body (sender_id is always derived from the bearer
// token, never from the wire).
type RoomChatHandlers struct {
	inter *roomchat.Interactor
}

// NewRoomChatHandlers constructs RoomChatHandlers with the chat
// interactor.
func NewRoomChatHandlers(inter *roomchat.Interactor) *RoomChatHandlers {
	return &RoomChatHandlers{inter: inter}
}

// --- Request/response shapes ---

// chatPostReq is the POST body. `content` is the only allowed
// field; anything else is ignored. The handler trims/normalizes the
// value via entity.NormalizeChatContent before persistence.
type chatPostReq struct {
	Content string `json:"content"`
}

// chatMessageWire is the R11a per-message wire shape used by both
// the GET response array entries and the POST 201 body. The wire
// intentionally OMITS the user email (per the R11a privacy stance in
// 016-room-chat-feature.md) and never carries the internal numeric
// room_id (the slug is the external identifier).
type chatMessageWire struct {
	ID        int64                  `json:"id"`
	RoomSlug  string                 `json:"room_slug"`
	Sender    chatMessageSenderWire  `json:"sender"`
	Content   string                 `json:"content"`
	CreatedAt time.Time              `json:"created_at"`
}

// chatMessageSenderWire is the nested sender identity. `display_name`
// is the resolved user.DisplayName with a "user #<id>" fallback when
// the user row has no display name (per the R11a fallback rule).
type chatMessageSenderWire struct {
	UserID      int    `json:"user_id"`
	DisplayName string `json:"display_name"`
}

// chatListResp is the response body for GET /api/rooms/{slug}/chat/messages.
type chatListResp struct {
	Messages []chatMessageWire `json:"messages"`
}

// chatPostResp is the response body for POST /api/rooms/{slug}/chat/messages
// (201 Created). The post-mutation envelope is wrapped under `message` so
// the wire shape mirrors the room_chat_message_created WebSocket data
// payload (`{ message: ... }`) and the frontend can apply both the REST
// response and the WS event through the same store merge path. Without
// the wrapper the POST body is indistinguishable from a single list
// entry, which breaks the symmetry that the R11a frontend relies on.
type chatPostResp struct {
	Message chatMessageWire `json:"message"`
}

// --- Handlers ---

// HandleListChatMessages: GET /api/rooms/{slug}/chat/messages?limit=50
//
// Any active member of an active room may list recent messages.
// limit defaults to roomchat.DefaultChatHistoryLimit (50); values
// outside 1..roomchat.MaxChatHistoryLimit (100) map to 400. Returns
// 200 with `{"messages": [...]}` ordered oldest → newest. The wire
// never exposes the sender email.
//
// 400 on invalid slug / invalid limit
// 401 on missing session
// 403 on non-member caller
// 404 on unknown slug
// 409 on archived room
func (h *RoomChatHandlers) HandleListChatMessages(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	limit := roomchat.DefaultChatHistoryLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	rows, err := h.inter.ListRecent(r.Context(), slug, actorUserID, limit)
	if err != nil {
		writeChatError(w, err)
		return
	}
	out := make([]chatMessageWire, 0, len(rows))
	for _, row := range rows {
		out = append(out, chatMessageWire{
			ID:        row.Message.ID,
			RoomSlug:  slug,
			Sender:    chatMessageSenderWire{UserID: row.Message.SenderID, DisplayName: row.DisplayName},
			Content:   row.Message.Content,
			CreatedAt: row.Message.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, chatListResp{Messages: out})
}

// HandlePostChatMessage: POST /api/rooms/{slug}/chat/messages
//
// Body: {"content": "<plain text>"}. Any active member of an active
// room may post. On success returns 201 with the post-mutation
// envelope (same shape as a single list entry). Sender identity is
// resolved from the bearer token; the request body's sender_id (if
// any) is intentionally ignored.
//
// 400 on invalid slug / empty-after-trim content / content > 500 chars
// 401 on missing session
// 403 on non-member caller
// 404 on unknown slug
// 409 on archived room
func (h *RoomChatHandlers) HandlePostChatMessage(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !entity.IsValidSlug(slug) {
		http.Error(w, "invalid room slug", http.StatusBadRequest)
		return
	}
	var req chatPostReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	posted, err := h.inter.Send(r.Context(), slug, actorUserID, req.Content)
	if err != nil {
		writeChatError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, chatPostResp{Message: chatMessageWire{
		ID:        posted.Message.ID,
		RoomSlug:  slug,
		Sender:    chatMessageSenderWire{UserID: posted.Message.SenderID, DisplayName: posted.DisplayName},
		Content:   posted.Message.Content,
		CreatedAt: posted.Message.CreatedAt,
	}})
}

// writeChatError maps use-case sentinel errors to the documented
// status codes for the R11a chat endpoints.
//
//	400 — ErrInvalidSlug, ErrInvalidLimit, ErrEmptyContent, ErrContentTooLong
//	401 — handled inline before the interactor call
//	403 — ErrForbidden
//	404 — ErrRoomNotFound (slug not found)
//	409 — ErrArchived
//	500 — ErrSenderNotFound, default
//
// ErrSenderNotFound is a 500 because it indicates the session is
// valid but the underlying users row is missing — a server-side
// invariant violation, not a client error.
//
// Unexpected errors (the default branch) MUST NOT leak their
// err.Error() to the wire: a wrapped repository error may include
// a slug, an internal table name, or a connection string fragment
// that is useful to an attacker. The default branch returns a
// generic "internal server error" body and logs the detailed error
// server-side. The log line intentionally does NOT include any
// request body, header, cookie, or bearer token — only the error
// itself, the route slug, and the actor id (when available).
func writeChatError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, roomchat.ErrInvalidSlug):
		http.Error(w, "invalid room slug", http.StatusBadRequest)
	case errors.Is(err, roomchat.ErrInvalidLimit):
		http.Error(w, "invalid limit", http.StatusBadRequest)
	case errors.Is(err, roomchat.ErrEmptyContent):
		http.Error(w, "empty content", http.StatusBadRequest)
	case errors.Is(err, roomchat.ErrContentTooLong):
		http.Error(w, "content too long", http.StatusBadRequest)
	case errors.Is(err, roomchat.ErrRoomNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, roomchat.ErrArchived):
		http.Error(w, "room archived", http.StatusConflict)
	case errors.Is(err, roomchat.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, roomchat.ErrSenderNotFound):
		http.Error(w, "sender not found", http.StatusInternalServerError)
	default:
		// Log the full error server-side (no PII — see function
		// comment) and return a generic body to the wire. The slug
		// argument is already a path value the client supplied;
		// including it in the log is safe and useful for tracing.
		log.Printf("roomchat internal error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}