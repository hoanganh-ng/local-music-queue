package http

import (
	"encoding/json"
	"fmt"
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

	// Broadcast DELTA: only user info and activity
	h.hub.Broadcast(ws.EventUserJoined, ws.UserJoinedData{
		DisplayName: user.DisplayName,
		Role:        string(user.Role),
		Activity:    joinActivity,
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

	// Get current state to determine position
	state, _ := h.queue.GetState(r.Context())
	position := len(state.Songs) - 1 // Song was added at end

	// Get the activity that was just logged
	activity := entity.NewActivity(entity.ActivitySongAdded, req.AddedBy,
		fmt.Sprintf("added \"%s\"", song.Title))

	// Broadcast DELTA: only the new song
	h.hub.Broadcast(ws.EventSongAdded, ws.SongAddedData{
		Song:     *song,
		Position: position,
		Activity: activity,
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

	// Get state before skip to know previous index
	stateBefore, _ := h.queue.GetState(r.Context())
	previousIndex := stateBefore.CurrentIndex

	err := h.queue.SkipSong(r.Context(), req.RequestedBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get state after skip
	stateAfter, _ := h.queue.GetState(r.Context())

	var currentSong *entity.Song
	if stateAfter.CurrentIndex >= 0 && stateAfter.CurrentIndex < len(stateAfter.Songs) {
		currentSong = &stateAfter.Songs[stateAfter.CurrentIndex]
	}

	activity := entity.NewActivity(entity.ActivitySongSkipped, req.RequestedBy,
		"skipped the current song")

	// Broadcast DELTA: only index changes and new current song
	h.hub.Broadcast(ws.EventSongSkipped, ws.SongSkippedData{
		PreviousIndex: previousIndex,
		NewIndex:      stateAfter.CurrentIndex,
		CurrentSong:   currentSong,
		Status:        stateAfter.Status,
		Elapsed:       stateAfter.Elapsed,
		Activity:      activity,
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

	state, _ := h.queue.GetState(r.Context())
	activity := entity.NewActivity(entity.ActivityPlayback, req.RequestedBy,
		fmt.Sprintf("changed status to %s", string(req.Status)))

	// Broadcast DELTA: only status and elapsed
	h.hub.Broadcast(ws.EventStatusChanged, ws.StatusChangedData{
		Status:   state.Status,
		Elapsed:  state.Elapsed,
		Activity: activity,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleSearchYouTube(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "query parameter 'q' is required", http.StatusBadRequest)
		return
	}

	results, err := h.queue.SearchYouTube(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(results)
}

type SyncPlaybackRequest struct {
	Elapsed int `json:"elapsed"`
}

func (h *Handlers) HandleSyncPlayback(w http.ResponseWriter, r *http.Request) {
	var req SyncPlaybackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	err := h.queue.SyncPlayback(r.Context(), req.Elapsed)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.hub.Broadcast(ws.EventElapsedSync, ws.ElapsedSyncData{
		Elapsed: req.Elapsed,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleSongEnded(w http.ResponseWriter, r *http.Request) {
	stateBefore, _ := h.queue.GetState(r.Context())
	previousIndex := stateBefore.CurrentIndex

	err := h.queue.SongEnded(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stateAfter, _ := h.queue.GetState(r.Context())

	var currentSong *entity.Song
	if stateAfter.CurrentIndex >= 0 && stateAfter.CurrentIndex < len(stateAfter.Songs) {
		currentSong = &stateAfter.Songs[stateAfter.CurrentIndex]
	}

	activity := entity.NewActivity(entity.ActivityPlayback, "System", "song finished playing")

	h.hub.Broadcast(ws.EventSongSkipped, ws.SongSkippedData{
		PreviousIndex: previousIndex,
		NewIndex:      stateAfter.CurrentIndex,
		CurrentSong:   currentSong,
		Status:        stateAfter.Status,
		Elapsed:       stateAfter.Elapsed,
		Activity:      activity,
	})

	w.WriteHeader(http.StatusNoContent)
}

type PrevRequest struct {
	RequestedBy string `json:"requested_by"`
}

func (h *Handlers) HandlePrevSong(w http.ResponseWriter, r *http.Request) {
	var req PrevRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	stateBefore, _ := h.queue.GetState(r.Context())
	previousIndex := stateBefore.CurrentIndex

	err := h.queue.PrevSong(r.Context(), req.RequestedBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stateAfter, _ := h.queue.GetState(r.Context())

	var currentSong *entity.Song
	if stateAfter.CurrentIndex >= 0 && stateAfter.CurrentIndex < len(stateAfter.Songs) {
		currentSong = &stateAfter.Songs[stateAfter.CurrentIndex]
	}

	activity := entity.NewActivity(entity.ActivityPlayback, req.RequestedBy, "went to the previous song")

	h.hub.Broadcast(ws.EventSongPrevious, ws.SongPreviousData{
		PreviousIndex: previousIndex,
		NewIndex:      stateAfter.CurrentIndex,
		CurrentSong:   currentSong,
		Status:        stateAfter.Status,
		Elapsed:       stateAfter.Elapsed,
		Activity:      activity,
	})

	w.WriteHeader(http.StatusNoContent)
}

type RemoveSongRequest struct {
	Index       int    `json:"index"`
	RequestedBy string `json:"requested_by"`
}

func (h *Handlers) HandleRemoveSong(w http.ResponseWriter, r *http.Request) {
	var req RemoveSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	err := h.queue.RemoveSong(r.Context(), req.RequestedBy, req.Index)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stateAfter, _ := h.queue.GetState(r.Context())
	activity := entity.NewActivity(entity.ActivityPlayback, req.RequestedBy, "removed a song from queue")

	h.hub.Broadcast(ws.EventSongRemoved, ws.SongRemovedData{
		RemovedIndex: req.Index,
		NewIndex:     stateAfter.CurrentIndex,
		Status:       stateAfter.Status,
		Activity:     activity,
	})

	w.WriteHeader(http.StatusNoContent)
}

type ClearQueueRequest struct {
	RequestedBy string `json:"requested_by"`
}

func (h *Handlers) HandleClearQueue(w http.ResponseWriter, r *http.Request) {
	var req ClearQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	err := h.queue.ClearQueue(r.Context(), req.RequestedBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stateAfter, _ := h.queue.GetState(r.Context())
	activity := entity.NewActivity(entity.ActivityPlayback, req.RequestedBy, "cleared the queue")

	h.hub.Broadcast(ws.EventQueueCleared, ws.QueueClearedData{
		Status:   stateAfter.Status,
		Activity: activity,
	})

	w.WriteHeader(http.StatusNoContent)
}

