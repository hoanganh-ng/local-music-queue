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

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local network use
	},
}

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

// ClientState tracks per-client connection info
type ClientState struct {
	conn        *websocket.Conn
	connectedAt time.Time
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
	clients            map[*websocket.Conn]*ClientState
	broadcast          chan *BroadcastMessage
	register           chan *websocket.Conn
	unregister         chan *websocket.Conn
	mu                 sync.Mutex
	seqNum             int64
	getQueueState      func(context.Context) (*entity.Queue, error)
	voteInteractor     VoteExpiryRunner
	priorityInteractor PriorityChecker
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
			h.clients[client] = &ClientState{
				conn:        client,
				connectedAt: time.Now(),
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

// RegisterHandler upgrades connections to WebSockets and sends initial sync.
func (h *Hub) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	// Extract user_id from query parameters
	userIDStr := r.URL.Query().Get("user_id")
	var userID int
	if userIDStr != "" {
		_, _ = fmt.Sscanf(userIDStr, "%d", &userID)
	}

	// Send full sync immediately before registering
	if h.getQueueState != nil {
		state, err := h.getQueueState(r.Context())
		if err == nil {
			h.SendFullSync(conn, state)
		}
	}

	// Send active vote sessions as individual vote_updated events
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

	// Check and award daily priority if user_id provided
	if userID > 0 && h.priorityInteractor != nil {
		go func() {
			ctx := context.Background()
			err := h.priorityInteractor.CheckAndAwardDailyPriority(ctx, userID)
			if err != nil {
				log.Printf("Failed to check daily priority for user %d: %v", userID, err)
			} else {
				// Get updated balance and broadcast
				balance, err := h.priorityInteractor.GetUserPriorityBalance(ctx, userID)
				if err == nil {
					h.Broadcast(EventPriorityBalanceUpdated, PriorityBalanceUpdatedData{
						UserID:  userID,
						Balance: balance,
					})
				}
			}
		}()
	}

	// Start read loop to detect disconnections
	go h.readPump(conn)
}

// readPump reads messages from the client to detect disconnections and
// handles client-initiated requests (e.g. full-sync recovery on sequence gap).
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

		switch clientMsg.Type {
		case ClientMsgRequestFullSync:
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
