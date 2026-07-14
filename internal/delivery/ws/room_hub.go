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
// NOT mutate it. The value is snapshotted into RoomWSHub at construction
// time so concurrent tests with different intervals don't race.
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
	roomID      int64
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
	pingInterval    time.Duration

	mu      sync.Mutex
	clients map[string]map[*websocket.Conn]*roomClientState // roomSlug -> clients
	seqNum  map[string]int64                               // roomSlug -> next seq

	broadcast       chan roomBroadcast
	broadcastExcept chan roomBroadcastExcept
	register        chan registerReq
	unregister      chan *roomClientState

	closed chan struct{}
	once   sync.Once
}

// registerReq couples a connecting client with the channel the hub loop
// closes after the initial sync attempt completes (or fails). The buffered
// channel lets RegisterHandler wait without leaking a goroutine if the hub
// loop is busy with another slow write.
type registerReq struct {
	cs          *roomClientState
	initialSync chan error
}

// RoomSessionResolver is satisfied by *auth.Interactor. Same shape as
// the global hub's SessionResolver.
type RoomSessionResolver interface {
	ResolveSession(ctx context.Context, token string) (*entity.User, error)
}

// roomBroadcast is the internal envelope sent through the hub loop. It
// carries the raw (msgType, data) pair and is intentionally NOT a
// pre-sequenced BroadcastMessage: the hub loop is the single owner of
// per-room sequence allocation, so the seq_num is stamped inside Run
// when the broadcast case is dequeued. This guarantees that any
// broadcast dequeued after the register case (which added the client
// and stamped the initial sync) carries a strictly greater seq than
// the initial sync for every connected client.
type roomBroadcast struct {
	roomSlug string
	msgType  string
	data     interface{}
}

// roomBroadcastExcept is the internal envelope for BroadcastRoomMembersChanged:
// it carries an excludeUserID so the hub loop can skip the removed user's
// connections (which are about to be closed).
type roomBroadcastExcept struct {
	roomSlug      string
	msgType       string
	data          interface{}
	excludeUserID int
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
		pingInterval: roomPingInterval,
		clients:      map[string]map[*websocket.Conn]*roomClientState{},
		seqNum:       map[string]int64{},
		broadcast:       make(chan roomBroadcast),
		broadcastExcept: make(chan roomBroadcastExcept),
		register:        make(chan registerReq),
		unregister:      make(chan *roomClientState),
		closed:          make(chan struct{}),
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
//
// nextSeq is called ONLY from inside the hub loop (the register case
// stamps the initial-sync seq, the broadcast case stamps every delta
// seq). Centralising the allocation in Run is what preserves the
// sequenced-delta contract: the register case finishes allocating its
// seq before any subsequent broadcast case allocates the next one, so
// the seq stamped on the initial sync is strictly less than the seq
// stamped on any broadcast that reaches the new client.
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
	pingTicker := time.NewTicker(h.pingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-h.closed:
			return
		case req := <-h.register:
			cs := req.cs
			h.mu.Lock()
			if h.clients[cs.roomSlug] == nil {
				h.clients[cs.roomSlug] = map[*websocket.Conn]*roomClientState{}
			}
			h.clients[cs.roomSlug][cs.conn] = cs
			h.mu.Unlock()
			log.Printf("room ws: client connected to %s", cs.roomSlug)

			// Initial sync runs INSIDE the hub loop so the seq allocation is
			// linearised with the broadcast channel: any later dispatch() call
			// will allocate a strictly greater seq_num than the one we stamp here.
			state, err := h.resolver.QueueByRoomID(context.Background(), cs.roomID)
			if err != nil || state == nil {
				log.Printf("room ws: initial sync fetch failed (%s): %v", cs.roomSlug, err)
				close(req.initialSync)
				continue
			}
			h.sendInitialSync(cs, state)
			close(req.initialSync)
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
		case msg := <-h.broadcastExcept:
			h.mu.Lock()
			conns := make([]*roomClientState, 0, len(h.clients[msg.roomSlug]))
			for _, cs := range h.clients[msg.roomSlug] {
				if cs != nil && cs.userID != msg.excludeUserID {
					conns = append(conns, cs)
				}
			}
			h.mu.Unlock()

			seq := h.nextSeq(msg.roomSlug)
			payload := BroadcastMessage{
				Type:      msg.msgType,
				Data:      msg.data,
				SeqNum:    seq,
				Timestamp: time.Now(),
			}
			data, err := json.Marshal(payload)
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
		case msg := <-h.broadcast:
			// Snapshot clients under the lock; writes happen without the lock
			// so a slow peer cannot stall the hub loop.
			h.mu.Lock()
			conns := make([]*roomClientState, 0, len(h.clients[msg.roomSlug]))
			for _, cs := range h.clients[msg.roomSlug] {
				conns = append(conns, cs)
			}
			h.mu.Unlock()

			// Stamp the seq_num HERE, inside the hub loop. Doing it here
			// (rather than in dispatch) is what makes the initial-sync
			// contract safe: any register case that ran before this
			// broadcast was dequeued has already stamped its own seq under
			// the same hub-loop invariant, so the seq we allocate now is
			// strictly greater than every initial-sync seq stamped earlier
			// in the room.
			seq := h.nextSeq(msg.roomSlug)
			payload := BroadcastMessage{
				Type:      msg.msgType,
				Data:      msg.data,
				SeqNum:    seq,
				Timestamp: time.Now(),
			}
			data, err := json.Marshal(payload)
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
//
// dispatch does NOT allocate a seq_num; that happens in the hub loop
// when the broadcast is dequeued. Allocating the seq inside the loop is
// the single point of ordering for per-room sequences, so the
// initial-sync seq stamped by the register case is guaranteed to be
// strictly less than the seq stamped by this broadcast whenever the
// register case ran first.
func (h *RoomWSHub) dispatch(roomSlug string, msgType string, data interface{}) {
	go func() {
		select {
		case <-h.closed:
			return
		case h.broadcast <- roomBroadcast{roomSlug: roomSlug, msgType: msgType, data: data}:
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

// BroadcastRoomQueueSongPrioritized is fired after a successful
// POST /api/rooms/{slug}/queue/prioritize. Per R07b invariants it
// does NOT allocate a seq itself — dispatch() is a goroutine fan-out
// that enqueues a (roomSlug, msgType, data) tuple onto the hub's
// broadcast channel, and the hub loop stamps the seq on dequeue.
// Centralising seq allocation in Run is what guarantees the initial
// sync (stamp under the register case) is the floor for every
// subsequent delta on every connected client.
func (h *RoomWSHub) BroadcastRoomQueueSongPrioritized(roomSlug string, fromIndex, toIndex int, song entity.Song, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomQueueSongPrioritized, RoomQueueSongPrioritizedData{
		RoomSlug: roomSlug, FromIndex: fromIndex, ToIndex: toIndex, Song: song, State: state,
	})
}

// --- R09a playback broadcast methods ---
//
// All three follow the same dispatch pattern as the R07b/R07d
// broadcasters above: dispatch() does NOT allocate seq; the hub
// loop stamps seq on dequeue. Adding them does not change the global
// /ws 16-event inventory — they ride the per-room endpoint only.

func (h *RoomWSHub) BroadcastRoomPlaybackStatusChanged(roomSlug string, status entity.PlaybackStatus, elapsed int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomPlaybackStatusChanged, RoomPlaybackStatusChangedData{
		RoomSlug: roomSlug, Status: status, Elapsed: elapsed, State: state,
	})
}

func (h *RoomWSHub) BroadcastRoomPlaybackElapsedSync(roomSlug string, elapsed int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomPlaybackElapsedSync, RoomPlaybackElapsedSyncData{
		RoomSlug: roomSlug, Elapsed: elapsed, State: state,
	})
}

func (h *RoomWSHub) BroadcastRoomPlaybackSongAdvanced(roomSlug, reason string, previousIndex, newIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomPlaybackSongAdvanced, RoomPlaybackSongAdvancedData{
		RoomSlug: roomSlug, Reason: reason, PreviousIndex: previousIndex, NewIndex: newIndex,
		CurrentSong: currentSong, Status: status, Elapsed: elapsed, State: state,
	})
}

// --- R09b vote broadcast methods ---
//
// Follow the same dispatch pattern as the R07b/R07d/R09a broadcasters:
// dispatch() does NOT allocate seq; the hub loop stamps seq on
// dequeue. Adding them does not change the global /ws 16-event
// inventory — they ride the per-room endpoint only.

func (h *RoomWSHub) BroadcastRoomVoteUpdated(roomSlug string, session *entity.VoteSession, actorUserID int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomVoteUpdated, RoomVoteUpdatedData{
		RoomSlug: roomSlug, Session: newRoomVoteSessionDTO(session), ActorUserID: actorUserID, State: state,
	})
}

func (h *RoomWSHub) BroadcastRoomVoteResolved(roomSlug string, sessionID, outcome string, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomVoteResolved, RoomVoteResolvedData{
		RoomSlug: roomSlug, SessionID: sessionID, Outcome: outcome, State: state,
	})
}

// --- R09c playback volume broadcast ---
//
// dispatch() does NOT allocate seq; the hub loop stamps seq on
// dequeue. Adding this method does not change the global /ws
// 16-event inventory — the event rides the per-room endpoint only.
//
// There is no entity.Queue volume field and no persisted volume
// state, so the broadcast payload intentionally OMITS a state
// snapshot (R07d/R09a broadcasts include one; R09c does not because
// nothing changed on the persisted queue).

func (h *RoomWSHub) BroadcastRoomPlaybackVolumeChanged(roomSlug, direction string) {
	h.dispatch(roomSlug, EventRoomPlaybackVolumeChanged, RoomPlaybackVolumeChangedData{
		RoomSlug: roomSlug, Direction: direction,
	})
}

// --- R09d playback previous broadcast ---
//
// dispatch() does NOT allocate seq; the hub loop stamps seq on
// dequeue. Adding this method does not change the global /ws
// 16-event inventory — the event rides the per-room endpoint only.
//
// The payload carries the full post-mutation state snapshot
// (mirroring R09a's BroadcastRoomPlaybackSongAdvanced shape) so
// subscribers can render the new queue without a follow-up sync.

func (h *RoomWSHub) BroadcastRoomPlaybackSongPrevious(roomSlug string, previousIndex, newIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomPlaybackSongPrevious, RoomPlaybackSongPreviousData{
		RoomSlug:      roomSlug,
		PreviousIndex: previousIndex,
		NewIndex:      newIndex,
		CurrentSong:   currentSong,
		Status:        status,
		Elapsed:       elapsed,
		State:         state,
	})
}

// --- R09f per-room auto-queue broadcast methods ---
//
// dispatch() does NOT allocate seq; the hub loop stamps seq on
// dequeue. Adding these methods does not change the global /ws
// 16-event inventory — both events ride the per-room endpoint only.
// The roomautoqueue use case invokes these via the roomqueue
// .Broadcaster seam after a successful AddRoomAutoQueueSong (room
// auto-add) or SetEnabled (room auto-config-changed).

// BroadcastRoomAutoQueueAdded emits room_auto_queue_added. The
// payload carries the auto-added song, its source song title, the
// authoritative post-mutation snapshot (current_index, current_song,
// status, elapsed — captured by the roomautoqueue use case from the
// roomqueue.AddRoomAutoQueueSong return tuple), and the full queue
// state. Stale candidates never reach this call site — the
// roomautoqueue use case only invokes the seam on a successful
// insertion.
func (h *RoomWSHub) BroadcastRoomAutoQueueAdded(roomSlug string, song entity.Song, sourceSongTitle string, currentIndex int, currentSong *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue) {
	h.dispatch(roomSlug, EventRoomAutoQueueAdded, RoomAutoQueueAddedData{
		RoomSlug:        roomSlug,
		Song:            song,
		SourceSongTitle: sourceSongTitle,
		CurrentIndex:    currentIndex,
		CurrentSong:     currentSong,
		Status:          status,
		Elapsed:         elapsed,
		State:           state,
	})
}

// BroadcastRoomAutoQueueConfigChanged emits room_auto_queue_config_changed.
// The HTTP toggle handler invokes this after a successful SetEnabled
// so per-room WebSocket subscribers observe the new config without a
// follow-up fetch.
func (h *RoomWSHub) BroadcastRoomAutoQueueConfigChanged(roomSlug string, enabled bool, strategy string) {
	h.dispatch(roomSlug, EventRoomAutoQueueConfigChanged, RoomAutoQueueConfigChangedData{
		RoomSlug: roomSlug,
		Enabled:  enabled,
		Strategy: strategy,
	})
}

// --- R10b archive / member-removal broadcasts ---

// BroadcastRoomArchived emits the existing room_archived wire value
// on the per-room hub. It uses the existing RoomArchivedData payload
// (R06) — R10b reuses the R06 envelope shape and only adds the
// reason sentinel "host_archived" on top of the existing
// "player_lease_expired" / "explicit" / "host_left" sentinels. The
// global /ws archive broadcast continues to ride the R06 path
// unchanged; this method is the per-room counterpart.
//
// R10b: the reason sentinel "host_archived" is new. The existing
// entity.PlayerLeaseArchiveReason sentinels remain untouched.
func (h *RoomWSHub) BroadcastRoomArchived(roomSlug string, reason string, archivedAt time.Time) {
	h.dispatch(roomSlug, EventRoomArchived, RoomArchivedData{
		RoomID:     0, // per-room clients identify the room by slug, not id
		Reason:     reason,
		ArchivedAt: archivedAt,
	})
}

// BroadcastRoomMemberRemoved delivers a targeted room_member_removed
// envelope to the per-room WS connections owned by targetUserID for
// the given roomSlug. The envelope is sent BEFORE the server closes
// those connections (the close-frame order is the close helper's
// contract). R10b addition; rides /ws/rooms/{slug} only.
func (h *RoomWSHub) BroadcastRoomMemberRemoved(roomSlug string, targetUserID int, reason string) {
	// Snapshot the target connections under h.mu so a concurrent
	// register/unregister cannot interleave.
	h.mu.Lock()
	conns := make([]*roomClientState, 0)
	if m, ok := h.clients[roomSlug]; ok {
		for _, cs := range m {
			if cs != nil && cs.userID == targetUserID {
				conns = append(conns, cs)
			}
		}
	}
	h.mu.Unlock()

	// Stamp seq outside the broadcast path so the targeted frame
	// carries a strictly greater seq than the latest broadcast. We
	// bypass dispatch() (which is per-room fan-out) because the
	// target set is a per-user subset.
	seq := h.nextSeq(roomSlug)
	payload := BroadcastMessage{
		Type: EventRoomMemberRemoved,
		Data: RoomMemberRemovedData{
			RoomSlug: roomSlug,
			UserID:   targetUserID,
			Reason:   reason,
		},
		SeqNum:    seq,
		Timestamp: time.Now(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("room ws: marshal room_member_removed failed: %v", err)
		return
	}
	for _, cs := range conns {
		if err := cs.writeMessage(websocket.TextMessage, raw); err != nil {
			log.Printf("room ws: write room_member_removed failed (%s): %v", roomSlug, err)
			cs.unregisterAndClose(h)
		}
	}
}

// BroadcastRoomMembersChanged delivers a room_members_changed envelope
// to every per-room WS client of roomSlug (excluding the removed
// user's connections — they are being closed by the close helper).
// R10b addition; rides /ws/rooms/{slug} only.
//
// excludeUserID is the user id of the removed client whose
// connections are about to be closed; pass 0 to deliver to all
// clients of the room.
func (h *RoomWSHub) BroadcastRoomMembersChanged(roomSlug string, members []entity.RoomMember, excludeUserID int) {
	out := make([]RoomMemberInfo, 0, len(members))
	for _, m := range members {
		out = append(out, RoomMemberInfo{UserID: m.UserID, Role: m.Role})
	}
	// Take a per-room fan-out path but skip the excluded user via
	// the post-snapshot filter. dispatch() does NOT allocate seq;
	// the hub loop stamps seq on dequeue.
	h.dispatchToRoomExcept(roomSlug, EventRoomMembersChanged, RoomMembersChangedData{
		RoomSlug: roomSlug,
		Members:  out,
	}, excludeUserID)
}

// BroadcastRoomChatMessageCreated delivers a room_chat_message_created
// envelope to every per-room WS client of roomSlug after a
// successful POST /api/rooms/{slug}/chat/messages. R11a addition;
// rides /ws/rooms/{slug} only. The global /ws 16-event inventory is
// unchanged.
//
// The sender display_name is supplied by the caller (the chat
// interactor resolves it via UserRepository). The wire NEVER carries
// the sender email.
func (h *RoomWSHub) BroadcastRoomChatMessageCreated(roomSlug string, msg *entity.RoomChatMessage, displayName string) {
	h.dispatch(roomSlug, EventRoomChatMessageCreated, RoomChatMessageCreatedData{
		Message: RoomChatMessage{
			ID:       msg.ID,
			RoomSlug: roomSlug,
			Sender: RoomChatMessageSender{
				UserID:      msg.SenderID,
				DisplayName: displayName,
			},
			Content:   msg.Content,
			CreatedAt: msg.CreatedAt,
		},
	})
}

// dispatchToRoomExcept is like dispatch but lets the hub loop skip
// connections owned by a specific user. Used by R10b to deliver
// room_members_changed to remaining clients when the removed user's
// connections are about to be closed.
func (h *RoomWSHub) dispatchToRoomExcept(roomSlug string, msgType string, data interface{}, excludeUserID int) {
	go func() {
		select {
		case <-h.closed:
			return
		case h.broadcastExcept <- roomBroadcastExcept{
			roomSlug:      roomSlug,
			msgType:       msgType,
			data:          data,
			excludeUserID: excludeUserID,
		}:
		}
	}()
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
		roomID:      roomObj.ID,
		userID:      user.ID,
		connectedAt: time.Now(),
	}

	// Order-safe registration: the hub loop is the only place that allocates
	// per-room seqs, so any subsequent broadcast will carry a strictly greater
	// seq than the initial sync the loop is about to send.
	initialSync := make(chan error, 1)
	select {
	case h.register <- registerReq{cs: cs, initialSync: initialSync}:
	case <-h.closed:
		conn.Close()
		return
	}

	// Wait briefly for the initial sync to be sent. The buffered channel
	// guarantees the hub loop's close() does not block; the timeout guards
	// against a wedged hub loop without hanging the HTTP request forever.
	select {
	case <-initialSync:
	case <-time.After(2 * time.Second):
		log.Printf("room ws: initial sync timed out for %s", slug)
	case <-h.closed:
		conn.Close()
		return
	}

	go h.readPump(cs)
}

// sendInitialSync stamps a room_queue_sync envelope with the NEXT per-room
// seq (allocated under h.mu via nextSeq) and writes it to a single client.
// Always called from inside the hub loop so the seq allocation is
// linearised with broadcasts.
func (h *RoomWSHub) sendInitialSync(cs *roomClientState, state *entity.Queue) {
	seq := h.nextSeq(cs.roomSlug)
	payload := BroadcastMessage{
		Type:      EventRoomQueueSync,
		Data:      RoomQueueSyncData{RoomSlug: cs.roomSlug, State: state},
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
		cs.unregisterAndClose(h)
	}
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

// UniqueConnectedUserIDs returns the count of distinct user IDs
// currently connected to the room via the per-room hub. Multiple
// connections from the same user (e.g. two browser tabs) collapse to
// one. Used by the R09b vote interactor to derive the strict-majority
// vote threshold at session creation.
//
// Returns 0 when the hub has no clients in the room (or no clients
// yet). The vote interactor treats a zero unique-user-count as
// "threshold=2" (so even one connected user can never pass a vote
// alone, matching the global strict-majority rule with a 2-voter
// floor).
//
// The set is read under h.mu so concurrent register/unregister cases
// cannot interleave with the snapshot.
func (h *RoomWSHub) UniqueConnectedUserIDs(roomSlug string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns, ok := h.clients[roomSlug]
	if !ok {
		return 0
	}
	seen := make(map[int]struct{}, len(conns))
	for _, cs := range conns {
		if cs == nil || cs.userID == 0 {
			continue
		}
		seen[cs.userID] = struct{}{}
	}
	return len(seen)
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

// closeWithCode closes a single per-room WS connection with a
// specified close code + reason. R10b uses this to close the
// removed client's connections with code 1008 (policy violation)
// AFTER the targeted room_member_removed envelope has been sent.
//
// The close-frame send is wrapped in a short write deadline so a
// wedged peer cannot stall the hub loop. After sending the close
// frame we unregister the client so subsequent broadcasts do not
// include it.
func (h *RoomWSHub) closeWithCode(cs *roomClientState, code int, reason string) {
	// Best-effort close-frame write under a deadline.
	_ = cs.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	msg := websocket.FormatCloseMessage(code, reason)
	if err := cs.writeMessage(websocket.CloseMessage, msg); err != nil {
		log.Printf("room ws: close frame send failed (%s): %v", cs.roomSlug, err)
	}
	// Unregister under the hub lock so concurrent broadcasts
	// cannot re-add the conn after we remove it.
	cs.unregisterAndClose(h)
}

// CloseRemovedClient is the R10b seam: after the targeted
// room_member_removed envelope has been delivered, the handler
// invokes this method to close the removed user's per-room
// connections with code 1008 (policy violation) and unregister
// them from the hub. Safe to call when no connections exist for
// the targetUserID.
func (h *RoomWSHub) CloseRemovedClient(roomSlug string, targetUserID int) {
	h.mu.Lock()
	conns := make([]*roomClientState, 0)
	if m, ok := h.clients[roomSlug]; ok {
		for _, cs := range m {
			if cs != nil && cs.userID == targetUserID {
				conns = append(conns, cs)
			}
		}
	}
	h.mu.Unlock()
	for _, cs := range conns {
		h.closeWithCode(cs, websocket.ClosePolicyViolation, "removed from room")
	}
}