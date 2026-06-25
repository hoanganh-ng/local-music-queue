package http

import (
	"bytes"
	"context"
	"encoding/json"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain/entity"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// R05 — focused regression coverage that an unauthorized client cannot
// mutate queue, playback, priority, vote, or auto-queue state. Each test
// also asserts zero broadcasts on rejection, so a successful 200 with a
// missing broadcast would still fail the test.
//
// The handlers are called directly (without the full middleware stack) so
// the tests stay fast and decoupled from PostgreSQL. The auth check inside
// the handler body is what we are validating.

func TestR05_QueueMutation_Unauthenticated(t *testing.T) {
	tc := newTestContext(t)

	cases := []struct {
		name string
		call func() *httptest.ResponseRecorder
	}{
		{
			name: "add",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(AddSongRequest{URL: "https://youtube.com/watch?v=u1", AddedBy: "Mallory"})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
				// Deliberately no Authorization header, no injected user.
				rr := httptest.NewRecorder()
				tc.handlers.HandleAddSong(rr, req)
				return rr
			},
		},
		{
			name: "remove",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(RemoveSongRequest{Index: ptrInt(1)})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/remove", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleRemoveSong(rr, req)
				return rr
			},
		},
		{
			name: "clear",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(ClearQueueRequest{RequestedBy: "Mallory"})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/clear", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleClearQueue(rr, req)
				return rr
			},
		},
		{
			name: "prioritize",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(PrioritizeSongRequest{UserID: tc.guestUser.ID, SongIndex: 2})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/prioritize", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandlePrioritizeSong(rr, req)
				return rr
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// Capture broadcast count BEFORE the call to assert it
			// doesn't grow on rejection.
			pre := len(tc.broadcaster.broadcasts)
			rr := c.call()
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d; body: %s", rr.Code, rr.Body.String())
			}
			if got := len(tc.broadcaster.broadcasts); got != pre {
				t.Errorf("expected no broadcasts on rejection; pre=%d post=%d", pre, got)
			}
		})
	}

	// Additionally verify the queue state is unchanged: 3 songs still
	// present in their original order.
	state, err := tc.queueInteractor.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if len(state.Songs) != 3 {
		t.Errorf("expected 3 songs unchanged, got %d", len(state.Songs))
	}
}

func TestR05_PlaybackControl_Unauthenticated(t *testing.T) {
	tc := newTestContext(t)

	cases := []struct {
		name string
		call func() *httptest.ResponseRecorder
	}{
		{
			name: "skip",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(SkipRequest{RequestedBy: "Mallory"})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/skip", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleSkipSong(rr, req)
				return rr
			},
		},
		{
			name: "status",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(StatusRequest{Status: entity.StatusPaused, RequestedBy: "Mallory"})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/status", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleSetStatus(rr, req)
				return rr
			},
		},
		{
			name: "sync",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(SyncPlaybackRequest{Elapsed: 42})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/sync", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleSyncPlayback(rr, req)
				return rr
			},
		},
		{
			name: "ended",
			call: func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/api/queue/ended", nil)
				rr := httptest.NewRecorder()
				tc.handlers.HandleSongEnded(rr, req)
				return rr
			},
		},
		{
			name: "prev",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(PrevRequest{RequestedBy: "Mallory"})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/prev", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandlePrevSong(rr, req)
				return rr
			},
		},
		{
			name: "volume",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(VolumeRequest{Direction: "up"})
				req := httptest.NewRequest(http.MethodPost, "/api/queue/volume", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleChangeVolume(rr, req)
				return rr
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pre := len(tc.broadcaster.broadcasts)
			rr := c.call()
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d; body: %s", rr.Code, rr.Body.String())
			}
			if got := len(tc.broadcaster.broadcasts); got != pre {
				t.Errorf("expected no broadcasts on rejection; pre=%d post=%d", pre, got)
			}
		})
	}

	// State unchanged: current song still vid0, status still playing.
	state, _ := tc.queueInteractor.GetState(context.Background())
	if state.CurrentIndex != 0 {
		t.Errorf("expected CurrentIndex 0 unchanged, got %d", state.CurrentIndex)
	}
	if len(state.Songs) != 3 {
		t.Errorf("expected 3 songs unchanged, got %d", len(state.Songs))
	}
}

func TestR05_PlaybackControl_GuestForbidden(t *testing.T) {
	tc := newTestContext(t)

	// Role-based access control lives in the route wiring (RequireRole),
	// not in the handler body. These tests wrap the handler in the full
	// auth+role stack so the role check is exercised end-to-end. A guest
	// user must be rejected with 403; the handler must NOT run, so no
	// broadcast is emitted and queue state is unchanged.
	hostOrAdmin := RequireRole(entity.RoleHost, entity.RoleAdmin)
	auth := RequireAuth(tc.authInteractor)
	wrappedSkip := auth(hostOrAdmin(tc.handlers.HandleSkipSong))
	wrappedClear := auth(hostOrAdmin(tc.handlers.HandleClearQueue))
	wrappedVolume := auth(hostOrAdmin(tc.handlers.HandleChangeVolume))

	cases := []struct {
		name    string
		handler http.HandlerFunc
		body    string
	}{
		{name: "skip", handler: wrappedSkip, body: `{"requested_by":"Guest"}`},
		{name: "clear", handler: wrappedClear, body: `{"requested_by":"Guest"}`},
		{name: "volume", handler: wrappedVolume, body: `{"direction":"down"}`},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pre := len(tc.broadcaster.broadcasts)
			req := httptest.NewRequest(http.MethodPost, "/api/queue/"+c.name, bytes.NewReader([]byte(c.body)))
			req.Header.Set("Authorization", "Bearer "+tc.guestToken)
			rr := httptest.NewRecorder()
			c.handler(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
			}
			if got := len(tc.broadcaster.broadcasts); got != pre {
				t.Errorf("expected no broadcasts on rejection; pre=%d post=%d", pre, got)
			}
		})
	}
}

func TestR05_VotePrivileged_Unauthenticated(t *testing.T) {
	tc := newTestContext(t)

	cases := []struct {
		name string
		call func() *httptest.ResponseRecorder
	}{
		{
			name: "skip",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(VoteSkipRequest{UserID: tc.guestUser.ID, UserRole: string(entity.RoleGuest)})
				req := httptest.NewRequest(http.MethodPost, "/api/vote/skip", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleVoteSkip(rr, req)
				return rr
			},
		},
		{
			name: "prioritize",
			call: func() *httptest.ResponseRecorder {
				body, _ := json.Marshal(VotePriorityRequest{UserID: tc.guestUser.ID, UserRole: string(entity.RoleGuest), SongIndex: 1})
				req := httptest.NewRequest(http.MethodPost, "/api/vote/prioritize", bytes.NewReader(body))
				rr := httptest.NewRecorder()
				tc.handlers.HandleVotePriority(rr, req)
				return rr
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pre := len(tc.broadcaster.broadcasts)
			rr := c.call()
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d; body: %s", rr.Code, rr.Body.String())
			}
			if got := len(tc.broadcaster.broadcasts); got != pre {
				t.Errorf("expected no broadcasts on rejection; pre=%d post=%d", pre, got)
			}
		})
	}
}

func TestR05_VotePrivileged_HostForbidden(t *testing.T) {
	tc := newTestContext(t)
	// Host role is excluded from voting (canVote returns false for host).
	body, _ := json.Marshal(VoteSkipRequest{UserID: tc.hostUser.ID, UserRole: string(entity.RoleHost)})
	req := httptest.NewRequest(http.MethodPost, "/api/vote/skip", bytes.NewReader(body))
	req = withUserCtx(req, tc.hostUser)
	rr := httptest.NewRecorder()
	tc.handlers.HandleVoteSkip(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if len(tc.broadcaster.broadcasts) != 0 {
		t.Errorf("expected no broadcasts on rejection, got %d", len(tc.broadcaster.broadcasts))
	}
}

func TestR05_Prioritize_RejectsSpoofedUserID(t *testing.T) {
	tc := newTestContext(t)

	// A guest sends a prioritize request with user_id = some other user's
	// id, hoping to spend the other user's priority token. The server
	// must ignore the body user_id and use the token's user (guest).
	// The test grants the guest a priority balance so the call can
	// proceed, and asserts the broadcast UserID is the token user
	// (guest), not the spoofed body value.
	ctx := context.Background()
	if err := tc.repo.user.IncrementPriority(ctx, tc.guestUser.ID); err != nil {
		t.Fatalf("seed priority balance: %v", err)
	}
	tc.guestUser.PriorityBalance = 1

	// A guest sends user_id = admin's id, attempting to spend admin's
	// priority tokens. The server must use the token user (guest) and
	// emit the broadcast with UserID = guest, not admin.
	body, _ := json.Marshal(PrioritizeSongRequest{UserID: tc.adminUser.ID, SongIndex: 2})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/prioritize", bytes.NewReader(body))
	req = withUserCtx(req, tc.guestUser)
	rr := httptest.NewRecorder()
	tc.handlers.HandlePrioritizeSong(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 (server uses token user, not body user_id), got %d; body: %s", rr.Code, rr.Body.String())
	}
	if len(tc.broadcaster.broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(tc.broadcaster.broadcasts))
	}
	b := tc.broadcaster.broadcasts[0]
	if b.eventType != "song_prioritized" {
		t.Fatalf("expected song_prioritized event, got %s", b.eventType)
	}
	data := b.data.(ws.SongPrioritizedData)
	if data.UserID != tc.guestUser.ID {
		t.Errorf("expected UserID %d (server-resolved guest), got %d (spoofed)", tc.guestUser.ID, data.UserID)
	}
	if data.UserID == tc.adminUser.ID {
		t.Errorf("body user_id leaked into broadcast — R05 violation")
	}
}

func TestR05_AutoQueueToggle_Unauthenticated(t *testing.T) {
	tc := newTestContext(t)
	body, _ := json.Marshal(ToggleAutoQueueRequest{Enabled: false})
	req := httptest.NewRequest(http.MethodPost, "/api/autoqueue/toggle", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	tc.autoQueueHandlers.HandleToggleAutoQueue(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if len(tc.broadcaster.broadcasts) != 0 {
		t.Errorf("expected no broadcasts on rejection, got %d", len(tc.broadcaster.broadcasts))
	}
}

func TestR05_AutoQueueToggle_GuestForbidden(t *testing.T) {
	tc := newTestContext(t)
	// Role-based access control lives in the route wiring, not the
	// handler body. Wrap with the full auth+role stack so the role
	// check is exercised end-to-end.
	hostOrAdmin := RequireRole(entity.RoleHost, entity.RoleAdmin)
	auth := RequireAuth(tc.authInteractor)
	wrapped := auth(hostOrAdmin(tc.autoQueueHandlers.HandleToggleAutoQueue))

	body, _ := json.Marshal(ToggleAutoQueueRequest{Enabled: false})
	req := httptest.NewRequest(http.MethodPost, "/api/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tc.guestToken)
	rr := httptest.NewRecorder()
	wrapped(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if len(tc.broadcaster.broadcasts) != 0 {
		t.Errorf("expected no broadcasts on rejection, got %d", len(tc.broadcaster.broadcasts))
	}
}

func TestR05_AutoQueueToggle_HostAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}
	tc := newTestContext(t)
	hostOrAdmin := RequireRole(entity.RoleHost, entity.RoleAdmin)
	auth := RequireAuth(tc.authInteractor)
	wrapped := auth(hostOrAdmin(tc.autoQueueHandlers.HandleToggleAutoQueue))

	body, _ := json.Marshal(ToggleAutoQueueRequest{Enabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/autoqueue/toggle", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tc.hostToken)
	rr := httptest.NewRecorder()
	wrapped(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	// The auto-queue handler broadcasts via the ws.Hub, not the mock
	// broadcaster wired into Handlers. The end-to-end broadcast coverage
	// for auto_queue_config_changed is exercised in the auto-queue
	// interactor tests. Here we only assert the handler returned 200.
}

func TestR05_AddSong_ServerAttributionIgnoresBodyAddedBy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}
	tc := newTestContext(t)
	body, _ := json.Marshal(AddSongRequest{URL: "https://youtube.com/watch?v=attribution", AddedBy: "SpoofedDisplayName"})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
	req = withUserCtx(req, tc.guestUser)
	rr := httptest.NewRecorder()
	tc.handlers.HandleAddSong(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if len(tc.broadcaster.broadcasts) == 0 {
		t.Fatal("expected at least one broadcast")
	}
	last := tc.broadcaster.broadcasts[len(tc.broadcaster.broadcasts)-1]
	data := last.data.(ws.SongAddedData)
	if data.Song.AddedBy != tc.guestUser.DisplayName {
		t.Errorf("expected attribution to %q, got %q (body field leaked through)", tc.guestUser.DisplayName, data.Song.AddedBy)
	}
	if data.Song.AddedByID != tc.guestUser.ID {
		t.Errorf("expected AddedByID %d, got %d (body field leaked through)", tc.guestUser.ID, data.Song.AddedByID)
	}
}

// ptrInt is a tiny helper for *int literals.
func ptrInt(v int) *int { return &v }

// Note: the existing newTestContext helper is the same one used by
// handlers_test.go; it is the canonical R05 test fixture. It seeds three
// songs with deterministic ownership and creates session tokens for
// guest, otherGuest, host, and admin users.
