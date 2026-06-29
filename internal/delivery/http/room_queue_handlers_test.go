package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/roomqueue"
)

// seedUserQueue inserts a user row with the given role and a fixed id.
// Lives in the same per-test schema as newRoomHandlers so foreign keys
// in room_members resolve correctly. Used here in addition to the
// same-name helper in room_handlers_test.go so the queue-handler tests
// can pin explicit ids (42, 200, 999) without colliding on the user
// id sequence. profile_picture is set to an empty string so the NOT-NULL
// tolerant scan in GetUserByID does not fail.
func seedUserQueue(t *testing.T, db *sql.DB, id int, email string, role entity.Role) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO users (id, email, display_name, profile_picture, role, priority_balance, created_at, updated_at)
		 VALUES ($1, $2, $2, '', $3, 0, NOW(), NOW())`,
		id, email, role,
	); err != nil {
		t.Fatalf("seed user %d (%s): %v", id, email, err)
	}
}

// newRoomQueueHandlers builds RoomQueueHandlers against the same
// per-test schema newRoomHandlers creates. The schema is dropped and
// the *sql.DB closed on test cleanup. PG connectivity is optional:
// when LMQ_TEST_DATABASE_URL is unreachable the calling test is
// skipped by newRoomHandlers.
func newRoomQueueHandlers(t *testing.T) (*RoomQueueHandlers, *sql.DB, func()) {
	t.Helper()
	_, _, db := newRoomHandlers(t)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	authI := auth.NewInteractor(
		persistence.NewPostgresUserRepository(db),
		"", nil, nil, nil, nil,
	)
	rqh := NewRoomQueueHandlers(roomqueue.NewInteractor(roomRepo, queueRepo, nil), authI)
	cleanup := func() {
		// schema drop + db close registered by newRoomHandlers' t.Cleanup
	}
	return rqh, db, cleanup
}

// mustRoomIDQueue looks up the numeric room id for slug via the same
// scoped *sql.DB the rest of the test uses.
func mustRoomIDQueue(t *testing.T, db *sql.DB, slug string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM rooms WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("lookup room %q: %v", slug, err)
	}
	return id
}

func TestRoomQueue_GetQueue_ReturnsEmptyForNewRoom(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-get@example.com", entity.RoleHost)
	// Create the room via the repo so user 42 is the host member.
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-rest", "RQRest", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/rq-rest/queue", nil)
	rr := httptest.NewRecorder()
	rqh.HandleGetRoomQueue(rr, req, "rq-rest", 42)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got entity.Queue
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if len(got.Songs) != 0 {
		t.Errorf("expected empty queue, got %d songs", len(got.Songs))
	}
}

func TestRoomQueue_AddSong_ReturnsInsertedSong(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-add@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-rest-add", "RQAdd", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}

	body := []byte(`{"metadata":{"id":"vid-add","title":"Add Me","artist":"Tester","duration":120,"thumbnail":"","url":"https://example/vid-add"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-rest-add/queue/add", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleAddRoomSong(rr, req, "rq-rest-add", 42)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got entity.Song
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode song: %v", err)
	}
	if got.ID != "vid-add" {
		t.Errorf("expected song id vid-add, got %s", got.ID)
	}
	if got.AddedByID != 42 {
		t.Errorf("expected AddedByID=42, got %d", got.AddedByID)
	}
}

func TestRoomQueue_RemoveSong_AsHost_Succeeds(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-rm@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-rest-rm", "RQRM", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}

	// Host adds a song at index 0 so the subsequent remove has
	// something to delete.
	addBody := []byte(`{"metadata":{"id":"vid-rm","title":"Remove Me","url":"https://example/rm"}}`)
	addReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-rest-rm/queue/add", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addRR := httptest.NewRecorder()
	rqh.HandleAddRoomSong(addRR, addReq, "rq-rest-rm", 42)
	if addRR.Code != http.StatusOK {
		t.Fatalf("add pre-condition: expected 200, got %d body=%s", addRR.Code, addRR.Body.String())
	}

	// Host removes index 0.
	rmBody := []byte(`{"index":0}`)
	rmReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-rest-rm/queue/remove", bytes.NewReader(rmBody))
	rmReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRemoveRoomSong(rr, rmReq, "rq-rest-rm", 42)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("remove expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_ClearQueue_AsGuest_Returns403(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-clr@example.com", entity.RoleHost)
	seedUserQueue(t, db, 200, "guest-rq-clr@example.com", entity.RoleGuest)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-rest-clr", "RQClear", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, "rq-rest-clr")
	if err := roomRepo.AddMember(ctx, roomID, 200, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
		t.Fatalf("add guest member: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-rest-clr/queue/clear", nil)
	rr := httptest.NewRecorder()
	rqh.HandleClearRoomQueue(rr, req, "rq-rest-clr", 200)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("guest clear expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_GetQueue_AsNonMember_Returns403(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-nm@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-rest-nm", "RQNM", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	// user 999 is NOT seeded as a user and NOT added as a room member.

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/rq-rest-nm/queue", nil)
	rr := httptest.NewRecorder()
	rqh.HandleGetRoomQueue(rr, req, "rq-rest-nm", 999)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-member get expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_AddSong_RequiresAuthenticatedActor(t *testing.T) {
	rqh, _, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	body := []byte(`{"metadata":{"id":"any","title":"T","url":"https://x"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/some-room/queue/add", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	// actorUserID=0 → handler must reject before touching DB.
	rqh.HandleAddRoomSong(rr, req, "some-room", 0)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// recordingRoomBroadcaster captures broadcaster calls without a real
// WebSocket. The handler tests assert it was invoked on success and
// NOT invoked on error paths.
type recordingRoomBroadcaster struct {
	mu          sync.Mutex
	syncCalls   []string
	addCalls    []string
	removeCalls []int
	clearCalls  []string
}

func (r *recordingRoomBroadcaster) BroadcastRoomQueueSync(slug string, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncCalls = append(r.syncCalls, slug)
}
func (r *recordingRoomBroadcaster) BroadcastRoomQueueSongAdded(slug string, _ entity.Song, _ int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addCalls = append(r.addCalls, slug)
}
func (r *recordingRoomBroadcaster) BroadcastRoomQueueSongRemoved(slug string, idx int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeCalls = append(r.removeCalls, idx)
}
func (r *recordingRoomBroadcaster) BroadcastRoomQueueCleared(slug string, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearCalls = append(r.clearCalls, slug)
}

func TestRoomQueue_AddSong_BroadcastsSongAdded(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-bc-add@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-bc-add", "RQBcAdd", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"metadata":{"id":"vid-bc-1","title":"BC Add","url":"https://x/1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-bc-add/queue/add", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleAddRoomSong(rr, req, "rq-bc-add", 42)
	if rr.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.addCalls) != 1 || bc.addCalls[0] != "rq-bc-add" {
		t.Fatalf("expected 1 add broadcast for rq-bc-add, got %+v", bc.addCalls)
	}
}

func TestRoomQueue_RemoveSong_BroadcastsSongRemoved(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-bc-rm@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-bc-rm", "RQBcRm", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}

	addBody := []byte(`{"metadata":{"id":"vid-bc-rm","title":"R","url":"https://x/r"}}`)
	addReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-bc-rm/queue/add", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addRR := httptest.NewRecorder()
	rqh.HandleAddRoomSong(addRR, addReq, "rq-bc-rm", 42)
	if addRR.Code != http.StatusOK {
		t.Fatalf("seed add: %d body=%s", addRR.Code, addRR.Body.String())
	}

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	rmBody := []byte(`{"index":0}`)
	rmReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-bc-rm/queue/remove", bytes.NewReader(rmBody))
	rmReq.Header.Set("Content-Type", "application/json")
	rmRR := httptest.NewRecorder()
	rqh.HandleRemoveRoomSong(rmRR, rmReq, "rq-bc-rm", 42)
	if rmRR.Code != http.StatusNoContent {
		t.Fatalf("remove: expected 204, got %d", rmRR.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.removeCalls) != 1 || bc.removeCalls[0] != 0 {
		t.Fatalf("expected 1 remove broadcast at idx 0, got %+v", bc.removeCalls)
	}
}

func TestRoomQueue_ClearQueue_BroadcastsCleared(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-bc-clr@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-bc-clr", "RQBcClr", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-bc-clr/queue/clear", nil)
	rr := httptest.NewRecorder()
	rqh.HandleClearRoomQueue(rr, req, "rq-bc-clr", 42)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("clear: expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.clearCalls) != 1 || bc.clearCalls[0] != "rq-bc-clr" {
		t.Fatalf("expected 1 clear broadcast for rq-bc-clr, got %+v", bc.clearCalls)
	}
}

// Error paths must NOT broadcast.
func TestRoomQueue_RemoveSong_NoBroadcastOnForbidden(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	seedUserQueue(t, db, 42, "host-rq-bc-fb@example.com", entity.RoleHost)
	seedUserQueue(t, db, 200, "guest-rq-bc-fb@example.com", entity.RoleGuest)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-bc-fb", "RQBcFb", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, "rq-bc-fb")
	if err := roomRepo.AddMember(ctx, roomID, 200, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
		t.Fatalf("add guest: %v", err)
	}

	addBody := []byte(`{"metadata":{"id":"vid-fb","title":"F","url":"https://x/f"}}`)
	addReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-bc-fb/queue/add", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	rqh.HandleAddRoomSong(httptest.NewRecorder(), addReq, "rq-bc-fb", 42)

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	rmBody := []byte(`{"index":0}`)
	rmReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-bc-fb/queue/remove", bytes.NewReader(rmBody))
	rmReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleRemoveRoomSong(rr, rmReq, "rq-bc-fb", 200)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.removeCalls) != 0 {
		t.Errorf("expected NO remove broadcast on forbidden, got %d", len(bc.removeCalls))
	}
}

// stubYTHandler is a service.YouTubeService used to drive the bare-URL
// branch of HandleAddRoomSong from HTTP tests without yt-dlp.
type stubYTHandler struct {
	song *entity.Song
}

func (s *stubYTHandler) FetchMetadata(ctx context.Context, url string) (*entity.Song, error) {
	return s.song, nil
}
func (s *stubYTHandler) SearchYouTube(ctx context.Context, query string, maxResults int) ([]*entity.SearchResult, error) {
	return nil, nil
}

// TestRoomQueue_AddSong_URLOnly_PersistsServerAttribution covers task
// (2) above: a bare-URL add (no metadata body) must result in a song
// whose AddedByID / AddedBy are server-resolved from the bearer token,
// and a guest must be able to remove their own upcoming URL-added
// song (proving attribution is persisted and consulted).
func TestRoomQueue_AddSong_URLOnly_PersistsServerAttribution(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	// Need both the host (42) for room creation and the guest (999)
	// for the URL add + own-removal assertions.
	seedUserQueue(t, db, 42, "host-rq-url@example.com", entity.RoleHost)
	seedUserQueue(t, db, 999, "guest-rq-url@example.com", entity.RoleGuest)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-rest-url", "RQUrl", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, "rq-rest-url")
	if err := roomRepo.AddMember(ctx, roomID, 999, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
		t.Fatalf("add guest: %v", err)
	}

	// Drive the URL-only branch by attaching a stub YouTubeService that
	// returns a song. The constructor uses nil for youtube; we attach
	// via SetYouTube for this test only.
	rqh.inter.SetYouTube(&stubYTHandler{song: &entity.Song{
		ID: "yt-url-1", Title: "FromURL", Artist: "YT", Duration: 60, Thumbnail: "t",
		URL: "https://youtube.com/watch?v=yt-url-1",
	}})

	// Guest 999 issues a URL-only add. The body MUST NOT contain
	// added_by / added_by_id / user_id / display_name — those are
	// ignored.
	body := []byte(`{"url":"https://youtube.com/watch?v=yt-url-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-rest-url/queue/add", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleAddRoomSong(rr, req, "rq-rest-url", 999)
	if rr.Code != http.StatusOK {
		t.Fatalf("url add: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got entity.Song
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode song: %v", err)
	}
	if got.AddedByID != 999 {
		t.Errorf("expected AddedByID=999 from server-resolved actor, got %d", got.AddedByID)
	}
	if got.AddedBy == "" || got.AddedBy == "user-999" {
		t.Errorf("expected AddedBy populated from server-side display name/email, got %q", got.AddedBy)
	}

	// Persisted queue must carry the server-resolved attribution.
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	persisted, err := queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load persisted queue: %v", err)
	}
	if len(persisted.Songs) != 1 {
		t.Fatalf("expected 1 persisted song, got %d", len(persisted.Songs))
	}
	if persisted.Songs[0].AddedByID != 999 {
		t.Errorf("persisted song: expected AddedByID=999, got %d", persisted.Songs[0].AddedByID)
	}
	if persisted.Songs[0].AddedBy == "" {
		t.Errorf("persisted song: expected AddedBy populated, got empty")
	}

	// Guest 999 adds a SECOND URL-only song so we have an upcoming one
	// to remove.
	rqh.inter.SetYouTube(&stubYTHandler{song: &entity.Song{
		ID: "yt-url-2", Title: "FromURL2", Artist: "YT", Duration: 60, Thumbnail: "t",
		URL: "https://youtube.com/watch?v=yt-url-2",
	}})
	body2 := []byte(`{"url":"https://youtube.com/watch?v=yt-url-2"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-rest-url/queue/add", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	rqh.HandleAddRoomSong(rr2, req2, "rq-rest-url", 999)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second url add: expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	// Now the guest removes their own upcoming song at index 1. The
	// ownership check uses AddedByID == actorUserID; this succeeds
	// only because the server-resolved attribution was persisted.
	rmBody := []byte(`{"index":1}`)
	rmReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-rest-url/queue/remove", bytes.NewReader(rmBody))
	rmReq.Header.Set("Content-Type", "application/json")
	rmRR := httptest.NewRecorder()
	rqh.HandleRemoveRoomSong(rmRR, rmReq, "rq-rest-url", 999)
	if rmRR.Code != http.StatusNoContent {
		t.Fatalf("guest remove own upcoming: expected 204, got %d body=%s", rmRR.Code, rmRR.Body.String())
	}
	after, err := queueRepo.Load(ctx, roomID)
	if err != nil {
		t.Fatalf("load after remove: %v", err)
	}
	if len(after.Songs) != 1 {
		t.Fatalf("expected 1 persisted song after remove, got %d (songs=%+v)", len(after.Songs), after.Songs)
	}
	if after.Songs[0].ID != "yt-url-1" {
		t.Errorf("expected only yt-url-1 to remain, got %s", after.Songs[0].ID)
	}
}
