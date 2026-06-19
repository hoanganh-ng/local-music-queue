package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/infrastructure/session"
	"local-music-queue/internal/infrastructure/youtube"
	"local-music-queue/internal/usecase/activity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/priority"
	"local-music-queue/internal/usecase/queue"
	"local-music-queue/internal/usecase/vote"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

	// Create user repository
	userRepo := persistence.NewSQLiteUserRepository(repo.DB())

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
	sessionClock := auth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)
	authInteractor := auth.NewInteractor(userRepo, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)
	actInteractor := activity.NewInteractor(repo)
	priorityInteractor := priority.NewInteractor(userRepo, repo)
	voteInteractor := vote.NewInteractor(repo, userRepo, 0)
	hub := ws.NewHub(queueInteractor.GetState)
	go hub.Run()

	return NewHandlers(queueInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub)
}

// --- Login Tests ---

func TestHandleLogin_Deprecated(t *testing.T) {
	h := newTestHandlers(t)

	body, _ := json.Marshal(LoginRequest{PIN: "1234", DisplayName: "TestUser"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleLogin(rr, req)

	// PIN login is deprecated, should return 400
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (deprecated), got %d", rr.Code)
	}
}

func TestHandleLogin_InvalidPIN_Deprecated(t *testing.T) {
	h := newTestHandlers(t)

	body, _ := json.Marshal(LoginRequest{PIN: "0000", DisplayName: "User"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.HandleLogin(rr, req)

	// PIN login is deprecated, should return 400
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (deprecated), got %d", rr.Code)
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

// --- SearchYouTube Tests ---

func TestHandleSearchYouTube_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	repo, err := persistence.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	// Create a fake yt-dlp script that returns search results
	script := filepath.Join(dir, "fake-ytdlp")
	content := `#!/bin/sh
cat <<'EOF'
{"id":"result1","title":"Test Song 1","uploader":"Artist 1","duration":180.0,"thumbnail":"thumb1.jpg","webpage_url":"https://youtube.com/watch?v=result1"}
{"id":"result2","title":"Test Song 2","uploader":"Artist 2","duration":240.0,"thumbnail":"thumb2.jpg","webpage_url":"https://youtube.com/watch?v=result2"}
EOF
`
	os.WriteFile(script, []byte(content), 0755)
	ytSvc := youtube.NewYTDLPService(script)

	// Create user repository
	userRepo := persistence.NewSQLiteUserRepository(repo.DB())

	queueInteractor := queue.NewInteractor(repo, ytSvc)
	sessionClock := auth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)
	authInteractor := auth.NewInteractor(userRepo, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)
	actInteractor := activity.NewInteractor(repo)
	priorityInteractor := priority.NewInteractor(userRepo, repo)
	voteInteractor := vote.NewInteractor(repo, userRepo, 0)
	hub := ws.NewHub(queueInteractor.GetState)
	go hub.Run()

	h := NewHandlers(queueInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub)

	req := httptest.NewRequest(http.MethodGet, "/api/youtube/search?q=test", nil)
	rr := httptest.NewRecorder()

	h.HandleSearchYouTube(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var results []map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&results)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestHandleSearchYouTube_MissingQuery(t *testing.T) {
	h := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/api/youtube/search", nil)
	rr := httptest.NewRecorder()

	h.HandleSearchYouTube(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleSearchYouTube_EmptyQuery(t *testing.T) {
	h := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/api/youtube/search?q=", nil)
	rr := httptest.NewRecorder()

	h.HandleSearchYouTube(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func newTestHandlersWithStore(t *testing.T) (*Handlers, repository.UserRepository, auth.SessionStore) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	repo, err := persistence.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	userRepo := persistence.NewSQLiteUserRepository(repo.DB())

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
		ytSvc = youtube.NewYTDLPService("echo")
	}

	queueInteractor := queue.NewInteractor(repo, ytSvc)
	sessionClock := auth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)
	authInteractor := auth.NewInteractor(userRepo, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)
	actInteractor := activity.NewInteractor(repo)
	priorityInteractor := priority.NewInteractor(userRepo, repo)
	voteInteractor := vote.NewInteractor(repo, userRepo, 0)
	hub := ws.NewHub(queueInteractor.GetState)
	go hub.Run()

	return NewHandlers(queueInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub), userRepo, sessionStore
}

func intPtr(v int) *int {
	return &v
}

type mockBroadcaster struct {
	broadcasts []struct {
		eventType string
		data      interface{}
	}
	connectedCount int
}

func (m *mockBroadcaster) Broadcast(eventType string, data interface{}) {
	m.broadcasts = append(m.broadcasts, struct {
		eventType string
		data      interface{}
	}{eventType, data})
}

func (m *mockBroadcaster) ConnectedCount() int {
	return m.connectedCount
}

type failingQueueRepo struct {
	repository.QueueRepository
	saveErr error
}

func (m *failingQueueRepo) Save(ctx context.Context, q *entity.Queue) error {
	return m.saveErr
}

type customUserRepo struct {
	repository.UserRepository
	getErr error
}

func (m *customUserRepo) GetUserByID(ctx context.Context, id int) (*entity.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.UserRepository.GetUserByID(ctx, id)
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHandleRemoveSong_HTTP(t *testing.T) {
	ctx := context.Background()
	h, userRepo, sessionStore := newTestHandlersWithStore(t)

	guestUser := &entity.User{
		Email:       "guest@urekamedia.vn",
		DisplayName: "GuestOne",
		Role:        entity.RoleGuest,
	}
	err := userRepo.CreateUser(ctx, guestUser)
	if err != nil {
		t.Fatalf("failed to create guest user: %v", err)
	}

	otherGuestUser := &entity.User{
		Email:       "other@urekamedia.vn",
		DisplayName: "GuestTwo",
		Role:        entity.RoleGuest,
	}
	err = userRepo.CreateUser(ctx, otherGuestUser)
	if err != nil {
		t.Fatalf("failed to create other guest: %v", err)
	}

	hostUser := &entity.User{
		Email:       "host@example.com",
		DisplayName: "HostUser",
		Role:        entity.RoleHost,
	}
	err = userRepo.CreateUser(ctx, hostUser)
	if err != nil {
		t.Fatalf("failed to create host user: %v", err)
	}

	guestToken, _, _ := sessionStore.Create(ctx, guestUser.ID, time.Hour)
	_, _, _ = sessionStore.Create(ctx, otherGuestUser.ID, time.Hour)
	hostToken, _, _ := sessionStore.Create(ctx, hostUser.ID, time.Hour)

	setupQueue := func() {
		state, _ := h.queue.GetState(ctx)
		state.Songs = nil
		state.CurrentIndex = -1
		state.Status = entity.StatusIdle
		_ = h.queue.AddSongDirect(ctx, &entity.Song{ID: "vid0", Title: "Song 0", AddedByID: guestUser.ID, AddedBy: guestUser.DisplayName})
		_ = h.queue.AddSongDirect(ctx, &entity.Song{ID: "vid1", Title: "Song 1", AddedByID: otherGuestUser.ID, AddedBy: otherGuestUser.DisplayName})
		_ = h.queue.AddSongDirect(ctx, &entity.Song{ID: "vid2", Title: "Song 2", AddedByID: guestUser.ID, AddedBy: guestUser.DisplayName})
	}

	t.Run("Missing bearer header returns 401", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Malformed bearer header returns 401", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer")
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Invalid token returns 401", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer invalid-token")
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Expired token returns 401", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		expiredToken, _, _ := sessionStore.Create(ctx, guestUser.ID, -time.Hour)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+expiredToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Malformed JSON returns 400", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader([]byte("not json")))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Body is null returns 400", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader([]byte("null")))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Missing index returns 400", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Missing index with requested_by present returns 400", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body := []byte(`{"requested_by": "guest"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Invalid index returns 400", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(10), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Guest ownership failure returns 403", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Guest non-upcoming removal returns 403", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(0), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("requested_by does not override auth role/permissions", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "host@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 even with host requested_by, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Success returns 204 and broadcasts exactly once", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rr.Code)
		}

		if len(mockHub.broadcasts) != 1 {
			t.Fatalf("expected exactly 1 broadcast, got %d", len(mockHub.broadcasts))
		}
		b := mockHub.broadcasts[0]
		if b.eventType != ws.EventSongRemoved {
			t.Errorf("expected event song_removed, got %s", b.eventType)
		}
		data, ok := b.data.(ws.SongRemovedData)
		if !ok {
			t.Fatalf("expected ws.SongRemovedData, got %T", b.data)
		}
		if data.RemovedIndex != 2 {
			t.Errorf("expected removed_index 2, got %d", data.RemovedIndex)
		}
	})

	t.Run("Explicit index zero for authorized host/admin succeeds", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(0)})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+hostToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 1 {
			t.Errorf("expected 1 broadcast, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Valid token plus GetUserByID failure returns 500", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		// Inject failing custom user repo
		originalAuth := h.auth
		defer func() { h.auth = originalAuth }()

		mockRepo := &customUserRepo{
			UserRepository: userRepo,
			getErr:         errors.New("sqlite lookup failed"),
		}
		sessionClock := auth.RealClock{}
		h.auth = auth.NewInteractor(mockRepo, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2)})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
		if rr.Body.String() == "sqlite lookup failed\n" {
			t.Error("should not return raw DB error text in 500 body")
		}
	})

	t.Run("Queue Save failure returns 500 and does not broadcast", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		// Inject failing queue repo
		originalQueue := h.queue
		defer func() { h.queue = originalQueue }()

		state, _ := h.queue.GetState(ctx)
		mockQR := &failingQueueRepo{
			QueueRepository: h.queue.GetRepository(),
			saveErr:         errors.New("sqlite save failure"),
		}
		// In handlers_test.go newTestHandlersWithStore uses youtube service
		// We need to retrieve it or pass nil (not needed for simple removal)
		h.queue = queue.NewInteractor(mockQR, nil)
		// Restore the state in the new interactor
		mockQR.QueueRepository.Save(ctx, state)

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2)})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
		if len(mockHub.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(mockHub.broadcasts))
		}
	})

	t.Run("Spoofed requested_by does not change activity attribution", func(t *testing.T) {
		setupQueue()
		mockHub := &mockBroadcaster{}
		h.hub = mockHub

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2), RequestedBy: "SpoofedUser"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+guestToken)
		rr := httptest.NewRecorder()
		h.HandleRemoveSong(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rr.Code)
		}

		if len(mockHub.broadcasts) != 1 {
			t.Fatalf("expected exactly 1 broadcast, got %d", len(mockHub.broadcasts))
		}
		b := mockHub.broadcasts[0]
		data := b.data.(ws.SongRemovedData)
		if data.Activity.User != guestUser.DisplayName {
			t.Errorf("expected activity user to be '%s', got '%s'", guestUser.DisplayName, data.Activity.User)
		}
	})
}

func TestHandleGoogleLogin_HTTP(t *testing.T) {
	h, userRepo, _ := newTestHandlersWithStore(t)

	// Hijack http.DefaultClient to mock Google token info endpoint
	oldTransport := http.DefaultClient.Transport
	defer func() { http.DefaultClient.Transport = oldTransport }()

	t.Run("Successful login-response serialization and user reload", func(t *testing.T) {
		http.DefaultClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			tokenInfo := `{
				"email": "login-success@urekamedia.vn",
				"name": "Login Success User",
				"picture": "http://example.com/success.jpg",
				"aud": "test-client-id",
				"email_verified": "true"
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(tokenInfo))),
				Header:     make(http.Header),
			}, nil
		})

		body := []byte(`{"id_token":"valid-google-token"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/google", bytes.NewReader(body))
		rr := httptest.NewRecorder()

		h.HandleGoogleLogin(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp["session_token"] == nil || resp["session_token"] == "" {
			t.Error("expected session_token in response")
		}
		if resp["session_expires_at"] == nil || resp["session_expires_at"] == "" {
			t.Error("expected session_expires_at in response")
		}
		if resp["email"] != "login-success@urekamedia.vn" {
			t.Errorf("expected email 'login-success@urekamedia.vn', got %v", resp["email"])
		}
	})

	t.Run("Final-user reload failure returns 500", func(t *testing.T) {
		originalAuth := h.auth
		defer func() { h.auth = originalAuth }()

		mockRepo := &customUserRepo{
			UserRepository: userRepo,
			getErr:         errors.New("reload db failure"),
		}
		sessionClock := auth.RealClock{}
		h.auth = auth.NewInteractor(mockRepo, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, h.auth.GetSessionStore(), sessionClock)

		http.DefaultClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			tokenInfo := `{
				"email": "login-reload-fail@urekamedia.vn",
				"name": "Reload Fail User",
				"picture": "http://example.com/fail.jpg",
				"aud": "test-client-id",
				"email_verified": "true"
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(tokenInfo))),
				Header:     make(http.Header),
			}, nil
		})

		body := []byte(`{"id_token":"valid-google-token"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/google", bytes.NewReader(body))
		rr := httptest.NewRecorder()

		h.HandleGoogleLogin(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})
}
