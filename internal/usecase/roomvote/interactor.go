// Package roomvote hosts the room-scoped vote use cases. Vote sessions
// are in-memory, room-scoped, current-song-scoped, 30-second expiring,
// single-instance only, never persisted. Threshold is captured at
// session creation via the supplied Resolver (sourced from the per-room
// WS hub's UniqueConnectedUserIDs) using the strict-majority rule
// max(2, n/2).
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
	"sync"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/room"
	"local-music-queue/internal/usecase/roomqueue"
)

// ErrStaleSession mirrors roomqueue.ErrStaleSkipVote for callers who
// want the roomvote-package shape. Pass-through on resolution.
var ErrStaleSession = roomqueue.ErrStaleSkipVote

// QueueSkipping is the slice of the roomqueue interactor surface that
// CastSkipVote + ExpireSessions require. Defined in this package (not
// on the queue package) to keep the dependency one-way and to let tests
// inject a fake without constructing a real *roomqueue.Interactor.
//
// The production *roomqueue.Interactor satisfies this interface because
// it implements all four methods (RoomBySlug + IsMember were added in
// Task 4; SkipVote was added in Task 4; GetStateByRoomID is from R07d).
type QueueSkipping interface {
	RoomBySlug(ctx context.Context, slug string) (*entity.Room, error)
	IsMember(ctx context.Context, roomID int64, actorUserID int) bool
	GetStateByRoomID(ctx context.Context, roomID int64) (*entity.Queue, error)
	SkipVote(ctx context.Context, slug, expectedSongID string) (*entity.Queue, int, int, *entity.Song, error)
}

// Resolver exposes the unique-connected-user count for a room so the
// interactor can derive the threshold at session creation. Source is
// (*ws.RoomWSHub).UniqueConnectedUserIDs(slug).
type Resolver interface {
	UniqueConnectedUserIDs(roomSlug string) int
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
}

// NewInteractor constructs a vote interactor. resolver may be nil in
// unit tests (the threshold falls back to 0 → threshold(0)=2 in that
// case); expiry defaults to 30s when zero. queueInter is the slice of
// the roomqueue interactor the vote use cases need; production wiring
// passes the real *roomqueue.Interactor (which satisfies QueueSkipping
// implicitly).
func NewInteractor(queueInter QueueSkipping, resolver Resolver, expiry time.Duration) *Interactor {
	if expiry == 0 {
		expiry = 30 * time.Second
	}
	return &Interactor{
		queueInter: queueInter,
		resolver:   resolver,
		sessions:   make(map[string]*entity.VoteSession),
		expiry:     expiry,
		now:        time.Now,
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
	ExpiredID     string
	ExpiredQueue  *entity.Queue
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

// CastSkipVote casts one skip vote for the current song of the room.
// Sessions are created lazily on first vote; the threshold is captured
// at that moment. Resolutions are returned in the Outcome — the caller
// fans out RoomVoteResolved + room_playback_song_advanced.
//
// Errors:
//   room.ErrInvalidSlug        → malformed slug
//   room.ErrRoomNotFound       → no room for slug
//   room.ErrArchived           → room archived
//   room.ErrForbidden          → actor is not an active member
//   entity.ErrNoCurrentSong    → queue has no current song
//   entity.ErrAlreadyVoted     → this user already voted
//   entity.ErrVoteSessionExpired → session existed but expired (race
//                                 against the clock after eviction)
//   ErrStaleSession            → vote passed but SkipVote refused
//                                 because the queue advanced under us
//
// Actor identity comes ONLY from the resolved session token (the
// handler reads actorFromCtx). The actorUserID parameter is
// authoritative; body-supplied identity is ignored.
func (i *Interactor) CastSkipVote(ctx context.Context, slug string, actorUserID int) (*Outcome, error) {
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
	return out, nil
}

// ExpireSessions evicts any sessions whose Expiry time has passed and
// returns them as ExpiredOutcomes for the caller to broadcast. The
// handler invokes this on a best-effort ticker; failure is non-fatal.
//
// Sessions are evicted across ALL rooms (the in-memory map is small).
// StateQueue is best-effort and may be nil.
func (i *Interactor) ExpireSessions(ctx context.Context) ([]ExpiredOutcome, error) {
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
		}
		out = append(out, ExpiredOutcome{
			SessionID:  key,
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

// --- helpers ---

// sessionKey derives the in-memory key for a (slug, songID) pair.
func sessionKey(slug, songID string) string {
	return fmt.Sprintf("skip:%s:%s", slug, songID)
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
// connected user count. Falls back to 2 when n <= 2 (server-
// misconfigured, empty room, or resolver nil).
func threshold(n int) int {
	t := n / 2
	if t < 2 {
		return 2
	}
	return t
}