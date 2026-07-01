package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/room"
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
	mu                     sync.Mutex
	syncCalls              []string
	addCalls               []string
	removeCalls            []int
	clearCalls             []string
	prioCalls              []recordingPrioCall
	statusCalls            []recordingStatusCall
	elapsedCalls           []recordingElapsedCall
	advancedCalls          []recordingAdvancedCall
	volumeCalls            []recordingVolumeCall
	previousCalls          []recordingPreviousCall
	autoQueueAddedCalls    []recordingAutoQueueAddedCall
	autoQueueConfigChangedCalls []recordingAutoQueueConfigChangedCall
}

type recordingPrioCall struct {
	slug string
	from int
	to   int
	song entity.Song
}

type recordingStatusCall struct {
	slug    string
	status  entity.PlaybackStatus
	elapsed int
}

type recordingElapsedCall struct {
	slug    string
	elapsed int
}

type recordingAdvancedCall struct {
	slug    string
	reason  string
	prev    int
	next    int
	song    *entity.Song
	status  entity.PlaybackStatus
	elapsed int
}

// recordingVolumeCall: append {slug, direction} whenever
// BroadcastRoomPlaybackVolumeChanged is invoked.
type recordingVolumeCall struct {
	slug      string
	direction string
}

// recordingPreviousCall: append {slug, prev, next, song, status, elapsed}
// whenever BroadcastRoomPlaybackSongPrevious is invoked. R09d.
type recordingPreviousCall struct {
	slug    string
	prev    int
	next    int
	song    *entity.Song
	status  entity.PlaybackStatus
	elapsed int
}

// recordingAutoQueueAddedCall: append {slug, song, sourceTitle,
// currentIndex, currentSong, status, elapsed} whenever
// BroadcastRoomAutoQueueAdded is invoked. R09f.
type recordingAutoQueueAddedCall struct {
	slug         string
	song         entity.Song
	sourceTitle  string
	currentIndex int
	currentSong  *entity.Song
	status       entity.PlaybackStatus
	elapsed      int
}

// recordingAutoQueueConfigChangedCall: append {slug, enabled, strategy}
// whenever BroadcastRoomAutoQueueConfigChanged is invoked. R09f.
type recordingAutoQueueConfigChangedCall struct {
	slug     string
	enabled  bool
	strategy string
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
func (r *recordingRoomBroadcaster) BroadcastRoomQueueSongPrioritized(slug string, from, to int, song entity.Song, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prioCalls = append(r.prioCalls, recordingPrioCall{slug: slug, from: from, to: to, song: song})
}
func (r *recordingRoomBroadcaster) BroadcastRoomPlaybackStatusChanged(slug string, status entity.PlaybackStatus, elapsed int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusCalls = append(r.statusCalls, recordingStatusCall{slug: slug, status: status, elapsed: elapsed})
}
func (r *recordingRoomBroadcaster) BroadcastRoomPlaybackElapsedSync(slug string, elapsed int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.elapsedCalls = append(r.elapsedCalls, recordingElapsedCall{slug: slug, elapsed: elapsed})
}
func (r *recordingRoomBroadcaster) BroadcastRoomPlaybackSongAdvanced(slug, reason string, prev, next int, song *entity.Song, status entity.PlaybackStatus, elapsed int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.advancedCalls = append(r.advancedCalls, recordingAdvancedCall{slug: slug, reason: reason, prev: prev, next: next, song: song, status: status, elapsed: elapsed})
}
func (r *recordingRoomBroadcaster) BroadcastRoomVoteUpdated(_ string, _ *entity.VoteSession, _ int, _ *entity.Queue) {}
func (r *recordingRoomBroadcaster) BroadcastRoomVoteResolved(_, _, _ string, _ *entity.Queue) {}
func (r *recordingRoomBroadcaster) BroadcastRoomPlaybackVolumeChanged(slug, direction string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.volumeCalls = append(r.volumeCalls, recordingVolumeCall{slug: slug, direction: direction})
}
func (r *recordingRoomBroadcaster) BroadcastRoomPlaybackSongPrevious(slug string, prev, next int, song *entity.Song, status entity.PlaybackStatus, elapsed int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.previousCalls = append(r.previousCalls, recordingPreviousCall{slug: slug, prev: prev, next: next, song: song, status: status, elapsed: elapsed})
}
func (r *recordingRoomBroadcaster) BroadcastRoomAutoQueueAdded(slug string, song entity.Song, sourceTitle string, currentIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, _ *entity.Queue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoQueueAddedCalls = append(r.autoQueueAddedCalls, recordingAutoQueueAddedCall{slug: slug, song: song, sourceTitle: sourceTitle, currentIndex: currentIndex, currentSong: currentSong, status: status, elapsed: elapsed})
}
func (r *recordingRoomBroadcaster) BroadcastRoomAutoQueueConfigChanged(slug string, enabled bool, strategy string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoQueueConfigChangedCalls = append(r.autoQueueConfigChangedCalls, recordingAutoQueueConfigChangedCall{slug: slug, enabled: enabled, strategy: strategy})
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

// --- R07d Prioritize handler tests ---

// seedRoomQueueWithTwoSongs adds a host (42), creates a room, and seeds a
// queue with current at 0 and one upcoming song at 1. Returns the
// RoomQueueHandlers + db + slug for assertion.
func seedRoomQueueWithTwoSongs(t *testing.T, slug string) (*RoomQueueHandlers, *sql.DB, func()) {
	t.Helper()
	rqh, db, cleanup := newRoomQueueHandlers(t)
	seedUserQueue(t, db, 42, "host-rq-prio-"+slug+"@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, slug, "PR-"+slug, 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, slug)
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "cur", Title: "Cur", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "up1", Title: "Up1", URL: "u", AddedBy: "Host", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	return rqh, db, cleanup
}

// seedRoomQueueWithCurrentNotZero seeds a queue whose CurrentIndex is
// NOT 0 (e.g. a played song at idx 0, current at idx 1, upcoming at
// idx 2). Used by the missing-body test so the old bug (missing
// song_index silently defaulting to 0) would mutate index 0 — the
// "played" song — and change its IsPrioritized flag. With the fix in
// place the request is rejected with 400 and the persisted queue is
// untouched.
func seedRoomQueueWithCurrentNotZero(t *testing.T, slug string) (*RoomQueueHandlers, *sql.DB, func()) {
	t.Helper()
	rqh, db, cleanup := newRoomQueueHandlers(t)
	seedUserQueue(t, db, 42, "host-rq-prio-nz-"+slug+"@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, slug, "PR-"+slug, 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, slug)
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "played", Title: "Played", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "cur", Title: "Cur", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "up", Title: "Up", URL: "u", AddedBy: "Host", AddedByID: 42},
	}
	seed.CurrentIndex = 1
	seed.Status = entity.StatusPlaying
	if err := queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	return rqh, db, cleanup
}

func TestRoomQueue_PrioritizeSong_HostReturns204(t *testing.T) {
	rqh, _, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-ok")
	defer cleanup()

	body := []byte(`{"song_index":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-ok/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-ok", 42)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_PrioritizeSong_BroadcastsSongPrioritized(t *testing.T) {
	rqh, _, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-bc")
	defer cleanup()

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"song_index":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-bc/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-bc", 42)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.prioCalls) != 1 {
		t.Fatalf("expected 1 prioritize broadcast, got %d", len(bc.prioCalls))
	}
	if bc.prioCalls[0].slug != "rq-h-prio-bc" || bc.prioCalls[0].from != 1 || bc.prioCalls[0].to != 1 {
		t.Errorf("expected prio call slug=rq-h-prio-bc from=1 to=1, got %+v", bc.prioCalls[0])
	}
}

// TestRoomQueue_PrioritizeSong_BroadcastSongIsPrioritizedTrue covers
// the R07d important-fix invariant at the handler/broadcast seam: the
// song entity the handler hands to BroadcastRoomQueueSongPrioritized
// must carry IsPrioritized=true so the front-end visual indicator
// works. The pre-fix interactor returned the pre-mutation snapshot
// (IsPrioritized=false), so the broadcast payload would not have
// flipped the indicator even though the authoritative state already
// had. With the post-mutation fix, the broadcast song matches the
// authoritative state. Uses a three-song seed so the move actually
// executes (entity.Prioritize is a no-op when the song is already at
// the target slot).
func TestRoomQueue_PrioritizeSong_BroadcastSongIsPrioritizedTrue(t *testing.T) {
	rqh, db, cleanup := newRoomQueueHandlers(t)
	defer cleanup()
	seedUserQueue(t, db, 42, "host-rq-prio-bcsong@example.com", entity.RoleHost)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, "rq-h-prio-bc-prio", "PRBcs", 42, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, "rq-h-prio-bc-prio")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "cur", Title: "Cur", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "up1", Title: "Up1", URL: "u", AddedBy: "Host", AddedByID: 42},
		{ID: "up2", Title: "Up2", URL: "u", AddedBy: "Host", AddedByID: 42},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"song_index":2}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-bc-prio/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-bc-prio", 42)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.prioCalls) != 1 {
		t.Fatalf("expected 1 prioritize broadcast, got %d", len(bc.prioCalls))
	}
	if !bc.prioCalls[0].song.IsPrioritized {
		t.Errorf("expected broadcast song IsPrioritized=true, got %+v", bc.prioCalls[0].song)
	}
}

func TestRoomQueue_PrioritizeSong_NoBroadcastOnError(t *testing.T) {
	rqh, _, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-eb")
	defer cleanup()

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	// Current song → 400 + no broadcast.
	body := []byte(`{"song_index":0}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-eb/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-eb", 42)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.prioCalls) != 0 {
		t.Errorf("expected NO prio broadcast on error, got %d", len(bc.prioCalls))
	}
}

func TestRoomQueue_PrioritizeSong_InvalidIndexReturns400(t *testing.T) {
	rqh, _, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-bad")
	defer cleanup()

	for _, idx := range []int{-1, 99} {
		body := []byte(fmt.Sprintf(`{"song_index":%d}`, idx))
		req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-bad/queue/prioritize", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-bad", 42)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("idx=%d: expected 400, got %d", idx, rr.Code)
		}
	}
}

func TestRoomQueue_PrioritizeSong_CurrentSongReturns400(t *testing.T) {
	rqh, _, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-cur")
	defer cleanup()

	body := []byte(`{"song_index":0}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-cur/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-cur", 42)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for current song, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_PrioritizeSong_MalformedJSONReturns400(t *testing.T) {
	rqh, _, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-mf")
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-mf/queue/prioritize", bytes.NewReader([]byte(`{not json`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-mf", 42)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestRoomQueue_PrioritizeSong_MissingBodyReturns400 covers the R07d
// blocking-fix invariant: POST /api/rooms/{slug}/queue/prioritize with
// body {} must return 400 (missing song_index) and MUST NOT broadcast
// or mutate the persisted queue. The seeded queue has current_index=1
// (not 0) so the pre-fix bug (missing → int default 0) would have
// mutated the played song at index 0 and stamped IsPrioritized=true on
// it. With the *int fix the request is rejected and the persisted
// queue is byte-for-byte unchanged.
func TestRoomQueue_PrioritizeSong_MissingBodyReturns400(t *testing.T) {
	rqh, db, cleanup := seedRoomQueueWithCurrentNotZero(t, "rq-h-prio-mb")
	defer cleanup()

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	roomID := mustRoomIDQueue(t, db, "rq-h-prio-mb")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	before, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("load pre: %v", err)
	}
	if before.CurrentIndex != 1 {
		t.Fatalf("precondition: expected CurrentIndex=1, got %d", before.CurrentIndex)
	}
	if before.Songs[0].IsPrioritized {
		t.Fatalf("precondition: expected Songs[0].IsPrioritized=false, got true")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-mb/queue/prioritize", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-mb", 42)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing song_index, got %d body=%s", rr.Code, rr.Body.String())
	}

	// No broadcast on the error path.
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.prioCalls) != 0 {
		t.Errorf("expected NO prioritize broadcast on missing song_index, got %d", len(bc.prioCalls))
	}

	// Persisted queue must be untouched: no song at index 0 got
	// IsPrioritized=true and the order is preserved.
	after, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("load post: %v", err)
	}
	if len(after.Songs) != len(before.Songs) {
		t.Fatalf("expected %d songs after, got %d", len(before.Songs), len(after.Songs))
	}
	if after.CurrentIndex != before.CurrentIndex {
		t.Errorf("expected CurrentIndex unchanged (%d), got %d", before.CurrentIndex, after.CurrentIndex)
	}
	for i := range before.Songs {
		if after.Songs[i].ID != before.Songs[i].ID {
			t.Errorf("song at idx %d changed: was %q now %q", i, before.Songs[i].ID, after.Songs[i].ID)
		}
		if after.Songs[i].IsPrioritized != before.Songs[i].IsPrioritized {
			t.Errorf("IsPrioritized at idx %d changed: was %v now %v", i, before.Songs[i].IsPrioritized, after.Songs[i].IsPrioritized)
		}
	}
}

func TestRoomQueue_PrioritizeSong_GuestReturns403(t *testing.T) {
	rqh, db, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-g")
	defer cleanup()

	seedUserQueue(t, db, 200, "guest-rq-prio@example.com", entity.RoleGuest)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if err := roomRepo.AddMember(ctx, mustRoomIDQueue(t, db, "rq-h-prio-g"), 200, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
		t.Fatalf("add guest: %v", err)
	}

	body := []byte(`{"song_index":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-g/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-g", 200)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for guest, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_PrioritizeSong_NonMemberReturns403(t *testing.T) {
	rqh, _, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-nm")
	defer cleanup()

	body := []byte(`{"song_index":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-nm/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-nm", 999)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_PrioritizeSong_ArchivedRoomReturns409(t *testing.T) {
	rqh, db, cleanup := seedRoomQueueWithTwoSongs(t, "rq-h-prio-ar")
	defer cleanup()

	roomRepo := persistence.NewPostgresRoomRepository(db)
	if err := roomRepo.ArchiveRoom(context.Background(), mustRoomIDQueue(t, db, "rq-h-prio-ar"), time.Now().UTC()); err != nil {
		t.Fatalf("archive: %v", err)
	}

	body := []byte(`{"song_index":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-prio-ar/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "rq-h-prio-ar", 42)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for archived room, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoomQueue_PrioritizeSong_RequiresAuthenticatedActor(t *testing.T) {
	rqh, _, cleanup := newRoomQueueHandlers(t)
	defer cleanup()

	body := []byte(`{"song_index":0}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/any/queue/prioritize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandlePrioritizeRoomSong(rr, req, "any", 0)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
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

// --- R09a playback handler tests ---

// playbackRoomFixture creates a room with a 2-song queue and wires
// the roomqueue interactor to a player-lease interactor with the
// given holder as the active lease holder. Returns the handler, the
// db handle, and the cleanup. The room's host is always user 200;
// when the holder is a different user (e.g. user 42) we add 42 as a
// guest so the holder claim can still succeed via the host check
// (the holder user must be a member of the room).
func playbackRoomFixture(t *testing.T, slug string, holderID int) (*RoomQueueHandlers, *sql.DB, func()) {
	t.Helper()
	rqh, db, cleanup := newRoomQueueHandlers(t)
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	seedUserQueue(t, db, 200, "host-rq-pb-"+slug+"@example.com", entity.RoleHost)
	// holderID == 0 is the "no lease" sentinel: skip seeding the holder
	// user and guest-membership so the resulting room has no active
	// lease. Used by TestRoomQueue_ChangePlaybackVolume_MissingLeaseReturns404.
	if holderID != 200 && holderID != 0 {
		seedUserQueue(t, db, holderID, fmt.Sprintf("holder-rq-pb-%s@example.com", slug), entity.RoleGuest)
	}
	if _, err := roomRepo.CreateRoomAndHost(ctx, slug, "PB-"+slug, 200, time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, slug)
	if holderID != 200 && holderID != 0 {
		if err := roomRepo.AddMember(ctx, roomID, holderID, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
			t.Fatalf("add holder guest: %v", err)
		}
	}
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "cur", Title: "Cur", URL: "u", AddedBy: "H", AddedByID: 200},
		{ID: "next", Title: "Next", URL: "u", AddedBy: "H", AddedByID: 200},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	if err := queueRepo.Save(ctx, roomID, seed); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	leaseRepo := persistence.NewPostgresPlayerLeaseRepository(db)
	leaseInter := room.NewPlayerLeaseInteractor(leaseRepo, roomRepo, db, room.DefaultLeaseDuration, room.DefaultLeaseGrace)
	// holderID == 0 is the "no lease" sentinel: skip the Claim so the
	// resulting room has no active lease. Used by
	// TestRoomQueue_ChangePlaybackVolume_MissingLeaseReturns404.
	if holderID != 0 {
		if _, err := leaseInter.Claim(ctx, slug, holderID); err != nil {
			t.Fatalf("claim lease for holder %d: %v", holderID, err)
		}
	}
	rqh.inter.SetLeaseAuthorizer(leaseInter)
	return rqh, db, cleanup
}

// TestRoomQueue_SetPlaybackStatus_HolderReturns204 pins the happy
// path: holder sends {"status":"paused"} and the handler returns 204
// with one status broadcast carrying the post-mutation snapshot.
func TestRoomQueue_SetPlaybackStatus_HolderReturns204(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-status", 200)
	defer cleanup()

	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"status":"paused"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-status/playback/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleSetRoomPlaybackStatus(rr, req, "rq-h-pb-status", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.statusCalls) != 1 || bc.statusCalls[0].status != entity.StatusPaused {
		t.Fatalf("expected 1 paused status broadcast, got %+v", bc.statusCalls)
	}
	if bc.statusCalls[0].slug != "rq-h-pb-status" {
		t.Errorf("expected slug rq-h-pb-status, got %q", bc.statusCalls[0].slug)
	}
}

// TestRoomQueue_SetPlaybackStatus_RejectsIdleWith400 pins the
// client-supplied idle rejection at the handler layer.
func TestRoomQueue_SetPlaybackStatus_RejectsIdleWith400(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-idle", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"status":"idle"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-idle/playback/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleSetRoomPlaybackStatus(rr, req, "rq-h-pb-idle", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.statusCalls) != 0 {
		t.Errorf("expected 0 status broadcasts on 400, got %d", len(bc.statusCalls))
	}
}

// TestRoomQueue_SetPlaybackStatus_NonHolderReturns403AndNoBroadcast
// pins the lease-holder gate at the handler level.
func TestRoomQueue_SetPlaybackStatus_NonHolderReturns403AndNoBroadcast(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-nonholder", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"status":"paused"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-nonholder/playback/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	// 42 is a guest member but NOT the lease holder (200 holds it).
	rqh.HandleSetRoomPlaybackStatus(rr, req, "rq-h-pb-nonholder", 42)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.statusCalls) != 0 {
		t.Errorf("expected 0 status broadcasts on 403, got %d", len(bc.statusCalls))
	}
}

// TestRoomQueue_SyncPlayback_HolderReturns204 pins the happy path
// for elapsed sync.
func TestRoomQueue_SyncPlayback_HolderReturns204(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-sync", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"elapsed":17}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-sync/playback/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleSyncRoomPlayback(rr, req, "rq-h-pb-sync", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.elapsedCalls) != 1 || bc.elapsedCalls[0].elapsed != 17 {
		t.Fatalf("expected 1 elapsed=17 broadcast, got %+v", bc.elapsedCalls)
	}
}

// TestRoomQueue_SyncPlayback_MissingElapsedReturns400 pins the
// *int request shape: missing field → 400, NOT 0.
func TestRoomQueue_SyncPlayback_MissingElapsedReturns400(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-syncmiss", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-syncmiss/playback/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleSyncRoomPlayback(rr, req, "rq-h-pb-syncmiss", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.elapsedCalls) != 0 {
		t.Errorf("expected 0 elapsed broadcasts on 400, got %d", len(bc.elapsedCalls))
	}
}

// TestRoomQueue_SyncPlayback_NegativeElapsedReturns400
func TestRoomQueue_SyncPlayback_NegativeElapsedReturns400(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-syncneg", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"elapsed":-1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-syncneg/playback/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleSyncRoomPlayback(rr, req, "rq-h-pb-syncneg", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.elapsedCalls) != 0 {
		t.Errorf("expected 0 elapsed broadcasts on 400, got %d", len(bc.elapsedCalls))
	}
}

// TestRoomQueue_SkipPlayback_HolderAdvancesAndBroadcasts
func TestRoomQueue_SkipPlayback_HolderAdvancesAndBroadcasts(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-skip", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-skip/playback/skip", nil)
	rr := httptest.NewRecorder()
	rqh.HandleSkipRoomPlayback(rr, req, "rq-h-pb-skip", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.advancedCalls) != 1 {
		t.Fatalf("expected 1 advanced broadcast, got %d", len(bc.advancedCalls))
	}
	if bc.advancedCalls[0].reason != "skip" || bc.advancedCalls[0].prev != 0 || bc.advancedCalls[0].next != 1 {
		t.Errorf("unexpected advanced capture: %+v", bc.advancedCalls[0])
	}
}

// TestRoomQueue_SkipPlayback_NoNextSongReturns400 pins the
// no-partial-mutation invariant at the handler layer.
func TestRoomQueue_SkipPlayback_NoNextSongReturns400(t *testing.T) {
	rqh, db, cleanup := playbackRoomFixture(t, "rq-h-pb-skipnone", 200)
	defer cleanup()
	roomID := mustRoomIDQueue(t, db, "rq-h-pb-skipnone")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "only", Title: "Only", URL: "u", AddedBy: "H", AddedByID: 200},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	seed.Elapsed = 99
	if err := queueRepo.Save(context.Background(), roomID, seed); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-skipnone/playback/skip", nil)
	rr := httptest.NewRecorder()
	rqh.HandleSkipRoomPlayback(rr, req, "rq-h-pb-skipnone", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.advancedCalls) != 0 {
		t.Errorf("expected 0 advanced broadcasts on no-next skip, got %d", len(bc.advancedCalls))
	}
}

// TestRoomQueue_Ended_AdvancesAndBroadcastsReasonEnded
func TestRoomQueue_Ended_AdvancesAndBroadcastsReasonEnded(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-h-pb-ended", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-ended/playback/ended", nil)
	rr := httptest.NewRecorder()
	rqh.HandleRoomSongEnded(rr, req, "rq-h-pb-ended", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.advancedCalls) != 1 || bc.advancedCalls[0].reason != "ended" {
		t.Fatalf("expected 1 ended advanced broadcast, got %+v", bc.advancedCalls)
	}
	if len(bc.statusCalls) != 0 {
		t.Errorf("expected no status broadcast on advance path, got %d", len(bc.statusCalls))
	}
}

// TestRoomQueue_Ended_NoNextSongPausesAndBroadcastsStatus
func TestRoomQueue_Ended_NoNextSongPausesAndBroadcastsStatus(t *testing.T) {
	rqh, db, cleanup := playbackRoomFixture(t, "rq-h-pb-endnone", 200)
	defer cleanup()
	roomID := mustRoomIDQueue(t, db, "rq-h-pb-endnone")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "final", Title: "Final", URL: "u", AddedBy: "H", AddedByID: 200},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPlaying
	seed.Elapsed = 77
	if err := queueRepo.Save(context.Background(), roomID, seed); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-h-pb-endnone/playback/ended", nil)
	rr := httptest.NewRecorder()
	rqh.HandleRoomSongEnded(rr, req, "rq-h-pb-endnone", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.statusCalls) != 1 || bc.statusCalls[0].status != entity.StatusPaused {
		t.Fatalf("expected 1 paused status broadcast, got %+v", bc.statusCalls)
	}
	if len(bc.advancedCalls) != 0 {
		t.Errorf("expected 0 advanced broadcasts at end-of-queue, got %d", len(bc.advancedCalls))
	}
}

// TestRoomQueue_ChangePlaybackVolume_HolderReturns204 pins the happy
// path: holder sends {"direction":"up"}, handler returns 204, broadcaster
// receives exactly one {slug, direction="up"} call. No queue mutation.
func TestRoomQueue_ChangePlaybackVolume_HolderReturns204(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-c-vol-h-204", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"direction":"up"}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-c-vol-h-204/playback/volume",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackVolume(rr, req, "rq-c-vol-h-204", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.volumeCalls) != 1 || bc.volumeCalls[0].direction != "up" {
		t.Fatalf("expected 1 up broadcast, got %+v", bc.volumeCalls)
	}
	if bc.volumeCalls[0].slug != "rq-c-vol-h-204" {
		t.Errorf("expected slug rq-c-vol-h-204, got %q", bc.volumeCalls[0].slug)
	}
}

// TestRoomQueue_ChangePlaybackVolume_DownAlsoReturns204 pins the
// other valid direction.
func TestRoomQueue_ChangePlaybackVolume_DownAlsoReturns204(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-c-vol-h-down", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"direction":"down"}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-c-vol-h-down/playback/volume",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackVolume(rr, req, "rq-c-vol-h-down", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.volumeCalls) != 1 || bc.volumeCalls[0].direction != "down" {
		t.Fatalf("expected 1 down broadcast, got %+v", bc.volumeCalls)
	}
}

// TestRoomQueue_ChangePlaybackVolume_InvalidDirectionReturns400
func TestRoomQueue_ChangePlaybackVolume_InvalidDirectionReturns400(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-c-vol-bad", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	for _, dir := range []string{"left", "UP", "increase"} {
		body := []byte(fmt.Sprintf(`{"direction":%q}`, dir))
		req := httptest.NewRequest(http.MethodPost,
			"/api/rooms/rq-c-vol-bad/playback/volume",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		rqh.HandleChangeRoomPlaybackVolume(rr, req, "rq-c-vol-bad", 200)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("direction=%q: expected 400, got %d", dir, rr.Code)
		}
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.volumeCalls) != 0 {
		t.Errorf("expected 0 broadcasts on bad direction, got %d", len(bc.volumeCalls))
	}
}

// TestRoomQueue_ChangePlaybackVolume_MissingDirectionReturns400
func TestRoomQueue_ChangePlaybackVolume_MissingDirectionReturns400(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-c-vol-missing", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-c-vol-missing/playback/volume",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackVolume(rr, req, "rq-c-vol-missing", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.volumeCalls) != 0 {
		t.Errorf("expected 0 broadcasts, got %d", len(bc.volumeCalls))
	}
}

// TestRoomQueue_ChangePlaybackVolume_MalformedJSONReturns400
func TestRoomQueue_ChangePlaybackVolume_MalformedJSONReturns400(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-c-vol-malformed", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{not-json`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-c-vol-malformed/playback/volume",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackVolume(rr, req, "rq-c-vol-malformed", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.volumeCalls) != 0 {
		t.Errorf("expected 0 broadcasts, got %d", len(bc.volumeCalls))
	}
}

// TestRoomQueue_ChangePlaybackVolume_NonHolderReturns403AndNoBroadcast
func TestRoomQueue_ChangePlaybackVolume_NonHolderReturns403AndNoBroadcast(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-c-vol-nh", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"direction":"up"}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-c-vol-nh/playback/volume",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackVolume(rr, req, "rq-c-vol-nh", 42) // 42 is guest
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.volumeCalls) != 0 {
		t.Errorf("expected 0 broadcasts on 403, got %d", len(bc.volumeCalls))
	}
}

// TestRoomQueue_ChangePlaybackVolume_MissingLeaseReturns404
func TestRoomQueue_ChangePlaybackVolume_MissingLeaseReturns404(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-c-vol-ml", 0) // 0 = no lease
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	body := []byte(`{"direction":"up"}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-c-vol-ml/playback/volume",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackVolume(rr, req, "rq-c-vol-ml", 200)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.volumeCalls) != 0 {
		t.Errorf("expected 0 broadcasts on missing lease, got %d", len(bc.volumeCalls))
	}
}

// --- R09d PrevPlayback handler tests ---

// TestRoomQueue_PrevPlayback_HolderReturns204AndBroadcasts pins the
// happy path: lease holder posts empty body; queue moves to previous
// song (index 1→0); handler returns 204; broadcaster receives exactly
// one {slug, prev=1, next=0, song=cur, status=playing, elapsed=0} call.
func TestRoomQueue_PrevPlayback_HolderReturns204AndBroadcasts(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-d-pb-prev-ok", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	// Advance the fixture's seed (CurrentIndex=0) to index 1 via the
	// lease-only SkipPlayback path so "previous" has a target.
	skipReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-d-pb-prev-ok/playback/skip", nil)
	skipRR := httptest.NewRecorder()
	rqh.HandleSkipRoomPlayback(skipRR, skipReq, "rq-d-pb-prev-ok", 200)
	if skipRR.Code != http.StatusNoContent {
		t.Fatalf("seed-advance: expected 204, got %d", skipRR.Code)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-d-pb-prev-ok/playback/prev", nil)
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackPrevious(rr, req, "rq-d-pb-prev-ok", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.previousCalls) != 1 {
		t.Fatalf("expected 1 previous broadcast, got %d", len(bc.previousCalls))
	}
	pc := bc.previousCalls[0]
	if pc.slug != "rq-d-pb-prev-ok" || pc.prev != 1 || pc.next != 0 {
		t.Errorf("unexpected previous capture: %+v", pc)
	}
	if pc.song == nil || pc.song.ID != "cur" {
		t.Errorf("expected previous song id=cur, got %+v", pc.song)
	}
	if pc.status != entity.StatusPlaying || pc.elapsed != 0 {
		t.Errorf("expected status=playing elapsed=0, got status=%q elapsed=%d", pc.status, pc.elapsed)
	}
}

// TestRoomQueue_PrevPlayback_AlreadyOnFirstReturns400 pins the
// no-partial-mutation invariant at the handler boundary: queue on
// index 0 with Elapsed=42 — PrevPlayback returns 400 and the queue is
// not mutated.
func TestRoomQueue_PrevPlayback_AlreadyOnFirstReturns400(t *testing.T) {
	rqh, db, cleanup := playbackRoomFixture(t, "rq-d-pb-prev-first", 200)
	defer cleanup()
	roomID := mustRoomIDQueue(t, db, "rq-d-pb-prev-first")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	seed := entity.NewQueue()
	seed.Songs = []entity.Song{
		{ID: "first", Title: "First", URL: "u", AddedBy: "H", AddedByID: 200},
	}
	seed.CurrentIndex = 0
	seed.Status = entity.StatusPaused
	seed.Elapsed = 42
	if err := queueRepo.Save(context.Background(), roomID, seed); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-d-pb-prev-first/playback/prev", nil)
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackPrevious(rr, req, "rq-d-pb-prev-first", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.previousCalls) != 0 {
		t.Errorf("expected 0 broadcasts on no-prev, got %d", len(bc.previousCalls))
	}
	persisted, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("post-load: %v", err)
	}
	if persisted.CurrentIndex != 0 || persisted.Elapsed != 42 || persisted.Status != entity.StatusPaused {
		t.Errorf("queue mutated on no-prev: %+v", persisted)
	}
}

// TestRoomQueue_PrevPlayback_EmptyQueueReturns400 pins the
// empty-queue branch: PrevPlayback on a fresh room returns 400 and
// does NOT broadcast.
func TestRoomQueue_PrevPlayback_EmptyQueueReturns400(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-d-pb-prev-empty", 200)
	defer cleanup()
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-d-pb-prev-empty/playback/prev", nil)
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackPrevious(rr, req, "rq-d-pb-prev-empty", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.previousCalls) != 0 {
		t.Errorf("expected 0 broadcasts on empty queue, got %d", len(bc.previousCalls))
	}
}

// TestRoomQueue_PrevPlayback_NonHolderReturns403AndNoBroadcast
// exercises the lease-authorizer seam at the handler boundary: a guest
// is rejected with 403 and the broadcaster is never invoked.
func TestRoomQueue_PrevPlayback_NonHolderReturns403AndNoBroadcast(t *testing.T) {
	rqh, _, cleanup := playbackRoomFixture(t, "rq-d-pb-prev-nh", 200)
	defer cleanup()
	// Advance so prev has a target.
	skipReq := httptest.NewRequest(http.MethodPost, "/api/rooms/rq-d-pb-prev-nh/playback/skip", nil)
	skipRR := httptest.NewRecorder()
	rqh.HandleSkipRoomPlayback(skipRR, skipReq, "rq-d-pb-prev-nh", 200)
	if skipRR.Code != http.StatusNoContent {
		t.Fatalf("seed-advance: expected 204, got %d", skipRR.Code)
	}
	bc := &recordingRoomBroadcaster{}
	rqh.inter.SetBroadcaster(bc)

	// caller 42 is a guest (not the lease holder).
	req := httptest.NewRequest(http.MethodPost,
		"/api/rooms/rq-d-pb-prev-nh/playback/prev", nil)
	rr := httptest.NewRecorder()
	rqh.HandleChangeRoomPlaybackPrevious(rr, req, "rq-d-pb-prev-nh", 42)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.previousCalls) != 0 {
		t.Errorf("expected 0 broadcasts on non-holder, got %d", len(bc.previousCalls))
	}
}
