package ws

import (
	"encoding/json"
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
	hub := NewHub()
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

func TestHub_RegisterAndBroadcast(t *testing.T) {
	hub, server := setupTestHub(t)
	defer server.Close()

	conn := dialWS(t, server)
	defer conn.Close()

	// Wait for registration to be processed
	time.Sleep(50 * time.Millisecond)

	// Broadcast a message
	msg := map[string]string{"type": "test", "data": "hello"}
	hub.Broadcast(msg)

	// Read the message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var received map[string]string
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if received["type"] != "test" {
		t.Errorf("expected type 'test', got '%s'", received["type"])
	}
	if received["data"] != "hello" {
		t.Errorf("expected data 'hello', got '%s'", received["data"])
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

	msg := map[string]string{"type": "broadcast"}
	hub.Broadcast(msg)

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
		var received map[string]string
		json.Unmarshal(data, &received)
		if received["type"] != "broadcast" {
			t.Errorf("%s: expected type 'broadcast', got '%s'", name, received["type"])
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
		hub.Broadcast(map[string]string{"type": "cleanup"})
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
	hub := NewHub()

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
	hub.Broadcast(make(chan int))

	time.Sleep(50 * time.Millisecond)

	// Ensure the hub hasn't panicked and the client is still connected
	hub.mu.Lock()
	clientCount := len(hub.clients)
	hub.mu.Unlock()
	if clientCount != 1 {
		t.Errorf("expected 1 client to remain after marshal error, got %d", clientCount)
	}
}
