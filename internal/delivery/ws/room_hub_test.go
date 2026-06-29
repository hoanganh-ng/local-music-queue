package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"local-music-queue/internal/domain/entity"
)

// stubRoomResolver supplies a fixed room + queue snapshot for hub tests.
type stubRoomResolver struct {
	mu     sync.Mutex
	rooms  map[string]*entity.Room
	queues map[int64]*entity.Queue
	// nonMemberIDs lists user ids that should be rejected as non-members.
	nonMemberIDs map[int]bool
	// resolveErr returns this error from RoomBySlug when non-nil.
	resolveErr error
}

func newStubRoomResolver() *stubRoomResolver {
	return &stubRoomResolver{
		rooms:        map[string]*entity.Room{},
		queues:       map[int64]*entity.Queue{},
		nonMemberIDs: map[int]bool{999: true},
	}
}

func (s *stubRoomResolver) RoomBySlug(_ context.Context, slug string) (*entity.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	r, ok := s.rooms[slug]
	if !ok {
		return nil, errStubRoomNotFound
	}
	return r, nil
}

func (s *stubRoomResolver) IsActiveMember(_ context.Context, _ int64, userID int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.nonMemberIDs[userID], nil
}

func (s *stubRoomResolver) QueueByRoomID(_ context.Context, roomID int64) (*entity.Queue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.queues[roomID]
	if !ok {
		return entity.NewQueue(), nil
	}
	return q, nil
}

func (s *stubRoomResolver) SetRoom(slug string, room *entity.Room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rooms[slug] = room
}

func (s *stubRoomResolver) SetQueue(roomID int64, q *entity.Queue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queues[roomID] = q
}

var errStubRoomNotFound = errStubInvalid // reuse the existing stub error

// dialRoomWS dials the per-room endpoint on a test server.
func dialRoomWS(t *testing.T, server *httptest.Server, slug, token string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/" + slug
	if token != "" {
		u += "?session_token=" + token
	}
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial room ws: %v", err)
	}
	return conn
}

// TestRoomHub_SendsInitialSyncOnConnect pins the on-connect sync contract
// (single-process / in-memory): every room WS connection receives a
// single room_queue_sync envelope scoped to the slug it connected with.
func TestRoomHub_SendsInitialSyncOnConnect(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "T1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 1, Role: entity.RoleHost, DisplayName: "H"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	conn := dialRoomWS(t, server, "alpha", "valid")
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var env struct {
		Type string            `json:"type"`
		Data RoomQueueSyncData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != EventRoomQueueSync {
		t.Fatalf("expected %q, got %q", EventRoomQueueSync, env.Type)
	}
	if env.Data.RoomSlug != "alpha" {
		t.Errorf("expected room_slug=alpha, got %q", env.Data.RoomSlug)
	}
	if env.Data.State == nil || len(env.Data.State.Songs) != 1 {
		t.Errorf("expected state with 1 song, got %#v", env.Data.State)
	}
}

// TestRoomHub_BroadcastSongAdded_ReachesAllRoomClients asserts that a
// BroadcastRoomQueueSongAdded invocation only reaches clients connected
// to the matching room slug — clients in other rooms do not receive it.
func TestRoomHub_BroadcastSongAdded_ReachesAllRoomClients(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetRoom("beta", &entity.Room{ID: 22, Slug: "beta", Status: entity.RoomStatusActive})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"alpha": {ID: 1, Role: entity.RoleHost, DisplayName: "A"},
			"beta":  {ID: 2, Role: entity.RoleHost, DisplayName: "B"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	a1 := dialRoomWS(t, server, "alpha", "alpha")
	defer a1.Close()
	a1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := a1.ReadMessage(); err != nil {
		t.Fatalf("alpha drain: %v", err)
	}
	b1 := dialRoomWS(t, server, "beta", "beta")
	defer b1.Close()
	b1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := b1.ReadMessage(); err != nil {
		t.Fatalf("beta drain: %v", err)
	}

	state := &entity.Queue{Songs: []entity.Song{{ID: "added", Title: "Added"}}}
	hub.BroadcastRoomQueueSongAdded("alpha", entity.Song{ID: "added", Title: "Added"}, 0, state)

	a1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := a1.ReadMessage()
	if err != nil {
		t.Fatalf("alpha read broadcast: %v", err)
	}
	var env struct {
		Type string `json:"type"`
	}
	json.Unmarshal(data, &env)
	if env.Type != EventRoomQueueSongAdded {
		t.Fatalf("alpha: expected %q, got %q", EventRoomQueueSongAdded, env.Type)
	}

	// Beta must not receive anything within a short window.
	b1.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := b1.ReadMessage(); err == nil {
		t.Errorf("beta unexpectedly received a broadcast for alpha")
	}
}

// TestRoomHub_RegisterHandler_NonMemberRejected verifies the non-member
// gate: even with a valid session token, a non-member receives 403 on
// upgrade and does not appear in the room's client map.
func TestRoomHub_RegisterHandler_NonMemberRejected(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 999, Role: entity.RoleGuest, DisplayName: "X"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	u := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/alpha?session_token=valid"
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatalf("expected dial to fail for non-member")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %v", resp)
	}
}

// TestRoomHub_RegisterHandler_RequiresSessionToken verifies the auth
// gate: a missing/invalid session token results in 401.
func TestRoomHub_RegisterHandler_RequiresSessionToken(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 1, Role: entity.RoleHost, DisplayName: "H"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	// No session_token in URL.
	u := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/alpha"
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatalf("expected dial to fail without session token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session token, got %v", resp)
	}
}