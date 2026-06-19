package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/activity"
	"local-music-queue/internal/usecase/auth"
	"local-music-queue/internal/usecase/priority"
	"local-music-queue/internal/usecase/queue"
	"local-music-queue/internal/usecase/vote"
	"net/http"
	"strings"
	"time"
)

type Broadcaster interface {
	Broadcast(eventType string, data interface{})
	ConnectedCount() int
}

type Handlers struct {
	queue    *queue.Interactor
	auth     *auth.Interactor
	activity *activity.Interactor
	priority *priority.Interactor
	vote     *vote.Interactor
	hub      Broadcaster
}

func NewHandlers(q *queue.Interactor, a *auth.Interactor, act *activity.Interactor, p *priority.Interactor, v *vote.Interactor, hub Broadcaster) *Handlers {
	return &Handlers{
		queue:    q,
		auth:     a,
		activity: act,
		priority: p,
		vote:     v,
		hub:      hub,
	}
}

type GoogleLoginRequest struct {
	IDToken string `json:"id_token"`
}

func (h *Handlers) HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	var req GoogleLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	user, err := h.auth.LoginWithGoogle(r.Context(), req.IDToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Check and award daily priority
	err = h.priority.CheckAndAwardDailyPriority(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else {
		//debug log for awarded priority
		fmt.Printf("Awarded daily priority to user %d\n", user.ID)
	}

	// Reload user to get updated priority balance
	var reloadErr error
	user, reloadErr = h.auth.GetUserByID(r.Context(), user.ID)
	if reloadErr != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Create session token
	token, expiresAt, err := h.auth.CreateSession(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "failed to create session: "+err.Error(), http.StatusInternalServerError)
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

	response := struct {
		*entity.User
		SessionToken     string    `json:"session_token"`
		SessionExpiresAt time.Time `json:"session_expires_at"`
	}{
		User:             user,
		SessionToken:     token,
		SessionExpiresAt: expiresAt,
	}

	json.NewEncoder(w).Encode(response)
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

	// This is deprecated - keeping for backward compatibility
	http.Error(w, "PIN login is deprecated, please use Google Sign-In", http.StatusBadRequest)
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
	URL       string               `json:"url"`
	AddedBy   string               `json:"added_by"`
	AddedByID int                  `json:"added_by_id"`
	Metadata  *entity.SearchResult `json:"metadata,omitempty"`
}

func (h *Handlers) HandleAddSong(w http.ResponseWriter, r *http.Request) {
	var req AddSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	song, err := h.queue.AddSong(r.Context(), req.URL, req.AddedBy, req.AddedByID, req.Metadata)
	if err != nil {
		if errors.Is(err, entity.ErrSongAlreadyInQueue) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
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

	// If we reached the end of the queue, broadcast status change instead of song skipped
	if stateAfter.Status == entity.StatusPaused && previousIndex == stateAfter.CurrentIndex {
		h.hub.Broadcast(ws.EventStatusChanged, ws.StatusChangedData{
			Status:   stateAfter.Status,
			Elapsed:  stateAfter.Elapsed,
			Activity: activity,
		})
	} else {
		h.hub.Broadcast(ws.EventSongSkipped, ws.SongSkippedData{
			PreviousIndex: previousIndex,
			NewIndex:      stateAfter.CurrentIndex,
			CurrentSong:   currentSong,
			Status:        stateAfter.Status,
			Elapsed:       stateAfter.Elapsed,
			Activity:      activity,
		})
	}

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
	Index       *int   `json:"index"`
	RequestedBy string `json:"requested_by"`
}

func extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}
	return parts[1]
}

func (h *Handlers) HandleRemoveSong(w http.ResponseWriter, r *http.Request) {
	var req *RemoveSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: malformed JSON", http.StatusBadRequest)
		return
	}
	if req == nil {
		http.Error(w, "invalid request: body is null", http.StatusBadRequest)
		return
	}
	if req.Index == nil {
		http.Error(w, "invalid request: missing index", http.StatusBadRequest)
		return
	}

	token := extractToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.auth.ResolveSession(r.Context(), token)
	if err != nil {
		if errors.Is(err, auth.ErrSessionInvalid) || errors.Is(err, auth.ErrSessionExpired) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	res, err := h.queue.RemoveSong(r.Context(), user, *req.Index)
	if err != nil {
		if errors.Is(err, queue.ErrInvalidIndex) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, queue.ErrNotSongOwner) || errors.Is(err, queue.ErrCannotRemoveSong) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.hub.Broadcast(ws.EventSongRemoved, ws.SongRemovedData{
		RemovedIndex: res.RemovedIndex,
		NewIndex:     res.ResultingIndex,
		Status:       res.ResultingStatus,
		Activity:     res.Activity,
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

type PrioritizeSongRequest struct {
	UserID    int `json:"user_id"`
	SongIndex int `json:"song_index"`
}

func (h *Handlers) HandlePrioritizeSong(w http.ResponseWriter, r *http.Request) {
	var req PrioritizeSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Get state before prioritization
	stateBefore, _ := h.queue.GetState(r.Context())

	err := h.priority.PrioritizeSong(r.Context(), req.UserID, req.SongIndex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get state after prioritization
	stateAfter, _ := h.queue.GetState(r.Context())

	// Get user for balance
	balance, _ := h.priority.GetUserPriorityBalance(r.Context(), req.UserID)

	// Find the prioritized song
	targetIndex := stateBefore.CurrentIndex + 1
	song := stateAfter.Songs[targetIndex]

	activity := entity.NewActivity(entity.ActivityPlayback, song.AddedBy,
		fmt.Sprintf("prioritized \"%s\"", song.Title))

	// Broadcast delta event
	h.hub.Broadcast(ws.EventSongPrioritized, ws.SongPrioritizedData{
		FromIndex:   req.SongIndex,
		ToIndex:     targetIndex,
		Song:        song,
		UserID:      req.UserID,
		UserBalance: balance,
		Activity:    activity,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleGetPriorityBalance(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		http.Error(w, "user_id parameter required", http.StatusBadRequest)
		return
	}

	var userID int
	if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
		http.Error(w, "invalid user_id", http.StatusBadRequest)
		return
	}

	balance, err := h.priority.GetUserPriorityBalance(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]int{"balance": balance})
}

type VolumeRequest struct {
	Direction string `json:"direction"`
}

func (h *Handlers) HandleChangeVolume(w http.ResponseWriter, r *http.Request) {
	var req VolumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	err := h.queue.ChangeVolume(r.Context(), req.Direction)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.hub.Broadcast(ws.EventVolumeChanged, ws.VolumeChangedData{
		Direction: req.Direction,
	})

	w.WriteHeader(http.StatusNoContent)
}

// canVote checks if a role is allowed to vote
func canVote(role entity.Role) bool {
	return role == entity.RoleGuest || role == entity.RoleAdmin
}

type VoteSkipRequest struct {
	UserID   int    `json:"user_id"`
	UserRole string `json:"user_role"`
}

func (h *Handlers) HandleVoteSkip(w http.ResponseWriter, r *http.Request) {
	var req VoteSkipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	role := entity.Role(req.UserRole)
	if !canVote(role) {
		http.Error(w, "only guests and admins can vote", http.StatusForbidden)
		return
	}

	connectedUsers := h.hub.ConnectedCount()
	outcome, err := h.vote.CastSkipVote(r.Context(), req.UserID, connectedUsers)
	if err != nil {
		if errors.Is(err, entity.ErrAlreadyVoted) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, entity.ErrQueueEmpty) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast any expired sessions that were cleaned up
	for _, expired := range outcome.ExpiredSessions {
		h.hub.Broadcast(ws.EventVoteResolved, ws.VoteResolvedData{
			SessionID: expired.SessionID,
			Outcome:   "expired",
			Activity:  expired.Activity,
		})
	}

	// Broadcast vote_updated event with vote cast activity
	h.hub.Broadcast(ws.EventVoteUpdated, ws.VoteUpdatedData{
		Session:  outcome.Session,
		Activity: *outcome.VoteCastActivity,
	})

	if outcome.Passed {
		// Load fresh queue state
		state, _ := h.queue.GetState(r.Context())

		var currentSong *entity.Song
		if state.CurrentIndex >= 0 && state.CurrentIndex < len(state.Songs) {
			currentSong = &state.Songs[state.CurrentIndex]
		}

		// Broadcast song_skipped event with action activity
		h.hub.Broadcast(ws.EventSongSkipped, ws.SongSkippedData{
			PreviousIndex: state.CurrentIndex - 1,
			NewIndex:      state.CurrentIndex,
			CurrentSong:   currentSong,
			Status:        state.Status,
			Elapsed:       state.Elapsed,
			Activity:      *outcome.ActionActivity,
		})

		// Broadcast vote_resolved event with vote passed activity
		h.hub.Broadcast(ws.EventVoteResolved, ws.VoteResolvedData{
			SessionID: outcome.Session.ID,
			Outcome:   "passed",
			Activity:  *outcome.VotePassedActivity,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

type VotePriorityRequest struct {
	UserID    int    `json:"user_id"`
	UserRole  string `json:"user_role"`
	SongIndex int    `json:"song_index"`
}

func (h *Handlers) HandleVotePriority(w http.ResponseWriter, r *http.Request) {
	var req VotePriorityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	role := entity.Role(req.UserRole)
	if !canVote(role) {
		http.Error(w, "only guests and admins can vote", http.StatusForbidden)
		return
	}

	connectedUsers := h.hub.ConnectedCount()
	outcome, err := h.vote.CastPriorityVote(r.Context(), req.UserID, req.SongIndex, connectedUsers)
	if err != nil {
		if errors.Is(err, entity.ErrAlreadyVoted) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, entity.ErrVoteOnCurrentSong) || errors.Is(err, entity.ErrSongNotFound) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast any expired sessions that were cleaned up
	for _, expired := range outcome.ExpiredSessions {
		h.hub.Broadcast(ws.EventVoteResolved, ws.VoteResolvedData{
			SessionID: expired.SessionID,
			Outcome:   "expired",
			Activity:  expired.Activity,
		})
	}

	// Broadcast vote_updated event with vote cast activity
	h.hub.Broadcast(ws.EventVoteUpdated, ws.VoteUpdatedData{
		Session:  outcome.Session,
		Activity: *outcome.VoteCastActivity,
	})

	if outcome.Passed {
		// Load fresh state
		state, _ := h.queue.GetState(r.Context())

		// Find the prioritized song (should be at currentIndex + 1)
		targetIndex := state.CurrentIndex + 1
		song := state.Songs[targetIndex]

		// Broadcast song_prioritized event with UserID: 0 to signal vote-driven
		h.hub.Broadcast(ws.EventSongPrioritized, ws.SongPrioritizedData{
			FromIndex:   req.SongIndex,
			ToIndex:     targetIndex,
			Song:        song,
			UserID:      0,
			UserBalance: 0,
			Activity:    *outcome.ActionActivity,
		})

		// Broadcast vote_resolved event with vote passed activity
		h.hub.Broadcast(ws.EventVoteResolved, ws.VoteResolvedData{
			SessionID: outcome.Session.ID,
			Outcome:   "passed",
			Activity:  *outcome.VotePassedActivity,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
