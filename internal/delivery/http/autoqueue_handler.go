package http

import (
	"encoding/json"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/usecase/autoqueue"
	"net/http"
)

type AutoQueueHandlers struct {
	autoQueue *autoqueue.Interactor
	hub       *ws.Hub
}

func NewAutoQueueHandlers(aq *autoqueue.Interactor, hub *ws.Hub) *AutoQueueHandlers {
	return &AutoQueueHandlers{
		autoQueue: aq,
		hub:       hub,
	}
}

type ToggleAutoQueueRequest struct {
	Enabled bool `json:"enabled"`
}

type AutoQueueStatusResponse struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
}

// HandleToggleAutoQueue toggles auto-queue on/off (Host/Admin only —
// enforced by the RequireRole wrapper in the route wiring).
func (h *AutoQueueHandlers) HandleToggleAutoQueue(w http.ResponseWriter, r *http.Request) {
	if UserFromCtx(r.Context()) == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req ToggleAutoQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	err := h.autoQueue.SetEnabled(r.Context(), req.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cfg, err := h.autoQueue.GetConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast config change to all connected clients
	h.hub.Broadcast(ws.EventAutoQueueConfigChanged, ws.AutoQueueConfigChangedData{
		Enabled:  cfg.Enabled,
		Strategy: string(cfg.Strategy),
	})

	json.NewEncoder(w).Encode(AutoQueueStatusResponse{
		Enabled:  cfg.Enabled,
		Strategy: string(cfg.Strategy),
	})
}

// HandleGetAutoQueueStatus returns the current auto-queue status.
func (h *AutoQueueHandlers) HandleGetAutoQueueStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.autoQueue.GetConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(AutoQueueStatusResponse{
		Enabled:  cfg.Enabled,
		Strategy: string(cfg.Strategy),
	})
}
