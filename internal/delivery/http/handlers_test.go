package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/infrastructure/session"
	"local-music-queue/internal/infrastructure/youtube"
	"local-music-queue/internal/usecase/activity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/autoqueue"
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

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testRepo bundles the per-test postgres DB and the repos derived from it.
type testRepo struct {
	db       *sql.DB
	queue    repository.QueueRepository
	user     repository.UserRepository
	autoQueue *persistence.PostgresAutoQueueRepository
}

// newTestRepo opens a per-test throwaway PG schema with migrations applied.
func newTestRepo(t *testing.T) *testRepo {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := root.PingContext(ctx); err != nil {
		t.Skipf("postgres unavailable (ping): %v", err)
	}
	schema := fmt.Sprintf("lmq_http_test_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	scoped, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() {
		drop, _ := sql.Open("pgx", dsn)
		if drop != nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
		_ = scoped.Close()
	})
	if err := persistence.RunEmbeddedMigrationsUp(scoped); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return &testRepo{
		db:        scoped,
		queue:     persistence.NewPostgresRepository(scoped),
		user:      persistence.NewPostgresUserRepository(scoped),
		autoQueue: persistence.NewPostgresAutoQueueRepository(scoped),
	}
}

// newTestHandlers creates Handlers wired to a per-test PostgreSQL schema and
// a fake yt-dlp.
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	dir := t.TempDir()
	r := newTestRepo(t)

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

	queueInteractor := queue.NewInteractor(r.queue, ytSvc)
	sessionClock := auth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)
	authInteractor := auth.NewInteractor(r.user, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)
	actInteractor := activity.NewInteractor(r.queue)
	priorityInteractor := priority.NewInteractor(r.user, r.queue)
	voteInteractor := vote.NewInteractor(r.queue, r.user, 0)
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

	tc := newTestContext(t)

	body, _ := json.Marshal(AddSongRequest{URL: "https://youtube.com/watch?v=test", AddedBy: "Alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
	req = withUserCtx(req, tc.guestUser)
	rr := httptest.NewRecorder()

	tc.handlers.HandleAddSong(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAddSong_BadJSON(t *testing.T) {
	tc := newTestContext(t)

	req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader([]byte("bad")))
	req = withUserCtx(req, tc.guestUser)
	rr := httptest.NewRecorder()

	tc.handlers.HandleAddSong(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// --- SkipSong Tests ---

func TestHandleSkipSong_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	tc := newTestContext(t)

	// First add two songs (any auth role can add; use host for simplicity)
	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(AddSongRequest{URL: "url", AddedBy: "HostUser"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
		req = withUserCtx(req, tc.hostUser)
		rr := httptest.NewRecorder()
		tc.handlers.HandleAddSong(rr, req)
	}

	// Now skip with a host user (playback control is host/admin)
	body, _ := json.Marshal(SkipRequest{RequestedBy: "HostUser"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/skip", bytes.NewReader(body))
	req = withUserCtx(req, tc.hostUser)
	rr := httptest.NewRecorder()

	tc.handlers.HandleSkipSong(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleSkipSong_Error(t *testing.T) {
	tc := newTestContext(t)

	// Skip on empty queue should fail. The handler needs an authenticated
	// user (host) to reach the interactor. newTestContext seeds 3 songs so
	// the empty-queue error path requires a fresh repo. Use the standard
	// path: skip the current song; the queue only has the current + 2
	// upcoming, so this advances to the next song (204), not 500. To still
	// exercise the "no next song" error path, skip until the queue is
	// exhausted.
	for {
		body, _ := json.Marshal(SkipRequest{RequestedBy: "HostUser"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/skip", bytes.NewReader(body))
		req = withUserCtx(req, tc.hostUser)
		rr := httptest.NewRecorder()
		tc.handlers.HandleSkipSong(rr, req)
		if rr.Code != http.StatusNoContent {
			// We expect a final 500 when there is no next song.
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("expected 500 after exhausting queue, got %d; body: %s", rr.Code, rr.Body.String())
			}
			return
		}
	}
}

// --- SetStatus Tests ---

func TestHandleSetStatus_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	tc := newTestContext(t)

	// Add a song first
	addBody, _ := json.Marshal(AddSongRequest{URL: "url", AddedBy: "HostUser"})
	addReq := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(addBody))
	addReq = withUserCtx(addReq, tc.hostUser)
	tc.handlers.HandleAddSong(httptest.NewRecorder(), addReq)

	// Set status (host)
	body, _ := json.Marshal(StatusRequest{Status: entity.StatusPaused, RequestedBy: "HostUser"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/status", bytes.NewReader(body))
	req = withUserCtx(req, tc.hostUser)
	rr := httptest.NewRecorder()

	tc.handlers.HandleSetStatus(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleSetStatus_Error(t *testing.T) {
	tc := newTestContext(t)

	// newTestContext seeds 3 songs with vid0 currently playing, so a
	// transition to "playing" is valid and returns 204. To exercise the
	// error path we need an empty queue. We use a fresh repo for that.
	r := newTestRepo(t)
	queueInteractor := queue.NewInteractor(r.queue, nil)
	authInteractor := auth.NewInteractor(r.user, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, tc.sessionStore, tc.sessionClock)
	actInteractor := activity.NewInteractor(r.queue)
	priorityInteractor := priority.NewInteractor(r.user, r.queue)
	voteInteractor := vote.NewInteractor(r.queue, r.user, 0)
	handlers := NewHandlers(queueInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, &mockBroadcaster{})

	// Empty queue: "playing" is an invalid transition -> 500.
	body, _ := json.Marshal(StatusRequest{Status: entity.StatusPlaying, RequestedBy: "HostUser"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/status", bytes.NewReader(body))
	req = withUserCtx(req, tc.hostUser)
	rr := httptest.NewRecorder()

	handlers.HandleSetStatus(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on empty queue, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// --- SearchYouTube Tests ---

func TestHandleSearchYouTube_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}

	dir := t.TempDir()
	r := newTestRepo(t)

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

	queueInteractor := queue.NewInteractor(r.queue, ytSvc)
	sessionClock := auth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)
	authInteractor := auth.NewInteractor(r.user, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)
	actInteractor := activity.NewInteractor(r.queue)
	priorityInteractor := priority.NewInteractor(r.user, r.queue)
	voteInteractor := vote.NewInteractor(r.queue, r.user, 0)
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
	r := newTestRepo(t)

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

	queueInteractor := queue.NewInteractor(r.queue, ytSvc)
	sessionClock := auth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)
	authInteractor := auth.NewInteractor(r.user, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)
	actInteractor := activity.NewInteractor(r.queue)
	priorityInteractor := priority.NewInteractor(r.user, r.queue)
	voteInteractor := vote.NewInteractor(r.queue, r.user, 0)
	hub := ws.NewHub(queueInteractor.GetState)
	go hub.Run()

	return NewHandlers(queueInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub), r.user, sessionStore
}

func intPtr(v int) *int {
	return &v
}

// withUserCtx injects a resolved *entity.User into the request context so
// privileged handlers (which call UserFromCtx) see an authenticated caller
// even when a test invokes the handler directly without going through the
// RequireAuth middleware. This keeps existing tests focused on handler
// behavior; the middleware itself is covered by auth_middleware_test.go.
func withUserCtx(req *http.Request, user *entity.User) *http.Request {
	ctx := context.WithValue(req.Context(), userKey{}, user)
	return req.WithContext(ctx)
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

type testContext struct {
	t                 *testing.T
	dir               string
	repo              *testRepo
	userRepo          repository.UserRepository
	sessionClock      auth.RealClock
	sessionStore      auth.SessionStore
	queueInteractor   *queue.Interactor
	authInteractor    *auth.Interactor
	handlers          *Handlers
	autoQueueHandlers *AutoQueueHandlers
	broadcaster       *mockBroadcaster
	guestUser         *entity.User
	otherGuestUser    *entity.User
	hostUser          *entity.User
	adminUser         *entity.User
	guestToken        string
	otherGuestToken   string
	hostToken         string
	adminToken        string
}

func newTestContext(t *testing.T) *testContext {
	t.Helper()
	dir := t.TempDir()
	r := newTestRepo(t)

	var ytSvc *youtube.YTDLPService
	if runtime.GOOS != "windows" {
		script := filepath.Join(dir, "fake-ytdlp")
		content := `#!/bin/sh
cat <<'EOF'
{"id":"test123","title":"Test Song","uploader":"Artist","duration":180.0,"thumbnail":"thumb.jpg","webpage_url":"https://youtube.com/watch?v=test123"}
EOF
`
		if err := os.WriteFile(script, []byte(content), 0755); err != nil {
			t.Fatalf("failed to write fake-ytdlp script: %v", err)
		}
		ytSvc = youtube.NewYTDLPService(script)
	} else {
		ytSvc = youtube.NewYTDLPService("echo")
	}

	queueInteractor := queue.NewInteractor(r.queue, ytSvc)
	sessionClock := auth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)
	authInteractor := auth.NewInteractor(r.user, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)
	actInteractor := activity.NewInteractor(r.queue)
	priorityInteractor := priority.NewInteractor(r.user, r.queue)
	voteInteractor := vote.NewInteractor(r.queue, r.user, 0)
	// Auto-queue interactor needs a real fetcher and the add-song callback
	// wired up so SetEnabled / GetConfig work end-to-end in R05 tests.
	autoQueueInteractor := autoqueue.NewInteractor(r.autoQueue, r.queue, nil)
	autoQueueInteractor.SetAddAutoQueueSongFunc(func(ctx context.Context, song *entity.Song, expectedSourceSongID string) (*autoqueue.AddSongResult, error) {
		res, err := queueInteractor.AddAutoQueueSong(ctx, song, expectedSourceSongID)
		if err != nil {
			return nil, err
		}
		return &autoqueue.AddSongResult{
			Song:         res.Song,
			Position:     res.Position,
			CurrentIndex: res.CurrentIndex,
			CurrentSong:  res.CurrentSong,
			Status:       res.Status,
			Elapsed:      res.Elapsed,
			Activity:     res.Activity,
		}, nil
	})
	mockHub := &mockBroadcaster{}
	// AutoQueueHandlers needs a concrete *ws.Hub for the broadcaster
	// reference. The test asserts zero broadcasts on auth rejection via
	// the mock broadcaster wired into Handlers; the auto-queue handler
	// uses this separate hub for the auto_queue_config_changed
	// broadcast which the auto-queue tests inspect.
	realHub := ws.NewHub(queueInteractor.GetState)
	go realHub.Run()
	autoQueueHandlers := NewAutoQueueHandlers(autoQueueInteractor, realHub)

	handlers := NewHandlers(queueInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, mockHub)

	ctx := context.Background()

	userRepo := r.user
	guestUser := &entity.User{
		Email:       "guest@urekamedia.vn",
		DisplayName: "GuestOne",
		Role:        entity.RoleGuest,
	}
	if err := userRepo.CreateUser(ctx, guestUser); err != nil {
		t.Fatalf("failed to create guest user: %v", err)
	}

	otherGuestUser := &entity.User{
		Email:       "other@urekamedia.vn",
		DisplayName: "GuestTwo",
		Role:        entity.RoleGuest,
	}
	if err := userRepo.CreateUser(ctx, otherGuestUser); err != nil {
		t.Fatalf("failed to create other guest: %v", err)
	}

	hostUser := &entity.User{
		Email:       "host@example.com",
		DisplayName: "HostUser",
		Role:        entity.RoleHost,
	}
	if err := userRepo.CreateUser(ctx, hostUser); err != nil {
		t.Fatalf("failed to create host user: %v", err)
	}

	adminUser := &entity.User{
		Email:       "admin@example.com",
		DisplayName: "AdminUser",
		Role:        entity.RoleAdmin,
	}
	if err := userRepo.CreateUser(ctx, adminUser); err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}

	guestToken, _, err := sessionStore.Create(ctx, guestUser.ID, time.Hour)
	if err != nil {
		t.Fatalf("failed to create guest token: %v", err)
	}

	otherGuestToken, _, err := sessionStore.Create(ctx, otherGuestUser.ID, time.Hour)
	if err != nil {
		t.Fatalf("failed to create other guest token: %v", err)
	}

	hostToken, _, err := sessionStore.Create(ctx, hostUser.ID, time.Hour)
	if err != nil {
		t.Fatalf("failed to create host token: %v", err)
	}

	adminToken, _, err := sessionStore.Create(ctx, adminUser.ID, time.Hour)
	if err != nil {
		t.Fatalf("failed to create admin token: %v", err)
	}

	// Deterministic Queue setup:
	// Song 0: guestUser owned
	// Song 1: otherGuestUser owned
	// Song 2: guestUser owned
	if _, err = queueInteractor.AddSongDirect(ctx, &entity.Song{ID: "vid0", Title: "Song 0", AddedByID: guestUser.ID, AddedBy: guestUser.DisplayName}); err != nil {
		t.Fatalf("failed to add song 0: %v", err)
	}
	if _, err = queueInteractor.AddSongDirect(ctx, &entity.Song{ID: "vid1", Title: "Song 1", AddedByID: otherGuestUser.ID, AddedBy: otherGuestUser.DisplayName}); err != nil {
		t.Fatalf("failed to add song 1: %v", err)
	}
	if _, err = queueInteractor.AddSongDirect(ctx, &entity.Song{ID: "vid2", Title: "Song 2", AddedByID: guestUser.ID, AddedBy: guestUser.DisplayName}); err != nil {
		t.Fatalf("failed to add song 2: %v", err)
	}

	return &testContext{
		t:                 t,
		dir:               dir,
		repo:              r,
		userRepo:          userRepo,
		sessionClock:      sessionClock,
		sessionStore:      sessionStore,
		queueInteractor:   queueInteractor,
		authInteractor:    authInteractor,
		handlers:          handlers,
		autoQueueHandlers: autoQueueHandlers,
		broadcaster:       mockHub,
		guestUser:         guestUser,
		otherGuestUser:    otherGuestUser,
		hostUser:          hostUser,
		adminUser:         adminUser,
		guestToken:        guestToken,
		otherGuestToken:   otherGuestToken,
		hostToken:         hostToken,
		adminToken:        adminToken,
	}
}

func TestHandleRemoveSong_HTTP(t *testing.T) {
	ctx := context.Background()

	t.Run("Missing bearer header returns 401", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Malformed bearer header returns 401", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer")
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Invalid token returns 401", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer invalid-token")
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Expired token returns 401", func(t *testing.T) {
		tc := newTestContext(t)
		expiredToken, _, err := tc.sessionStore.Create(ctx, tc.guestUser.ID, -time.Hour)
		if err != nil {
			t.Fatalf("failed to create expired token: %v", err)
		}
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+expiredToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Malformed JSON returns 400", func(t *testing.T) {
		tc := newTestContext(t)
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader([]byte("not json")))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Body is null returns 400", func(t *testing.T) {
		tc := newTestContext(t)
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader([]byte("null")))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Missing index returns 400", func(t *testing.T) {
		tc := newTestContext(t)
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Missing index with requested_by present returns 400", func(t *testing.T) {
		tc := newTestContext(t)
		body := []byte(`{"requested_by": "guest"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Invalid index returns 400", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(10), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Guest ownership failure returns 403", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Guest non-upcoming removal returns 403", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(0), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("requested_by does not override auth role/permissions", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1), RequestedBy: "host@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 even with host requested_by, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Success returns 204 and broadcasts exactly once", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2), RequestedBy: "guest"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rr.Code)
		}

		if len(tc.broadcaster.broadcasts) != 1 {
			t.Fatalf("expected exactly 1 broadcast, got %d", len(tc.broadcaster.broadcasts))
		}
		b := tc.broadcaster.broadcasts[0]
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
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(0)})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.hostToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 1 {
			t.Errorf("expected 1 broadcast, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Valid token plus GetUserByID failure returns 500", func(t *testing.T) {
		tc := newTestContext(t)
		// Inject failing custom user repo
		originalAuth := tc.handlers.auth
		defer func() { tc.handlers.auth = originalAuth }()

		mockRepo := &customUserRepo{
			UserRepository: tc.userRepo,
			getErr:         errors.New("sqlite lookup failed"),
		}
		tc.handlers.auth = auth.NewInteractor(mockRepo, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, tc.sessionStore, tc.sessionClock)

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2)})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
		if rr.Body.String() == "sqlite lookup failed\n" {
			t.Error("should not return raw DB error text in 500 body")
		}
	})

	t.Run("Queue Save failure returns 500 and does not broadcast", func(t *testing.T) {
		tc := newTestContext(t)
		state, err := tc.queueInteractor.GetState(ctx)
		if err != nil {
			t.Fatalf("failed to get state: %v", err)
		}
		mockQR := &failingQueueRepo{
			QueueRepository: tc.repo.queue,
			saveErr:         errors.New("save failure"),
		}
		tc.handlers.queue = queue.NewInteractor(mockQR, nil)
		err = mockQR.QueueRepository.Save(ctx, state)
		if err != nil {
			t.Fatalf("failed to restore queue state in repo: %v", err)
		}

		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2)})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
		if len(tc.broadcaster.broadcasts) != 0 {
			t.Errorf("expected 0 broadcasts, got %d", len(tc.broadcaster.broadcasts))
		}
	})

	t.Run("Spoofed requested_by does not change activity attribution", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(2), RequestedBy: "SpoofedUser"})
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.guestToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rr.Code)
		}

		if len(tc.broadcaster.broadcasts) != 1 {
			t.Fatalf("expected exactly 1 broadcast, got %d", len(tc.broadcaster.broadcasts))
		}
		b := tc.broadcaster.broadcasts[0]
		data := b.data.(ws.SongRemovedData)
		if data.Activity.User != tc.guestUser.DisplayName {
			t.Errorf("expected activity user to be '%s', got '%s'", tc.guestUser.DisplayName, data.Activity.User)
		}
	})

	t.Run("Admin can remove another user's song and broadcasts exactly once", func(t *testing.T) {
		tc := newTestContext(t)
		body, _ := json.Marshal(RemoveSongRequest{Index: intPtr(1)}) // otherGuestUser's song
		req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tc.adminToken)
		rr := httptest.NewRecorder()
		tc.handlers.HandleRemoveSong(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		if len(tc.broadcaster.broadcasts) != 1 {
			t.Fatalf("expected exactly 1 broadcast, got %d", len(tc.broadcaster.broadcasts))
		}
		b := tc.broadcaster.broadcasts[0]
		if b.eventType != ws.EventSongRemoved {
			t.Errorf("expected event song_removed, got %s", b.eventType)
		}
		data, ok := b.data.(ws.SongRemovedData)
		if !ok {
			t.Fatalf("expected ws.SongRemovedData, got %T", b.data)
		}
		if data.RemovedIndex != 1 {
			t.Errorf("expected removed_index 1, got %d", data.RemovedIndex)
		}
	})
}

func TestHandleVoteSkip_LiveUpdateIsNotInitialSync(t *testing.T) {
	tc := newTestContext(t)
	tc.broadcaster.connectedCount = 3

	// R05: server-resolved identity. The handler ignores body user_id /
	// user_role; the request must inject the guest user so the canVote
	// role check passes.
	body, _ := json.Marshal(VoteSkipRequest{
		UserID:   tc.guestUser.ID,
		UserRole: string(entity.RoleGuest),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/vote/skip", bytes.NewReader(body))
	req = withUserCtx(req, tc.guestUser)
	rr := httptest.NewRecorder()

	tc.handlers.HandleVoteSkip(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", rr.Code)
	}
	if len(tc.broadcaster.broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(tc.broadcaster.broadcasts))
	}
	broadcast := tc.broadcaster.broadcasts[0]
	if broadcast.eventType != ws.EventVoteUpdated {
		t.Fatalf("expected %q broadcast, got %q", ws.EventVoteUpdated, broadcast.eventType)
	}
	data, ok := broadcast.data.(ws.VoteUpdatedData)
	if !ok {
		t.Fatalf("expected VoteUpdatedData, got %T", broadcast.data)
	}
	if data.InitialSync {
		t.Fatal("expected live vote update to omit initial_sync")
	}
	if data.Session == nil || data.Session.ID != "skip:vid0" {
		t.Fatalf("expected live skip session for vid0, got %#v", data.Session)
	}
}

func TestHandleGoogleLogin_HTTP(t *testing.T) {
	h, userRepo, sessionStore := newTestHandlersWithStore(t)

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
		h.auth = auth.NewInteractor(mockRepo, "test-client-id", []string{"host@example.com"}, []string{"admin@example.com"}, sessionStore, sessionClock)

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

// TestHandleAddSong_BroadcastCarriesAuthoritativeFields covers Issue #8 case 1:
// HandleAddSong must broadcast SongAddedData with current_index, current_song,
// status, and elapsed taken from the snapshot captured inside the locked
// AddSong mutation — not from a follow-up GetState reload that could race.
func TestHandleAddSong_BroadcastCarriesAuthoritativeFields(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}

	tc := newTestContext(t)

	// Seed an existing playing song so the new add is appended at position 3
	// (newTestContext already added vid0,vid1,vid2 with current playing vid0).
	body, _ := json.Marshal(AddSongRequest{
		URL:     "https://youtube.com/watch?v=newone",
		AddedBy: "Alice",
		Metadata: &entity.SearchResult{
			ID:    "newone",
			Title: "Brand New",
			URL:   "https://youtube.com/watch?v=newone",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
	req = withUserCtx(req, tc.hostUser)
	rr := httptest.NewRecorder()
	tc.handlers.HandleAddSong(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	if len(tc.broadcaster.broadcasts) == 0 {
		t.Fatal("expected at least one broadcast")
	}
	last := tc.broadcaster.broadcasts[len(tc.broadcaster.broadcasts)-1]
	if last.eventType != ws.EventSongAdded {
		t.Fatalf("expected event %q, got %q", ws.EventSongAdded, last.eventType)
	}
	data, ok := last.data.(ws.SongAddedData)
	if !ok {
		t.Fatalf("expected ws.SongAddedData, got %T", last.data)
	}

	if data.Song.ID != "newone" {
		t.Errorf("expected song id 'newone', got %q", data.Song.ID)
	}
	if data.Position != 3 {
		t.Errorf("expected Position 3, got %d", data.Position)
	}
	if data.CurrentIndex != 0 {
		t.Errorf("expected CurrentIndex 0 (vid0 still playing), got %d", data.CurrentIndex)
	}
	if data.CurrentSong == nil || data.CurrentSong.ID != "vid0" {
		t.Errorf("expected CurrentSong 'vid0', got %+v", data.CurrentSong)
	}
	if data.Status != entity.StatusPlaying {
		t.Errorf("expected Status playing, got %s", data.Status)
	}
	// R05: server-resolved identity. The handler ignores the body AddedBy
	// "Alice" and uses the authenticated user's display name (HostUser).
	if data.Activity.Type != entity.ActivitySongAdded || data.Activity.User != "HostUser" {
		t.Errorf("expected ActivitySongAdded for HostUser, got %+v", data.Activity)
	}
}
