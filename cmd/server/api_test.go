package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/jackc/pgx/v5/stdlib"

	deliveryhttp "local-music-queue/internal/delivery/http"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

// apiTestDB opens a per-test throwaway PostgreSQL schema, applies embedded
// migrations, and returns a DSN scoped to that schema. Skips when PG is
// unreachable.
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

// fakeYTDLPScript writes a tiny shell script that emits a fake video metadata
// JSON line so the queue interactor's yt-dlp call succeeds.
func fakeYTDLPScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := dir + "/fake-ytdlp"
	content := `#!/bin/sh
cat <<'EOF'
{"id":"test123","title":"Test Song","uploader":"Artist","duration":180.0,"thumbnail":"thumb.jpg","webpage_url":"https://youtube.com/watch?v=test123"}
EOF
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake-ytdlp: %v", err)
	}
	return script
}

// TestAPIIntegration_ServerStarts proves the server can boot against the
// real PostgreSQL backend and that the resulting mux serves CORS preflight
// requests on a known route.
func TestAPIIntegration_ServerStarts(t *testing.T) {
	scopedDSN := apiTestDB(t)

	ytPath := os.Getenv("YTDLP_PATH")
	if ytPath == "" {
		ytPath = fakeYTDLPScript(t)
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	os.Setenv("YTDLP_PATH", ytPath)
	t.Cleanup(func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("YTDLP_PATH")
	})

	mux, _, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(requestLogger(enableCORS(mux)))
	defer server.Close()

	client := server.Client()
	req, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/auth", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /api/auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("OPTIONS /api/auth status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS Allow-Origin = %q, want *", got)
	}
}

// TestAPIIntegration_QueueREST exercises the queue add + get round trip
// against the real PostgreSQL backend via the real handlers and mux wired
// by setupApp. The PIN login path is intentionally avoided; the test uses
// the AddSong handler which (per handlers.go) accepts an `added_by` field
// directly without requiring a session.
func TestAPIIntegration_QueueREST(t *testing.T) {
	scopedDSN := apiTestDB(t)

	ytPath := os.Getenv("YTDLP_PATH")
	if ytPath == "" {
		ytPath = fakeYTDLPScript(t)
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	os.Setenv("YTDLP_PATH", ytPath)
	t.Cleanup(func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("YTDLP_PATH")
	})

	mux, _, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(mux)
	defer server.Close()

	addBody, _ := json.Marshal(deliveryhttp.AddSongRequest{
		URL:     "https://www.youtube.com/watch?v=test123",
		AddedBy: "IntegrationTester",
	})
	addReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/queue/add", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addResp, err := server.Client().Do(addReq)
	if err != nil {
		t.Fatalf("POST /api/queue/add: %v", err)
	}
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusOK {
		t.Errorf("POST /api/queue/add status = %d, want 200", addResp.StatusCode)
	}

	getResp, err := server.Client().Get(server.URL + "/api/queue")
	if err != nil {
		t.Fatalf("GET /api/queue: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/queue status = %d, want 200", getResp.StatusCode)
	}

	var got entity.Queue
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if len(got.Songs) == 0 {
		t.Errorf("expected at least 1 song after add; got 0")
	}
}

// TestAPIIntegration_WebSocketBroadcast proves the real WebSocket hub
// (wired by setupApp) broadcasts a song_added event when POST /api/queue/add
// is invoked. This is the regression coverage the R03 fix-pass needed in
// place of the deprecated PIN login path.
func TestAPIIntegration_WebSocketBroadcast(t *testing.T) {
	scopedDSN := apiTestDB(t)

	ytPath := os.Getenv("YTDLP_PATH")
	if ytPath == "" {
		ytPath = fakeYTDLPScript(t)
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	os.Setenv("YTDLP_PATH", ytPath)
	t.Cleanup(func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("YTDLP_PATH")
	})

	mux, _, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(requestLogger(enableCORS(mux)))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer wsConn.Close()

	if err := wsConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := wsConn.ReadMessage(); err != nil {
		t.Fatalf("read initial sync: %v", err)
	}

	msgChan := make(chan []byte, 1)
	go func() {
		_, message, rerr := wsConn.ReadMessage()
		if rerr == nil {
			msgChan <- message
		}
	}()

	addBody, _ := json.Marshal(deliveryhttp.AddSongRequest{
		URL:     "https://www.youtube.com/watch?v=test123",
		AddedBy: "WSTester",
	})
	addReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/queue/add", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addResp, err := server.Client().Do(addReq)
	if err != nil {
		t.Fatalf("POST /api/queue/add: %v", err)
	}
	addResp.Body.Close()
	if addResp.StatusCode != http.StatusOK {
		t.Errorf("POST /api/queue/add status = %d, want 200", addResp.StatusCode)
	}

	select {
	case msg := <-msgChan:
		if !strings.Contains(string(msg), ws.EventSongAdded) {
			t.Errorf("expected broadcast containing %q, got: %s", ws.EventSongAdded, string(msg))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for song_added broadcast")
	}
}