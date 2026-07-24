package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomqueue"
	"local-music-queue/internal/usecase/roomvote"
)

// RoomVoteHandlers wires the room-scoped vote REST endpoints. Identity is
// supplied by the roomAuth routing wrapper in main.go which has already
// resolved the bearer token (R07b+). R09b adds a single endpoint:
//
//   POST /api/rooms/{slug}/vote/skip
//
// Any active room member may cast a vote against the current song.
// Sessions are room-scoped, current-song-scoped, 30-second expiring,
// in-memory only, never persisted. On a passing vote the queue
// advances and the per-room broadcaster fans out room_vote_updated,
// room_vote_resolved, and (reuse) room_playback_song_advanced with
// reason="skip". Body-supplied identity is ignored; the actor user id
// is authoritative.
type RoomVoteHandlers struct {
	vote  *roomvote.Interactor
	queue *roomqueue.Interactor
}

// NewRoomVoteHandlers constructs the handlers. queue is the SAME
// roomqueue.Interactor that owns queue state; the handler reads its
// Broadcaster() to dispatch per-room WebSocket events. Wiring the
// real *roomqueue.Interactor (not an interface) keeps the handler in
// lock-step with the roomqueue mutex and persistence layer.
func NewRoomVoteHandlers(vote *roomvote.Interactor, queue *roomqueue.Interactor) *RoomVoteHandlers {
	return &RoomVoteHandlers{vote: vote, queue: queue}
}

// HandleCastRoomVoteSkip: POST /api/rooms/{slug}/vote/skip — any active
// room member. Empty body (or {}). The actor user id is supplied by
// roomAuth via actorFromCtx; body-supplied identity fields are
// ignored.
//
// Response semantics:
//
//	204 No Content — the vote was cast and did not pass; the handler
//	  has already dispatched room_vote_updated with the post-cast
//	  session via the queue interactor's broadcaster.
//	200 OK {"resolution":"passed"} — the vote was cast and PASSED the
//	  threshold; the handler has already dispatched room_vote_updated,
//	  room_vote_resolved, AND room_playback_song_advanced reason="skip"
//	  via the roomqueue broadcaster.
//	200 OK {"resolution":"expired"} — the vote was cast and the prior
//	  session for this (room, song) was evicted on expiry; the handler
//	  has already dispatched room_vote_resolved{"expired"} for the
//	  evicted session AND room_vote_updated for the new session.
//	400 Bad Request — malformed slug.
//	401 Unauthorized — defense-in-depth when actorUserID == 0.
//	403 Forbidden — actor is not an active member of the room.
//	404 Not Found — room slug unknown.
//	400 Bad Request — malformed slug OR the queue has no current
//	  song (empty queue). The room itself is valid; the request is
//	  not actionable until the queue has a current song.
//	409 Conflict — room archived, duplicate vote, or stale session
//	  (the queue advanced under the vote).
//	500 Internal Server Error — anything else.
func (h *RoomVoteHandlers) HandleCastRoomVoteSkip(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Body is intentionally unread; identity is server-resolved.
	out, err := h.vote.CastSkipVote(r.Context(), slug, actorUserID)
	if err != nil {
		writeRoomVoteError(w, err)
		return
	}

	// The expired branch carries an evicted session that we MUST
	// broadcast room_vote_resolved{"expired"} for BEFORE the
	// room_vote_updated for the new session, so clients can render the
	// final state of the old session before the new one's update lands.
	//
	// The passed branch carries AdvanceQueue/AdvanceSong for the
	// post-mutation state. The plain-cast branch (Resolution == "")
	// has no post-mutation state, so we fetch the current queue via
	// the queue interactor for the vote_updated broadcast.
	switch out.Resolution {
	case "expired":
		if bc := h.queue.Broadcaster(); bc != nil {
			bc.BroadcastRoomVoteResolved(slug, out.ExpiredID, "expired", out.ExpiredQueue)
			bc.BroadcastRoomVoteUpdated(slug, out.Session, actorUserID, out.ExpiredQueue)
		}
		writeJSON(w, http.StatusOK, map[string]string{"resolution": "expired"})
		return
	case "passed":
		if bc := h.queue.Broadcaster(); bc != nil {
			bc.BroadcastRoomVoteUpdated(slug, out.Session, actorUserID, out.AdvanceQueue)
			bc.BroadcastRoomVoteResolved(slug, out.Session.ID, "passed", out.AdvanceQueue)
			bc.BroadcastRoomPlaybackSongAdvanced(
				slug, "skip",
				out.AdvancePrev, out.AdvanceNext, out.AdvanceSong,
				out.AdvanceQueue.Status, out.AdvanceQueue.Elapsed,
				out.AdvanceQueue,
			)
		}
		writeJSON(w, http.StatusOK, map[string]string{"resolution": "passed"})
		return
	}

	// Plain cast (no resolution): fan out room_vote_updated with the
	// current (pre-pass) queue state.
	if out.Session != nil {
		if bc := h.queue.Broadcaster(); bc != nil {
			state := out.AdvanceQueue
			if state == nil {
				if q, qerr := h.queue.GetState(r.Context(), slug, actorUserID); qerr == nil {
					state = q
				}
			}
			bc.BroadcastRoomVoteUpdated(slug, out.Session, actorUserID, state)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// roomVotePrioritizeRequest is the strict body shape for POST
// /api/rooms/{slug}/vote/prioritize. SongIndex is a *int so a missing
// field is distinguishable from an explicit 0. Unknown fields are
// rejected via DisallowUnknownFields; trailing data is rejected via
// dec.More().
type roomVotePrioritizeRequest struct {
	SongIndex *int `json:"song_index"`
}

// HandleCastRoomVotePrioritize: POST /api/rooms/{slug}/vote/prioritize
// — any active room member. The request body is exactly
// {"song_index": <integer>} with strict JSON decoding: missing,
// negative, malformed, trailing, and unknown fields are all rejected
// with 400. The actor user id is supplied by roomAuth via actorFromCtx;
// body-supplied identity fields are ignored.
//
// Response semantics mirror HandleCastRoomVoteSkip minus the eviction-
// on-entry "expired" branch (prioritize sessions expire only via the
// shared sweep):
//
//	204 No Content — the vote was cast and did not pass; the handler
//	  has already dispatched room_vote_updated with the post-cast
//	  session.
//	200 OK {"resolution":"passed"} — the vote passed the threshold; the
//	  handler has already dispatched room_vote_updated,
//	  room_vote_resolved, AND room_queue_song_prioritized.
//	200 OK {"resolution":"expired"} — the ballot arrived after the prior
//	  session for this target had expired; the handler has already
//	  dispatched room_vote_resolved{"expired"} for the evicted session
//	  AND room_vote_updated for the fresh session.
//	400 Bad Request — malformed slug, malformed/invalid body, or
//	  out-of-range / current-song index.
//	401 Unauthorized — defense-in-depth when actorUserID == 0.
//	403 Forbidden — actor is not an active member of the room.
//	404 Not Found — room slug unknown.
//	409 Conflict — room archived, duplicate vote, or stale target (the
//	  target moved under the vote).
//	410 Gone — session expired between checks.
//	500 Internal Server Error — anything else.
func (h *RoomVoteHandlers) HandleCastRoomVotePrioritize(w http.ResponseWriter, r *http.Request, slug string, actorUserID int) {
	if actorUserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req roomVotePrioritizeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if dec.More() {
		http.Error(w, "unexpected trailing data in request body", http.StatusBadRequest)
		return
	}
	if req.SongIndex == nil {
		http.Error(w, "song_index is required", http.StatusBadRequest)
		return
	}
	if *req.SongIndex < 0 {
		http.Error(w, "song_index must be non-negative", http.StatusBadRequest)
		return
	}

	out, err := h.vote.CastPrioritizeVote(r.Context(), slug, *req.SongIndex, actorUserID)
	if err != nil {
		writeRoomVoteError(w, err)
		return
	}

	switch out.Resolution {
	case "expired":
		// The prior session for this exact target expired; broadcast its
		// resolution BEFORE the fresh session's update so clients render
		// the final state of the old session first, then reply 200.
		if bc := h.queue.Broadcaster(); bc != nil {
			bc.BroadcastRoomVoteResolved(slug, out.ExpiredID, "expired", out.ExpiredQueue)
			bc.BroadcastRoomVoteUpdated(slug, out.Session, actorUserID, out.ExpiredQueue)
		}
		writeJSON(w, http.StatusOK, map[string]string{"resolution": "expired"})
		return
	case "passed":
		if bc := h.queue.Broadcaster(); bc != nil {
			bc.BroadcastRoomVoteUpdated(slug, out.Session, actorUserID, out.PrioritizeQueue)
			bc.BroadcastRoomVoteResolved(slug, out.Session.ID, "passed", out.PrioritizeQueue)
			bc.BroadcastRoomQueueSongPrioritized(slug, out.FromIndex, out.ToIndex, out.PrioritizeSong, out.PrioritizeQueue)
		}
		writeJSON(w, http.StatusOK, map[string]string{"resolution": "passed"})
		return
	}

	// Plain cast (no resolution): fan out room_vote_updated with the
	// current (pre-pass) queue state.
	if out.Session != nil {
		if bc := h.queue.Broadcaster(); bc != nil {
			state := out.PrioritizeQueue
			if state == nil {
				if q, qerr := h.queue.GetState(r.Context(), slug, actorUserID); qerr == nil {
					state = q
				}
			}
			bc.BroadcastRoomVoteUpdated(slug, out.Session, actorUserID, state)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeRoomVoteError maps use-case sentinel errors to the documented
// status codes. Mirrors the roomqueue handler's writeRoomQueueError
// surface so clients see consistent semantics across the migration.
func writeRoomVoteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrInvalidSlug),
		errors.Is(err, entity.ErrNoCurrentSong),
		errors.Is(err, roomqueue.ErrInvalidIndex),
		errors.Is(err, entity.ErrVoteOnCurrentSong):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, room.ErrRoomNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, room.ErrArchived):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, room.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, entity.ErrAlreadyVoted):
		http.Error(w, "already voted", http.StatusConflict)
	case errors.Is(err, entity.ErrVoteSessionExpired):
		// Rare race: session existed but expired between the eviction
		// check and the cast. The client can retry.
		http.Error(w, "vote session expired", http.StatusGone)
	case errors.Is(err, roomvote.ErrStalePrioritizeSession):
		// The prioritize target moved / was removed / became current
		// under the vote session. 409 surfaces the conflict; the client
		// should re-fetch state. Checked BEFORE ErrStaleSession because
		// the two are distinct sentinel values.
		http.Error(w, "prioritize target moved under the vote", http.StatusConflict)
	case errors.Is(err, roomvote.ErrStaleSession):
		// The queue advanced under the vote session (lease-holder skip
		// or PlaybackEnded landed first). 409 surfaces the conflict;
		// the client should re-fetch state.
		http.Error(w, "queue advanced under the vote", http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
