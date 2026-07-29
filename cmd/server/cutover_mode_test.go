package main

// R14c true-mode composition and retirement tests. PG-gated (skip when
// PostgreSQL is unreachable) through setupPostgresForTest; each test
// owns a throwaway schema. They exercise the REAL composition path:
//
//   - authoritative startup fails closed when the durable cutover
//     marker is absent;
//   - authoritative startup composes ONE shared
//     *persistence.PostgresRoomActivityRepository into all three
//     activity-producing interactors;
//   - every retired legacy pattern (15 REST pairs + global /ws) answers
//     410 Gone with the exact retirement envelope, while the live
//     contracts stay live in both modes.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"local-music-queue/internal/infrastructure/persistence"
)

// insertCutoverMarkerForTest writes the durable marker row into the
// test's scoped schema so the fail-closed guard passes. Synthetic
// fixture values only.
func insertCutoverMarkerForTest(t *testing.T, scopedDSN string) {
	t.Helper()
	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped dsn: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		INSERT INTO room_cutover_marker
			(id, room_cutover_id, target_room_slug, target_room_id, host_user_id,
			 source_hashes, target_hashes, legacy_id_offset, cutover_pre_commit_at, binary_build_sha)
		VALUES
			(1, '00000000-0000-0000-0000-000000000001', 'rehearsal-room', 1, 1,
			 '{}', '{}', 0, NOW(), 'test-build-sha')`)
	if err != nil {
		t.Fatalf("insert cutover marker: %v", err)
	}
}

func setCutoverTestEnv(t *testing.T, scopedDSN string) {
	t.Helper()
	if os.Getenv("YTDLP_PATH") == "" {
		t.Setenv("YTDLP_PATH", "/bin/true")
	}
	t.Setenv("DATABASE_URL", scopedDSN)
}

func TestSetupApp_AuthoritativeMode_FailsClosedWithoutMarker(t *testing.T) {
	scopedDSN := setupPostgresForTest(t) // migrated to head, but NO marker
	setCutoverTestEnv(t, scopedDSN)

	_, _, _, _, _, cleanup, err := setupApp(setupOptions{roomCutoverAuthoritative: true})
	if err == nil {
		cleanup()
		t.Fatal("authoritative setup must fail when room_cutover_marker id=1 is absent")
	}
	if !strings.Contains(err.Error(), "room_cutover_marker") {
		t.Errorf("startup error %q should name the missing marker", err.Error())
	}
}

func TestSetupApp_AuthoritativeMode_ComposesSharedPostgresWriter(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	insertCutoverMarkerForTest(t, scopedDSN)
	setCutoverTestEnv(t, scopedDSN)

	comp, roomVoteInteractor := runObservedSetup(t, setupOptions{roomCutoverAuthoritative: true})
	if comp.roomVote != roomVoteInteractor {
		t.Error("observed roomvote interactor is not the one setup returned")
	}

	writers := map[string]interface{}{
		"roomqueue":     comp.roomQueue.ActivityWriterSeam(),
		"roomvote":      comp.roomVote.ActivityWriterSeam(),
		"roomautoqueue": comp.roomAutoQueue.ActivityWriterSeam(),
	}
	var pg *persistence.PostgresRoomActivityRepository
	for name, w := range writers {
		got, ok := w.(*persistence.PostgresRoomActivityRepository)
		if !ok {
			t.Errorf("%s: expected *persistence.PostgresRoomActivityRepository in authoritative mode, got %T", name, w)
			continue
		}
		if pg == nil {
			pg = got
		} else if got != pg {
			t.Errorf("%s: expected the same PostgreSQL writer instance across all producers", name)
		}
	}
}

// requestPattern issues the request implied by a mux registration
// pattern ("METHOD /path" or a method-less "/path", exercised as GET).
func requestPattern(t *testing.T, mux *http.ServeMux, pattern string) *httptest.ResponseRecorder {
	t.Helper()
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		method, path = http.MethodGet, pattern
	}
	req := httptest.NewRequest(method, path, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestSetupApp_AuthoritativeMode_TombstonesRetiredContracts(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	insertCutoverMarkerForTest(t, scopedDSN)
	setCutoverTestEnv(t, scopedDSN)

	mux, _, _, _, _, cleanup, err := setupApp(setupOptions{roomCutoverAuthoritative: true})
	if err != nil {
		t.Fatalf("setupApp(authoritative): %v", err)
	}
	t.Cleanup(cleanup)

	const wantDoc = "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"
	for _, pattern := range retiredGlobalContractPatterns {
		t.Run(pattern, func(t *testing.T) {
			rr := requestPattern(t, mux, pattern)
			if rr.Code != http.StatusGone {
				t.Fatalf("%s: got status %d, want 410", pattern, rr.Code)
			}
			if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("%s: Content-Type = %q, want application/json", pattern, ct)
			}
			if link := rr.Header().Get("Link"); link != `</api/rooms>; rel="successor-version"` {
				t.Errorf("%s: Link = %q, want </api/rooms>; rel=\"successor-version\"", pattern, link)
			}
			var body struct {
				Error         string `json:"error"`
				Code          string `json:"code"`
				Documentation string `json:"documentation"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s: body %q is not JSON: %v", pattern, rr.Body.String(), err)
			}
			if body.Error != "gone" || body.Code != "global_contract_retired" || body.Documentation != wantDoc {
				t.Errorf("%s: envelope = %+v, want {gone global_contract_retired %s}", pattern, body, wantDoc)
			}
			// The /ws tombstone must answer the plain HTTP phase and
			// never negotiate a WebSocket upgrade.
			if strings.Contains(rr.Header().Get("Upgrade"), "websocket") {
				t.Errorf("%s: tombstone must never upgrade", pattern)
			}
		})
	}
}

// TestSetupApp_AuthoritativeMode_LiveContractsStayLive proves the
// retirement is scoped: auth, priority-balance, YouTube search, room,
// invite, and per-room WS routes stay live in authoritative mode. Each
// probe asserts the exact status the real registered route returns for
// an unauthenticated, empty request, not merely that it is not 410 — a
// 404 from a mistyped path would also satisfy "!= 410" and hide a route
// that was silently retired or never registered.
func TestSetupApp_AuthoritativeMode_LiveContractsStayLive(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	insertCutoverMarkerForTest(t, scopedDSN)
	setCutoverTestEnv(t, scopedDSN)

	mux, _, _, _, _, cleanup, err := setupApp(setupOptions{roomCutoverAuthoritative: true})
	if err != nil {
		t.Fatalf("setupApp(authoritative): %v", err)
	}
	t.Cleanup(cleanup)

	// Expected unauthenticated/empty-request status for each surviving
	// contract as wired in setupAppWithActivityObserver:
	//   - the four open utility routes reject a missing body/param with
	//     400 (their handlers run before any auth);
	//   - the roomAuth-wrapped room and invite-redeem routes reject a
	//     missing bearer token with 401;
	//   - /ws/rooms/{slug} rejects a missing session_token with 401
	//     (pinned independently by TestSetupApp_RegistersRoomWSRoute).
	// The invite route is the REAL registered pattern .../redeem; the
	// previous test probed a nonexistent .../accept path, which only
	// ever returned 404 and proved nothing.
	liveProbes := []struct {
		pattern string
		want    int
	}{
		{"POST /api/auth/google", http.StatusBadRequest},
		{"POST /api/auth", http.StatusBadRequest},
		{"GET /api/user/priority-balance", http.StatusBadRequest},
		{"GET /api/youtube/search", http.StatusBadRequest},
		{"POST /api/rooms", http.StatusUnauthorized},
		{"GET /api/rooms/some-slug", http.StatusUnauthorized},
		{"POST /api/invites/some-token/redeem", http.StatusUnauthorized},
		{"GET /ws/rooms/some-slug", http.StatusUnauthorized},
	}
	for _, probe := range liveProbes {
		rr := requestPattern(t, mux, probe.pattern)
		if rr.Code == http.StatusGone {
			t.Errorf("%s: live contract must not be tombstoned (got 410)", probe.pattern)
			continue
		}
		if rr.Code != probe.want {
			t.Errorf("%s: got status %d, want %d (registered live route)", probe.pattern, rr.Code, probe.want)
		}
	}
}

// TestSetupApp_FalseMode_KeepsLegacyContractsLive proves the false-mode
// (pre-cutover/rollback) pair still serves the real legacy handlers:
// no retired pattern answers 410.
func TestSetupApp_FalseMode_KeepsLegacyContractsLive(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	setCutoverTestEnv(t, scopedDSN)

	mux, _, _, _, _, cleanup, err := setupApp(setupOptions{})
	if err != nil {
		t.Fatalf("setupApp(false mode): %v", err)
	}
	t.Cleanup(cleanup)

	for _, pattern := range retiredGlobalContractPatterns {
		rr := requestPattern(t, mux, pattern)
		if rr.Code == http.StatusGone {
			t.Errorf("%s: legacy contract must stay live in false mode (got 410)", pattern)
		}
	}
	// Spot-check the read-only legacy queue endpoint actually works.
	rr := requestPattern(t, mux, "GET /api/queue")
	if rr.Code != http.StatusOK {
		t.Errorf("GET /api/queue in false mode: got %d, want 200", rr.Code)
	}
}
