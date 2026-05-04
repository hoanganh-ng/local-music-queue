package ws

import (
	"context"
	"encoding/json"
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
	ExpireOldSessions(ctx context.Context)
	GetActiveSessions() []*entity.VoteSession
}

// ClientState tracks per-client connection info
type ClientState struct {
	conn        *websocket.Conn
	connectedAt time.Time
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
	clients        map[*websocket.Conn]*ClientState
	broadcast      chan *BroadcastMessage
	register       chan *websocket.Conn
	unregister     chan *websocket.Conn
	mu             sync.Mutex
	seqNum         int64
	getQueueState  func(context.Context) (*entity.Queue, error)
	voteInteractor VoteExpiryRunner
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
	defer ticker.Stop()

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

			for client := range h.clients {
				err := client.WriteMessage(websocket.TextMessage, data)
				if err != nil {
					log.Printf("Error writing message to client: %v", err)
					client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()

		case <-ticker.C:
			if h.voteInteractor != nil {
				h.voteInteractor.ExpireOldSessions(context.Background())
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
			})
		}
	}

	h.register <- conn
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
	h.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
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
	h.mu.Unlock()

	msgData, err := json.Marshal(msg)
	if err != nil {
		return err
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
