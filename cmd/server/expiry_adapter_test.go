package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/roomqueue"
	"local-music-queue/internal/usecase/roomvote"
)

// TestRoomVoteHandlers_HubIsReal ensures the construction path used
// by setupApp() never hands roomvote a typed-nil *ws.RoomWSHub.
// Regression guard for the R09b wiring-order panic: a typed-nil
// pointer stored in a non-nil interface still routes method calls,
// but the inner receiver dereference panics on the first call.
func TestRoomVoteHandlers_HubIsReal(t *testing.T) {
	// Build a real hub via the production constructor.
	resolver := ws.RoomQueueResolverFunc(func(_ context.Context, _ string) (*entity.Room, error) {
		return &entity.Room{ID: 1, Slug: "x", Status: entity.RoomStatusActive}, nil
	})
	member := ws.RoomMemberResolverFunc(func(_ context.Context, _ int64, _ int) (bool, error) { return true, nil })
	state := ws.RoomQueueStateResolverFunc(func(_ context.Context, _ int64) (*entity.Queue, error) {
		q := entity.NewQueue()
		q.Songs = []entity.Song{{ID: "s", Title: "S", URL: "u"}}
		q.CurrentIndex = 0
		return q, nil
	})
	hub := ws.NewRoomWSHub(resolver, member, state)
	t.Cleanup(hub.Close)

	// Smoke: a real hub returns the live count (here 0 because no
	// clients are connected) without panicking. A typed-nil hub would
	// panic on the first method call.
	if got := hub.UniqueConnectedUserIDs("x"); got != 0 {
		t.Fatalf("expected 0 unique users on empty hub, got %d", got)
	}
}

// fakeRoomvoteForExpiry captures the outcomes returned by the runner
// and lets the test invoke them on a stub.
type fakeRoomvoteForExpiry struct {
	mu        sync.Mutex
	expired   []roomvote.ExpiredOutcome
	outErr    error
	callCount int
}

func (f *fakeRoomvoteForExpiry) ExpireSessions(_ context.Context) ([]roomvote.ExpiredOutcome, error) {
	f.mu.Lock()
	f.callCount++
	f.mu.Unlock()
	return f.expired, f.outErr
}

type resolvedRoom struct{ slug, outcome string }
type expiryTestHub struct {
	mu        sync.Mutex
	resolved  []resolvedRoom
	closeOnce sync.Once
}

func (h *expiryTestHub) BroadcastRoomVoteResolved(slug, _, outcome string, _ *entity.Queue) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resolved = append(h.resolved, resolvedRoom{slug: slug, outcome: outcome})
}

func (h *expiryTestHub) Close() { h.closeOnce.Do(func() {}) }

// the rest of the Broadcaster methods are no-ops; tests only exercise
// BroadcastRoomVoteResolved.
func (*expiryTestHub) BroadcastRoomQueueSync(_ string, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomQueueSongAdded(_ string, _ entity.Song, _ int, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomQueueSongRemoved(_ string, _ int, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomQueueCleared(_ string, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomQueueSongPrioritized(_ string, _, _ int, _ entity.Song, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomPlaybackStatusChanged(_ string, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomPlaybackElapsedSync(_ string, _ int, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomPlaybackSongAdvanced(_ string, _ string, _, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomVoteUpdated(_ string, _ *entity.VoteSession, _ int, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomPlaybackVolumeChanged(_ string, _ string)                          {}
func (*expiryTestHub) BroadcastRoomPlaybackSongPrevious(_ string, _, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue) {}
func (*expiryTestHub) BroadcastRoomAutoQueueAdded(_ string, _ entity.Song, _ string, _ int, _ *entity.Song, _ entity.PlaybackStatus, _ int, _ *entity.Queue)    {}
func (*expiryTestHub) BroadcastRoomAutoQueueConfigChanged(_ string, _ bool, _ string)       {}

var (
	_ roomqueue.Broadcaster = (*expiryTestHub)(nil)
	_ votingExpireRunner    = (*fakeRoomvoteForExpiry)(nil)
)

func newExpiryTestHub() *expiryTestHub { return &expiryTestHub{} }

// TestExpiryAdapter_FansOutExpiredResolution asserts the server-owned
// adapter calls BroadcastRoomVoteResolved once per expired outcome
// without the roomvote package importing delivery/ws.
func TestExpiryAdapter_FansOutExpiredResolution(t *testing.T) {
	hub := newExpiryTestHub()
	t.Cleanup(hub.Close)

	f := &fakeRoomvoteForExpiry{
		expired: []roomvote.ExpiredOutcome{{
			SessionID: "skip:x:s1",
			RoomSlug:  "x",
			Session:   &entity.VoteSession{ID: "skip:x:s1", Type: entity.VoteTypeSkip, SongID: "s1"},
		}},
	}
	ad := expiryAdapter{broadcaster: hub, vote: f}
	ad.runOnce(context.Background())

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if got := len(hub.resolved); got != 1 {
		t.Fatalf("expected 1 resolved broadcast, got %d", got)
	}
	if hub.resolved[0].slug != "x" || hub.resolved[0].outcome != "expired" {
		t.Errorf("unexpected resolution: %+v", hub.resolved[0])
	}
}

// TestExpiryAdapter_FansOutPrioritizeAndSkipResolutions pins the R09h
// generalization at the server-owned adapter layer: the sweep is
// type-agnostic, so a mixed batch of skip and prioritize expired
// outcomes each fans out exactly one BroadcastRoomVoteResolved with
// outcome="expired" and the slug recovered from the (generalized)
// session key. This also guards that adding prioritize sessions did
// not regress skip expiry fan-out.
func TestExpiryAdapter_FansOutPrioritizeAndSkipResolutions(t *testing.T) {
	hub := newExpiryTestHub()
	t.Cleanup(hub.Close)

	f := &fakeRoomvoteForExpiry{
		expired: []roomvote.ExpiredOutcome{
			{
				SessionID: "skip:alpha:s1",
				RoomSlug:  "alpha",
				Session:   &entity.VoteSession{ID: "skip:alpha:s1", Type: entity.VoteTypeSkip, SongID: "s1"},
			},
			{
				SessionID: "prioritize:beta:s3",
				RoomSlug:  "beta",
				Session:   &entity.VoteSession{ID: "prioritize:beta:s3", Type: entity.VoteTypePrioritize, SongID: "s3"},
			},
		},
	}
	ad := expiryAdapter{broadcaster: hub, vote: f}
	ad.runOnce(context.Background())

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if got := len(hub.resolved); got != 2 {
		t.Fatalf("expected 2 resolved broadcasts, got %d (%+v)", got, hub.resolved)
	}
	slugs := map[string]string{}
	for _, r := range hub.resolved {
		slugs[r.slug] = r.outcome
	}
	if slugs["alpha"] != "expired" {
		t.Errorf("expected skip room alpha resolved expired, got %q", slugs["alpha"])
	}
	if slugs["beta"] != "expired" {
		t.Errorf("expected prioritize room beta resolved expired, got %q", slugs["beta"])
	}
}

// TestExpiryAdapter_RunnerTickCallsExpireSessions ensures the runner
// calls ExpireSessions at least twice within ~3x the configured
// interval.
func TestExpiryAdapter_RunnerTickCallsExpireSessions(t *testing.T) {
	hub := newExpiryTestHub()
	t.Cleanup(hub.Close)
	f := &fakeRoomvoteForExpiry{}

	ad := expiryAdapter{broadcaster: hub, vote: f, interval: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ad.run(ctx)

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := f.callCount
		f.mu.Unlock()
		if n >= 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.callCount < 2 {
		t.Fatalf("expected >=2 ExpireSessions calls within 200ms, got %d", f.callCount)
	}
}