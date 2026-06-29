package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"local-music-queue/internal/domain/entity"
)

// stubRoomResolver supplies a fixed room + queue snapshot for hub tests.
type stubRoomResolver struct {
	mu     sync.Mutex
	rooms  map[string]*entity.Room
	queues map[int64]*entity.Queue
	// nonMemberIDs lists user ids that should be rejected as non-members.
	nonMemberIDs map[int]bool
	// resolveErr returns this error from RoomBySlug when non-nil.
	resolveErr error
	// onQueueByRoomID, when set, is invoked from inside QueueByRoomID.
	// It runs inside the hub loop's register case (between adding the
	// client to h.clients and the initial-sync nextSeq call), so callers
	// can use it to deterministically interleave a broadcast() with the
	// initial-sync path.
	onQueueByRoomID func(roomID int64)
}

func newStubRoomResolver() *stubRoomResolver {
	return &stubRoomResolver{
		rooms:        map[string]*entity.Room{},
		queues:       map[int64]*entity.Queue{},
		nonMemberIDs: map[int]bool{999: true},
	}
}

func (s *stubRoomResolver) RoomBySlug(_ context.Context, slug string) (*entity.Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	r, ok := s.rooms[slug]
	if !ok {
		return nil, errStubRoomNotFound
	}
	return r, nil
}

func (s *stubRoomResolver) IsActiveMember(_ context.Context, _ int64, userID int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.nonMemberIDs[userID], nil
}

func (s *stubRoomResolver) QueueByRoomID(_ context.Context, roomID int64) (*entity.Queue, error) {
	s.mu.Lock()
	q, ok := s.queues[roomID]
	if !ok {
		q = entity.NewQueue()
	}
	hook := s.onQueueByRoomID
	s.mu.Unlock()

	if hook != nil {
		hook(roomID)
	}
	return q, nil
}

func (s *stubRoomResolver) SetRoom(slug string, room *entity.Room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rooms[slug] = room
}

func (s *stubRoomResolver) SetQueue(roomID int64, q *entity.Queue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queues[roomID] = q
}

var errStubRoomNotFound = errStubInvalid // reuse the existing stub error

// dialRoomWS dials the per-room endpoint on a test server.
func dialRoomWS(t *testing.T, server *httptest.Server, slug, token string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/" + slug
	if token != "" {
		u += "?session_token=" + token
	}
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial room ws: %v", err)
	}
	return conn
}

// TestRoomHub_SendsInitialSyncOnConnect pins the on-connect sync contract
// (single-process / in-memory): every room WS connection receives a
// single room_queue_sync envelope scoped to the slug it connected with.
func TestRoomHub_SendsInitialSyncOnConnect(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "T1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 1, Role: entity.RoleHost, DisplayName: "H"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	conn := dialRoomWS(t, server, "alpha", "valid")
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var env struct {
		Type string            `json:"type"`
		Data RoomQueueSyncData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != EventRoomQueueSync {
		t.Fatalf("expected %q, got %q", EventRoomQueueSync, env.Type)
	}
	if env.Data.RoomSlug != "alpha" {
		t.Errorf("expected room_slug=alpha, got %q", env.Data.RoomSlug)
	}
	if env.Data.State == nil || len(env.Data.State.Songs) != 1 {
		t.Errorf("expected state with 1 song, got %#v", env.Data.State)
	}
}

// TestRoomHub_BroadcastSongAdded_ReachesAllRoomClients asserts that a
// BroadcastRoomQueueSongAdded invocation only reaches clients connected
// to the matching room slug — clients in other rooms do not receive it.
func TestRoomHub_BroadcastSongAdded_ReachesAllRoomClients(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetRoom("beta", &entity.Room{ID: 22, Slug: "beta", Status: entity.RoomStatusActive})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"alpha": {ID: 1, Role: entity.RoleHost, DisplayName: "A"},
			"beta":  {ID: 2, Role: entity.RoleHost, DisplayName: "B"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	a1 := dialRoomWS(t, server, "alpha", "alpha")
	defer a1.Close()
	a1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := a1.ReadMessage(); err != nil {
		t.Fatalf("alpha drain: %v", err)
	}
	b1 := dialRoomWS(t, server, "beta", "beta")
	defer b1.Close()
	b1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := b1.ReadMessage(); err != nil {
		t.Fatalf("beta drain: %v", err)
	}

	state := &entity.Queue{Songs: []entity.Song{{ID: "added", Title: "Added"}}}
	hub.BroadcastRoomQueueSongAdded("alpha", entity.Song{ID: "added", Title: "Added"}, 0, state)

	a1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := a1.ReadMessage()
	if err != nil {
		t.Fatalf("alpha read broadcast: %v", err)
	}
	var env struct {
		Type string `json:"type"`
	}
	json.Unmarshal(data, &env)
	if env.Type != EventRoomQueueSongAdded {
		t.Fatalf("alpha: expected %q, got %q", EventRoomQueueSongAdded, env.Type)
	}

	// Beta must not receive anything within a short window.
	b1.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := b1.ReadMessage(); err == nil {
		t.Errorf("beta unexpectedly received a broadcast for alpha")
	}
}

// TestRoomHub_RegisterHandler_NonMemberRejected verifies the non-member
// gate: even with a valid session token, a non-member receives 403 on
// upgrade and does not appear in the room's client map.
func TestRoomHub_RegisterHandler_NonMemberRejected(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 999, Role: entity.RoleGuest, DisplayName: "X"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	u := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/alpha?session_token=valid"
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatalf("expected dial to fail for non-member")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %v", resp)
	}
}

// TestRoomHub_PingKeepalive_UnregistersOnFailedPing asserts the room
// hub sends WebSocket pings to connected clients on the configured
// interval and unregisters a client whose ping write fails (idle peer
// closed the TCP connection without sending a Close frame).
func TestRoomHub_PingKeepalive_UnregistersOnFailedPing(t *testing.T) {
	// Shorten the ping interval so the test runs in milliseconds.
	orig := roomPingInterval
	roomPingInterval = 20 * time.Millisecond
	t.Cleanup(func() { roomPingInterval = orig })

	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "T1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 1, Role: entity.RoleHost, DisplayName: "H"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	conn := dialRoomWS(t, server, "alpha", "valid")
	defer conn.Close()
	// Drain the initial sync envelope so the read pump is unblocked.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("drain initial sync: %v", err)
	}

	// Wait long enough for at least one ping cycle. With roomPingInterval
	// set to 20ms, the hub will attempt to ping a couple of times before
	// we tear down the connection below.
	time.Sleep(60 * time.Millisecond)

	// Drop the client side so the next ping write fails and the hub
	// unregisters the connection.
	conn.Close()

	// Give the hub loop enough time to observe the failed ping and
	// unregister. Then assert the client set is empty.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		n := len(hub.clients["alpha"])
		hub.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	hub.mu.Lock()
	n := len(hub.clients["alpha"])
	hub.mu.Unlock()
	t.Fatalf("expected hub to unregister client after failed ping, got %d remaining", n)
}

// TestRoomHub_RegisterHandler_RequiresSessionToken verifies the auth
// gate: a missing/invalid session token results in 401.
func TestRoomHub_RegisterHandler_RequiresSessionToken(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 1, Role: entity.RoleHost, DisplayName: "H"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	// No session_token in URL.
	u := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/alpha"
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatalf("expected dial to fail without session token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session token, got %v", resp)
	}
}

// TestRoomHub_InitialSyncOrderedBeforeBroadcasts pins the contract that
// when a client connects AFTER a broadcast has been issued, the initial
// room_queue_sync seq is strictly greater than that prior broadcast's seq.
//
// Without the hub-loop ordering fix, RegisterHandler reads the queue state
// via the resolver, then calls nextSeq in RegisterHandler before sending
// to the register channel. A concurrent dispatch() running in the
// broadcaster can call nextSeq first, allocate a HIGHER seq, and then the
// client's initial sync gets the LOWER seq. With the fix, registration
// and initial sync are both performed inside the hub loop, so the
// registerReq handler is the ONLY consumer of nextSeq that touches the
// client register path; broadcasts that were dispatched AFTER the
// RegisterHandler entered the hub loop must therefore carry seqs strictly
// greater than the sync seq.
func TestRoomHub_InitialSyncOrderedBeforeBroadcasts(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "T1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 1, Role: entity.RoleHost, DisplayName: "H"}},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	// Drain: connect a throwaway client first to make the next broadcast
	// the only outstanding operation, so its seq is exactly 1. Then close
	// the throwaway so only the real client remains connected.
	warmup := dialRoomWS(t, server, "alpha", "valid")
	warmup.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := warmup.ReadMessage(); err != nil {
		t.Fatalf("warmup drain: %v", err)
	}
	warmup.Close()

	// Issue a broadcast BEFORE the real client connects. Because the
	// hub-loop ordering fix only applies to registration, this broadcast's
	// seq is allocated independently. The real client must receive an
	// initial sync whose seq is STRICTLY GREATER than this broadcast's seq.
	state := &entity.Queue{Songs: []entity.Song{{ID: "added", Title: "A"}}}
	hub.BroadcastRoomQueueSongAdded("alpha", entity.Song{ID: "added", Title: "A"}, 0, state)
	// dispatch() allocates nextSeq synchronously before returning, so
	// reading the seq counter now reflects the broadcast's seq.
	hub.mu.Lock()
	broadcastSeq := hub.seqNum["alpha"]
	hub.mu.Unlock()

	// Now connect the real client. The initial sync seq MUST be > broadcastSeq.
	conn := dialRoomWS(t, server, "alpha", "valid")
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var env struct {
		Type   string            `json:"type"`
		SeqNum int64             `json:"seq_num"`
		Data   RoomQueueSyncData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}
	if env.Type != EventRoomQueueSync {
		t.Fatalf("expected first frame to be %q, got %q", EventRoomQueueSync, env.Type)
	}
	if env.SeqNum <= broadcastSeq {
		t.Fatalf("initial sync seq (%d) must be strictly greater than the broadcast seq (%d) issued before connect — hub-loop ordering fix is not in effect", env.SeqNum, broadcastSeq)
	}
}

// TestRoomHub_InitialSyncOrderedBeforeInterleavedBroadcast pins the
// sequenced-delta contract for the dangerous interleaving where a
// broadcast is dispatched AFTER the connecting client has been inserted
// into the hub's client map (so the broadcast reaches the new client)
// but BEFORE the initial sync has been stamped with a seq.
//
// Pre-fix, dispatch() allocates nextSeq synchronously before pushing to
// the broadcast channel; sendInitialSync() then allocates a LATER seq
// inside the hub loop. The new client therefore receives the initial
// sync with a HIGHER seq than the delta that was racing the sync, which
// violates the contract that the initial sync is the authoritative
// floor for every subsequent delta on that connection.
//
// The test forces the interleaving deterministically by hooking the
// resolver's QueueByRoomID call. The hub loop calls QueueByRoomID inside
// the register case, AFTER adding the client to h.clients and BEFORE
// the initial-sync nextSeq call. From inside that hook we dispatch a
// broadcast; with the fix, that broadcast's nextSeq happens in the hub
// loop AFTER the initial-sync seq, so the client sees sync < delta.
// Without the fix, dispatch() already stamped the seq outside the loop
// and the order is reversed.
func TestRoomHub_InitialSyncOrderedBeforeInterleavedBroadcast(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "T1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{"valid": {ID: 1, Role: entity.RoleHost, DisplayName: "H"}},
	})

	// Fire exactly one broadcast from inside the resolver hook. The hook
	// runs in the hub loop's register case after the client is in
	// h.clients and before sendInitialSync stamps its seq — the exact
	// window the contract must survive.
	dispatched := make(chan struct{}, 1)
	resolver.mu.Lock()
	resolver.onQueueByRoomID = func(_ int64) {
		// Snapshot hub state to avoid a data race with the hub loop;
		// we only need to call dispatch, which is safe to call from
		// any goroutine.
		state := &entity.Queue{Songs: []entity.Song{{ID: "interleaved", Title: "I"}}}
		hub.BroadcastRoomQueueSongAdded("alpha", entity.Song{ID: "interleaved", Title: "I"}, 0, state)
		select {
		case dispatched <- struct{}{}:
		default:
		}
	}
	resolver.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	conn := dialRoomWS(t, server, "alpha", "valid")
	defer conn.Close()

	// First frame MUST be the initial sync. The interleaved broadcast
	// is enqueued behind it on the channel; reading the first frame
	// and asserting it is the sync is sufficient to prove the
	// hub-loop ordering fix is in effect, because with the bug the
	// broadcast would carry a LOWER seq and would still arrive after
	// the sync in TCP order — the contract violation is in the seq
	// numbers, not in frame order.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, firstData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read first frame: %v", err)
	}
	var firstEnv struct {
		Type   string            `json:"type"`
		SeqNum int64             `json:"seq_num"`
		Data   RoomQueueSyncData `json:"data"`
	}
	if err := json.Unmarshal(firstData, &firstEnv); err != nil {
		t.Fatalf("unmarshal first frame: %v", err)
	}
	if firstEnv.Type != EventRoomQueueSync {
		t.Fatalf("expected first frame to be %q, got %q (broadcast raced ahead of initial sync)", EventRoomQueueSync, firstEnv.Type)
	}

	// The dispatch from the hook must have happened; confirm before
	// reading the second frame so the test fails fast if the hook did
	// not fire.
	select {
	case <-dispatched:
	case <-time.After(2 * time.Second):
		t.Fatal("resolver hook did not fire — interleaving test setup is broken")
	}

	// Second frame is the broadcast. Its seq must be strictly greater
	// than the sync's seq: the initial sync is the floor for the
	// connection, every subsequent delta carries a strictly greater
	// seq.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, secondData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read second frame: %v", err)
	}
	var secondEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	if err := json.Unmarshal(secondData, &secondEnv); err != nil {
		t.Fatalf("unmarshal second frame: %v", err)
	}
	if secondEnv.Type != EventRoomQueueSongAdded {
		t.Fatalf("expected second frame to be %q, got %q", EventRoomQueueSongAdded, secondEnv.Type)
	}
	if secondEnv.SeqNum <= firstEnv.SeqNum {
		t.Fatalf("interleaved broadcast seq (%d) must be strictly greater than initial sync seq (%d) — dispatch() is stamping seq outside the hub loop", secondEnv.SeqNum, firstEnv.SeqNum)
	}
}