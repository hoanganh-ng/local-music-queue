package ws

import (
	"context"
	"encoding/json"
	"local-music-queue/internal/domain/entity"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func setupTestHub(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	// Pass nil for getQueueState since tests don't need initial sync
	hub := NewHub(nil)
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(hub.RegisterHandler))
	return hub, server
}

func dialWS(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial WebSocket: %v", err)
	}
	return conn
}

type fakeVoteExpiryRunner struct {
	sessions []*entity.VoteSession
}

func (f *fakeVoteExpiryRunner) ExpireOldSessions(ctx context.Context) []entity.ExpiredSession {
	return nil
}

func (f *fakeVoteExpiryRunner) GetActiveSessions() []*entity.VoteSession {
	return f.sessions
}

func TestHub_RegisterAndBroadcast(t *testing.T) {
	hub, server := setupTestHub(t)
	defer server.Close()

	conn := dialWS(t, server)
	defer conn.Close()

	// Wait for registration to be processed
	time.Sleep(50 * time.Millisecond)

	// Broadcast a message using new signature
	testData := map[string]string{"message": "hello"}
	hub.Broadcast("test", testData)

	// Read the message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var received struct {
		Type   string            `json:"type"`
		Data   map[string]string `json:"data"`
		SeqNum int64             `json:"seq_num"`
	}
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if received.Type != "test" {
		t.Errorf("expected type 'test', got '%s'", received.Type)
	}
	if received.Data["message"] != "hello" {
		t.Errorf("expected message 'hello', got '%s'", received.Data["message"])
	}
	if received.SeqNum != 1 {
		t.Errorf("expected seq_num 1, got %d", received.SeqNum)
	}
}

func TestHub_RegisterHandler_MarksActiveVoteReplayInitialSync(t *testing.T) {
	hub := NewHub(nil)
	createdAt := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	hub.SetVoteInteractor(&fakeVoteExpiryRunner{
		sessions: []*entity.VoteSession{
			{
				ID:        "skip:song-1",
				Type:      entity.VoteTypeSkip,
				SongID:    "song-1",
				SongTitle: "Song One",
				SongIndex: 0,
				VotedBy:   map[int]bool{1: true},
				Threshold: 2,
				CreatedAt: createdAt,
				ExpiresAt: createdAt.Add(30 * time.Second),
			},
		},
	})
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(hub.RegisterHandler))
	defer server.Close()

	conn := dialWS(t, server)
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read active vote replay: %v", err)
	}

	var received struct {
		Type string `json:"type"`
		Data struct {
			Session     *entity.VoteSession `json:"session"`
			InitialSync bool                `json:"initial_sync"`
		} `json:"data"`
		SeqNum int64 `json:"seq_num"`
	}
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("failed to unmarshal replay: %v", err)
	}
	if received.Type != EventVoteUpdated {
		t.Fatalf("expected %q replay, got %q", EventVoteUpdated, received.Type)
	}
	if received.Data.Session == nil || received.Data.Session.ID != "skip:song-1" {
		t.Fatalf("expected replayed session skip:song-1, got %#v", received.Data.Session)
	}
	if !received.Data.InitialSync {
		t.Fatal("expected active vote replay to be marked initial_sync")
	}
	if received.SeqNum != 0 {
		t.Errorf("expected replay to preserve current seq_num 0, got %d", received.SeqNum)
	}
}

func TestHub_MultipleClients(t *testing.T) {
	hub, server := setupTestHub(t)
	defer server.Close()

	conn1 := dialWS(t, server)
	defer conn1.Close()
	conn2 := dialWS(t, server)
	defer conn2.Close()

	time.Sleep(50 * time.Millisecond)

	hub.Broadcast("broadcast", map[string]string{"test": "data"})

	var wg sync.WaitGroup
	wg.Add(2)

	readMsg := func(conn *websocket.Conn, name string) {
		defer wg.Done()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("%s: failed to read: %v", name, err)
			return
		}
		var received struct {
			Type string `json:"type"`
		}
		json.Unmarshal(data, &received)
		if received.Type != "broadcast" {
			t.Errorf("%s: expected type 'broadcast', got '%s'", name, received.Type)
		}
	}

	go readMsg(conn1, "client1")
	go readMsg(conn2, "client2")
	wg.Wait()
}

func TestHub_ClientDisconnect(t *testing.T) {
	hub, server := setupTestHub(t)
	defer server.Close()

	conn := dialWS(t, server)

	time.Sleep(50 * time.Millisecond)

	// Verify client is registered
	hub.mu.Lock()
	clientCount := len(hub.clients)
	hub.mu.Unlock()
	if clientCount != 1 {
		t.Errorf("expected 1 client, got %d", clientCount)
	}

	// Send a proper close frame and close the connection
	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conn.Close()

	// Allow TCP teardown to complete
	time.Sleep(200 * time.Millisecond)

	// Send broadcasts to trigger cleanup of the dead client.
	// The hub removes clients when WriteMessage fails.
	for i := 0; i < 3; i++ {
		hub.Broadcast("cleanup", map[string]string{"test": "data"})
		time.Sleep(100 * time.Millisecond)
	}

	hub.mu.Lock()
	clientCount = len(hub.clients)
	hub.mu.Unlock()
	if clientCount != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", clientCount)
	}
}

func TestHub_Unregister(t *testing.T) {
	hub, server := setupTestHub(t)
	defer server.Close()

	conn := dialWS(t, server)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	hub.mu.Lock()
	var serverConn *websocket.Conn
	for c := range hub.clients {
		serverConn = c
		break
	}
	hub.mu.Unlock()

	if serverConn == nil {
		t.Fatal("expected to find a server connection")
	}

	// Explicitly unregister
	hub.unregister <- serverConn

	time.Sleep(50 * time.Millisecond)

	hub.mu.Lock()
	clientCount := len(hub.clients)
	hub.mu.Unlock()
	if clientCount != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", clientCount)
	}
}

func TestHub_RegisterHandler_UpgradeError(t *testing.T) {
	hub := NewHub(nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	// Not an upgrade request, so upgrader.Upgrade will fail
	rr := httptest.NewRecorder()

	hub.RegisterHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", rr.Code)
	}
}

func TestHub_Broadcast_MarshalError(t *testing.T) {
	hub, server := setupTestHub(t)
	defer server.Close()

	conn := dialWS(t, server)
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	// Broadcast an unmarshalable value (a channel)
	hub.Broadcast("test", make(chan int))

	time.Sleep(50 * time.Millisecond)

	// Ensure the hub hasn't panicked and the client is still connected
	hub.mu.Lock()
	clientCount := len(hub.clients)
	hub.mu.Unlock()
	if clientCount != 1 {
		t.Errorf("expected 1 client to remain after marshal error, got %d", clientCount)
	}
}

func setupTestHubWithState(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	fakeQueue := &entity.Queue{
		Songs:        []entity.Song{{ID: "abc", Title: "Test Song"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      5,
	}
	getState := func(_ context.Context) (*entity.Queue, error) {
		return fakeQueue, nil
	}
	hub := NewHub(getState)
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(hub.RegisterHandler))
	return hub, server
}

func TestHub_RequestFullSync_SendsOnlyToRequester(t *testing.T) {
	_, server := setupTestHubWithState(t)
	defer server.Close()

	// Connect two clients.
	requester := dialWS(t, server)
	defer requester.Close()
	bystander := dialWS(t, server)
	defer bystander.Close()

	// Both clients receive an initial full_sync on connect. Drain them.
	requester.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := requester.ReadMessage()
	if err != nil {
		t.Fatalf("requester: failed to read initial sync: %v", err)
	}
	bystander.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = bystander.ReadMessage()
	if err != nil {
		t.Fatalf("bystander: failed to read initial sync: %v", err)
	}

	// Wait for registration to complete.
	time.Sleep(50 * time.Millisecond)

	// Requester sends request_full_sync.
	reqMsg := `{"type":"request_full_sync"}`
	if err := requester.WriteMessage(websocket.TextMessage, []byte(reqMsg)); err != nil {
		t.Fatalf("failed to send request_full_sync: %v", err)
	}

	// Requester should receive a full_sync response.
	requester.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := requester.ReadMessage()
	if err != nil {
		t.Fatalf("requester: failed to read full_sync response: %v", err)
	}
	var received struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if received.Type != EventFullSync {
		t.Errorf("requester: expected type %q, got %q", EventFullSync, received.Type)
	}

	// Bystander should NOT receive any message within a short window.
	bystander.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, unexpected, err := bystander.ReadMessage()
	if err == nil {
		var msg struct {
			Type string `json:"type"`
		}
		json.Unmarshal(unexpected, &msg)
		t.Errorf("bystander: expected no message, got type %q", msg.Type)
	}
}

func TestHub_RequestFullSync_WithNilGetState(t *testing.T) {
	// Hub has no getQueueState; request_full_sync should not crash or close
	// the connection.
	hub, server := setupTestHub(t)
	defer server.Close()

	conn := dialWS(t, server)
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	reqMsg := `{"type":"request_full_sync"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(reqMsg)); err != nil {
		t.Fatalf("failed to send request_full_sync: %v", err)
	}

	// Give readPump time to process the message.
	time.Sleep(100 * time.Millisecond)

	// Hub should still have the client connected.
	hub.mu.Lock()
	clientCount := len(hub.clients)
	hub.mu.Unlock()
	if clientCount != 1 {
		t.Errorf("expected 1 client to remain, got %d", clientCount)
	}
}

func TestHub_MalformedAndUnknownMessages_DoNotBlockFullSync(t *testing.T) {
	_, server := setupTestHubWithState(t)
	defer server.Close()

	conn := dialWS(t, server)
	defer conn.Close()

	// Drain the initial full_sync sent on connect.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read initial full_sync: %v", err)
	}

	// Wait for registration.
	time.Sleep(50 * time.Millisecond)

	// 1. Send a malformed (invalid JSON) message.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{not json`)); err != nil {
		t.Fatalf("failed to send malformed message: %v", err)
	}

	// 2. Send a message with an unknown type.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"bogus_type"}`)); err != nil {
		t.Fatalf("failed to send unknown-type message: %v", err)
	}

	// 3. Send a valid request_full_sync after the noise.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"request_full_sync"}`)); err != nil {
		t.Fatalf("failed to send request_full_sync: %v", err)
	}

	// The server must respond with a full_sync.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read full_sync response: %v", err)
	}

	var received struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if received.Type != EventFullSync {
		t.Errorf("expected type %q after malformed/unknown messages, got %q", EventFullSync, received.Type)
	}
}
