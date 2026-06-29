package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// VoteExpiryRunner is satisfied by vote.Interactor.
// Defined here to avoid an import cycle.
type VoteExpiryRunner interface {
	ExpireOldSessions(ctx context.Context) []entity.ExpiredSession
	GetActiveSessions() []*entity.VoteSession
}

// PriorityChecker is satisfied by priority.Interactor.
// Defined here to avoid an import cycle.
type PriorityChecker interface {
	CheckAndAwardDailyPriority(ctx context.Context, userID int) error
	GetUserPriorityBalance(ctx context.Context, userID int) (int, error)
}

// SessionResolver is satisfied by *auth.Interactor. Defined here to keep
// the ws package decoupled from the auth infrastructure so tests can
// supply a stub.
type SessionResolver interface {
	ResolveSession(ctx context.Context, token string) (*entity.User, error)
}

// RoomArchivedBroadcast is the payload the hub needs to broadcast a
// room_archived event. The usecase/room layer converts its internal
// RoomArchivedEvent into this shape before handing it to the hub. Defined
// here (not in usecase/room) so the ws package does not import usecase.
type RoomArchivedBroadcast struct {
	RoomID     int64
	Reason     string
	ArchivedAt time.Time
}

// PlayerLeaseSweeper is satisfied by an adapter that wraps
// room.PlayerLeaseInteractor.SweepExpired. Defined here to keep ws
// decoupled from usecase/room.
type PlayerLeaseSweeper interface {
	SweepExpired(ctx context.Context) []RoomArchivedBroadcast
}

// ClientState tracks per-client connection info
type ClientState struct {
	conn        *websocket.Conn
	connectedAt time.Time
	// authenticated reports whether the connection presented a valid bearer
	// session token. Unauthenticated connections can still receive broadcasts
	// and the initial full_sync envelope (preserves backwards compatibility
	// for read-only spectators), but client-originated requests such as
	// request_full_sync after the initial handshake are rejected.
	authenticated bool
	userID        int
	role          entity.Role
	// writeMu serialises all WriteMessage calls for this connection so that
	// the readPump (request_full_sync) cannot write concurrently with hub
	// broadcasts or pings.
	writeMu sync.Mutex
}

// writeMessage acquires the per-connection write lock before sending.
func (cs *ClientState) writeMessage(msgType int, data []byte) error {
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()
	return cs.conn.WriteMessage(msgType, data)
}

// BroadcastMessage wraps messages with sequence numbers
type BroadcastMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	SeqNum    int64       `json:"seq_num"`
	Timestamp time.Time   `json:"timestamp"`
}

// Hub manages WebSocket connections and broadcasts updates.
type Hub struct {
	clients             map[*websocket.Conn]*ClientState
	broadcast           chan *BroadcastMessage
	register            chan *websocket.Conn
	unregister          chan *websocket.Conn
	mu                  sync.Mutex
	seqNum              int64
	getQueueState       func(context.Context) (*entity.Queue, error)
	voteInteractor      VoteExpiryRunner
	priorityInteractor  PriorityChecker
	authInteractor      SessionResolver
	playerLeaseSweeper  PlayerLeaseSweeper
	// originChecker is invoked by RegisterHandler for both the upgrade gate
	// (CheckOrigin) and any pre-upgrade classification. nil means "allow
	// everything" — tests rely on this default so the legacy package-level
	// upgrader is no longer needed.
	originChecker func(r *http.Request) bool
}

// NewHub creates a new Hub.
func NewHub(getQueueState func(context.Context) (*entity.Queue, error)) *Hub {
	return &Hub{
		clients:       make(map[*websocket.Conn]*ClientState),
		broadcast:     make(chan *BroadcastMessage),
		register:      make(chan *websocket.Conn),
		unregister:    make(chan *websocket.Conn),
		getQueueState: getQueueState,
	}
}

// SetAuthInteractor wires the auth interactor used to resolve session tokens
// presented at WebSocket connect time. Required for R05 — unauthenticated
// connections cannot originate client-originated requests.
func (h *Hub) SetAuthInteractor(a SessionResolver) {
	h.authInteractor = a
}

// SetOriginChecker wires the per-request origin allow check. When the
// function returns false, RegisterHandler rejects the upgrade with 403
// before invoking websocket.Upgrade. Passing nil restores the permissive
// default (test-only convenience).
func (h *Hub) SetOriginChecker(fn func(r *http.Request) bool) {
	h.originChecker = fn
}

// Run starts the Hub main loop.
func (h *Hub) Run() {
	ticker := time.NewTicker(5 * time.Second)
	pingTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer pingTicker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// R05: RegisterHandler pre-registers the client with auth state
			// (so the initial full_sync and readPump can see it). If a
			// pre-registration already exists, do not clobber it.
			if _, ok := h.clients[client]; !ok {
				h.clients[client] = &ClientState{
					conn:        client,
					connectedAt: time.Now(),
				}
			}
			h.mu.Unlock()
			log.Println("New WebSocket client connected")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
			log.Println("WebSocket client disconnected")

		case message := <-h.broadcast:
			h.mu.Lock()
			data, err := json.Marshal(message)
			if err != nil {
				log.Printf("Error marshaling broadcast message: %v", err)
				h.mu.Unlock()
				continue
			}

			// Snapshot the client set under the lock so we can write
			// without holding h.mu (avoids deadlock with readPump
			// calling SendFullSync which also needs h.mu briefly).
			clientStates := make([]*ClientState, 0, len(h.clients))
			for _, cs := range h.clients {
				clientStates = append(clientStates, cs)
			}
			h.mu.Unlock()

			for _, cs := range clientStates {
				if err := cs.writeMessage(websocket.TextMessage, data); err != nil {
					log.Printf("Error writing message to client: %v", err)
					cs.conn.Close()
					h.mu.Lock()
					delete(h.clients, cs.conn)
					h.mu.Unlock()
				}
			}

		case <-ticker.C:
			if h.playerLeaseSweeper != nil {
				events := h.playerLeaseSweeper.SweepExpired(context.Background())
				// Run each broadcast in a goroutine — h.broadcast is unbuffered
				// and consumed only by this same Run loop, so sending from
				// inside Run would deadlock.
				for _, e := range events {
					go func(ev RoomArchivedBroadcast) {
						h.Broadcast(EventRoomArchived, RoomArchivedData{
							RoomID:     ev.RoomID,
							Reason:     ev.Reason,
							ArchivedAt: ev.ArchivedAt,
						})
					}(e)
				}
			}
			if h.voteInteractor != nil {
				expired := h.voteInteractor.ExpireOldSessions(context.Background())
				for _, e := range expired {
					// Run in a goroutine to prevent deadlocking the unbuffered broadcast channel
					go func(exp entity.ExpiredSession) {
						h.Broadcast(EventVoteResolved, VoteResolvedData{
							SessionID: exp.SessionID,
							Outcome:   "expired",
							Activity:  exp.Activity,
						})
					}(e)
				}
			}

		case <-pingTicker.C:
			h.mu.Lock()
			clientStates := make([]*ClientState, 0, len(h.clients))
			for _, cs := range h.clients {
				clientStates = append(clientStates, cs)
			}
			h.mu.Unlock()

			for _, cs := range clientStates {
				if err := cs.writeMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("Error sending ping to client: %v", err)
					cs.conn.Close()
					h.mu.Lock()
					delete(h.clients, cs.conn)
					h.mu.Unlock()
				}
			}
		}
	}
}

func (h *Hub) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if h.originChecker != nil && !h.originChecker(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	// The early-return above (line 212) is the real origin gate. The upgrader
	// is unconditionally permissive because the request has already been
	// vetted by h.originChecker(r); browsers will be blocked before Upgrade.
	up := websocket.Upgrader{
		CheckOrigin: func(req *http.Request) bool { return true },
	}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	q := r.URL.Query()

	// Resolve session token if provided.
	var authenticated bool
	var resolvedUserID int
	var resolvedRole entity.Role
	if token := q.Get("session_token"); token != "" && h.authInteractor != nil {
		user, sessErr := h.authInteractor.ResolveSession(r.Context(), token)
		if sessErr == nil && user != nil {
			authenticated = true
			resolvedUserID = user.ID
			resolvedRole = user.Role
		}
	}

	// Parse legacy user_id purely for diagnostic logging. It is NEVER used
	// for auth, attribution, or daily-priority attribution; the resolved
	// session token user is the sole identity source.
	legacyUserID := 0
	if userIDStr := q.Get("user_id"); userIDStr != "" {
		_, _ = fmt.Sscanf(userIDStr, "%d", &legacyUserID)
	}
	if legacyUserID > 0 {
		log.Printf("WebSocket connect presented legacy user_id=%d without session token; ignoring for auth and priority", legacyUserID)
	}

	// Pre-register so the initial full_sync uses the per-connection write
	// lock.
	h.mu.Lock()
	h.clients[conn] = &ClientState{
		conn:          conn,
		connectedAt:   time.Now(),
		authenticated: authenticated,
		userID:        resolvedUserID,
		role:          resolvedRole,
	}
	h.mu.Unlock()

	// Send full sync immediately before registering.
	if h.getQueueState != nil {
		state, err := h.getQueueState(r.Context())
		if err == nil {
			h.SendFullSync(conn, state)
		}
	}

	// Send active vote sessions as individual vote_updated events.
	if h.voteInteractor != nil {
		sessions := h.voteInteractor.GetActiveSessions()
		for _, session := range sessions {
			h.sendToClient(conn, EventVoteUpdated, VoteUpdatedData{
				Session: session,
				Activity: entity.Activity{
					Timestamp:   session.CreatedAt,
					Type:        entity.ActivityVoteCast,
					User:        "System",
					Description: "Active vote session",
				},
				InitialSync: true,
			})
		}
	}

	h.register <- conn

	// Daily priority runs ONLY when the connection is session-authenticated.
	// The legacy ?user_id= hint is intentionally ignored here — it was
	// previously used to call CheckAndAwardDailyPriority, which is exactly
	// the spoofable surface A01 closes.
	if authenticated && resolvedUserID > 0 && h.priorityInteractor != nil {
		go func() {
			ctx := context.Background()
			userID := resolvedUserID
			err := h.priorityInteractor.CheckAndAwardDailyPriority(ctx, userID)
			if err != nil {
				log.Printf("Failed to check daily priority for user %d: %v", userID, err)
				return
			}
			balance, err := h.priorityInteractor.GetUserPriorityBalance(ctx, userID)
			if err == nil {
				h.Broadcast(EventPriorityBalanceUpdated, PriorityBalanceUpdatedData{
					UserID:  userID,
					Balance: balance,
				})
			}
		}()
	}

	// Start read loop to detect disconnections.
	go h.readPump(conn)
}

// readPump reads messages from the client to detect disconnections and
// handles client-initiated requests (e.g. full-sync recovery on sequence gap).
//
// R05 — unauthenticated clients cannot originate client-initiated requests.
// The single known client message type (request_full_sync) is gated on
// h.clients[conn].authenticated; an unauthenticated sender receives a
// structured EventError envelope and the connection stays open so
// spectators keep receiving broadcasts.
func (h *Hub) readPump(conn *websocket.Conn) {
	defer func() {
		h.unregister <- conn
	}()

	// Set read deadline for ping/pong
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			// Connection closed or error occurred
			break
		}

		var clientMsg ClientMessage
		if err := json.Unmarshal(msgBytes, &clientMsg); err != nil {
			log.Printf("Invalid client message: %v", err)
			continue
		}

		// Snapshot the auth state under the hub lock so we don't race
		// with concurrent registrations.
		h.mu.Lock()
		cs := h.clients[conn]
		var authenticated bool
		if cs != nil {
			authenticated = cs.authenticated
		}
		h.mu.Unlock()

		switch clientMsg.Type {
		case ClientMsgRequestFullSync:
			if !authenticated {
				h.sendToClient(conn, EventError, ErrorData{
					Code:    "unauthorized",
					Message: "session_token required to issue client requests",
				})
				continue
			}
			if h.getQueueState != nil {
				state, err := h.getQueueState(context.Background())
				if err != nil {
					log.Printf("Failed to get queue state for full sync recovery: %v", err)
					continue
				}
				if err := h.SendFullSync(conn, state); err != nil {
					log.Printf("Failed to send full sync recovery to client: %v", err)
				}
			}
		default:
			// Unknown / unhandled client messages. R05: reject anything
			// from an unauthenticated client so they cannot probe for
			// future privileged commands.
			if !authenticated {
				h.sendToClient(conn, EventError, ErrorData{
					Code:    "unauthorized",
					Message: "session_token required to issue client requests",
				})
				continue
			}
			log.Printf("Unknown client message type: %s", clientMsg.Type)
		}
	}
}

// Broadcast sends a delta message to all connected clients with sequence number.
func (h *Hub) Broadcast(msgType string, data interface{}) {
	h.mu.Lock()
	h.seqNum++
	msg := &BroadcastMessage{
		Type:      msgType,
		Data:      data,
		SeqNum:    h.seqNum,
		Timestamp: time.Now(),
	}
	h.mu.Unlock()

	h.broadcast <- msg
}

// SendFullSync sends complete state to a specific client.
func (h *Hub) SendFullSync(conn *websocket.Conn, state *entity.Queue) error {
	h.mu.Lock()
	msg := &BroadcastMessage{
		Type:      EventFullSync,
		Data:      FullSyncData{State: state},
		SeqNum:    h.seqNum,
		Timestamp: time.Now(),
	}
	// Look up ClientState to use per-connection write lock. When called
	// from RegisterHandler before the client is added to h.clients, cs is
	// nil and we fall back to a direct write (safe: no concurrent writers
	// exist until readPump starts after h.register <- conn).
	cs := h.clients[conn]
	h.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if cs != nil {
		return cs.writeMessage(websocket.TextMessage, data)
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// sendToClient sends a message to a specific client.
func (h *Hub) sendToClient(conn *websocket.Conn, msgType string, data interface{}) error {
	h.mu.Lock()
	msg := &BroadcastMessage{
		Type:      msgType,
		Data:      data,
		SeqNum:    h.seqNum,
		Timestamp: time.Now(),
	}
	cs := h.clients[conn]
	h.mu.Unlock()

	msgData, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if cs != nil {
		return cs.writeMessage(websocket.TextMessage, msgData)
	}
	return conn.WriteMessage(websocket.TextMessage, msgData)
}

// ConnectedCount returns the number of connected clients.
func (h *Hub) ConnectedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// SetVoteInteractor sets the vote interactor for expiry handling.
func (h *Hub) SetVoteInteractor(v VoteExpiryRunner) {
	h.voteInteractor = v
}

// SetPriorityInteractor sets the priority interactor for daily token checks.
func (h *Hub) SetPriorityInteractor(pi PriorityChecker) {
	h.priorityInteractor = pi
}

// SetPlayerLeaseSweeper wires the periodic lease expiry sweep. When set,
// the existing 5 s ticker calls SweepExpired and broadcasts a room_archived
// event for every returned archive. R06 addition.
func (h *Hub) SetPlayerLeaseSweeper(s PlayerLeaseSweeper) {
	h.playerLeaseSweeper = s
}
