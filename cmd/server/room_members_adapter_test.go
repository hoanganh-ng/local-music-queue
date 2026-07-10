package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
)

// TestRoomMembersBroadcasterAdapter_ExcludesRemovedUser pins the R10b
// production-wiring contract: when the interactor calls the
// roomMembersBroadcasterAdapter.BroadcastRoomMembersChanged with the
// target user id, the hub MUST NOT deliver the room_members_changed
// envelope to that target's connections. This guards against the
// regression where the adapter hard-coded excludeUserID=0, which
// would race the close-frame path and let the removed user see the
// fan-out envelope.
func TestRoomMembersBroadcasterAdapter_ExcludesRemovedUser(t *testing.T) {
	resolver := ws.RoomQueueResolverFunc(func(_ context.Context, slug string) (*entity.Room, error) {
		return &entity.Room{ID: 1, Slug: slug, Status: entity.RoomStatusActive}, nil
	})
	member := ws.RoomMemberResolverFunc(func(_ context.Context, _ int64, _ int) (bool, error) { return true, nil })
	state := ws.RoomQueueStateResolverFunc(func(_ context.Context, _ int64) (*entity.Queue, error) {
		q := entity.NewQueue()
		q.Songs = []entity.Song{{ID: "s1", Title: "S1", URL: "u"}}
		q.CurrentIndex = 0
		return q, nil
	})
	hub := ws.NewRoomWSHub(resolver, member, state)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&adapterTestSessionResolver{
		users: map[string]*entity.User{
			"host":  {ID: 1, Role: entity.RoleHost, DisplayName: "Host"},
			"guest": {ID: 2, Role: entity.RoleGuest, DisplayName: "Guest"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	cHost := dialAdapterWS(t, server, "adapter-room", "host")
	defer cHost.Close()
	cGuest := dialAdapterWS(t, server, "adapter-room", "guest")
	defer cGuest.Close()

	// Drain initial sync for both connections.
	for _, c := range []*websocket.Conn{cHost, cGuest} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("drain: %v", err)
		}
	}

	// PRODUCTION ADAPTER: this is the seam cmd/server wires to the
	// interactor. It MUST carry excludeUserID through to the hub.
	adapter := roomMembersBroadcasterAdapter{hub: hub}
	remaining := []entity.RoomMember{
		{RoomID: 1, UserID: 1, Role: entity.RoomRoleHost},
	}
	adapter.BroadcastRoomMembersChanged("adapter-room", remaining, 2 /*excludeUserID=guest*/)

	// Host (user 1) receives the envelope.
	cHost.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := cHost.ReadMessage()
	if err != nil {
		t.Fatalf("host read members-changed: %v", err)
	}
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != "room_members_changed" {
		t.Errorf("expected type room_members_changed, got %q", env.Type)
	}

	// Guest (user 2 — the excluded user) MUST NOT receive this envelope.
	cGuest.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := cGuest.ReadMessage(); err == nil {
		t.Errorf("guest (excluded) unexpectedly received room_members_changed — production wiring regression")
	}
}

// TestRoomMembersBroadcasterAdapter_ArchiveCarriesThrough pins the
// per-room archive seam: BroadcastRoomArchived on the production adapter
// delivers the room_archived envelope on the per-room hub with the
// correct reason.
func TestRoomMembersBroadcasterAdapter_ArchiveCarriesThrough(t *testing.T) {
	resolver := ws.RoomQueueResolverFunc(func(_ context.Context, slug string) (*entity.Room, error) {
		return &entity.Room{ID: 1, Slug: slug, Status: entity.RoomStatusActive}, nil
	})
	member := ws.RoomMemberResolverFunc(func(_ context.Context, _ int64, _ int) (bool, error) { return true, nil })
	state := ws.RoomQueueStateResolverFunc(func(_ context.Context, _ int64) (*entity.Queue, error) {
		q := entity.NewQueue()
		q.Songs = []entity.Song{{ID: "s1", Title: "S1", URL: "u"}}
		q.CurrentIndex = 0
		return q, nil
	})
	hub := ws.NewRoomWSHub(resolver, member, state)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&adapterTestSessionResolver{
		users: map[string]*entity.User{
			"host": {ID: 1, Role: entity.RoleHost, DisplayName: "Host"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	cHost := dialAdapterWS(t, server, "archive-room", "host")
	defer cHost.Close()
	cHost.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := cHost.ReadMessage(); err != nil {
		t.Fatalf("drain: %v", err)
	}

	adapter := roomMembersBroadcasterAdapter{hub: hub}
	adapter.BroadcastRoomArchived("archive-room", "host_archived")

	cHost.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := cHost.ReadMessage()
	if err != nil {
		t.Fatalf("host read archive: %v", err)
	}
	var env struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != "room_archived" {
		t.Errorf("expected type room_archived, got %q", env.Type)
	}
	if reason, _ := env.Data["reason"].(string); reason != "host_archived" {
		t.Errorf("expected reason=host_archived, got %v", env.Data["reason"])
	}
}

// adapterTestSessionResolver is a minimal stub that satisfies
// ws.RoomSessionResolver for the adapter tests.
type adapterTestSessionResolver struct {
	users map[string]*entity.User
}

func (s *adapterTestSessionResolver) ResolveSession(_ context.Context, token string) (*entity.User, error) {
	if u, ok := s.users[token]; ok {
		return u, nil
	}
	return nil, errors.New("invalid session")
}

// dialAdapterWS opens a per-room WS connection for the adapter tests.
func dialAdapterWS(t *testing.T, server *httptest.Server, slug, token string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + server.URL[len("http"):] + "/ws/rooms/" + slug + "?session_token=" + token
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", wsURL, err)
	}
	return conn
}