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

	deliveryhttp "local-music-queue/internal/delivery/http"
	"local-music-queue/internal/delivery/origin"
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
	if os.Getenv("GOOGLE_CLIENT_ID") == "" {
		os.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
	}
	t.Cleanup(func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("YTDLP_PATH")
		os.Unsetenv("GOOGLE_CLIENT_ID")
	})

	mux, _, _, _, _, cleanup, err := setupApp(setupOptions{})
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	policy, perr := origin.Parse(map[string]string{"APP_ENV": "test", "ALLOWED_ORIGINS": "https://app.example.com"})
	if perr != nil {
		t.Fatalf("origin.Parse: %v", perr)
	}
	server := httptest.NewServer(requestLogger(enableCORS(policy, mux)))
	defer server.Close()

	client := server.Client()
	req, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/auth", nil)
	req.Header.Set("Origin", "https://app.example.com")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /api/auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("OPTIONS /api/auth status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("CORS Allow-Origin = %q, want %q", got, "https://app.example.com")
	}
}

// mintTestSessionToken drives the real POST /api/auth/google endpoint with a
// mocked Google tokeninfo response and returns the session_token issued by
// the live server. R05 — every privileged REST route now requires a valid
// bearer token, so integration tests must obtain one the same way real
// clients do.
func mintTestSessionToken(t *testing.T, server *httptest.Server, email string) string {
	t.Helper()
	// The auth interactor verifies aud == configured client ID. The
	// setupApp call above loaded whatever GOOGLE_CLIENT_ID was in the
	// environment; the mocked tokeninfo below always reports
	// "test-client-id". Set both consistently.
	if os.Getenv("GOOGLE_CLIENT_ID") == "" {
		t.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
	}
	oldTransport := http.DefaultClient.Transport
	t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })

	http.DefaultClient.Transport = roundTripGoogleTokenInfo(t, email)

	body, _ := json.Marshal(map[string]string{"id_token": "test-id-token"})
	resp, err := server.Client().Post(server.URL+"/api/auth/google", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/google: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/auth/google status = %d, want 200", resp.StatusCode)
	}
	var parsed struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if parsed.SessionToken == "" {
		t.Fatalf("login response missing session_token")
	}
	return parsed.SessionToken
}

// roundTripGoogleTokenInfo returns a RoundTripper that fakes a successful
// Google tokeninfo response. The test email must end in @urekamedia.vn to
// pass the auth interactor's domain check.
func roundTripGoogleTokenInfo(t *testing.T, email string) roundTripFunc {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "oauth2.googleapis.com/tokeninfo") {
			return nil, fmt.Errorf("unexpected outbound URL in test transport: %s", req.URL.String())
		}
		tokenInfo := fmt.Sprintf(`{
			"email": %q,
			"name": "Integration Tester",
			"picture": "http://example.com/p.jpg",
			"aud": "test-client-id",
			"email_verified": "true"
		}`, email)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(tokenInfo)),
			Header:     make(http.Header),
		}, nil
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
func TestAPIIntegration_QueueREST(t *testing.T) {
	scopedDSN := apiTestDB(t)

	ytPath := os.Getenv("YTDLP_PATH")
	if ytPath == "" {
		ytPath = fakeYTDLPScript(t)
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	os.Setenv("YTDLP_PATH", ytPath)
	// R05: setupApp reads GOOGLE_CLIENT_ID once at startup; the auth
	// interactor's aud check needs it to match the mocked tokeninfo
	// response ("test-client-id").
	if os.Getenv("GOOGLE_CLIENT_ID") == "" {
		os.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
	}
	t.Cleanup(func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("YTDLP_PATH")
		os.Unsetenv("GOOGLE_CLIENT_ID")
	})

	mux, _, _, _, _, cleanup, err := setupApp(setupOptions{})
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(mux)
	defer server.Close()

	// R05: obtain a valid session token via the real Google login path.
	token := mintTestSessionToken(t, server, "integration-tester@urekamedia.vn")

	addBody, _ := json.Marshal(deliveryhttp.AddSongRequest{
		URL:     "https://www.youtube.com/watch?v=test123",
		AddedBy: "IntegrationTester",
	})
	addReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/queue/add", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addReq.Header.Set("Authorization", "Bearer "+token)
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
	if os.Getenv("GOOGLE_CLIENT_ID") == "" {
		os.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
	}
	t.Cleanup(func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("YTDLP_PATH")
		os.Unsetenv("GOOGLE_CLIENT_ID")
	})

	mux, _, _, _, _, cleanup, err := setupApp(setupOptions{})
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	policy, perr := origin.Parse(map[string]string{"APP_ENV": "test", "ALLOWED_ORIGINS": "https://app.example.com"})
	if perr != nil {
		t.Fatalf("origin.Parse: %v", perr)
	}
	server := httptest.NewServer(requestLogger(enableCORS(policy, mux)))
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

	// R05: a Google login happens later in the test (to mint the session
	// token) and broadcasts a user_joined event. Drain that and any other
	// pre-song_added messages, looking for song_added with a short timeout.
	msgChan := make(chan []byte, 8)
	stop := make(chan struct{})
	go func() {
		for {
			_, message, rerr := wsConn.ReadMessage()
			if rerr != nil {
				close(stop)
				return
			}
			select {
			case msgChan <- message:
			case <-stop:
				return
			}
		}
	}()

	// R05: obtain a valid session token for the privileged add endpoint.
	token := mintTestSessionToken(t, server, "ws-tester@urekamedia.vn")

	addBody, _ := json.Marshal(deliveryhttp.AddSongRequest{
		URL:     "https://www.youtube.com/watch?v=test123",
		AddedBy: "WSTester",
	})
	addReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/queue/add", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addReq.Header.Set("Authorization", "Bearer "+token)
	addResp, err := server.Client().Do(addReq)
	if err != nil {
		t.Fatalf("POST /api/queue/add: %v", err)
	}
	addResp.Body.Close()
	if addResp.StatusCode != http.StatusOK {
		t.Errorf("POST /api/queue/add status = %d, want 200", addResp.StatusCode)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-msgChan:
			if strings.Contains(string(msg), ws.EventSongAdded) {
				return
			}
			// otherwise keep draining until we see song_added
		case <-deadline:
			t.Fatal("timed out waiting for song_added broadcast")
		}
	}
}
