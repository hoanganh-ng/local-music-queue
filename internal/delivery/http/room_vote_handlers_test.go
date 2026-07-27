package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomqueue"
	"local-music-queue/internal/usecase/roomvote"
)

// --- test broadcaster for the vote handler ---
//
// voteTestBroadcaster is a fresh test double (separate from
// recordingRoomBroadcaster in room_queue_handlers_test.go) that
// implements the eleven-method roomqueue.Broadcaster interface. The
// R09b-specific methods capture their arguments verbatim; the rest
// are no-ops kept solely to satisfy the interface. The handler is
// the only thing that calls the R09b methods.
type voteTestBroadcaster struct {
	mu           sync.Mutex
	voteUpdated  []voteUpdatedArg
	voteResolved []voteResolvedArg
	advanced     []recordingAdvancedCall
	prioritized  []votePrioritizedArg
}

type votePrioritizedArg struct {
	Slug      string
	FromIndex int
	ToIndex   int
	Song      entity.Song
	State     *entity.Queue
}

type voteUpdatedArg struct {
	Slug        string
	ActorUserID int
	Session     *entity.VoteSession
	State       *entity.Queue
}

type voteResolvedArg struct {
	Slug      string
	SessionID string
	Outcome   string
	State     *entity.Queue
}

func (b *voteTestBroadcaster) BroadcastRoomQueueSync(_ string, _ *entity.Queue) {}
func (b *voteTestBroadcaster) BroadcastRoomQueueSongAdded(_ string, _ entity.Song, _ int, _ *entity.Queue) {
}
func (b *voteTestBroadcaster) BroadcastRoomQueueSongRemoved(_ string, _ int, _ *entity.Queue) {}
func (b *voteTestBroadcaster) BroadcastRoomQueueCleared(_ string, _ *entity.Queue)        {}
func (b *voteTestBroadcaster) BroadcastRoomQueueSongPrioritized(slug string, from, to int, song entity.Song, state *entity.Queue) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prioritized = append(b.prioritized, votePrioritizedArg{
		Slug: slug, FromIndex: from, ToIndex: to, Song: song, State: state,
	})
}
func (b *voteTestBroadcaster) BroadcastRoomPlaybackStatusChanged(_ string, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {
}
func (b *voteTestBroadcaster) BroadcastRoomPlaybackElapsedSync(_ string, _ int, _ *entity.Queue) {
}
func (b *voteTestBroadcaster) BroadcastRoomPlaybackSongAdvanced(slug, reason string, prev, next int, song *entity.Song, status entity.PlaybackStatus, elapsed int, state *entity.Queue) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.advanced = append(b.advanced, recordingAdvancedCall{
		slug: slug, reason: reason, prev: prev, next: next, song: song, status: status, elapsed: elapsed,
	})
}
func (b *voteTestBroadcaster) BroadcastRoomVoteUpdated(slug string, session *entity.VoteSession, actorUserID int, state *entity.Queue) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.voteUpdated = append(b.voteUpdated, voteUpdatedArg{
		Slug: slug, ActorUserID: actorUserID, Session: session, State: state,
	})
}
func (b *voteTestBroadcaster) BroadcastRoomVoteResolved(slug, sessionID, outcome string, state *entity.Queue) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.voteResolved = append(b.voteResolved, voteResolvedArg{
		Slug: slug, SessionID: sessionID, Outcome: outcome, State: state,
	})
}

// BroadcastRoomPlaybackVolumeChanged is a no-op stub kept on
// voteTestBroadcaster so it continues to satisfy roomqueue.Broadcaster
// after R09c added the volume method to the interface. The vote
// handlers never invoke this; the R09c handler is the only caller in
// the room-queue delivery layer.
func (b *voteTestBroadcaster) BroadcastRoomPlaybackVolumeChanged(_, _ string) {}

// BroadcastRoomPlaybackSongPrevious is a no-op stub kept on
// voteTestBroadcaster so it continues to satisfy roomqueue.Broadcaster
// after R09d added the prev method to the interface. The vote
// handlers never invoke this; the R09d handler is the only caller in
// the room-queue delivery layer.
func (b *voteTestBroadcaster) BroadcastRoomPlaybackSongPrevious(_ string, _, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {}
func (b *voteTestBroadcaster) BroadcastRoomAutoQueueAdded(_ string, _ entity.Song, _ string, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue)    {}
func (b *voteTestBroadcaster) BroadcastRoomAutoQueueConfigChanged(_ string, _ bool, _ string)       {}

// fixedResolver is a deterministic roomvote.Resolver that always
// returns the supplied unique-user count. Tests use it to pin the
// threshold (max(2, count/2 + 1)) at session creation time.
type fixedResolver struct{ count int }

func (f fixedResolver) UniqueConnectedUserIDs(_ string) int { return f.count }

// allowAllLeaseAuthorizer is a no-op lease authorizer used by tests
// that exercise the post-lease code paths (skip-playback to force
// advance) without claiming a real player lease. The roomqueue test
// package defines an identical type internally; we keep this one
// local to room_vote_handlers_test.go to avoid cross-package coupling.
type allowAllLeaseAuthorizer struct{}

func (allowAllLeaseAuthorizer) RequireActiveLeaseHolder(_ context.Context, _ string, _ int) error {
	return nil
}

// --- fixture ---
//
// voteRoomFixture creates a room with a 2-song queue ("cur-<slug>" at
// index 0, "next-<slug>" at index 1), seeded with the given members.
// Returns the vote handler, the *sql.DB, the underlying roomqueue
// interactor, and a cleanup.
//
// The first memberID is the host; the rest are guests. The threshold
// is derived from len(memberIDs) via the fixedResolver; the interactor
// uses threshold = max(2, n/2 + 1).
func voteRoomFixture(t *testing.T, slug string, memberIDs []int) (*RoomVoteHandlers, *sql.DB, *roomqueue.Interactor, func()) {
	t.Helper()
	if len(memberIDs) == 0 {
		t.Fatalf("voteRoomFixture: memberIDs must be non-empty")
	}
	rqh, db, cleanup := newRoomQueueHandlers(t)
	seedUserQueue(t, db, memberIDs[0], "host-rv-"+slug+"@example.com", entity.RoleHost)
	for i, id := range memberIDs[1:] {
		seedUserQueue(t, db, id, fmt.Sprintf("guest-rv-%d-%s@example.com", i, slug), entity.RoleGuest)
	}
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, slug, "RV-"+slug, memberIDs[0], time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, slug)
	for i, id := range memberIDs[1:] {
		if err := roomRepo.AddMember(ctx, roomID, id, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
			t.Fatalf("add guest %d: %v", i+1, err)
		}
	}
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	q := entity.NewQueue()
	q.Songs = []entity.Song{
		{ID: "cur-" + slug, Title: "C", URL: "u", AddedBy: "host", AddedByID: memberIDs[0]},
		{ID: "next-" + slug, Title: "N", URL: "u", AddedBy: "host", AddedByID: memberIDs[0]},
	}
	q.CurrentIndex = 0
	q.Status = entity.StatusPlaying
	if err := queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("save queue: %v", err)
	}
	inter := rqh.inter
	rvh := NewRoomVoteHandlers(
		roomvote.NewInteractor(inter, fixedResolver{count: len(memberIDs)}, 30*time.Second, nil),
		inter,
		nil,
	)
	return rvh, db, inter, cleanup
}

// castSkip invokes the handler with a fresh POST /api/rooms/{slug}/vote/skip
// request and an empty JSON body. The body is intentionally
// non-required by the spec; the handler ignores it.
func castSkip(t *testing.T, slug string, actorUserID int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/"+slug+"/vote/skip", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	return rr
}

// --- Test cases ---

// TestRoomVote_PlainCast_NoPass_Returns204AndBroadcastsUpdated pins the
// happy no-pass path: 2-member room, threshold 2, first vote does NOT
// pass → 204, one vote_updated broadcast with actor_user_id, no
// vote_resolved, no advanced.
func TestRoomVote_PlainCast_NoPass_Returns204AndBroadcastsUpdated(t *testing.T) {
	rvh, _, _, cleanup := voteRoomFixture(t, "rv-204", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	rr := castSkip(t, "rv-204", 200)
	rvh.HandleCastRoomVoteSkip(rr, httptest.NewRequest(http.MethodPost, "/api/rooms/rv-204/vote/skip", bytes.NewReader([]byte(`{}`))), "rv-204", 200)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 1 {
		t.Fatalf("expected 1 vote_updated broadcast, got %d", got)
	}
	if bc.voteUpdated[0].ActorUserID != 200 {
		t.Errorf("expected actor 200, got %d", bc.voteUpdated[0].ActorUserID)
	}
	if bc.voteUpdated[0].Session == nil {
		t.Errorf("expected non-nil session in vote_updated")
	} else if bc.voteUpdated[0].Session.Threshold != 2 {
		t.Errorf("expected threshold=2 for 2 members, got %d", bc.voteUpdated[0].Session.Threshold)
	}
	if got := len(bc.voteResolved); got != 0 {
		t.Errorf("expected 0 vote_resolved, got %d", got)
	}
	if got := len(bc.advanced); got != 0 {
		t.Errorf("expected 0 advanced, got %d", got)
	}
}

// TestRoomVote_PassingVoteCallsSkipVoteAndBroadcastsResolvedAndAdvanced
// pins the pass-through: second member's vote hits the threshold and
// triggers ALL three broadcasts (vote_updated x2, vote_resolved x1,
// advanced x1 reason=skip) plus the queue advances. Reloading the
// queue must show CurrentIndex == 1 and the next song's ID.
func TestRoomVote_PassingVoteCallsSkipVoteAndBroadcastsResolvedAndAdvanced(t *testing.T) {
	rvh, _, inter, cleanup := voteRoomFixture(t, "rv-pass", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	// First vote: 204.
	req1 := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-pass/vote/skip", bytes.NewReader([]byte(`{}`)))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr1, req1, "rv-pass", 200)
	if rr1.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d body=%s", rr1.Code, rr1.Body.String())
	}

	// Second vote: 200 + {"resolution":"passed"}.
	req2 := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-pass/vote/skip", bytes.NewReader([]byte(`{}`)))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr2, req2, "rv-pass", 300)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second cast: expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	var body struct {
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Resolution != "passed" {
		t.Errorf("expected resolution=passed, got %q", body.Resolution)
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 2 {
		t.Errorf("expected 2 vote_updated broadcasts, got %d", got)
	}
	if got := len(bc.voteResolved); got != 1 {
		t.Fatalf("expected 1 vote_resolved, got %d", got)
	}
	if bc.voteResolved[0].Outcome != "passed" {
		t.Errorf("expected outcome=passed, got %q", bc.voteResolved[0].Outcome)
	}
	if bc.voteResolved[0].SessionID == "" {
		t.Errorf("expected non-empty session_id on vote_resolved")
	}
	if bc.voteResolved[0].State == nil {
		t.Errorf("expected non-nil state on vote_resolved")
	}
	if got := len(bc.advanced); got != 1 {
		t.Fatalf("expected 1 advanced broadcast, got %d", got)
	}
	if bc.advanced[0].reason != "skip" || bc.advanced[0].slug != "rv-pass" {
		t.Errorf("expected advanced reason=skip slug=rv-pass, got %+v", bc.advanced[0])
	}

	// Reload the queue: must be on next-rv-pass.
	reloaded, err := inter.GetState(context.Background(), "rv-pass", 200)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.CurrentIndex != 1 {
		t.Errorf("expected CurrentIndex=1, got %d", reloaded.CurrentIndex)
	}
	if len(reloaded.Songs) < 2 || reloaded.Songs[1].ID != "next-rv-pass" {
		t.Errorf("expected songs[1].ID=next-rv-pass, got %+v", reloaded.Songs)
	}
}

// TestRoomVote_DuplicateVoteFromSameUser_Returns409 pins the
// single-vote-per-user invariant: a second vote from the same user
// must return 409 and produce only one vote_updated.
func TestRoomVote_DuplicateVoteFromSameUser_Returns409(t *testing.T) {
	rvh, _, _, cleanup := voteRoomFixture(t, "rv-dup", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	// First vote from 200 — succeeds.
	req1 := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-dup/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr1 := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr1, req1, "rv-dup", 200)
	if rr1.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d", rr1.Code)
	}

	// Second vote from 200 — duplicate, expect 409.
	req2 := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-dup/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr2 := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr2, req2, "rv-dup", 200)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("duplicate cast: expected 409, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(strings.ToLower(rr2.Body.String()), "already") &&
		!strings.Contains(strings.ToLower(rr2.Body.String()), "vote") {
		t.Errorf("expected 409 body to mention vote/already, got %q", rr2.Body.String())
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 1 {
		t.Errorf("expected 1 vote_updated total (no broadcast on 409), got %d", got)
	}
}

// TestRoomVote_UnauthorizedActor_Returns401 pins the defense-in-depth
// 401 gate: actorUserID == 0 must short-circuit before any interactor
// call. No broadcasts.
func TestRoomVote_UnauthorizedActor_Returns401(t *testing.T) {
	rvh, _, _, cleanup := voteRoomFixture(t, "rv-401", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-401/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr, req, "rv-401", 0)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 0 {
		t.Errorf("expected 0 broadcasts on 401, got %d", got)
	}
}

// TestRoomVote_NonMember_Returns403 pins the membership gate: a user
// who is not a member of the room is rejected with 403.
func TestRoomVote_NonMember_Returns403(t *testing.T) {
	rvh, _, _, cleanup := voteRoomFixture(t, "rv-403", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-403/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr, req, "rv-403", 9999)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-member: expected 403, got %d body=%s", rr.Code, rr2Body(t, rr))
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 0 {
		t.Errorf("expected 0 broadcasts on 403, got %d", got)
	}
}

// TestRoomVote_UnknownSlug_Returns404 pins the unknown-slug path:
// voteRoomFixture is NOT called (no fixture); the handler must return
// 404 because the room does not exist.
func TestRoomVote_UnknownSlug_Returns404(t *testing.T) {
	rvh, _, _, cleanup := voteRoomFixture(t, "rv-some", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-no-such-room/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr, req, "rv-no-such-room", 200)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown slug: expected 404, got %d body=%s", rr.Code, rr2Body(t, rr))
	}
}

// TestRoomVote_ArchivedRoom_Returns409 pins the archived-room gate:
// after ArchiveRoomIfActive, the handler must return 409.
func TestRoomVote_ArchivedRoom_Returns409(t *testing.T) {
	rvh, db, _, cleanup := voteRoomFixture(t, "rv-arch", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	roomRepo := persistence.NewPostgresRoomRepository(db)
	roomID := mustRoomIDQueue(t, db, "rv-arch")
	if _, err := roomRepo.ArchiveRoomIfActive(context.Background(), roomID, time.Now().UTC()); err != nil {
		t.Fatalf("archive: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-arch/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr, req, "rv-arch", 200)
	if rr.Code != http.StatusConflict {
		t.Fatalf("archived: expected 409, got %d body=%s", rr.Code, rr2Body(t, rr))
	}
}

// TestRoomVote_NoCurrentSong_Returns400 pins the no-current-song
// path: an empty queue (CurrentIndex stays at -1) must return 400
// Bad Request with entity.ErrNoCurrentSong mapped from the
// interactor. The room slug itself is valid; the queue state is what
// makes the request non-actionable.
//
// We seed a NEW room with no songs by reusing voteRoomFixture's
// builder, then overwriting the queue with an empty queue via the
// repo. We don't use a separate helper to keep the diff small.
func TestRoomVote_NoCurrentSong_Returns400(t *testing.T) {
	rvh, db, _, cleanup := voteRoomFixture(t, "rv-nocs", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	// Overwrite the seeded queue with an empty queue (CurrentIndex = -1).
	roomID := mustRoomIDQueue(t, db, "rv-nocs")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	empty := entity.NewQueue() // Songs: nil, CurrentIndex: -1
	if err := queueRepo.Save(context.Background(), roomID, empty); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-nocs/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr, req, "rv-nocs", 200)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("no current song: expected 400, got %d body=%s", rr.Code, rr2Body(t, rr))
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 0 {
		t.Errorf("expected 0 broadcasts on 400, got %d", got)
	}
}

// TestRoomVote_WriteRoomVoteError_MapsStaleAndExpired pins the error
// mapping for the rare races that the live-handler path cannot reach
// in a single-threaded test:
//
//   - roomvote.ErrStaleSession  → 409 (queue advanced under the vote).
//   - entity.ErrVoteSessionExpired → 410 (rare race; client can retry).
//   - entity.ErrAlreadyVoted    → 409 (covered inline in the
//                                  duplicate test, asserted here too).
//   - room.ErrInvalidSlug       → 400.
//   - entity.ErrNoCurrentSong   → 400 (empty queue / no current song;
//                                  covered inline in the no-current-song
//                                  test, asserted here too).
//
// The handler never broadcasts on error paths, so we wire a
// voteTestBroadcaster to confirm no broadcasts leak.
func TestRoomVote_WriteRoomVoteError_MapsStaleAndExpired(t *testing.T) {
	bc := &voteTestBroadcaster{}

	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"stale session", roomvote.ErrStaleSession, http.StatusConflict},
		{"expired race", entity.ErrVoteSessionExpired, http.StatusGone},
		{"already voted", entity.ErrAlreadyVoted, http.StatusConflict},
		{"invalid slug", room.ErrInvalidSlug, http.StatusBadRequest},
		{"no current song", entity.ErrNoCurrentSong, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeRoomVoteError(rr, tc.err)
			if rr.Code != tc.wantStatus {
				t.Errorf("err=%v: expected status %d, got %d body=%s",
					tc.err, tc.wantStatus, rr.Code, rr.Body.String())
			}
			if got := len(bc.voteUpdated); got != 0 {
				t.Errorf("err=%v: expected 0 vote_updated broadcasts, got %d", tc.err, got)
			}
		})
	}
}

// TestRoomVote_QueueAdvanceBetweenCastsDoesNotPass pins the invariant
// that the second cast, after the queue has advanced via the lease
// path, must NOT fire a vote_resolved{passed} or an
// room_playback_song_advanced broadcast. (It creates a NEW session
// against the new current song because the in-memory session is
// keyed by song ID; the prior cast's session is left alone.)
//
// This is the live-handler surface of the plan's "stale" case: the
// handler still returns 204 for the second cast (a fresh session
// vote, threshold not yet met for the new song) and broadcasts a
// single vote_updated — never a passed resolution. The
// writeRoomVoteError unit test above covers the actual stale-sentinel
// → 409 mapping.
func TestRoomVote_QueueAdvanceBetweenCastsDoesNotPass(t *testing.T) {
	rvh, _, inter, cleanup := voteRoomFixture(t, "rv-adv", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	// First cast (user 200) — 204, threshold 2, not yet passed.
	req1 := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-adv/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr1 := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr1, req1, "rv-adv", 200)
	if rr1.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d", rr1.Code)
	}

	// Force-advance the queue via the lease-only SkipPlayback path.
	inter.SetLeaseAuthorizer(allowAllLeaseAuthorizer{})
	if _, _, _, _, err := inter.SkipPlayback(context.Background(), "rv-adv", 200, ""); err != nil {
		t.Fatalf("force advance: %v", err)
	}

	// Second cast (user 300) — queue is on next-rv-adv now. The
	// in-memory session is keyed by the OLD song ID, so the interactor
	// creates a new session for the new song. Threshold=2, 1 vote → 204.
	req2 := httptest.NewRequest(http.MethodPost, "/api/rooms/rv-adv/vote/skip", bytes.NewReader([]byte(`{}`)))
	rr2 := httptest.NewRecorder()
	rvh.HandleCastRoomVoteSkip(rr2, req2, "rv-adv", 300)
	if rr2.Code != http.StatusNoContent {
		t.Fatalf("second cast: expected 204, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 2 {
		t.Errorf("expected 2 vote_updated broadcasts (one per cast), got %d", got)
	}
	for _, r := range bc.voteResolved {
		if r.Outcome == "passed" {
			t.Errorf("expected NO vote_resolved{passed} after queue advance, got %+v", r)
		}
	}
	if got := len(bc.advanced); got != 0 {
		t.Errorf("expected 0 advanced broadcasts (queue already advanced), got %d", got)
	}
}

// rr2Body is a small helper to read a recorder's body as a string in
// a single line for error messages.
func rr2Body(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	return rr.Body.String()
}

// --- R09h prioritize handler tests ---
//
// votePrioritizeFixture mirrors voteRoomFixture but seeds a 3-song
// queue ("cur-<slug>"@0, "mid-<slug>"@1, "last-<slug>"@2) so index 2
// is a distinct non-current prioritize target. Prioritizing index 2
// moves "last" to index 1 (immediately after current).
func votePrioritizeFixture(t *testing.T, slug string, memberIDs []int) (*RoomVoteHandlers, *sql.DB, *roomqueue.Interactor, func()) {
	t.Helper()
	if len(memberIDs) == 0 {
		t.Fatalf("votePrioritizeFixture: memberIDs must be non-empty")
	}
	rqh, db, cleanup := newRoomQueueHandlers(t)
	seedUserQueue(t, db, memberIDs[0], "host-pv-"+slug+"@example.com", entity.RoleHost)
	for i, id := range memberIDs[1:] {
		seedUserQueue(t, db, id, fmt.Sprintf("guest-pv-%d-%s@example.com", i, slug), entity.RoleGuest)
	}
	roomRepo := persistence.NewPostgresRoomRepository(db)
	ctx := context.Background()
	if _, err := roomRepo.CreateRoomAndHost(ctx, slug, "PV-"+slug, memberIDs[0], time.Now().UTC()); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomID := mustRoomIDQueue(t, db, slug)
	for i, id := range memberIDs[1:] {
		if err := roomRepo.AddMember(ctx, roomID, id, entity.RoomRoleGuest, time.Now().UTC()); err != nil {
			t.Fatalf("add guest %d: %v", i+1, err)
		}
	}
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	q := entity.NewQueue()
	q.Songs = []entity.Song{
		{ID: "cur-" + slug, Title: "C", URL: "u", AddedBy: "host", AddedByID: memberIDs[0]},
		{ID: "mid-" + slug, Title: "M", URL: "u", AddedBy: "host", AddedByID: memberIDs[0]},
		{ID: "last-" + slug, Title: "L", URL: "u", AddedBy: "host", AddedByID: memberIDs[0]},
	}
	q.CurrentIndex = 0
	q.Status = entity.StatusPlaying
	if err := queueRepo.Save(ctx, roomID, q); err != nil {
		t.Fatalf("save queue: %v", err)
	}
	inter := rqh.inter
	rvh := NewRoomVoteHandlers(
		roomvote.NewInteractor(inter, fixedResolver{count: len(memberIDs)}, 30*time.Second, nil),
		inter,
		nil,
	)
	return rvh, db, inter, cleanup
}

// castPrioritize POSTs a prioritize vote with the given raw JSON body.
func castPrioritize(t *testing.T, rvh *RoomVoteHandlers, slug string, actorUserID int, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/"+slug+"/vote/prioritize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rvh.HandleCastRoomVotePrioritize(rr, req, slug, actorUserID)
	return rr
}

// TestRoomVotePrioritize_StrictBodyValidation pins the strict JSON
// contract: missing, negative, malformed, trailing, and unknown
// fields are all rejected with 400 BEFORE the interactor is reached.
// The actor (200) is a valid member so ONLY the body shape drives the
// rejection.
func TestRoomVotePrioritize_StrictBodyValidation(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-body", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	cases := []struct {
		name string
		body string
	}{
		{"missing field", `{}`},
		{"null field", `{"song_index":null}`},
		{"negative", `{"song_index":-1}`},
		{"malformed json", `{"song_index":`},
		{"non-integer", `{"song_index":"2"}`},
		{"float", `{"song_index":1.5}`},
		{"trailing data", `{"song_index":2}{"song_index":1}`},
		{"unknown field", `{"song_index":2,"actor":"evil"}`},
		{"empty body", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := castPrioritize(t, rvh, "pv-body", 200, tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("body=%q: expected 400, got %d body=%s", tc.body, rr.Code, rr.Body.String())
			}
		})
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 0 {
		t.Errorf("expected 0 broadcasts on body-validation rejections, got %d", got)
	}
}

func TestRoomVotePrioritize_Unauthorized_Returns401(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-401", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	rr := castPrioritize(t, rvh, "pv-401", 0, `{"song_index":2}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 0 {
		t.Errorf("expected 0 broadcasts on 401, got %d", got)
	}
}

func TestRoomVotePrioritize_NonMember_Returns403(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-403", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	rr := castPrioritize(t, rvh, "pv-403", 9999, `{"song_index":2}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-member: expected 403, got %d body=%s", rr.Code, rr2Body(t, rr))
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 0 {
		t.Errorf("expected 0 broadcasts on 403, got %d", got)
	}
}

func TestRoomVotePrioritize_CurrentIndex_Returns400(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-cur", []int{200, 300})
	defer cleanup()

	rr := castPrioritize(t, rvh, "pv-cur", 200, `{"song_index":0}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("current index: expected 400, got %d body=%s", rr.Code, rr2Body(t, rr))
	}
}

func TestRoomVotePrioritize_OutOfRangeIndex_Returns400(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-oor", []int{200, 300})
	defer cleanup()

	rr := castPrioritize(t, rvh, "pv-oor", 200, `{"song_index":99}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range index: expected 400, got %d body=%s", rr.Code, rr2Body(t, rr))
	}
}

func TestRoomVotePrioritize_PlainCast_NoPass_Returns204(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-204", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	rr := castPrioritize(t, rvh, "pv-204", 200, `{"song_index":2}`)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 1 {
		t.Fatalf("expected 1 vote_updated, got %d", got)
	}
	if bc.voteUpdated[0].Session == nil || bc.voteUpdated[0].Session.Threshold != 2 {
		t.Errorf("expected threshold=2 session, got %+v", bc.voteUpdated[0].Session)
	}
	if got := len(bc.voteResolved); got != 0 {
		t.Errorf("expected 0 vote_resolved, got %d", got)
	}
	if got := len(bc.prioritized); got != 0 {
		t.Errorf("expected 0 prioritized broadcasts, got %d", got)
	}
}

func TestRoomVotePrioritize_PassingVote_Returns200AndBroadcasts(t *testing.T) {
	rvh, _, inter, cleanup := votePrioritizeFixture(t, "pv-pass", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	// First vote: 204.
	if rr := castPrioritize(t, rvh, "pv-pass", 200, `{"song_index":2}`); rr.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	// Second distinct voter hits threshold 2 → passed.
	rr := castPrioritize(t, rvh, "pv-pass", 300, `{"song_index":2}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("second cast: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Resolution != "passed" {
		t.Errorf("expected resolution=passed, got %q", body.Resolution)
	}

	bc.mu.Lock()
	if got := len(bc.voteUpdated); got != 2 {
		t.Errorf("expected 2 vote_updated, got %d", got)
	}
	if got := len(bc.voteResolved); got != 1 {
		t.Fatalf("expected 1 vote_resolved, got %d", got)
	}
	if bc.voteResolved[0].Outcome != "passed" || bc.voteResolved[0].SessionID == "" {
		t.Errorf("expected resolved passed with session id, got %+v", bc.voteResolved[0])
	}
	if got := len(bc.prioritized); got != 1 {
		t.Fatalf("expected 1 room_queue_song_prioritized, got %d", got)
	}
	if bc.prioritized[0].ToIndex != 1 || bc.prioritized[0].Song.ID != "last-pv-pass" {
		t.Errorf("expected last-pv-pass moved to index 1, got %+v", bc.prioritized[0])
	}
	if !bc.prioritized[0].Song.IsPrioritized {
		t.Errorf("expected broadcast song IsPrioritized=true")
	}
	bc.mu.Unlock()

	// Reload: target moved to index 1 and persisted.
	reloaded, err := inter.GetState(context.Background(), "pv-pass", 200)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Songs[1].ID != "last-pv-pass" || !reloaded.Songs[1].IsPrioritized {
		t.Errorf("expected last-pv-pass prioritized at idx 1, got %+v", reloaded.Songs)
	}
}

func TestRoomVotePrioritize_DuplicateBallot_Returns409(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-dup", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	if rr := castPrioritize(t, rvh, "pv-dup", 200, `{"song_index":2}`); rr.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d", rr.Code)
	}
	rr := castPrioritize(t, rvh, "pv-dup", 200, `{"song_index":2}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 1 {
		t.Errorf("expected 1 vote_updated total (no broadcast on 409), got %d", got)
	}
}

// TestRoomVotePrioritize_TargetRemovedBetweenCasts_NoPass pins the
// removed-target live behavior: after the target song is removed from
// the queue between casts, a resubmission carrying the now-out-of-range
// index is rejected up front by CastPrioritizeVote's fresh-state index
// validation (400 invalid index) and NEVER fires a prioritized
// broadcast or a passed resolution. (The true stale-sentinel → 409
// race is a concurrency window covered by the queue-layer PrioritizeVote
// tests, the use-case StaleTargetSurfacesConflict test, and the
// writeRoomVoteError mapping test below.)
func TestRoomVotePrioritize_TargetRemovedBetweenCasts_NoPass(t *testing.T) {
	rvh, db, _, cleanup := votePrioritizeFixture(t, "pv-stale", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	if rr := castPrioritize(t, rvh, "pv-stale", 200, `{"song_index":2}`); rr.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d", rr.Code)
	}

	// Remove the target song so the snapshot index no longer resolves.
	roomID := mustRoomIDQueue(t, db, "pv-stale")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	q, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	q.Songs = q.Songs[:2] // drop last-pv-stale at index 2
	if err := queueRepo.Save(context.Background(), roomID, q); err != nil {
		t.Fatalf("save shrink: %v", err)
	}

	rr := castPrioritize(t, rvh, "pv-stale", 300, `{"song_index":2}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("removed target (index now out of range): expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.prioritized); got != 0 {
		t.Errorf("expected 0 prioritized broadcasts on removed target, got %d", got)
	}
	for _, r := range bc.voteResolved {
		if r.Outcome == "passed" {
			t.Errorf("expected NO vote_resolved{passed} on removed target, got %+v", r)
		}
	}
}

// TestRoomVotePrioritize_WriteRoomVoteError_MapsSentinels pins the
// error mapping additions for the prioritize path: ErrInvalidIndex and
// ErrVoteOnCurrentSong → 400, ErrStalePrioritizeSession → 409. The
// distinct stale sentinel must NOT be confused with the skip stale
// sentinel (also 409, but a different value).
func TestRoomVotePrioritize_WriteRoomVoteError_MapsSentinels(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"invalid index", roomqueue.ErrInvalidIndex, http.StatusBadRequest},
		{"vote on current", entity.ErrVoteOnCurrentSong, http.StatusBadRequest},
		{"stale prioritize", roomvote.ErrStalePrioritizeSession, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeRoomVoteError(rr, tc.err)
			if rr.Code != tc.wantStatus {
				t.Errorf("err=%v: expected %d, got %d body=%s", tc.err, tc.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestRoomVotePrioritize_ExpiredMatchingSession_Returns200Expired pins
// the expired-on-next-ballot behavior end to end at the handler seam:
// after a first ballot opens a session and that session expires, the
// NEXT ballot on the exact same target must evict the expired session,
// open a fresh one, and return 200 {"resolution":"expired"}. The
// handler must fan out EXACTLY one room_vote_resolved{expired} for the
// evicted session followed by one fresh room_vote_updated — and no
// prioritized broadcast (threshold is not met by the single fresh
// ballot).
func TestRoomVotePrioritize_ExpiredMatchingSession_Returns200Expired(t *testing.T) {
	rvh, _, _, cleanup := votePrioritizeFixture(t, "pv-exp", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	// First ballot opens the session (threshold 2, 1 vote → 204).
	if rr := castPrioritize(t, rvh, "pv-exp", 200, `{"song_index":2}`); rr.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	first := rvh.vote.ActivePrioritizeSession("pv-exp", "last-pv-exp")
	if first == nil {
		t.Fatalf("expected a live session after first cast")
	}

	// Advance the vote interactor clock past the 30s expiry window so the
	// existing matching session is expired at the next ballot. The fresh
	// session created on the next cast is stamped from the same advanced
	// clock, so it is NOT already-expired.
	rvh.vote.SetClock(func() time.Time { return time.Now().Add(31 * time.Second) })

	rr := castPrioritize(t, rvh, "pv-exp", 300, `{"song_index":2}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expired restart: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Resolution != "expired" {
		t.Errorf("expected resolution=expired, got %q", body.Resolution)
	}

	// The live session for this target must now be a FRESH instance: a
	// distinct object from the evicted one, carrying only the restarting
	// caller's single ballot (the evicted ballot from user 200 is gone).
	// The session ID is deterministic ("prioritize:<songID>"), so freshness
	// is asserted by instance identity + ballot map, not by ID.
	restarted := rvh.vote.ActivePrioritizeSession("pv-exp", "last-pv-exp")
	if restarted == nil {
		t.Fatalf("expected a fresh live session after the expired restart")
	}
	if restarted == first {
		t.Errorf("expected the restart to create a NEW session instance, got the evicted one")
	}
	if restarted.VoteCount() != 1 || restarted.VotedBy[200] {
		t.Errorf("expected fresh session with only the restart ballot, got count=%d votedBy=%v", restarted.VoteCount(), restarted.VotedBy)
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteResolved); got != 1 {
		t.Fatalf("expected 1 vote_resolved, got %d", got)
	}
	if bc.voteResolved[0].Outcome != "expired" {
		t.Errorf("expected resolved outcome=expired, got %q", bc.voteResolved[0].Outcome)
	}
	if bc.voteResolved[0].SessionID != first.ID {
		t.Errorf("expected resolved to reference the evicted session id %q, got %q", first.ID, bc.voteResolved[0].SessionID)
	}
	// One vote_updated per cast (the 204 first ballot and the fresh
	// restarted session), both dispatched by the handler.
	if got := len(bc.voteUpdated); got != 2 {
		t.Errorf("expected 2 vote_updated (first ballot + fresh restart), got %d", got)
	}
	if got := len(bc.prioritized); got != 0 {
		t.Errorf("expected 0 prioritized broadcasts on expired restart, got %d", got)
	}
}

// TestRoomVotePrioritize_SameSongIDDifferentIndex_Returns409 pins the
// same-video-ID/different-index conflict guard at the handler seam:
// once a live session for a song ID is anchored at one snapshot index,
// a ballot naming a DIFFERENT index that resolves to the SAME song ID
// must be rejected 409 up front, WITHOUT mutating the existing ballot
// map and WITHOUT any prioritized broadcast.
func TestRoomVotePrioritize_SameSongIDDifferentIndex_Returns409(t *testing.T) {
	rvh, db, _, cleanup := votePrioritizeFixture(t, "pv-conf", []int{200, 300})
	defer cleanup()
	bc := &voteTestBroadcaster{}
	rvh.queue.SetBroadcaster(bc)

	// First ballot anchors a session on "last-pv-conf" at index 2.
	if rr := castPrioritize(t, rvh, "pv-conf", 200, `{"song_index":2}`); rr.Code != http.StatusNoContent {
		t.Fatalf("first cast: expected 204, got %d", rr.Code)
	}
	anchored := rvh.vote.ActivePrioritizeSession("pv-conf", "last-pv-conf")
	if anchored == nil || anchored.SongIndex != 2 || anchored.VoteCount() != 1 {
		t.Fatalf("expected anchored session at index 2 with 1 ballot, got %+v", anchored)
	}

	// Introduce a second queue entry that shares the SAME video ID at a
	// different index (index 1). Now index 1 and index 2 both carry
	// "last-pv-conf".
	roomID := mustRoomIDQueue(t, db, "pv-conf")
	queueRepo := persistence.NewPostgresRoomQueueRepository(db)
	q, err := queueRepo.Load(context.Background(), roomID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	q.Songs[1].ID = "last-pv-conf"
	if err := queueRepo.Save(context.Background(), roomID, q); err != nil {
		t.Fatalf("save dup id: %v", err)
	}

	// A ballot on index 1 resolves to the same song ID whose live session
	// is anchored at index 2 → stale conflict 409, no ballot change.
	rr := castPrioritize(t, rvh, "pv-conf", 300, `{"song_index":1}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-id/different-index: expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}

	// The anchored session must be untouched (still 1 ballot at index 2).
	after := rvh.vote.ActivePrioritizeSession("pv-conf", "last-pv-conf")
	if after == nil || after.SongIndex != 2 || after.VoteCount() != 1 {
		t.Errorf("expected anchored session unchanged (index 2, 1 ballot), got %+v", after)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if got := len(bc.voteUpdated); got != 1 {
		t.Errorf("expected 1 vote_updated total (no broadcast on 409), got %d", got)
	}
	if got := len(bc.prioritized); got != 0 {
		t.Errorf("expected 0 prioritized broadcasts on conflict, got %d", got)
	}
}

// Compile-time assertion: voteTestBroadcaster must satisfy
// roomqueue.Broadcaster. Keeps the test from silently breaking if the
// interface grows or shrinks.
var _ roomqueue.Broadcaster = (*voteTestBroadcaster)(nil)
var _ roomvote.Resolver = fixedResolver{}
var _ room.PlaybackLeaseAuthorizer = allowAllLeaseAuthorizer{}
