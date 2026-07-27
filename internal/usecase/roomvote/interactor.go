// Package roomvote hosts the room-scoped vote use cases. Vote sessions
// are in-memory, room-scoped, current-song-scoped, 30-second expiring,
// single-instance only, never persisted. Threshold is captured at
// session creation via the supplied Resolver (sourced from the per-room
// WS hub's UniqueConnectedUserIDs) using the strict-majority rule
// max(2, n/2 + 1) — a true majority (n = 3 → 2, n = 4 → 3, n = 5 → 3)
// with a 2-voter floor so a lone voter in an empty room can never pass
// alone.
//
// The interactor does NOT broadcast — successful vote casts and
// resolutions return outcome objects that the handler fans out via the
// existing roomqueue.Broadcaster surface. This keeps the usecase
// independent of delivery/ws and consistent with the roomqueue and
// global vote package patterns.
//
// Resolution ownership: when a vote session passes, the interactor
// calls (*QueueSkipping).SkipVote(ctx, slug, expectedSongID), which
// runs under the roomqueue mutex and validates the expected current
// song before mutating. The interactor MUST NOT mutate room_queue_state
// directly.
package roomvote

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomqueue"
)

// ErrStaleSession mirrors roomqueue.ErrStaleSkipVote for callers who
// want the roomvote-package shape. Pass-through on resolution.
var ErrStaleSession = roomqueue.ErrStaleSkipVote

// ErrStalePrioritizeSession mirrors roomqueue.ErrStalePrioritizeVote
// for callers who want the roomvote-package shape. Pass-through on
// resolution: a passing prioritize vote whose target moved/was removed
// under it surfaces this so the handler maps it to HTTP 409.
var ErrStalePrioritizeSession = roomqueue.ErrStalePrioritizeVote

// QueueSkipping is the slice of the roomqueue interactor surface that
// CastSkipVote + CastPrioritizeVote + ExpireSessions require. Defined
// in this package (not on the queue package) to keep the dependency
// one-way and to let tests inject a fake without constructing a real
// *roomqueue.Interactor.
//
// The production *roomqueue.Interactor satisfies this interface because
// it implements all methods (RoomBySlug + IsMember + SkipVote from R09b;
// GetStateByRoomID from R07d; PrioritizeVote from R09h).
type QueueSkipping interface {
	RoomBySlug(ctx context.Context, slug string) (*entity.Room, error)
	IsMember(ctx context.Context, roomID int64, actorUserID int) bool
	GetStateByRoomID(ctx context.Context, roomID int64) (*entity.Queue, error)
	SkipVote(ctx context.Context, slug, expectedSongID string) (*entity.Queue, int, int, *entity.Song, error)
	PrioritizeVote(ctx context.Context, slug, expectedSongID string, expectedIndex int) (*entity.Queue, int, int, entity.Song, error)
}

// Resolver exposes the unique-connected-user count for a room so the
// interactor can derive the threshold at session creation. Source is
// (*ws.RoomWSHub).UniqueConnectedUserIDs(slug).
type Resolver interface {
	UniqueConnectedUserIDs(roomSlug string) int
}

// ActivityWriter is the narrow R09i seam roomvote uses to append
// room-scoped activities. cmd/server injects the explicit no-op
// implementation pre-R14c; the shape matches
// repository.RoomActivityRepository.AddActivity.
type ActivityWriter interface {
	AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error
}

// Interactor owns the room-scoped vote use cases. Votes are stored
// ONLY in memory; nothing here persists to Postgres.
type Interactor struct {
	queueInter QueueSkipping
	resolver   Resolver
	mu         sync.Mutex
	sessions   map[string]*entity.VoteSession // "skip:{slug}:{songID}"
	expiry     time.Duration
	now        func() time.Time
	// activityWriter is the R09i room-activity seam. Ordered vote
	// activity batches are appended best-effort AFTER the session
	// mutex is released; a write failure NEVER fails the vote flow.
	// nil is tolerated (no activities are produced).
	activityWriter ActivityWriter
}

// NewInteractor constructs a vote interactor. resolver may be nil in
// unit tests (the threshold falls back to 0 → threshold(0)=2 in that
// case); expiry defaults to 30s when zero. queueInter is the slice of
// the roomqueue interactor the vote use cases need; production wiring
// passes the real *roomqueue.Interactor (which satisfies QueueSkipping
// implicitly). activityWriter may be nil (activity production
// disabled).
func NewInteractor(queueInter QueueSkipping, resolver Resolver, expiry time.Duration, activityWriter ActivityWriter) *Interactor {
	if expiry == 0 {
		expiry = 30 * time.Second
	}
	return &Interactor{
		queueInter:     queueInter,
		resolver:       resolver,
		sessions:       make(map[string]*entity.VoteSession),
		expiry:         expiry,
		now:            time.Now,
		activityWriter: activityWriter,
	}
}

// ActivityWriterSeam returns the wired activity writer, or nil when
// unset. Exposed so composition tests can verify the injected
// implementation.
func (i *Interactor) ActivityWriterSeam() ActivityWriter { return i.activityWriter }

// actorName resolves the activity actor label: the authenticated
// display name when present, otherwise the canonical "user #<id>"
// fallback (R09i actor identity rule).
func actorName(displayName string, userID int) string {
	if strings.TrimSpace(displayName) == "" {
		return fmt.Sprintf("user #%d", userID)
	}
	return displayName
}

// appendActivities writes one ordered activity batch sequentially,
// best-effort. Callers MUST invoke it after releasing i.mu (the R09i
// lock rule). A failed append is logged (room id/slug + type only)
// and the remaining batch continues.
func (i *Interactor) appendActivities(ctx context.Context, roomID int64, slug string, acts []entity.Activity) {
	if i.activityWriter == nil || roomID == 0 {
		return
	}
	for _, a := range acts {
		if err := i.activityWriter.AddActivity(ctx, roomID, a); err != nil {
			log.Printf("room activity: room %d (%s): append %s failed: %v", roomID, slug, a.Type, err)
		}
	}
}

// SetClock replaces the time source (tests only). Not safe to call
// while CastSkipVote / ExpireSessions are in flight; production code
// should not invoke it.
func (i *Interactor) SetClock(now func() time.Time) { i.now = now }

// Outcome represents a single vote cast's result. The handler fans
// out RoomVoteUpdated on every call (Session != nil). When
// Resolution == "passed" or "expired", the handler additionally fans
// out RoomVoteResolved with the matching outcome string. On
// "passed" the existing room_playback_song_advanced event is also
// fired (via the roomqueue.Broadcaster) with reason="skip" — the
// AdvanceQueue / AdvancePrev / AdvanceNext / AdvanceSong fields carry
// the data it needs.
//
// StaleSession is true when the SkipVote call was refused because the
// current song had moved on; the handler maps that to HTTP 409 and
// does NOT broadcast RoomVoteResolved (the room has already moved past
// the vote).
type Outcome struct {
	Session      *entity.VoteSession
	Passed       bool
	Resolution   string // "" | "passed" | "expired"
	AdvanceQueue *entity.Queue
	AdvancePrev  int
	AdvanceNext  int
	AdvanceSong  *entity.Song
	StaleSession bool
	// ExpiredID / ExpiredQueue / ExpiredSession describe a session that
	// was evicted on entry to CastSkipVote (because it had expired). The
	// handler fans out a single RoomVoteResolved{outcome:"expired"} for
	// the evicted session before broadcasting RoomVoteUpdated for the
	// new session. All three fields are zero-valued when no eviction
	// happened.
	ExpiredID      string
	ExpiredQueue   *entity.Queue
	ExpiredSession *entity.VoteSession
}

// ExpiredOutcome is what ExpireSessions returns per evicted session so
// the handler can broadcast RoomVoteResolved{outcome:"expired"} for
// each. StateQueue may be nil when the queue could not be read (the
// broadcaster tolerates a nil state).
type ExpiredOutcome struct {
	SessionID  string
	RoomSlug   string
	Session    *entity.VoteSession
	StateQueue *entity.Queue
}

// PrioritizeOutcome represents a single prioritize-vote cast's result.
// Mirrors Outcome but carries the room_queue_song_prioritized event
// tuple instead of the skip/advance tuple. The handler fans out
// room_vote_updated on every call (Session != nil). When
// Resolution == "passed", it additionally fans out
// room_vote_resolved{"passed"} and room_queue_song_prioritized with the
// (FromIndex, ToIndex, PrioritizeSong, PrioritizeQueue) payload.
//
// When Resolution == "expired", the ballot arrived after the prior
// session for this exact (slug, songID) target had expired: the old
// session is evicted and snapshotted into ExpiredID / ExpiredSession /
// ExpiredQueue, a fresh session is created and cast, and the handler
// fans out one room_vote_resolved{"expired"} for the evicted session
// followed by one room_vote_updated for the fresh session, replying
// 200 {"resolution":"expired"}. Unlike skip, prioritize expiry is
// otherwise handled entirely by the shared ExpireSessions sweep;
// eviction-on-entry here is scoped to the exact target key only, so a
// prioritize ballot never disturbs a coexisting session for a
// different target song. Resolution is therefore "", "passed", or
// "expired".
//
// StaleSession is true when the PrioritizeVote call was refused because
// the target song moved / was removed under the vote; the handler maps
// that to HTTP 409 and does NOT broadcast room_vote_resolved (the queue
// moved past the vote).
type PrioritizeOutcome struct {
	Session         *entity.VoteSession
	Passed          bool
	Resolution      string // "" | "passed" | "expired"
	PrioritizeQueue *entity.Queue
	FromIndex       int
	ToIndex         int
	PrioritizeSong  entity.Song
	StaleSession    bool
	// ExpiredID / ExpiredQueue / ExpiredSession describe a prioritize
	// session that was evicted on entry to CastPrioritizeVote because it
	// had expired. The handler fans out a single
	// RoomVoteResolved{outcome:"expired"} for the evicted session before
	// broadcasting RoomVoteUpdated for the fresh session. All three
	// fields are zero-valued when no eviction happened.
	ExpiredID      string
	ExpiredQueue   *entity.Queue
	ExpiredSession *entity.VoteSession
}

// CastSkipVote casts one skip vote for the current song of the room.
// Sessions are created lazily on first vote; the threshold is captured
// at that moment. Resolutions are returned in the Outcome — the caller
// fans out RoomVoteResolved + room_playback_song_advanced.
//
// Errors:
//
//	room.ErrInvalidSlug        → malformed slug
//	room.ErrRoomNotFound       → no room for slug
//	room.ErrArchived           → room archived
//	room.ErrForbidden          → actor is not an active member
//	entity.ErrNoCurrentSong    → queue has no current song
//	entity.ErrAlreadyVoted     → this user already voted
//	entity.ErrVoteSessionExpired → session existed but expired (race
//	                              against the clock after eviction)
//	ErrStaleSession            → vote passed but SkipVote refused
//	                              because the queue advanced under us
//
// Actor identity comes ONLY from the resolved session token (the
// handler reads actorFromCtx). The actorUserID parameter is
// authoritative; body-supplied identity is ignored.
// actorDisplayName is the backend-resolved display name used only for
// activity attribution; blank falls back to "user #<id>".
func (i *Interactor) CastSkipVote(ctx context.Context, slug string, actorUserID int, actorDisplayName string) (*Outcome, error) {
	// R09i: ordered activity batch (expired → cast → passed → action)
	// written AFTER the session mutex is released. The defer is
	// registered BEFORE the Lock so Go's LIFO defer ordering runs the
	// unlock first, then the sequential batch write.
	var pendingRoomID int64
	var pending []entity.Activity
	defer func() {
		i.appendActivities(ctx, pendingRoomID, slug, pending)
	}()

	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now()

	// Atomically (a) evict any expired session for this slug, (b) load
	// room + queue, (c) check current song, (d) reuse-or-create the new
	// session, (e) cast, (f) handle pass. Snapshot the OLD evicted
	// session so the handler can broadcast a "expired" resolution for
	// it before the new session goes out.
	var out *Outcome // declared up here so the eviction branch can populate it
	{
		var evictedID string
		var evictedSession *entity.VoteSession
		for key, s := range i.sessions {
			if !hasSlugPrefix(key, slug) {
				continue
			}
			if s.IsExpiredAt(now) {
				delete(i.sessions, key)
				evictedID = key
				evictedSession = s
				break
			}
		}
		if evictedSession != nil {
			out = &Outcome{
				Resolution:     "expired",
				ExpiredID:      evictedID,
				ExpiredSession: evictedSession,
			}
		}
	}

	roomObj, err := i.queueInter.RoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, room.ErrInvalidSlug) ||
			errors.Is(err, room.ErrRoomNotFound) ||
			errors.Is(err, room.ErrArchived) {
			return nil, err
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	pendingRoomID = roomObj.ID

	if !i.queueInter.IsMember(ctx, roomObj.ID, actorUserID) {
		return nil, room.ErrForbidden
	}

	queue, err := i.queueInter.GetStateByRoomID(ctx, roomObj.ID)
	if err != nil {
		return nil, fmt.Errorf("load room queue: %w", err)
	}
	if queue == nil || queue.CurrentIndex < 0 || queue.CurrentIndex >= len(queue.Songs) {
		return nil, entity.ErrNoCurrentSong
	}
	currentSong := queue.Songs[queue.CurrentIndex]
	key := sessionKey(slug, currentSong.ID)

	if out == nil {
		out = &Outcome{}
	}

	// On the expired branch, populate ExpiredQueue from the freshly
	// loaded (current-song) state. The handler uses this as the state
	// carried on the "expired" broadcast — the queue has not changed.
	if out.Resolution == "expired" {
		out.ExpiredQueue = queue
	}

	// Reuse-or-create the session.
	session, exists := i.sessions[key]
	if !exists || session.IsExpiredAt(now) {
		n := 0
		if i.resolver != nil {
			n = i.resolver.UniqueConnectedUserIDs(slug)
		}
		session = entity.NewVoteSession(entity.VoteTypeSkip, currentSong, queue.CurrentIndex, threshold(n), i.expiry)
		// NewVoteSession stamps ExpiresAt from time.Now; re-stamp with
		// our clock so test clocks drive expiry.
		session.CreatedAt = now
		session.ExpiresAt = now.Add(i.expiry)
		i.sessions[key] = session
	}

	if err := session.Cast(actorUserID); err != nil {
		return nil, err
	}
	out.Session = session

	// R09i: the ballot was accepted — record the eviction's vote_expired
	// (if any) before this ballot's vote_cast. A rejected ballot above
	// records nothing (including the eviction, which is coupled to the
	// replacement ballot per the R09i ordering contract).
	if out.Resolution == "expired" && out.ExpiredSession != nil {
		pending = append(pending, entity.NewActivity(entity.ActivityVoteExpired, "System",
			fmt.Sprintf("vote to %s \"%s\" expired", out.ExpiredSession.Type, out.ExpiredSession.SongTitle)))
	}
	actor := actorName(actorDisplayName, actorUserID)
	pending = append(pending, entity.NewActivity(entity.ActivityVoteCast, actor,
		fmt.Sprintf("voted to %s \"%s\" (%d/%d)", session.Type, session.SongTitle, session.VoteCount(), session.Threshold)))

	// Threshold path.
	if !session.IsPassed() {
		return out, nil
	}

	out.Passed = true

	// Delete the session BEFORE calling SkipVote so a concurrent cast
	// on the new current song sees a fresh-session path.
	delete(i.sessions, key)

	q, prevIdx, newIdx, newSong, err := i.queueInter.SkipVote(ctx, slug, currentSong.ID)
	if err != nil {
		if errors.Is(err, roomqueue.ErrStaleSkipVote) {
			out.StaleSession = true
			// The stale branch is special: the song moved on under us,
			// so there is no "passed" resolution to broadcast (the room
			// has already moved past the vote). We DO keep Session /
			// Passed on the outcome so the handler can log. The
			// eviction-side "expired" resolution (if any) is also
			// suppressed — the queue advanced, the old session is
			// moot either way.
			// R09i: only the already-recorded vote_cast (and any
			// eviction vote_expired) stand; no vote_passed / action
			// activity on the stale branch.
			out.Resolution = ""
			return out, ErrStaleSession
		}
		return nil, err
	}

	out.Resolution = "passed"
	out.AdvanceQueue = q
	out.AdvancePrev = prevIdx
	out.AdvanceNext = newIdx
	out.AdvanceSong = newSong
	// R09i: the queue action succeeded — record vote_passed then the
	// decisive-actor skip action, after the already-recorded vote_cast.
	pending = append(pending,
		entity.NewActivity(entity.ActivityVotePassed, "System",
			fmt.Sprintf("vote to %s \"%s\" passed", session.Type, session.SongTitle)),
		entity.NewActivity(entity.ActivitySongSkipped, actor,
			fmt.Sprintf("vote skipped \"%s\"", session.SongTitle)),
	)
	return out, nil
}

// CastPrioritizeVote casts one prioritize vote for a specific upcoming
// song of the room, identified by its current queue index. Any active
// member may vote; no room role, global role, or player lease is
// required. Sessions are keyed prioritize:{slug}:{songID}, created
// lazily on first vote, and expire after the interactor's expiry
// window. Multiple prioritize sessions can coexist per room (one per
// target song), unlike skip which is single-per-room (keyed on the
// current song).
//
// The target must be a valid, non-current song at the moment the
// session is created; the (songID, songIndex) pair is snapshotted into
// the session so resolution can detect a stale target. On a passing
// vote the interactor calls (*QueueSkipping).PrioritizeVote(ctx, slug,
// session.SongID, session.SongIndex) — which runs under the roomqueue
// mutex and re-validates the snapshot against fresh state before
// mutating. The interactor MUST NOT mutate room_queue_state directly
// and MUST NOT touch priority balances.
//
// Errors:
//
//	room.ErrInvalidSlug        → malformed slug
//	room.ErrRoomNotFound       → no room for slug
//	room.ErrArchived           → room archived
//	room.ErrForbidden          → actor is not an active member
//	roomqueue.ErrInvalidIndex  → songIndex out of range
//	entity.ErrVoteOnCurrentSong → songIndex identifies the current song
//	                             (at cast time OR the target became the
//	                             current song under a passing vote)
//	entity.ErrAlreadyVoted     → this user already voted this session
//	entity.ErrVoteSessionExpired → session existed but expired
//	ErrStalePrioritizeSession  → a live session for this song ID exists
//	                             at a DIFFERENT index (same-ID/different-
//	                             entry conflict), OR the vote passed but
//	                             PrioritizeVote refused because the
//	                             target moved / was removed under us
//
// Actor identity comes ONLY from the resolved session token; the
// actorUserID parameter is authoritative and body-supplied identity is
// ignored by the caller. actorDisplayName is the backend-resolved
// display name used only for activity attribution; blank falls back to
// "user #<id>".
func (i *Interactor) CastPrioritizeVote(ctx context.Context, slug string, songIndex, actorUserID int, actorDisplayName string) (*PrioritizeOutcome, error) {
	// R09i: ordered activity batch (expired → cast → passed → action)
	// written AFTER the session mutex is released (defer registered
	// BEFORE the Lock — see CastSkipVote).
	var pendingRoomID int64
	var pending []entity.Activity
	defer func() {
		i.appendActivities(ctx, pendingRoomID, slug, pending)
	}()

	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now()

	roomObj, err := i.queueInter.RoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, room.ErrInvalidSlug) ||
			errors.Is(err, room.ErrRoomNotFound) ||
			errors.Is(err, room.ErrArchived) {
			return nil, err
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	pendingRoomID = roomObj.ID

	if !i.queueInter.IsMember(ctx, roomObj.ID, actorUserID) {
		return nil, room.ErrForbidden
	}

	queue, err := i.queueInter.GetStateByRoomID(ctx, roomObj.ID)
	if err != nil {
		return nil, fmt.Errorf("load room queue: %w", err)
	}
	if queue == nil || songIndex < 0 || songIndex >= len(queue.Songs) {
		return nil, roomqueue.ErrInvalidIndex
	}
	if queue.CurrentIndex >= 0 && songIndex == queue.CurrentIndex {
		return nil, entity.ErrVoteOnCurrentSong
	}
	target := queue.Songs[songIndex]
	key := prioritizeSessionKey(slug, target.ID)

	out := &PrioritizeOutcome{}

	session, exists := i.sessions[key]

	// Conflict guard (same songID, different index): a live session
	// keyed on this target song ID whose snapshot index no longer matches
	// the requested index means two distinct queue entries share the same
	// video ID. Reject immediately as a stale conflict WITHOUT touching
	// the existing ballot map, so ballots for one entry can never accrue
	// toward a different entry's session.
	if exists && !session.IsExpiredAt(now) && session.SongIndex != songIndex {
		return nil, ErrStalePrioritizeSession
	}

	// Expired matching session: the prior session for this exact target
	// expired. Snapshot + evict it so the handler can broadcast a single
	// room_vote_resolved{"expired"} for the old session, then fall through
	// to create a fresh session and cast on it. Eviction is scoped to
	// THIS key only, so a coexisting session for a different target song
	// is never disturbed (that is left to the shared ExpireSessions
	// sweep).
	if exists && session.IsExpiredAt(now) {
		out.Resolution = "expired"
		out.ExpiredID = session.ID
		out.ExpiredSession = session
		out.ExpiredQueue = queue
		delete(i.sessions, key)
		exists = false
	}

	// Reuse-or-create the session for this exact (slug, songID). Expiry
	// of OTHER sessions is handled by the shared ExpireSessions sweep.
	if !exists {
		n := 0
		if i.resolver != nil {
			n = i.resolver.UniqueConnectedUserIDs(slug)
		}
		session = entity.NewVoteSession(entity.VoteTypePrioritize, target, songIndex, threshold(n), i.expiry)
		// NewVoteSession stamps ExpiresAt from time.Now; re-stamp with our
		// clock so test clocks drive expiry.
		session.CreatedAt = now
		session.ExpiresAt = now.Add(i.expiry)
		i.sessions[key] = session
	}

	if err := session.Cast(actorUserID); err != nil {
		return nil, err
	}
	out.Session = session

	// R09i: the ballot was accepted — record the eviction's vote_expired
	// (if any) before this ballot's vote_cast (see CastSkipVote).
	if out.Resolution == "expired" && out.ExpiredSession != nil {
		pending = append(pending, entity.NewActivity(entity.ActivityVoteExpired, "System",
			fmt.Sprintf("vote to %s \"%s\" expired", out.ExpiredSession.Type, out.ExpiredSession.SongTitle)))
	}
	actor := actorName(actorDisplayName, actorUserID)
	pending = append(pending, entity.NewActivity(entity.ActivityVoteCast, actor,
		fmt.Sprintf("voted to %s \"%s\" (%d/%d)", session.Type, session.SongTitle, session.VoteCount(), session.Threshold)))

	if !session.IsPassed() {
		return out, nil
	}

	out.Passed = true

	// Delete the session BEFORE calling PrioritizeVote so a concurrent
	// cast starts a fresh session rather than re-passing this one.
	delete(i.sessions, key)

	q, fromIdx, toIdx, song, err := i.queueInter.PrioritizeVote(ctx, slug, session.SongID, session.SongIndex)
	if err != nil {
		if errors.Is(err, roomqueue.ErrStalePrioritizeVote) {
			out.StaleSession = true
			// R09i: only the already-recorded vote_cast (and any
			// eviction vote_expired) stand; no vote_passed / action
			// activity on the stale branch.
			out.Resolution = ""
			return out, ErrStalePrioritizeSession
		}
		return nil, err
	}

	out.Resolution = "passed"
	out.PrioritizeQueue = q
	out.FromIndex = fromIdx
	out.ToIndex = toIdx
	out.PrioritizeSong = song
	// R09i: the queue action succeeded — record vote_passed then the
	// decisive-actor prioritize action, after the already-recorded
	// vote_cast.
	pending = append(pending,
		entity.NewActivity(entity.ActivityVotePassed, "System",
			fmt.Sprintf("vote to %s \"%s\" passed", session.Type, session.SongTitle)),
		entity.NewActivity(entity.ActivityPlayback, actor,
			fmt.Sprintf("vote prioritized \"%s\"", session.SongTitle)),
	)
	return out, nil
}

// ExpireSessions evicts any sessions whose Expiry time has passed and
// returns them as ExpiredOutcomes for the caller to broadcast. The
// handler invokes this on a best-effort ticker; failure is non-fatal.
//
// Sessions are evicted across ALL rooms (the in-memory map is small).
// StateQueue is best-effort and may be nil. ExpiredOutcome.SessionID is
// the prioritize session.ID ("prioritize:{songID}") for prioritize
// sessions so it matches room_vote_updated; skip sessions keep the
// internal map key to preserve the accepted R09b contract.
func (i *Interactor) ExpireSessions(ctx context.Context) ([]ExpiredOutcome, error) {
	// R09i: one System vote_expired per evicted session, written AFTER
	// the session mutex is released (defer registered BEFORE the Lock).
	// Sessions whose room cannot be resolved are skipped (no room id to
	// attribute the activity to).
	type pendingExpiry struct {
		roomID int64
		slug   string
		act    entity.Activity
	}
	var pendingActs []pendingExpiry
	defer func() {
		for _, p := range pendingActs {
			i.appendActivities(ctx, p.roomID, p.slug, []entity.Activity{p.act})
		}
	}()

	i.mu.Lock()
	defer i.mu.Unlock()
	now := i.now()
	var out []ExpiredOutcome
	for key, s := range i.sessions {
		if !s.IsExpiredAt(now) {
			continue
		}
		delete(i.sessions, key)
		slug, _ := splitSessionKey(key)
		var q *entity.Queue
		if r, err := i.queueInter.RoomBySlug(ctx, slug); err == nil {
			q, _ = i.queueInter.GetStateByRoomID(ctx, r.ID)
			if s != nil {
				pendingActs = append(pendingActs, pendingExpiry{
					roomID: r.ID,
					slug:   slug,
					act: entity.NewActivity(entity.ActivityVoteExpired, "System",
						fmt.Sprintf("vote to %s \"%s\" expired", s.Type, s.SongTitle)),
				})
			}
		}
		// Prioritize outcomes must carry the SAME session identifier a
		// client saw on room_vote_updated and the passed / expired-on-
		// entry paths — session.ID ("prioritize:{songID}") — so a client
		// can correlate a ticker expiry with the session it was tracking.
		// Skip outcomes keep the internal map key to preserve the accepted
		// R09b identifier contract (changing it needs a separate approval).
		sessionID := key
		if s != nil && s.Type == entity.VoteTypePrioritize {
			sessionID = s.ID
		}
		out = append(out, ExpiredOutcome{
			SessionID:  sessionID,
			RoomSlug:   slug,
			Session:    s,
			StateQueue: q,
		})
	}
	return out, nil
}

// ActiveSession returns a snapshot of the in-memory session for a
// (slug, songID) pair, or nil when none exists. Used by tests.
func (i *Interactor) ActiveSession(slug, songID string) *entity.VoteSession {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.sessions[sessionKey(slug, songID)]
}

// ActivePrioritizeSession returns a snapshot of the in-memory
// prioritize session for a (slug, songID) pair, or nil when none
// exists. Used by tests.
func (i *Interactor) ActivePrioritizeSession(slug, songID string) *entity.VoteSession {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.sessions[prioritizeSessionKey(slug, songID)]
}

// --- helpers ---

// sessionKey derives the in-memory key for a (slug, songID) pair.
func sessionKey(slug, songID string) string {
	return fmt.Sprintf("skip:%s:%s", slug, songID)
}

// prioritizeSessionKey derives the in-memory key for a prioritize vote
// session over a (slug, songID) pair. Prioritize sessions share the
// same map as skip sessions but never collide because the type prefix
// differs.
func prioritizeSessionKey(slug, songID string) string {
	return fmt.Sprintf("prioritize:%s:%s", slug, songID)
}

// hasSlugPrefix reports whether key encodes a session for slug. Keys
// are "skip:{slug}:{songID}", so a key belongs to slug iff it has the
// form "skip:{slug}:" (the trailing colon guards against prefix-only
// matches like "skip:foo" vs "skip:foobar").
func hasSlugPrefix(key, slug string) bool {
	prefix := "skip:" + slug + ":"
	return len(key) > len(prefix) && key[:len(prefix)] == prefix
}

// threshold returns the strict-majority threshold for a unique
// connected user count: max(2, n/2 + 1). The +1 makes the rule a
// strict majority (a tie is not enough); the 2 floor stops a single
// voter in an empty room from passing alone. Applied at session
// creation; flat floor for n <= 2 (server-misconfigured, empty room,
// or resolver nil).
func threshold(n int) int {
	t := n/2 + 1
	if t < 2 {
		return 2
	}
	return t
}
