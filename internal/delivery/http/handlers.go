package http

import (
	"encoding/json"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/activity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/queue"
	"net/http"
)

type Handlers struct {
	queue    *queue.Interactor
	auth     *auth.Interactor
	activity *activity.Interactor
	hub      *ws.Hub
}

func NewHandlers(q *queue.Interactor, a *auth.Interactor, act *activity.Interactor, hub *ws.Hub) *Handlers {
	return &Handlers{
		queue:    q,
		auth:     a,
		activity: act,
		hub:      hub,
	}
}

type LoginRequest struct {
	PIN         string `json:"pin"`
	DisplayName string `json:"display_name"`
}

func (h *Handlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	user, err := h.auth.Login(req.PIN, req.DisplayName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Log join activity
	joinActivity := entity.NewActivity(entity.ActivityUserJoined, user.DisplayName, "joined the room")
	_ = h.activity.LogActivity(r.Context(), joinActivity)

	// Broadcast update so others see the join in their activity log
	state, _ := h.queue.GetState(r.Context())
	h.hub.Broadcast(map[string]interface{}{
		"type":  "queue_updated",
		"state": state,
	})

	json.NewEncoder(w).Encode(user)
}

func (h *Handlers) HandleGetQueue(w http.ResponseWriter, r *http.Request) {
	state, err := h.queue.GetState(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(state)
}

type AddSongRequest struct {
	URL     string `json:"url"`
	AddedBy string `json:"added_by"`
}

func (h *Handlers) HandleAddSong(w http.ResponseWriter, r *http.Request) {
	var req AddSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	song, err := h.queue.AddSong(r.Context(), req.URL, req.AddedBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast update
	state, _ := h.queue.GetState(r.Context())
	h.hub.Broadcast(map[string]interface{}{
		"type":  "queue_updated",
		"state": state,
	})

	json.NewEncoder(w).Encode(song)
}

type SkipRequest struct {
	RequestedBy string `json:"requested_by"`
}

func (h *Handlers) HandleSkipSong(w http.ResponseWriter, r *http.Request) {
	var req SkipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	err := h.queue.SkipSong(r.Context(), req.RequestedBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast update
	state, _ := h.queue.GetState(r.Context())
	h.hub.Broadcast(map[string]interface{}{
		"type":  "queue_updated",
		"state": state,
	})

	w.WriteHeader(http.StatusNoContent)
}

type StatusRequest struct {
	Status      entity.PlaybackStatus `json:"status"`
	RequestedBy string                `json:"requested_by"`
}

func (h *Handlers) HandleSetStatus(w http.ResponseWriter, r *http.Request) {
	var req StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	err := h.queue.SetStatus(r.Context(), req.RequestedBy, req.Status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast update
	state, _ := h.queue.GetState(r.Context())
	h.hub.Broadcast(map[string]interface{}{
		"type":  "status_updated",
		"state": state,
	})

	w.WriteHeader(http.StatusNoContent)
}
