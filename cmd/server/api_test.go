package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAPIIntegration(t *testing.T) {
	// 1. Setup Environment
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ytdlpPath := os.Getenv("YTDLP_PATH")
	os.Setenv("DB_PATH", dbPath)
	os.Setenv("YTDLP_PATH", ytdlpPath)
	os.Setenv("CLIENT_PIN", "1111")
	os.Setenv("HOST_PIN", "2222")
	defer func() {
		os.Unsetenv("DB_PATH")
		os.Unsetenv("YTDLP_PATH")
		os.Unsetenv("CLIENT_PIN")
		os.Unsetenv("HOST_PIN")
	}()

	// 2. Start Server
	mux, _, err := setupApp()
	if err != nil {
		t.Fatalf("Failed to setup app: %v", err)
	}

	server := httptest.NewServer(enableCORS(mux))
	defer server.Close()

	client := server.Client()

	// 3. Test CORS Headers
	t.Run("CORS Headers", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/auth", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("OPTIONS request failed: %v", err)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("Expected CORS header, got %s", resp.Header.Get("Access-Control-Allow-Origin"))
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for OPTIONS, got %d", resp.StatusCode)
		}
	})

	// 4. Test Auth API
	t.Run("Auth Login", func(t *testing.T) {
		loginData := map[string]string{"pin": "1111", "display_name": "TestUser"}
		body, _ := json.Marshal(loginData)
		resp, err := client.Post(server.URL+"/api/auth", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("POST /api/auth failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if result["display_name"] != "TestUser" {
			t.Errorf("Expected TestUser, got %v", result["display_name"])
		}
	})

	// 5. Test WebSocket & Queue Add Broadcast
	t.Run("WebSocket Broadcast on Add", func(t *testing.T) {
		// Connect to WebSocket
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("WS connection failed: %v", err)
		}
		defer ws.Close()

		// Channel to receive messages
		msgChan := make(chan []byte, 1)
		go func() {
			_, message, err := ws.ReadMessage()
			if err == nil {
				msgChan <- message
			}
		}()

		// Add a song via HTTP
		addBody := map[string]string{"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "added_by": "TestUser"}
		body, _ := json.Marshal(addBody)
		resp, err := client.Post(server.URL+"/api/queue/add", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("POST /api/queue/add failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for add, got %d", resp.StatusCode)
		}

		// Wait for WS message
		select {
		case msg := <-msgChan:
			if !strings.Contains(string(msg), "queue_updated") {
				t.Errorf("Expected queue_updated message, got %s", string(msg))
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for WebSocket broadcast")
		}
	})

	// 6. Test Get Queue
	t.Run("Get Queue", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/api/queue")
		if err != nil {
			t.Fatalf("GET /api/queue failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		var queue map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&queue)
		songs := queue["songs"].([]interface{})
		if len(songs) == 0 {
			t.Error("Expected at least one song in queue")
		}
	})
}

func dumpBody(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}
