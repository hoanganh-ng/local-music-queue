package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/roomchat"
)

// recordingChatBroadcaster records every BroadcastRoomChatMessageCreated
// call so the WS broadcast tests can assert the no-broadcast-on-failure
// invariant from the handler boundary.
type recordingChatBroadcaster struct {
	mu    sync.Mutex
	calls []roomchatBroadcastCall
}

type roomchatBroadcastCall struct {
	Slug        string
	SenderID    int
	DisplayName string
	Content     string
}

func (b *recordingChatBroadcaster) BroadcastRoomChatMessageCreated(roomSlug string, msg *entity.RoomChatMessage, displayName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, roomchatBroadcastCall{
		Slug:        roomSlug,
		SenderID:    msg.SenderID,
		DisplayName: displayName,
		Content:     msg.Content,
	})
}

// pgChatHandlers wires the chat handlers against a per-test PG schema.
// Returns nil + skips the test when PG is unreachable.
func pgChatHandlers(t *testing.T) (*RoomChatHandlers, *sql.DB, *recordingChatBroadcaster, func()) {
	t.Helper()
	db, cleanup := persistence.NewRoomTestDB(t)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	chatRepo := persistence.NewPostgresRoomChatMessageRepository(db)
	userRepo := persistence.NewPostgresUserRepository(db)
	inter := roomchat.NewInteractor(chatRepo, roomRepo, userRepo)
	bc := &recordingChatBroadcaster{}
	inter.SetBroadcaster(bc)
	h := NewRoomChatHandlers(inter)
	return h, db, bc, cleanup
}

// seedChatFixtures creates a host user, a guest user, and a room with
// both as members. Returns (hostID, guestID, slug).
func seedChatFixtures(t *testing.T, db *sql.DB, slug string) (hostID, guestID int) {
	t.Helper()
	hostID = seedUser(t, db, "host@example.com", entity.RoleHost)
	guestID = seedUser(t, db, "guest@example.com", entity.RoleGuest)
	// GetUserByID scans profile_picture into a non-nullable Go string.
	if _, err := db.Exec(`UPDATE users SET profile_picture = '' WHERE id IN ($1, $2)`, hostID, guestID); err != nil {
		t.Fatalf("blank profile_picture: %v", err)
	}
	// Seed a room directly so we can use a known id + slug.
	if _, err := db.Exec(
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', $3, $3)`,
		slug, "Chat Room", time.Now(),
	); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	var roomID int64
	if err := db.QueryRow(`SELECT id FROM rooms WHERE slug = $1`, slug).Scan(&roomID); err != nil {
		t.Fatalf("get seeded room id: %v", err)
	}
	for _, m := range []struct {
		uid  int
		role entity.RoomMemberRole
	}{
		{hostID, entity.RoomRoleHost},
		{guestID, entity.RoomRoleGuest},
	} {
		if _, err := db.Exec(
			`INSERT INTO room_members (room_id, user_id, role, joined_at)
			 VALUES ($1, $2, $3, $4)`,
			roomID, m.uid, m.role, time.Now(),
		); err != nil {
			t.Fatalf("seed member uid=%d: %v", m.uid, err)
		}
	}
	return hostID, guestID
}

// --- POST success / error mapping ---

func TestHandlePostChatMessage_Success(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby", "Guest")

	body, _ := json.Marshal(map[string]string{"content": "  hello world  "})
	rr := postChatReq(t, h, "/api/rooms/lobby/chat/messages", body, guestID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201; body=%s", rr.Code, rr.Body.String())
	}

	// POST response is wrapped as { "message": { ... } } so the wire
	// shape mirrors the room_chat_message_created WS data payload and
	// the frontend can route both through the same store mutator.
	var wrapped chatPostResp
	if err := json.Unmarshal(rr.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	wire := wrapped.Message
	if wire.ID <= 0 {
		t.Fatalf("id=%d want positive", wire.ID)
	}
	if wire.RoomSlug != "lobby" {
		t.Fatalf("room_slug=%q want lobby", wire.RoomSlug)
	}
	if wire.Content != "hello world" {
		t.Fatalf("content=%q want trimmed %q", wire.Content, "hello world")
	}
	if wire.Sender.UserID != guestID {
		t.Fatalf("sender.user_id=%d want %d", wire.Sender.UserID, guestID)
	}
	if wire.Sender.DisplayName == "" {
		t.Fatalf("sender.display_name must not be empty")
	}
	// No email field anywhere in the wire — privacy stance.
	if strings.Contains(rr.Body.String(), "email") {
		t.Fatalf("response body must NOT expose sender email; got: %s", rr.Body.String())
	}
	// Broadcast must fire exactly once after persist.
	if len(bc.calls) != 1 {
		t.Fatalf("broadcast calls=%d want 1", len(bc.calls))
	}
	if bc.calls[0].Slug != "lobby" || bc.calls[0].Content != "hello world" {
		t.Fatalf("broadcast tuple=%+v", bc.calls[0])
	}
}

func TestHandlePostChatMessage_NoEmailOnEmptyDisplayName(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	// Use a fresh slug so we don't collide with seedChatFixturesForUser.
	slug := "no-name"
	guestID := seedChatFixturesForUser(t, db, slug, "Ghost")
	// Set the user's display_name to empty so the fallback fires.
	if _, err := db.Exec(`UPDATE users SET display_name = '' WHERE id = $1`, guestID); err != nil {
		t.Fatalf("blank display_name: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"content": "hi"})
	rr := postChatReq(t, h, "/api/rooms/"+slug+"/chat/messages", body, guestID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201; body=%s", rr.Code, rr.Body.String())
	}
	var wrapped chatPostResp
	_ = json.Unmarshal(rr.Body.Bytes(), &wrapped)
	wire := wrapped.Message
	if wire.Sender.DisplayName == "" {
		t.Fatalf("display_name must be populated (with fallback)")
	}
	if !strings.HasPrefix(wire.Sender.DisplayName, "user #") {
		t.Fatalf("display_name fallback = %q, want \"user #<id>\" prefix", wire.Sender.DisplayName)
	}
	// No email leak.
	if strings.Contains(rr.Body.String(), "@example.com") || strings.Contains(rr.Body.String(), "email") {
		t.Fatalf("response body must NOT expose sender email; got: %s", rr.Body.String())
	}
	if len(bc.calls) != 1 {
		t.Fatalf("broadcast calls=%d want 1", len(bc.calls))
	}
}

func TestHandlePostChatMessage_EmptyAfterTrim_BadRequest(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby2", "Guest")

	body, _ := json.Marshal(map[string]string{"content": "   \t  "})
	rr := postChatReq(t, h, "/api/rooms/lobby2/chat/messages", body, guestID)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rr.Code, rr.Body.String())
	}
	if len(bc.calls) != 0 {
		t.Fatalf("broadcast must NOT fire on validation failure")
	}
}

func TestHandlePostChatMessage_TooLong_BadRequest(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby3", "Guest")

	long := strings.Repeat("a", entity.MaxChatContentLen+1)
	body, _ := json.Marshal(map[string]string{"content": long})
	rr := postChatReq(t, h, "/api/rooms/lobby3/chat/messages", body, guestID)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rr.Code, rr.Body.String())
	}
	if len(bc.calls) != 0 {
		t.Fatalf("broadcast must NOT fire on too-long")
	}
}

func TestHandlePostChatMessage_AtMaxLength_Created(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-cap", "Guest")

	exact := strings.Repeat("a", entity.MaxChatContentLen)
	body, _ := json.Marshal(map[string]string{"content": exact})
	rr := postChatReq(t, h, "/api/rooms/lobby-cap/chat/messages", body, guestID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201; body=%s", rr.Code, rr.Body.String())
	}
	if len(bc.calls) != 1 {
		t.Fatalf("broadcast calls=%d want 1", len(bc.calls))
	}
}

func TestHandlePostChatMessage_InvalidSlug_BadRequest(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby", "Guest")

	body, _ := json.Marshal(map[string]string{"content": "hi"})
	rr := postChatReq(t, h, "/api/rooms/BAD%20SLUG/chat/messages", body, guestID)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandlePostChatMessage_NonMember_Forbidden(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	// Seed a room with a guest member, then send as a different user id.
	guestID := seedChatFixturesForUser(t, db, "lobby-nomember", "Guest")
	body, _ := json.Marshal(map[string]string{"content": "hi"})
	rr := postChatReq(t, h, "/api/rooms/lobby-nomember/chat/messages", body, guestID+9999)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403; body=%s", rr.Code, rr.Body.String())
	}
	if len(bc.calls) != 0 {
		t.Fatalf("broadcast must NOT fire for non-member")
	}
}

func TestHandlePostChatMessage_ArchivedRoom_Conflict(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-archived", "Guest")
	if _, err := db.Exec(`UPDATE rooms SET status = 'archived' WHERE slug = $1`, "lobby-archived"); err != nil {
		t.Fatalf("archive room: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"content": "hi"})
	rr := postChatReq(t, h, "/api/rooms/lobby-archived/chat/messages", body, guestID)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409; body=%s", rr.Code, rr.Body.String())
	}
	if len(bc.calls) != 0 {
		t.Fatalf("broadcast must NOT fire for archived room")
	}
}

func TestHandlePostChatMessage_MissingRoom_NotFound(t *testing.T) {
	h, db, bc, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "real", "Guest")

	body, _ := json.Marshal(map[string]string{"content": "hi"})
	rr := postChatReq(t, h, "/api/rooms/missing-room/chat/messages", body, guestID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404; body=%s", rr.Code, rr.Body.String())
	}
	if len(bc.calls) != 0 {
		t.Fatalf("broadcast must NOT fire for missing room")
	}
}

func TestHandlePostChatMessage_NoActor_Unauthorized(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	_ = seedChatFixturesForUser(t, db, "lobby-unauth", "Guest")

	body, _ := json.Marshal(map[string]string{"content": "hi"})
	rr := postChatReq(t, h, "/api/rooms/lobby-unauth/chat/messages", body, 0)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401; body=%s", rr.Code, rr.Body.String())
	}
}

// --- GET success / error mapping ---

func TestHandleListChatMessages_Success_OldestToNewest(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-list", "Guest")

	// Seed three rows directly in arrival order via the REST handler.
	for _, content := range []string{"first", "second", "third"} {
		body, _ := json.Marshal(map[string]string{"content": content})
		rr := postChatReq(t, h, "/api/rooms/lobby-list/chat/messages", body, guestID)
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed post %q: status=%d body=%s", content, rr.Code, rr.Body.String())
		}
	}

	rr := getChatReq(t, h, "/api/rooms/lobby-list/chat/messages", guestID)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rr.Code, rr.Body.String())
	}
	var resp chatListResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(resp.Messages) != 3 {
		t.Fatalf("messages len=%d want 3", len(resp.Messages))
	}
	want := []string{"first", "second", "third"}
	for i, m := range resp.Messages {
		if m.Content != want[i] {
			t.Fatalf("messages[%d].Content=%q want %q (oldest-to-newest)", i, m.Content, want[i])
		}
		if m.RoomSlug != "lobby-list" {
			t.Fatalf("messages[%d].room_slug=%q", i, m.RoomSlug)
		}
		if m.Sender.DisplayName == "" {
			t.Fatalf("messages[%d].sender.display_name empty", i)
		}
	}
	if strings.Contains(rr.Body.String(), "@example.com") || strings.Contains(rr.Body.String(), "email") {
		t.Fatalf("response must NOT expose email; got: %s", rr.Body.String())
	}
}

func TestHandleListChatMessages_Limit(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-lim", "Guest")

	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(map[string]string{"content": fmt.Sprintf("m%d", i)})
		rr := postChatReq(t, h, "/api/rooms/lobby-lim/chat/messages", body, guestID)
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed post: status=%d", rr.Code)
		}
	}

	// limit=2 → expect 2 most recent rows in oldest→newest order.
	rr := getChatReq(t, h, "/api/rooms/lobby-lim/chat/messages?limit=2", guestID)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rr.Code, rr.Body.String())
	}
	var resp chatListResp
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Messages) != 2 {
		t.Fatalf("len=%d want 2", len(resp.Messages))
	}
	if resp.Messages[0].Content != "m3" || resp.Messages[1].Content != "m4" {
		t.Fatalf("contents=%q,%q want m3,m4 (most recent 2, oldest→newest)",
			resp.Messages[0].Content, resp.Messages[1].Content)
	}
}

func TestHandleListChatMessages_InvalidLimit_BadRequest(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-ilim", "Guest")

	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=99999", "?limit=abc"} {
		rr := getChatReq(t, h, "/api/rooms/lobby-ilim/chat/messages"+q, guestID)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("limit %q: status=%d want 400; body=%s", q, rr.Code, rr.Body.String())
		}
	}
}

func TestHandleListChatMessages_ArchivedRoom_Conflict(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-arc", "Guest")
	if _, err := db.Exec(`UPDATE rooms SET status = 'archived' WHERE slug = $1`, "lobby-arc"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	rr := getChatReq(t, h, "/api/rooms/lobby-arc/chat/messages", guestID)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleListChatMessages_NonMember_Forbidden(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-nom", "Guest")
	rr := getChatReq(t, h, "/api/rooms/lobby-nom/chat/messages", guestID+9999)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleListChatMessages_NoActor_Unauthorized(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	_ = seedChatFixturesForUser(t, db, "lobby-u", "Guest")
	rr := getChatReq(t, h, "/api/rooms/lobby-u/chat/messages", 0)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleListChatMessages_InvalidSlug_BadRequest(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-x", "Guest")
	rr := getChatReq(t, h, "/api/rooms/BAD%20SLUG/chat/messages", guestID)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// --- helpers ---

func postChatReq(t *testing.T, h *RoomChatHandlers, path string, body []byte, actorUserID int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	slug := extractSlugForChat(req.URL.Path)
	h.HandlePostChatMessage(rr, req, slug, actorUserID)
	return rr
}

func getChatReq(t *testing.T, h *RoomChatHandlers, path string, actorUserID int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	slug := extractSlugForChat(req.URL.Path)
	h.HandleListChatMessages(rr, req, slug, actorUserID)
	return rr
}

// extractSlugForChat pulls the {slug} segment out of a chat URL of
// the form /api/rooms/{slug}/chat/messages[?limit=...].
func extractSlugForChat(p string) string {
	rest := strings.TrimPrefix(p, "/api/rooms/")
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return rest
	}
	return rest[:idx]
}

// seedChatFixturesForUser creates a room and a single guest member
// with a known display name. Used by every chat handler test that
// doesn't need a host row. Returns the guest user id.
func seedChatFixturesForUser(t *testing.T, db *sql.DB, slug string, displayName string) int {
	t.Helper()
	uid := seedUser(t, db, slug+"-user@example.com", entity.RoleGuest)
	// PostgresUserRepository.GetUserByID scans profile_picture into a
	// Go string, so the column must be non-NULL for the chat interactor
	// to resolve the sender's display name.
	if _, err := db.Exec(
		`UPDATE users SET display_name = $1, profile_picture = '' WHERE id = $2`,
		displayName, uid,
	); err != nil {
		t.Fatalf("set user fields: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', $3, $3)`,
		slug, "Chat Room", time.Now(),
	); err != nil {
		// Unique-slug collision — fall through and use a fresh id.
		var id int64
		if qerr := db.QueryRow(`SELECT id FROM rooms WHERE slug = $1`, slug).Scan(&id); qerr != nil {
			t.Fatalf("seed room: %v (select: %v)", err, qerr)
		}
	} else {
		var id int64
		if err := db.QueryRow(`SELECT id FROM rooms WHERE slug = $1`, slug).Scan(&id); err != nil {
			t.Fatalf("get seeded room id: %v", err)
		}
	}
	var roomID int64
	if err := db.QueryRow(`SELECT id FROM rooms WHERE slug = $1`, slug).Scan(&roomID); err != nil {
		t.Fatalf("get room id: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO room_members (room_id, user_id, role, joined_at)
		 VALUES ($1, $2, 'guest', $3)`,
		roomID, uid, time.Now(),
	); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	_ = context.TODO() // keep import in case future helpers need it
	return uid
}

// silence unused imports when a sub-test path skips body strings.
var _ = errors.New
// --- POST envelope shape (corrective pass) ---

// TestHandlePostChatMessage_PostBodyWrappedUnderMessage pins the
// corrective-pass contract: the POST 201 body is wrapped under
// { "message": { ... } } so the wire shape matches the
// room_chat_message_created WS data payload. A bare envelope (no
// wrapper) would break the symmetry the frontend store merge relies
// on.
func TestHandlePostChatMessage_PostBodyWrappedUnderMessage(t *testing.T) {
	h, db, _, cleanup := pgChatHandlers(t)
	defer cleanup()
	guestID := seedChatFixturesForUser(t, db, "lobby-wrap", "Guest")

	body, _ := json.Marshal(map[string]string{"content": "hi"})
	rr := postChatReq(t, h, "/api/rooms/lobby-wrap/chat/messages", body, guestID)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201; body=%s", rr.Code, rr.Body.String())
	}
	// The response body must be a single-key object with "message"
	// as the only top-level key.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode top-level: %v", err)
	}
	if len(top) != 1 {
		t.Fatalf("top-level keys = %d, want 1 (only \"message\")", len(top))
	}
	if _, ok := top["message"]; !ok {
		t.Fatalf("top-level must have \"message\" key; got keys: %v", top)
	}
}

// TestHandlePostChatMessage_DisplayNameLookupFailure_NoPersist
// covers the corrective-pass reorder at the use-case layer. The
// use-case test TestSend_DisplayNameMissingUser_NoPersistNoBroadcast
// pins the no-persist + no-broadcast invariant; an HTTP-level test
// would require bypassing the membership FK constraint to keep a
// membership row alive while deleting the user row, which the
// ON DELETE CASCADE schema cannot express. The use-case test is
// the authoritative coverage.
