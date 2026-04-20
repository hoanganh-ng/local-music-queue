package http

import (
	"bytes"
	"encoding/json"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/infrastructure/youtube"
	"local-music-queue/internal/usecase/activity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/queue"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// newTestHandlers creates Handlers wired to a temp SQLite DB and a fake yt-dlp.
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	repo, err := persistence.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	// Create a fake yt-dlp script
	var ytSvc *youtube.YTDLPService
	if runtime.GOOS != "windows" {
		script := filepath.Join(dir, "fake-ytdlp")
		content := `#!/bin/sh
cat <<'EOF'
{"id":"test123","title":"Test Song","uploader":"Artist","duration":180.0,"thumbnail":"thumb.jpg","webpage_url":"https://youtube.com/watch?v=test123"}
EOF
`
		os.WriteFile(script, []byte(content), 0755)
		ytSvc = youtube.NewYTDLPService(script)
	} else {
		ytSvc = youtube.NewYTDLPService("echo") // fallback
	}

	queueInteractor := queue.NewInteractor(repo, ytSvc)
	authInteractor := auth.NewInteractor("1234", "5678")
	actInteractor := activity.NewInteractor(repo)
	hub := ws.NewHub()
	go hub.Run()

	return NewHandlers(queueInteractor, authInteractor, actInteractor, hub)
}

// --- Login Tests ---

func TestHandleLogin_Success(t *testing.T) {
	h := newTestHandlers(t)

	body, _ := json.Marshal(LoginRequest{PIN: "1234", DisplayName: "TestUser"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var user entity.User
	json.NewDecoder(rr.Body).Decode(&user)
	if user.Role != entity.RoleGuest {
		t.Errorf("expected role guest, got %s", user.Role)
	}
	if user.DisplayName != "TestUser" {
		t.Errorf("expected 'TestUser', got '%s'", user.DisplayName)
	}
}

func TestHandleLogin_InvalidPIN(t *testing.T) {
	h := newTestHandlers(t)

	body, _ := json.Marshal(LoginRequest{PIN: "0000", DisplayName: "User"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleLogin(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandleLogin_BadJSON(t *testing.T) {
	h := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()

	h.HandleLogin(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- GetQueue Tests ---

func TestHandleGetQueue_Success(t *testing.T) {
	h := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/api/queue", nil)
	rr := httptest.NewRecorder()

	h.HandleGetQueue(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var q entity.Queue
	json.NewDecoder(rr.Body).Decode(&q)
	if q.Songs == nil {
		t.Error("expected songs array, got nil")
	}
}

// --- AddSong Tests ---

func TestHandleAddSong_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}

	h := newTestHandlers(t)

	body, _ := json.Marshal(AddSongRequest{URL: "https://youtube.com/watch?v=test", AddedBy: "Alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleAddSong(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAddSong_BadJSON(t *testing.T) {
	h := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader([]byte("bad")))
	rr := httptest.NewRecorder()

	h.HandleAddSong(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- SkipSong Tests ---

func TestHandleSkipSong_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	h := newTestHandlers(t)

	// First add two songs
	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(AddSongRequest{URL: "url", AddedBy: "Alice"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		h.HandleAddSong(rr, req)
	}

	// Now skip
	body, _ := json.Marshal(SkipRequest{RequestedBy: "Alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/skip", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleSkipSong(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleSkipSong_Error(t *testing.T) {
	h := newTestHandlers(t)

	// Skip on empty queue should fail
	body, _ := json.Marshal(SkipRequest{RequestedBy: "Alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/skip", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleSkipSong(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// --- SetStatus Tests ---

func TestHandleSetStatus_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	h := newTestHandlers(t)

	// Add a song first
	addBody, _ := json.Marshal(AddSongRequest{URL: "url", AddedBy: "Alice"})
	addReq := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(addBody))
	h.HandleAddSong(httptest.NewRecorder(), addReq)

	// Set status
	body, _ := json.Marshal(StatusRequest{Status: entity.StatusPaused, RequestedBy: "Alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/status", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleSetStatus(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleSetStatus_Error(t *testing.T) {
	h := newTestHandlers(t)

	// On empty queue, transitioning to "playing" should be invalid
	body, _ := json.Marshal(StatusRequest{Status: entity.StatusPlaying, RequestedBy: "Alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/status", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleSetStatus(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}
