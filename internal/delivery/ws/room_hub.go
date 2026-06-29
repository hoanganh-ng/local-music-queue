package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"local-music-queue/internal/domain/entity"
)

// RoomBySlugResolver returns the room for a slug. Implemented in
// cmd/server via a small adapter over the room repository.
type RoomBySlugResolver interface {
	RoomBySlug(ctx context.Context, slug string) (*entity.Room, error)
}

// RoomMemberResolver reports whether a user is an active member of a
// room. Implemented in cmd/server via a small adapter over the room
// repository.
type RoomMemberResolver interface {
	IsActiveMember(ctx context.Context, roomID int64, userID int) (bool, error)
}

// RoomQueueStateResolver returns the persisted queue for a room id.
// Implemented in cmd/server via a small adapter over the room queue
// interactor.
type RoomQueueStateResolver interface {
	QueueByRoomID(ctx context.Context, roomID int64) (*entity.Queue, error)
}

// RoomQueueResolverFunc adapts a plain func to RoomBySlugResolver.
type RoomQueueResolverFunc func(ctx context.Context, slug string) (*entity.Room, error)

func (f RoomQueueResolverFunc) RoomBySlug(ctx context.Context, slug string) (*entity.Room, error) {
	return f(ctx, slug)
}

// RoomMemberResolverFunc adapts a plain func to RoomMemberResolver.
type RoomMemberResolverFunc func(ctx context.Context, roomID int64, userID int) (bool, error)

func (f RoomMemberResolverFunc) IsActiveMember(ctx context.Context, roomID int64, userID int) (bool, error) {
	return f(ctx, roomID, userID)
}

// RoomQueueStateResolverFunc adapts a plain func to RoomQueueStateResolver.
type RoomQueueStateResolverFunc func(ctx context.Context, roomID int64) (*entity.Queue, error)

func (f RoomQueueStateResolverFunc) QueueByRoomID(ctx context.Context, roomID int64) (*entity.Queue, error) {
	return f(ctx, roomID)
}

// roomPingInterval controls how often the per-room hub sends a WebSocket
// PingMessage to each connected client to keep idle connections alive. It
// is a package-level var so tests can shorten it; production callers must
// NOT mutate it.
var roomPingInterval = 30 * time.Second

// multiRoomResolver composes the three single-method resolvers into
// one shape for the hub's internal use.
type multiRoomResolver struct {
	room   RoomBySlugResolver
	member RoomMemberResolver
	state  RoomQueueStateResolver
}

func (m multiRoomResolver) RoomBySlug(ctx context.Context, slug string) (*entity.Room, error) {
	return m.room.RoomBySlug(ctx, slug)
}
func (m multiRoomResolver) IsActiveMember(ctx context.Context, roomID int64, userID int) (bool, error) {
	return m.member.IsActiveMember(ctx, roomID, userID)
}
func (m multiRoomResolver) QueueByRoomID(ctx context.Context, roomID int64) (*entity.Queue, error) {
	return m.state.QueueByRoomID(ctx, roomID)
}

// roomClientState tracks a single room connection.
type roomClientState struct {
	conn        *websocket.Conn
	roomSlug    string
	userID      int
	writeMu     sync.Mutex
	connectedAt time.Time
}

func (cs *roomClientState) writeMessage(msgType int, data []byte) error {
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()
	return cs.conn.WriteMessage(msgType, data)
}

// RoomWSHub manages per-room WebSocket clients. It is separate from the
// global Hub so the global /ws contract and event envelope remain
// untouched. Sequencing is per-room, single-process, in-memory.
//
// The hub satisfies the roomqueue.Broadcaster interface implicitly (the
// method names match), so delivery/http can wire it directly without
// an adapter in the handler layer.
type RoomWSHub struct {
	resolver        multiRoomResolver
	sessionResolver RoomSessionResolver
	originChecker   func(r *http.Request) bool

	mu      sync.Mutex
	clients map[string]map[*websocket.Conn]*roomClientState // roomSlug -> clients
	seqNum  map[string]int64                               // roomSlug -> next seq

	broadcast  chan roomBroadcast
	register   chan *roomClientState
	unregister chan *roomClientState

	closed chan struct{}
	once   sync.Once
}

// RoomSessionResolver is satisfied by *auth.Interactor. Same shape as
// the global hub's SessionResolver.
type RoomSessionResolver interface {
	ResolveSession(ctx context.Context, token string) (*entity.User, error)
}

// roomBroadcast is the internal envelope sent through the hub loop.
type roomBroadcast struct {
	roomSlug string
	payload  BroadcastMessage
}

// NewRoomWSHub constructs a RoomWSHub. The three resolvers are split
// so each adapter in cmd/server is a single-function closure that
// delegates to the appropriate repository / interactor. Each adapter
// MUST be safe for concurrent use; the hub invokes them from multiple
// goroutines (the hub loop, RegisterHandler, the initial sync path).
func NewRoomWSHub(roomResolver RoomBySlugResolver, memberResolver RoomMemberResolver, stateResolver RoomQueueStateResolver) *RoomWSHub {
	return &RoomWSHub{
		resolver: multiRoomResolver{
			room:   roomResolver,
			member: memberResolver,
			state:  stateResolver,
		},
		clients:    map[string]map[*websocket.Conn]*roomClientState{},
		seqNum:     map[string]int64{},
		broadcast:  make(chan roomBroadcast),
		register:   make(chan *roomClientState),
		unregister: make(chan *roomClientState),
		closed:     make(chan struct{}),
	}
}

// SetOriginChecker wires the per-request origin allow check. When the
// function returns false, RegisterHandler rejects the upgrade with 403
// before invoking websocket.Upgrade. nil means "allow everything".
func (h *RoomWSHub) SetOriginChecker(fn func(r *http.Request) bool) {
	h.originChecker = fn
}

// SetSessionResolver wires the session resolver used to authenticate
// the session_token presented at WebSocket connect time.
func (h *RoomWSHub) SetSessionResolver(s RoomSessionResolver) {
	h.sessionResolver = s
}

// nextSeq returns the next per-room sequence number. Single-process /
// in-memory: no cross-process ordering claim.
func (h *RoomWSHub) nextSeq(roomSlug string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seqNum[roomSlug]++
	return h.seqNum[roomSlug]
}

// Close shuts down the hub loop. Safe to call multiple times.
func (h *RoomWSHub) Close() {
	h.once.Do(func() { close(h.closed) })
}

// Run is the hub main loop. Mirrors the global hub's select shape but
// with a per-room client set. The broadcast channel is unbuffered, so
// callers from outside Run MUST dispatch from a goroutine; dispatch()
// wraps the send accordingly.
func (h *RoomWSHub) Run() {
	pingTicker := time.NewTicker(roomPingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-h.closed:
			return
		case cs := <-h.register:
			h.mu.Lock()
			if h.clients[cs.roomSlug] == nil {
				h.clients[cs.roomSlug] = map[*websocket.Conn]*roomClientState{}
			}
			h.clients[cs.roomSlug][cs.conn] = cs
			h.mu.Unlock()
			log.Printf("room ws: client connected to %s", cs.roomSlug)
		case cs := <-h.unregister:
			h.mu.Lock()
			if m, ok := h.clients[cs.roomSlug]; ok {
				if _, present := m[cs.conn]; present {
					delete(m, cs.conn)
					cs.conn.Close()
				}
				if len(m) == 0 {
					delete(h.clients, cs.roomSlug)
				}
			}
			h.mu.Unlock()
			log.Printf("room ws: client disconnected from %s", cs.roomSlug)
		case msg := <-h.broadcast:
			// Snapshot clients under the lock; writes happen without the lock
			// so a slow peer cannot stall the hub loop.
			h.mu.Lock()
			conns := make([]*roomClientState, 0, len(h.clients[msg.roomSlug]))
			for _, cs := range h.clients[msg.roomSlug] {
				conns = append(conns, cs)
			}
			h.mu.Unlock()

			data, err := json.Marshal(msg.payload)
			if err != nil {
				log.Printf("room ws: marshal failed: %v", err)
				continue
			}
			for _, cs := range conns {
				if err := cs.writeMessage(websocket.TextMessage, data); err != nil {
					log.Printf("room ws: write failed (%s): %v", msg.roomSlug, err)
					cs.unregisterAndClose(h)
				}
			}
		case <-pingTicker.C:
			// Snapshot clients under the lock so a slow write cannot hold the
			// hub mutex for the duration of the network round-trip.
			h.mu.Lock()
			clients := make([]*roomClientState, 0)
			for _, m := range h.clients {
				for _, cs := range m {
					clients = append(clients, cs)
				}
			}
			h.mu.Unlock()

			for _, cs := range clients {
				if err := cs.writeMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("room ws: ping failed (%s): %v", cs.roomSlug, err)
					cs.unregisterAndClose(h)
				}
			}
		}
	}
}

// dispatch sends a payload to a room's clients. Non-blocking — wraps
// in a goroutine so the hub loop cannot deadlock on its own unbuffered
// broadcast channel (mirrors the global hub fix in hub.go ticker path).
func (h *RoomWSHub) dispatch(roomSlug string, msgType string, data interface{}) {
	seq := h.nextSeq(roomSlug)
	payload := BroadcastMessage{
		Type:      msgType,
		Data:      data,
		SeqNum:    seq,
		Timestamp: time.Now(),
	}
	go func() {
		select {
		case <-h.closed:
			return
		case h.broadcast <- roomBroadcast{roomSlug: roomSlug, payload: payload}:
		}
	}()
}

// --- Broadcaster methods (satisfy usecase/roomqueue.Broadcaster) ---

func (h *RoomWSHub) BroadcastRoomQueueSync(roomSlug string, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomQueueSync, RoomQueueSyncData{RoomSlug: roomSlug, State: state})
}

func (h *RoomWSHub) BroadcastRoomQueueSongAdded(roomSlug string, song entity.Song, position int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomQueueSongAdded, RoomQueueSongAddedData{
		RoomSlug: roomSlug, Song: song, Position: position, State: state,
	})
}

func (h *RoomWSHub) BroadcastRoomQueueSongRemoved(roomSlug string, removedIndex int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomQueueSongRemoved, RoomQueueSongRemovedData{
		RoomSlug: roomSlug, RemovedIndex: removedIndex, State: state,
	})
}

func (h *RoomWSHub) BroadcastRoomQueueCleared(roomSlug string, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomQueueCleared, RoomQueueClearedData{
		RoomSlug: roomSlug, State: state,
	})
}

// --- RegisterHandler ---

// RegisterHandler handles GET /ws/rooms/{slug}?session_token=<opaque>.
// Rejects when origin is not allowed, no session token is presented, or
// the resolved session user is not an active member of the room. On
// accept: sends initial room_queue_sync, registers the client, starts
// the read pump.
func (h *RoomWSHub) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if h.originChecker != nil && !h.originChecker(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	slug := r.PathValue("slug")

	// Resolve session token. Identity comes ONLY from the resolved
	// session user — query params / claims / body fields are ignored.
	token := r.URL.Query().Get("session_token")
	if token == "" || h.sessionResolver == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := h.sessionResolver.ResolveSession(r.Context(), token)
	if err != nil || user == nil || user.ID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	roomObj, err := h.resolver.RoomBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if roomObj.Status != entity.RoomStatusActive {
		http.Error(w, "archived", http.StatusConflict)
		return
	}
	ok, err := h.resolver.IsActiveMember(r.Context(), roomObj.ID, user.ID)
	if err != nil || !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	up := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("room ws upgrade failed: %v", err)
		return
	}

	cs := &roomClientState{
		conn:        conn,
		roomSlug:    slug,
		userID:      user.ID,
		connectedAt: time.Now(),
	}

	// Send initial room_queue_sync BEFORE registering so the client
	// receives its snapshot even if the register channel is slow.
	if state, err := h.resolver.QueueByRoomID(r.Context(), roomObj.ID); err == nil && state != nil {
		h.sendToClient(cs, EventRoomQueueSync, RoomQueueSyncData{RoomSlug: slug, State: state})
	}

	h.register <- cs
	go h.readPump(cs)
}

// sendToClient sends a single envelope to one client, using the
// per-connection write lock so concurrent broadcasts cannot interleave
// with this call.
func (h *RoomWSHub) sendToClient(cs *roomClientState, msgType string, data interface{}) {
	seq := h.nextSeq(cs.roomSlug)
	payload := BroadcastMessage{
		Type:      msgType,
		Data:      data,
		SeqNum:    seq,
		Timestamp: time.Now(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("room ws: marshal sync failed: %v", err)
		return
	}
	if err := cs.writeMessage(websocket.TextMessage, raw); err != nil {
		log.Printf("room ws: sync write failed: %v", err)
	}
}

// readPump detects disconnects. R07b room WS is read-only for clients —
// we ignore all inbound frames. Identity is fixed at connect time;
// client-supplied user_id fields in frames are intentionally ignored.
func (h *RoomWSHub) readPump(cs *roomClientState) {
	defer func() { h.unregister <- cs }()
	cs.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	cs.conn.SetPongHandler(func(string) error {
		cs.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		if _, _, err := cs.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// unregisterAndClose safely removes a client from the hub's client map and
// closes the underlying connection. The channel send is non-blocking via the
// default branch so a closed hub never panics here.
func (cs *roomClientState) unregisterAndClose(h *RoomWSHub) {
	h.mu.Lock()
	if m, ok := h.clients[cs.roomSlug]; ok {
		delete(m, cs.conn)
		if len(m) == 0 {
			delete(h.clients, cs.roomSlug)
		}
	}
	h.mu.Unlock()
	cs.conn.Close()
	select {
	case h.unregister <- cs:
	default:
	}
}