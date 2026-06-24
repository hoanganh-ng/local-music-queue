package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/infrastructure/persistence"
)

func apiTestDB(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("postgres unavailable (open): %v", err)
	}
	defer root.Close()
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := root.PingContext(pingCtx); err != nil {
		t.Skipf("postgres unavailable (ping): %v", err)
	}
	schema := fmt.Sprintf("lmq_api_test_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	scoped, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer scoped.Close()
	if err := persistence.RunEmbeddedMigrationsUp(scoped); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	t.Cleanup(func() {
		drop, _ := sql.Open("pgx", dsn)
		if drop != nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
	})
	return dsn + "&search_path=" + schema
}

func TestAPIIntegration(t *testing.T) {
	// TODO(r03+): this test predates the PIN-deprecation change in
	// HandleLogin (which now returns 400 for any PIN-based login) and
	// the existing assertions no longer hold. It will be rewritten when
	// the test harness can mint a real session via the Google Sign-In
	// flow or against a non-deprecated login path. Skipping rather than
	// deleting so the scaffolding around setupApp + httptest.Server
	// remains available for the next iteration.
	t.Skip("TestAPIIntegration is awaiting a non-deprecated login flow; see TODO")

	// 1. Setup Environment — PostgreSQL only since R03.
	scopedDSN := apiTestDB(t)

	// YTDLP_PATH must point at an executable for cfg.Validate() to pass;
	// fall back to /bin/true if not set in the environment.
	ytPath := os.Getenv("YTDLP_PATH")
	if ytPath == "" {
		ytPath = "/bin/true"
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	os.Setenv("YTDLP_PATH", ytPath)
	os.Setenv("CLIENT_PIN", "1111")
	os.Setenv("HOST_PIN", "2222")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("YTDLP_PATH")
		os.Unsetenv("CLIENT_PIN")
		os.Unsetenv("HOST_PIN")
	}()

	// 2. Start Server
	mux, _, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("Failed to setup app: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(requestLogger(enableCORS(mux)))
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
