package ws

import (
	"bytes"
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

// TestRoomHub_BroadcastSongPrioritized_DeliversEnvelopeAndSeq asserts
// the R07d prioritize broadcast reaches the only connected client of the
// matching room with the expected envelope and a strictly greater seq
// than the initial sync (R07b hub-loop ownership invariant).
func TestRoomHub_BroadcastSongPrioritized_DeliversEnvelopeAndSeq(t *testing.T) {
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

	// Drain the initial sync; remember its seq.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	if err := json.Unmarshal(syncData, &syncEnv); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}
	if syncEnv.Type != EventRoomQueueSync {
		t.Fatalf("expected first frame %q, got %q", EventRoomQueueSync, syncEnv.Type)
	}

	// Fire the R07d prioritize broadcast.
	state := &entity.Queue{
		Songs: []entity.Song{
			{ID: "cur", Title: "Cur"},
			{ID: "moved", Title: "Moved", IsPrioritized: true},
		},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	}
	hub.BroadcastRoomQueueSongPrioritized("alpha", 1, 1, entity.Song{ID: "moved", Title: "Moved", IsPrioritized: true}, state)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read prioritize broadcast: %v", err)
	}
	var env struct {
		Type   string                       `json:"type"`
		SeqNum int64                        `json:"seq_num"`
		Data   RoomQueueSongPrioritizedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal prioritize: %v", err)
	}
	if env.Type != EventRoomQueueSongPrioritized {
		t.Fatalf("expected type %q, got %q", EventRoomQueueSongPrioritized, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Fatalf("prioritize seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.RoomSlug != "alpha" {
		t.Errorf("expected room_slug=alpha, got %q", env.Data.RoomSlug)
	}
	if env.Data.FromIndex != 1 || env.Data.ToIndex != 1 {
		t.Errorf("expected from=1 to=1, got from=%d to=%d", env.Data.FromIndex, env.Data.ToIndex)
	}
	if env.Data.Song.ID != "moved" || !env.Data.Song.IsPrioritized {
		t.Errorf("expected moved prioritized song, got %+v", env.Data.Song)
	}
	if env.Data.State == nil || len(env.Data.State.Songs) != 2 {
		t.Errorf("expected full state with 2 songs, got %+v", env.Data.State)
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

// --- R09a playback broadcast envelopes ---
//
// The three tests below mirror TestRoomHub_BroadcastSongPrioritized_
// DeliversEnvelopeAndSeq (R07d). They pin the new playback event
// envelopes (type string + payload field shape + snake_case JSON
// tags) and the hub-loop seq invariant: every delta carries a strictly
// greater seq than the initial sync for the same connection.

// TestRoomHub_BroadcastPlaybackStatusChanged_DeliversEnvelopeAndSeq
// pins the room_playback_status_changed envelope and the post-mutation
// snapshot delivery.
func TestRoomHub_BroadcastPlaybackStatusChanged_DeliversEnvelopeAndSeq(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{
		Songs:        []entity.Song{{ID: "cur", Title: "Cur"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      42,
	})

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
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	if err := json.Unmarshal(syncData, &syncEnv); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}
	if syncEnv.Type != EventRoomQueueSync {
		t.Fatalf("expected first frame %q, got %q", EventRoomQueueSync, syncEnv.Type)
	}

	state := &entity.Queue{
		Songs:        []entity.Song{{ID: "cur", Title: "Cur"}},
		CurrentIndex: 0,
		Status:       entity.StatusPaused,
		Elapsed:      42,
	}
	hub.BroadcastRoomPlaybackStatusChanged("alpha", entity.StatusPaused, 42, state)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read status broadcast: %v", err)
	}
	var env struct {
		Type   string                        `json:"type"`
		SeqNum int64                         `json:"seq_num"`
		Data   RoomPlaybackStatusChangedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal status broadcast: %v", err)
	}
	if env.Type != EventRoomPlaybackStatusChanged {
		t.Fatalf("expected type %q, got %q", EventRoomPlaybackStatusChanged, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Fatalf("status broadcast seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.RoomSlug != "alpha" {
		t.Errorf("expected room_slug=alpha, got %q", env.Data.RoomSlug)
	}
	if env.Data.Status != entity.StatusPaused {
		t.Errorf("expected status=paused, got %q", env.Data.Status)
	}
	if env.Data.Elapsed != 42 {
		t.Errorf("expected elapsed=42, got %d", env.Data.Elapsed)
	}
	if env.Data.State == nil || env.Data.State.Status != entity.StatusPaused {
		t.Errorf("expected post-mutation state with status=paused, got %#v", env.Data.State)
	}
}

// TestRoomHub_BroadcastPlaybackElapsedSync_DeliversEnvelopeAndSeq
// pins the room_playback_elapsed_sync envelope.
func TestRoomHub_BroadcastPlaybackElapsedSync_DeliversEnvelopeAndSeq(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{
		Songs:        []entity.Song{{ID: "cur", Title: "Cur"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      0,
	})

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
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	json.Unmarshal(syncData, &syncEnv)

	state := &entity.Queue{
		Songs:        []entity.Song{{ID: "cur", Title: "Cur"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      17,
	}
	hub.BroadcastRoomPlaybackElapsedSync("alpha", 17, state)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read elapsed broadcast: %v", err)
	}
	var env struct {
		Type   string                      `json:"type"`
		SeqNum int64                       `json:"seq_num"`
		Data   RoomPlaybackElapsedSyncData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal elapsed broadcast: %v", err)
	}
	if env.Type != EventRoomPlaybackElapsedSync {
		t.Fatalf("expected type %q, got %q", EventRoomPlaybackElapsedSync, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Fatalf("elapsed broadcast seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.Elapsed != 17 {
		t.Errorf("expected elapsed=17, got %d", env.Data.Elapsed)
	}
}

// TestRoomHub_BroadcastPlaybackSongAdvanced_DeliversEnvelopeAndSeq
// pins the room_playback_song_advanced envelope (skip and ended both
// ride it; the client distinguishes via reason).
func TestRoomHub_BroadcastPlaybackSongAdvanced_DeliversEnvelopeAndSeq(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{
		Songs:        []entity.Song{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	})

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
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	json.Unmarshal(syncData, &syncEnv)

	state := &entity.Queue{
		Songs:        []entity.Song{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}},
		CurrentIndex: 1,
		Status:       entity.StatusPlaying,
		Elapsed:      0,
	}
	next := entity.Song{ID: "b", Title: "B"}
	hub.BroadcastRoomPlaybackSongAdvanced("alpha", "skip", 0, 1, &next, entity.StatusPlaying, 0, state)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read advanced broadcast: %v", err)
	}
	var env struct {
		Type   string                       `json:"type"`
		SeqNum int64                        `json:"seq_num"`
		Data   RoomPlaybackSongAdvancedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal advanced broadcast: %v", err)
	}
	if env.Type != EventRoomPlaybackSongAdvanced {
		t.Fatalf("expected type %q, got %q", EventRoomPlaybackSongAdvanced, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Fatalf("advanced broadcast seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.Reason != "skip" {
		t.Errorf("expected reason=skip, got %q", env.Data.Reason)
	}
	if env.Data.PreviousIndex != 0 || env.Data.NewIndex != 1 {
		t.Errorf("expected prev=0 new=1, got prev=%d new=%d", env.Data.PreviousIndex, env.Data.NewIndex)
	}
	if env.Data.CurrentSong == nil || env.Data.CurrentSong.ID != "b" {
		t.Errorf("expected current_song.id=b, got %+v", env.Data.CurrentSong)
	}
	if env.Data.State == nil || env.Data.State.CurrentIndex != 1 {
		t.Errorf("expected post-mutation state with CurrentIndex=1, got %#v", env.Data.State)
	}
}

// --- R09b hub-observability + vote broadcast envelopes ---
//
// The four tests below pin the contracts that the R09b vote-to-skip
// backend relies on the per-room hub for:
//
//   - UniqueConnectedUserIDs: de-duplicates multi-tab users (so the
//     vote interactor derives threshold from distinct connected users,
//     not raw connection count).
//   - BroadcastRoomVoteUpdated: reaches room clients after a vote cast.
//   - BroadcastRoomVoteResolved: reaches room clients after a vote
//     session ends (passed or expired).
//
// All tests use the same stub resolver / dialRoomWS / session
// resolver pattern as the existing R07b/R07d/R09a tests.

// waitFor polls fn until it returns true or the deadline elapses.
// Returns true on success, false on timeout.
func waitFor(t *testing.T, deadline time.Duration, fn func() bool) bool {
	t.Helper()
	timeout := time.Now().Add(deadline)
	for time.Now().Before(timeout) {
		if fn() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fn()
}

// TestRoomHub_UniqueConnectedUserIDs_DeDuplicatesMultiTabUsers asserts
// that UniqueConnectedUserIDs counts DISTINCT user ids, not raw
// connections: a second tab from an already-connected user must NOT
// bump the count, and closing the original tab while the second tab
// remains open must NOT drop the count.
func TestRoomHub_UniqueConnectedUserIDs_DeDuplicatesMultiTabUsers(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "T1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"u1": {ID: 101, Role: entity.RoleGuest, DisplayName: "U1"},
			"u2": {ID: 102, Role: entity.RoleGuest, DisplayName: "U2"},
			"u3": {ID: 103, Role: entity.RoleGuest, DisplayName: "U3"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	c1 := dialRoomWS(t, server, "alpha", "u1")
	defer c1.Close()
	c2 := dialRoomWS(t, server, "alpha", "u2")
	defer c2.Close()
	c3 := dialRoomWS(t, server, "alpha", "u3")
	defer c3.Close()

	// Drain initial sync frames so the read pumps are unblocked.
	for _, c := range []*websocket.Conn{c1, c2, c3} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("drain initial sync: %v", err)
		}
	}

	// Three distinct users connected → count must reach 3.
	if !waitFor(t, 2*time.Second, func() bool {
		return hub.UniqueConnectedUserIDs("alpha") == 3
	}) {
		t.Fatalf("expected UniqueConnectedUserIDs=3 after 3 distinct users, got %d", hub.UniqueConnectedUserIDs("alpha"))
	}

	// Second tab from u1 — count MUST stay at 3 (de-duplicated).
	c1b := dialRoomWS(t, server, "alpha", "u1")
	defer c1b.Close()
	c1b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := c1b.ReadMessage(); err != nil {
		t.Fatalf("drain initial sync (u1 second tab): %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		// Wait until both u1 tabs are registered before checking the
		// de-dup property — otherwise we might observe count==3 because
		// the second tab hasn't been registered yet, not because of
		// de-dup. We know it's registered when client count for u1
		// hits 2.
		hub.mu.Lock()
		n := len(hub.clients["alpha"])
		hub.mu.Unlock()
		if n != 4 {
			return false
		}
		return hub.UniqueConnectedUserIDs("alpha") == 3
	}) {
		hub.mu.Lock()
		n := len(hub.clients["alpha"])
		hub.mu.Unlock()
		t.Fatalf("expected UniqueConnectedUserIDs=3 after second u1 tab (de-dup); got count=%d, raw clients=%d", hub.UniqueConnectedUserIDs("alpha"), n)
	}

	// Close the original u1 tab — u1 still has c1b connected, so the
	// distinct user count MUST stay at 3.
	c1.Close()
	if !waitFor(t, 2*time.Second, func() bool {
		hub.mu.Lock()
		n := len(hub.clients["alpha"])
		hub.mu.Unlock()
		if n != 3 {
			return false
		}
		return hub.UniqueConnectedUserIDs("alpha") == 3
	}) {
		hub.mu.Lock()
		n := len(hub.clients["alpha"])
		hub.mu.Unlock()
		t.Fatalf("expected UniqueConnectedUserIDs=3 after closing original u1 tab; got count=%d, raw clients=%d", hub.UniqueConnectedUserIDs("alpha"), n)
	}
}

// TestRoomHub_UniqueConnectedUserIDs_NoClientsReturnsZero asserts the
// empty-room floor: a fresh hub with no clients returns 0, and an
// unknown-slug lookup on a hub with no clients also returns 0.
func TestRoomHub_UniqueConnectedUserIDs_NoClientsReturnsZero(t *testing.T) {
	resolver := newStubRoomResolver()
	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })

	if got := hub.UniqueConnectedUserIDs("alpha"); got != 0 {
		t.Errorf("expected UniqueConnectedUserIDs(alpha)=0 on fresh hub, got %d", got)
	}
	if got := hub.UniqueConnectedUserIDs("unknown-slug"); got != 0 {
		t.Errorf("expected UniqueConnectedUserIDs(unknown-slug)=0 on fresh hub, got %d", got)
	}
}

// TestRoomHub_BroadcastRoomVoteUpdated_ReachesRoomClients pins the
// room_vote_updated envelope: type string, payload field shape, and
// hub-loop seq invariant (the broadcast's seq is strictly greater than
// the initial sync's seq for the same connection).
func TestRoomHub_BroadcastRoomVoteUpdated_ReachesRoomClients(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

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

	// Drain the initial sync; remember its seq.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	if err := json.Unmarshal(syncData, &syncEnv); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}
	if syncEnv.Type != EventRoomQueueSync {
		t.Fatalf("expected first frame %q, got %q", EventRoomQueueSync, syncEnv.Type)
	}

	now := time.Now()
	session := &entity.VoteSession{
		ID:        "skip:alpha:s1",
		Type:      entity.VoteTypeSkip,
		SongID:    "s1",
		SongTitle: "S1",
		SongIndex: 0,
		VotedBy:   map[int]bool{1: true},
		Threshold: 2,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Second),
	}
	state := &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}}
	hub.BroadcastRoomVoteUpdated("alpha", session, 1, state)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read vote-updated broadcast: %v", err)
	}
	var env struct {
		Type   string              `json:"type"`
		SeqNum int64               `json:"seq_num"`
		Data   RoomVoteUpdatedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal vote-updated: %v", err)
	}
	if env.Type != EventRoomVoteUpdated {
		t.Fatalf("expected type %q, got %q", EventRoomVoteUpdated, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Fatalf("vote-updated seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.RoomSlug != "alpha" {
		t.Errorf("expected room_slug=alpha, got %q", env.Data.RoomSlug)
	}
	if env.Data.ActorUserID != 1 {
		t.Errorf("expected actor_user_id=1, got %d", env.Data.ActorUserID)
	}
	if env.Data.Session == nil {
		t.Fatalf("expected session to be non-nil, got nil")
	}
	if env.Data.Session.ID != "skip:alpha:s1" {
		t.Errorf("expected session.id=skip:alpha:s1, got %q", env.Data.Session.ID)
	}
	if env.Data.Session.Type != string(entity.VoteTypeSkip) {
		t.Errorf("expected session.type=skip, got %q", env.Data.Session.Type)
	}
	if env.Data.Session.SongID != "s1" {
		t.Errorf("expected session.song_id=s1, got %q", env.Data.Session.SongID)
	}
	if env.Data.Session.SongTitle != "S1" {
		t.Errorf("expected session.song_title=S1, got %q", env.Data.Session.SongTitle)
	}
	if env.Data.Session.SongIndex != 0 {
		t.Errorf("expected session.song_index=0, got %d", env.Data.Session.SongIndex)
	}
	if env.Data.Session.Threshold != 2 {
		t.Errorf("expected session.threshold=2, got %d", env.Data.Session.Threshold)
	}
	if env.Data.Session.VoteCount != 1 {
		t.Errorf("expected session.vote_count=1, got %d", env.Data.Session.VoteCount)
	}
	if env.Data.State == nil || len(env.Data.State.Songs) != 1 || env.Data.State.Songs[0].ID != "s1" {
		t.Errorf("expected post-mutation state with s1, got %#v", env.Data.State)
	}

	// voted_by must NEVER appear in the WS payload. The session is a
	// DTO that mirrors the sanitised shape; this guard pins the contract.
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if bytes.Contains(raw, []byte("voted_by")) {
		t.Fatalf("payload must not contain voted_by, got %s", string(raw))
	}
	if bytes.Contains(raw, []byte("VotedBy")) {
		t.Fatalf("payload must not contain VotedBy, got %s", string(raw))
	}
}

// TestRoomHub_BroadcastRoomVoteResolved_ReachesRoomClients pins the
// room_vote_resolved envelope: type string, payload field shape, and
// hub-loop seq invariant.
func TestRoomHub_BroadcastRoomVoteResolved_ReachesRoomClients(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("alpha", &entity.Room{ID: 11, Slug: "alpha", Status: entity.RoomStatusActive})
	resolver.SetQueue(11, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

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
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	if err := json.Unmarshal(syncData, &syncEnv); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}
	if syncEnv.Type != EventRoomQueueSync {
		t.Fatalf("expected first frame %q, got %q", EventRoomQueueSync, syncEnv.Type)
	}

	state := &entity.Queue{Songs: []entity.Song{{ID: "s2", Title: "S2"}}}
	hub.BroadcastRoomVoteResolved("alpha", "skip:alpha:s1", "passed", state)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read vote-resolved broadcast: %v", err)
	}
	var env struct {
		Type   string               `json:"type"`
		SeqNum int64                `json:"seq_num"`
		Data   RoomVoteResolvedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal vote-resolved: %v", err)
	}
	if env.Type != EventRoomVoteResolved {
		t.Fatalf("expected type %q, got %q", EventRoomVoteResolved, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Fatalf("vote-resolved seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.RoomSlug != "alpha" {
		t.Errorf("expected room_slug=alpha, got %q", env.Data.RoomSlug)
	}
	if env.Data.SessionID != "skip:alpha:s1" {
		t.Errorf("expected session_id=skip:alpha:s1, got %q", env.Data.SessionID)
	}
	if env.Data.Outcome != "passed" {
		t.Errorf("expected outcome=passed, got %q", env.Data.Outcome)
	}
	if env.Data.State == nil || len(env.Data.State.Songs) != 1 || env.Data.State.Songs[0].ID != "s2" {
		t.Errorf("expected post-mutation state with s2, got %#v", env.Data.State)
	}
}

// --- R09c playback volume broadcast envelope ---
//
// Pin the contract that BroadcastRoomPlaybackVolumeChanged fans out
// to every client connected to the matching room (and only that room)
// with the right type, room_slug, and direction. Mirrors the R09a
// playback broadcast tests; does NOT include a per-room sequence
// invariant because the payload is deliberately stateless (no entity
// queue volume field exists — see events.go RoomPlaybackVolumeChangedData).

// TestRoomHub_BroadcastRoomPlaybackVolumeChanged_FansOutToRoom verifies
// that BroadcastRoomPlaybackVolumeChanged delivers a single envelope
// to every client connected on the per-room hub, with the right type,
// room slug, and direction. Mirrors the R09a playback broadcast tests
// but carries the R09c payload shape (no post-mutation snapshot).
func TestRoomHub_BroadcastRoomPlaybackVolumeChanged_FansOutToRoom(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r09c-vol", &entity.Room{ID: 71, Slug: "r09c-vol", Status: entity.RoomStatusActive})
	resolver.SetQueue(71, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"u1": {ID: 1, Role: entity.RoleHost, DisplayName: "U1"},
			"u2": {ID: 2, Role: entity.RoleGuest, DisplayName: "U2"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	// Two connected clients in the room "r09c-vol".
	c1 := dialRoomWS(t, server, "r09c-vol", "u1")
	defer c1.Close()
	c2 := dialRoomWS(t, server, "r09c-vol", "u2")
	defer c2.Close()

	// Drain initial sync frames so both read pumps are unblocked.
	for _, c := range []*websocket.Conn{c1, c2} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("drain initial sync: %v", err)
		}
	}

	hub.BroadcastRoomPlaybackVolumeChanged("r09c-vol", "up")

	for i, c := range []*websocket.Conn{c1, c2} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("conn %d: read volume broadcast: %v", i, err)
		}
		var env struct {
			Type string                 `json:"type"`
			Data map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("conn %d: unmarshal volume broadcast: %v", i, err)
		}
		if env.Type != EventRoomPlaybackVolumeChanged {
			t.Fatalf("conn %d: type=%q want %q", i, env.Type, EventRoomPlaybackVolumeChanged)
		}
		if env.Data["room_slug"] != "r09c-vol" {
			t.Errorf("conn %d: room_slug=%v want r09c-vol", i, env.Data["room_slug"])
		}
		if env.Data["direction"] != "up" {
			t.Errorf("conn %d: direction=%v want up", i, env.Data["direction"])
		}
	}
}

// --- R09d playback previous broadcast envelope ---
//
// Pin the contract that BroadcastRoomPlaybackSongPrevious fans out to
// every client connected to the matching room (and only that room)
// with the right type, room_slug, previous_index, new_index,
// current_song, status, elapsed, and state. Mirrors the R09a
// room_playback_song_advanced envelope shape (no seq-allocation
// claim — same hub-loop invariant).

// TestRoomHub_BroadcastRoomPlaybackSongPrevious_FansOutToRoom
// verifies that BroadcastRoomPlaybackSongPrevious delivers a single
// envelope to every client on the per-room hub, carrying the prev/next
// indices, the new current_song, status, elapsed, and the full state
// snapshot.
func TestRoomHub_BroadcastRoomPlaybackSongPrevious_FansOutToRoom(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r09d-prev", &entity.Room{ID: 81, Slug: "r09d-prev", Status: entity.RoomStatusActive})
	resolver.SetQueue(81, &entity.Queue{
		Songs: []entity.Song{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B"},
		},
		CurrentIndex: 1,
		Status:       entity.StatusPlaying,
	})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"u1": {ID: 1, Role: entity.RoleHost, DisplayName: "U1"},
			"u2": {ID: 2, Role: entity.RoleGuest, DisplayName: "U2"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	c1 := dialRoomWS(t, server, "r09d-prev", "u1")
	defer c1.Close()
	c2 := dialRoomWS(t, server, "r09d-prev", "u2")
	defer c2.Close()

	// Drain initial sync frames so both read pumps are unblocked.
	for _, c := range []*websocket.Conn{c1, c2} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("drain initial sync: %v", err)
		}
	}

	prev := &entity.Song{ID: "a", Title: "A"}
	hub.BroadcastRoomPlaybackSongPrevious("r09d-prev", 1, 0, prev, entity.StatusPlaying, 0, &entity.Queue{
		Songs:        []entity.Song{*prev, {ID: "b", Title: "B"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	})

	for i, c := range []*websocket.Conn{c1, c2} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("conn %d: read prev broadcast: %v", i, err)
		}
		var env struct {
			Type string                          `json:"type"`
			Data RoomPlaybackSongPreviousData    `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("conn %d: unmarshal prev broadcast: %v", i, err)
		}
		if env.Type != EventRoomPlaybackSongPrevious {
			t.Fatalf("conn %d: type=%q want %q", i, env.Type, EventRoomPlaybackSongPrevious)
		}
		d := env.Data
		if d.RoomSlug != "r09d-prev" {
			t.Errorf("conn %d: room_slug=%q want r09d-prev", i, d.RoomSlug)
		}
		if d.PreviousIndex != 1 || d.NewIndex != 0 {
			t.Errorf("conn %d: prev/new=%d/%d want 1/0", i, d.PreviousIndex, d.NewIndex)
		}
		if d.CurrentSong == nil || d.CurrentSong.ID != "a" {
			t.Errorf("conn %d: current_song=%+v want id=a", i, d.CurrentSong)
		}
		if d.Status != entity.StatusPlaying || d.Elapsed != 0 {
			t.Errorf("conn %d: status=%q elapsed=%d want playing/0", i, d.Status, d.Elapsed)
		}
		if d.State == nil || d.State.CurrentIndex != 0 || len(d.State.Songs) != 2 {
			t.Errorf("conn %d: state=%+v want snapshot with current=0, 2 songs", i, d.State)
		}
	}
}

// --- R09f auto-queue broadcast envelopes ---
//
// Tests pin the per-room auto-queue broadcast envelopes — both
// ride /ws/rooms/{slug} only and use the hub-loop seq allocation.

// TestRoomHub_BroadcastRoomAutoQueueAdded_DeliversEnvelopeAndSeq pins
// the room_auto_queue_added envelope + payload shape + hub-loop seq
// invariant (broadcast seq > sync seq).
func TestRoomHub_BroadcastRoomAutoQueueAdded_DeliversEnvelopeAndSeq(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r09f-add", &entity.Room{ID: 91, Slug: "r09f-add", Status: entity.RoomStatusActive})
	resolver.SetQueue(91, &entity.Queue{
		Songs:        []entity.Song{{ID: "cur", Title: "Cur"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	})

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

	conn := dialRoomWS(t, server, "r09f-add", "valid")
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	if err := json.Unmarshal(syncData, &syncEnv); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}

	state := &entity.Queue{
		Songs:        []entity.Song{{ID: "cur", Title: "Cur"}, {ID: "auto", Title: "AutoSong", AddedBy: entity.SystemUserID}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	}
	hub.BroadcastRoomAutoQueueAdded("r09f-add", entity.Song{
		ID: "auto", Title: "AutoSong", AddedBy: entity.SystemUserID, AddedByID: 0,
	}, "Cur", 0, &entity.Song{ID: "cur", Title: "Cur"}, entity.StatusPlaying, 0, state)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auto-queue-added broadcast: %v", err)
	}
	var env struct {
		Type   string                 `json:"type"`
		SeqNum int64                  `json:"seq_num"`
		Data   RoomAutoQueueAddedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal auto-queue-added: %v", err)
	}
	if env.Type != EventRoomAutoQueueAdded {
		t.Fatalf("expected type %q, got %q", EventRoomAutoQueueAdded, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Fatalf("auto-queue-added seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.RoomSlug != "r09f-add" {
		t.Errorf("expected room_slug=r09f-add, got %q", env.Data.RoomSlug)
	}
	if env.Data.Song.ID != "auto" {
		t.Errorf("expected song.id=auto, got %q", env.Data.Song.ID)
	}
	if env.Data.SourceSongTitle != "Cur" {
		t.Errorf("expected source_song_title=Cur, got %q", env.Data.SourceSongTitle)
	}
	if env.Data.CurrentIndex != 0 {
		t.Errorf("expected current_index=0, got %d", env.Data.CurrentIndex)
	}
	if env.Data.CurrentSong == nil || env.Data.CurrentSong.ID != "cur" {
		t.Errorf("expected current_song.id=cur, got %+v", env.Data.CurrentSong)
	}
	if env.Data.Status != entity.StatusPlaying {
		t.Errorf("expected status=playing, got %q", env.Data.Status)
	}
	if env.Data.Elapsed != 0 {
		t.Errorf("expected elapsed=0, got %d", env.Data.Elapsed)
	}
	if env.Data.State == nil || len(env.Data.State.Songs) != 2 {
		t.Errorf("expected post-mutation state with 2 songs, got %+v", env.Data.State)
	}
}

// TestRoomHub_BroadcastRoomAutoQueueConfigChanged_DeliversEnvelope
// pins the room_auto_queue_config_changed envelope type + payload
// shape + room_slug field.
func TestRoomHub_BroadcastRoomAutoQueueConfigChanged_DeliversEnvelope(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r09f-cfg", &entity.Room{ID: 92, Slug: "r09f-cfg", Status: entity.RoomStatusActive})
	resolver.SetQueue(92, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

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

	conn := dialRoomWS(t, server, "r09f-cfg", "valid")
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("drain initial sync: %v", err)
	}

	hub.BroadcastRoomAutoQueueConfigChanged("r09f-cfg", true, "related")

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auto-queue-config-changed broadcast: %v", err)
	}
	var env struct {
		Type string                            `json:"type"`
		Data RoomAutoQueueConfigChangedData    `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal auto-queue-config-changed: %v", err)
	}
	if env.Type != EventRoomAutoQueueConfigChanged {
		t.Fatalf("expected type %q, got %q", EventRoomAutoQueueConfigChanged, env.Type)
	}
	if env.Data.RoomSlug != "r09f-cfg" {
		t.Errorf("expected room_slug=r09f-cfg, got %q", env.Data.RoomSlug)
	}
	if !env.Data.Enabled || env.Data.Strategy != "related" {
		t.Errorf("expected enabled=true strategy=related, got %+v", env.Data)
	}
}

// TestRoomHub_BroadcastRoomAutoQueueAdded_DoesNotLeakToGlobal pins
// the contract that per-room auto-queue broadcasts must NOT cross to
// the global /ws endpoint. The hub only carries clients of the
// matching room; clients of an unrelated room do not receive the
// envelope. (Cross-room isolation is also asserted at the repo layer
// for persisted state.)
func TestRoomHub_BroadcastRoomAutoQueueAdded_DoesNotLeakToGlobal(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r09f-iso-a", &entity.Room{ID: 93, Slug: "r09f-iso-a", Status: entity.RoomStatusActive})
	resolver.SetRoom("r09f-iso-b", &entity.Room{ID: 94, Slug: "r09f-iso-b", Status: entity.RoomStatusActive})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"a": {ID: 1, Role: entity.RoleHost, DisplayName: "A"},
			"b": {ID: 2, Role: entity.RoleHost, DisplayName: "B"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	cA := dialRoomWS(t, server, "r09f-iso-a", "a")
	defer cA.Close()
	cA.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := cA.ReadMessage(); err != nil {
		t.Fatalf("a drain: %v", err)
	}
	cB := dialRoomWS(t, server, "r09f-iso-b", "b")
	defer cB.Close()
	cB.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := cB.ReadMessage(); err != nil {
		t.Fatalf("b drain: %v", err)
	}

	state := &entity.Queue{Songs: []entity.Song{{ID: "auto", Title: "Auto", AddedBy: entity.SystemUserID}}}
	hub.BroadcastRoomAutoQueueAdded("r09f-iso-a", entity.Song{ID: "auto", Title: "Auto", AddedBy: entity.SystemUserID}, "src", 0, &entity.Song{ID: "auto", Title: "Auto", AddedBy: entity.SystemUserID}, entity.StatusPlaying, 0, state)

	cA.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := cA.ReadMessage(); err != nil {
		t.Fatalf("a should receive broadcast: %v", err)
	}
	cB.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := cB.ReadMessage(); err == nil {
		t.Error("b unexpectedly received auto-queue broadcast for room a")
	}
}

// --- R10b archive / member-removal broadcasts ---
//
// The four tests below pin the WS-side contracts for the R10b room
// deletion / member removal backend runtime:
//
//   - room_archived fan-out to every client connected to the room
//     (reason = "host_archived", archived_at carried in the payload).
//   - room_member_removed is TARGETED: only the removed user's
//     connections receive it; the per-room fan-out path is reserved
//     for room_members_changed.
//   - room_members_changed fans out to the REMAINING clients; the
//     excluded user does not receive it (their connections are about
//     to be closed).
//   - CloseRemovedClient closes the removed user's connections with
//     code 1008 (policy violation) and unregisters them from the
//     hub's client set.
//
// Mirrors the R09f auto-queue broadcast envelope tests for sequencing
// and drain discipline.

// TestRoomHub_BroadcastRoomArchived_PerRoomEnvelope pins the per-room
// room_archived envelope + reason "host_archived" + hub-loop seq
// invariant. R10b addition.
func TestRoomHub_BroadcastRoomArchived_PerRoomEnvelope(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r10b-archive", &entity.Room{ID: 101, Slug: "r10b-archive", Status: entity.RoomStatusActive})
	resolver.SetQueue(101, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

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

	conn := dialRoomWS(t, server, "r10b-archive", "valid")
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, syncData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read initial sync: %v", err)
	}
	var syncEnv struct {
		Type   string `json:"type"`
		SeqNum int64  `json:"seq_num"`
	}
	json.Unmarshal(syncData, &syncEnv)

	now := time.Now()
	hub.BroadcastRoomArchived("r10b-archive", "host_archived", now)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read archive broadcast: %v", err)
	}
	var env struct {
		Type   string           `json:"type"`
		SeqNum int64            `json:"seq_num"`
		Data   RoomArchivedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal archive: %v", err)
	}
	if env.Type != EventRoomArchived {
		t.Errorf("expected type %q, got %q", EventRoomArchived, env.Type)
	}
	if env.SeqNum <= syncEnv.SeqNum {
		t.Errorf("archive seq (%d) must be strictly greater than sync seq (%d)", env.SeqNum, syncEnv.SeqNum)
	}
	if env.Data.Reason != "host_archived" {
		t.Errorf("expected reason=host_archived, got %q", env.Data.Reason)
	}
	if !env.Data.ArchivedAt.Equal(now) {
		t.Errorf("expected archived_at=%v, got %v", now, env.Data.ArchivedAt)
	}
}

// TestRoomHub_BroadcastRoomMemberRemoved_TargetedDelivery pins that
// the targeted room_member_removed envelope reaches ONLY the target
// user's per-room WS connections (other clients in the same room
// do not receive it; the per-room fan-out path is reserved for
// room_members_changed).
func TestRoomHub_BroadcastRoomMemberRemoved_TargetedDelivery(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r10b-rm", &entity.Room{ID: 102, Slug: "r10b-rm", Status: entity.RoomStatusActive})
	resolver.SetQueue(102, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"u1": {ID: 1, Role: entity.RoleHost, DisplayName: "U1"},
			"u2": {ID: 2, Role: entity.RoleGuest, DisplayName: "U2"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	c1 := dialRoomWS(t, server, "r10b-rm", "u1")
	defer c1.Close()
	c2 := dialRoomWS(t, server, "r10b-rm", "u2")
	defer c2.Close()

	// Drain initial sync.
	for _, c := range []*websocket.Conn{c1, c2} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("drain: %v", err)
		}
	}

	hub.BroadcastRoomMemberRemoved("r10b-rm", 2 /*targetUserID*/, "host_removed")

	// Only c2 (user 2) should receive the targeted envelope.
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := c2.ReadMessage()
	if err != nil {
		t.Fatalf("c2 read targeted: %v", err)
	}
	var env struct {
		Type string                `json:"type"`
		Data RoomMemberRemovedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != EventRoomMemberRemoved {
		t.Errorf("expected type %q, got %q", EventRoomMemberRemoved, env.Type)
	}
	if env.Data.RoomSlug != "r10b-rm" || env.Data.UserID != 2 || env.Data.Reason != "host_removed" {
		t.Errorf("unexpected targeted payload: %+v", env.Data)
	}

	// c1 must NOT receive the targeted envelope.
	c1.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := c1.ReadMessage(); err == nil {
		t.Errorf("c1 unexpectedly received the targeted envelope")
	}
}

// TestRoomHub_BroadcastRoomMembersChanged_FanOutRemaining pins that
// the room_members_changed envelope reaches remaining clients of the
// room (the excluded user — the one being removed — does not receive
// it because their connections are about to be closed).
func TestRoomHub_BroadcastRoomMembersChanged_FanOutRemaining(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r10b-changed", &entity.Room{ID: 103, Slug: "r10b-changed", Status: entity.RoomStatusActive})
	resolver.SetQueue(103, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"u1": {ID: 1, Role: entity.RoleHost, DisplayName: "U1"},
			"u2": {ID: 2, Role: entity.RoleGuest, DisplayName: "U2"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	c1 := dialRoomWS(t, server, "r10b-changed", "u1")
	defer c1.Close()
	c2 := dialRoomWS(t, server, "r10b-changed", "u2")
	defer c2.Close()

	// Drain initial sync.
	for _, c := range []*websocket.Conn{c1, c2} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("drain: %v", err)
		}
	}

	members := []entity.RoomMember{
		{RoomID: 103, UserID: 1, Role: entity.RoomRoleHost},
	}
	hub.BroadcastRoomMembersChanged("r10b-changed", members, 2 /*excludeUserID*/)

	// c1 receives the envelope.
	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := c1.ReadMessage()
	if err != nil {
		t.Fatalf("c1 read members-changed: %v", err)
	}
	var env struct {
		Type string                 `json:"type"`
		Data RoomMembersChangedData `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != EventRoomMembersChanged {
		t.Errorf("expected type %q, got %q", EventRoomMembersChanged, env.Type)
	}
	if env.Data.RoomSlug != "r10b-changed" {
		t.Errorf("expected room_slug=r10b-changed, got %q", env.Data.RoomSlug)
	}
	if len(env.Data.Members) != 1 || env.Data.Members[0].UserID != 1 || env.Data.Members[0].Role != entity.RoomRoleHost {
		t.Errorf("expected one host member, got %+v", env.Data.Members)
	}

	// c2 (the removed user) must NOT receive this envelope.
	c2.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := c2.ReadMessage(); err == nil {
		t.Errorf("c2 unexpectedly received the remaining-clients envelope")
	}
}

// TestRoomHub_CloseRemovedClient_SendsCode1008 pins the close-frame
// contract: the removed user's per-room WS connection is closed with
// code 1008 (policy violation) AFTER the targeted room_member_removed
// envelope has been sent. The hub removes the client from the
// room's client set so subsequent broadcasts do not include it.
func TestRoomHub_CloseRemovedClient_SendsCode1008(t *testing.T) {
	resolver := newStubRoomResolver()
	resolver.SetRoom("r10b-close", &entity.Room{ID: 104, Slug: "r10b-close", Status: entity.RoomStatusActive})
	resolver.SetQueue(104, &entity.Queue{Songs: []entity.Song{{ID: "s1", Title: "S1"}}})

	hub := NewRoomWSHub(resolver, resolver, resolver)
	hub.SetOriginChecker(func(_ *http.Request) bool { return true })
	hub.SetSessionResolver(&stubSessionResolver{
		users: map[string]*entity.User{
			"u1": {ID: 1, Role: entity.RoleHost, DisplayName: "U1"},
			"u2": {ID: 2, Role: entity.RoleGuest, DisplayName: "U2"},
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/rooms/{slug}", hub.RegisterHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	go hub.Run()
	defer hub.Close()

	c1 := dialRoomWS(t, server, "r10b-close", "u1")
	defer c1.Close()
	c2 := dialRoomWS(t, server, "r10b-close", "u2")
	// c2 will be closed by the test — do NOT defer c2.Close().

	// Drain initial sync.
	for _, c := range []*websocket.Conn{c1, c2} {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("drain: %v", err)
		}
	}

	// Send targeted envelope first, then close.
	hub.BroadcastRoomMemberRemoved("r10b-close", 2, "host_removed")
	// c2 reads the targeted frame.
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := c2.ReadMessage(); err != nil {
		t.Fatalf("c2 read targeted: %v", err)
	}

	// Close the removed user's connection.
	hub.CloseRemovedClient("r10b-close", 2)

	// Verify the hub removed the client from the room's client set.
	if !waitFor(t, 2*time.Second, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		m, ok := hub.clients["r10b-close"]
		if !ok {
			return true
		}
		for _, cs := range m {
			if cs != nil && cs.userID == 2 {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("expected hub to unregister removed client, still present")
	}

	// c2 should observe the close frame. ReadMessage returns
	// *websocket.CloseError with code 1008.
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := c2.ReadMessage()
	if cerr, ok := err.(*websocket.CloseError); !ok || cerr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("expected close code %d, got err=%v", websocket.ClosePolicyViolation, err)
	}
}
