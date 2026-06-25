package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/session"
	"local-music-queue/internal/usecase/auth"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// authTestEnv bundles the fixtures the auth middleware tests need: an
// auth interactor with a real session store, plus pre-minted tokens for
// a guest, a host, and an admin.
type authTestEnv struct {
	auth          Authenticator
	sessionStore  auth.SessionStore
	clock         auth.RealClock
	guestUser     *entity.User
	hostUser      *entity.User
	adminUser     *entity.User
	guestToken    string
	hostToken     string
	adminToken    string
	expiredToken  string
}

func newAuthTestEnv(t *testing.T) *authTestEnv {
	t.Helper()
	clock := auth.RealClock{}
	store := session.NewInMemoryStore(clock)

	guest := &entity.User{Email: "guest@urekamedia.vn", DisplayName: "Guest", Role: entity.RoleGuest}
	host := &entity.User{Email: "host@example.com", DisplayName: "Host", Role: entity.RoleHost}
	admin := &entity.User{Email: "admin@example.com", DisplayName: "Admin", Role: entity.RoleAdmin}

	// We use a stub authenticator that does NOT require a UserRepository
	// (ResolveSession just returns a fixed user by token). The existing
	// auth.NewInteractor requires a UserRepository; for unit tests of
	// the middleware alone, the stub is sufficient and decoupled from
	// the persistence layer.
	ai := newStubAuthenticator(store, map[string]*entity.User{
		"guest-token": guest,
		"host-token":  host,
		"admin-token": admin,
	})

	expired, _, err := store.Create(context.Background(), guest.ID, -time.Hour)
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}

	return &authTestEnv{
		auth:         ai,
		sessionStore: store,
		clock:        clock,
		guestUser:    guest,
		hostUser:     host,
		adminUser:    admin,
		guestToken:   "guest-token",
		hostToken:    "host-token",
		adminToken:   "admin-token",
		expiredToken: expired,
	}
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	env := newAuthTestEnv(t)
	wrapped := RequireAuth(env.auth)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_MalformedHeader(t *testing.T) {
	env := newAuthTestEnv(t)
	wrapped := RequireAuth(env.auth)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Token abc")
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	env := newAuthTestEnv(t)
	wrapped := RequireAuth(env.auth)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	env := newAuthTestEnv(t)
	wrapped := RequireAuth(env.auth)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer "+env.expiredToken)
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_ResolveError_Returns500_NoLeak(t *testing.T) {
	env := newAuthTestEnv(t)
	// Replace the interactor with one whose ResolveSession always
	// returns a non-auth, non-expired error (e.g. a DB failure).
	ai := newStubAuthenticatorWithError(env.sessionStore, errors.New("sqlite lookup failed: connection refused"))
	wrapped := RequireAuth(ai)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer guest-token")
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
	body := rr.Body.String()
	if bytes.Contains([]byte(body), []byte("sqlite")) {
		t.Errorf("response body leaks raw DB error: %q", body)
	}
}

func TestRequireAuth_HappyPath_InjectsUser(t *testing.T) {
	env := newAuthTestEnv(t)
	var seen *entity.User
	wrapped := RequireAuth(env.auth)(func(w http.ResponseWriter, r *http.Request) {
		seen = UserFromCtx(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer "+env.hostToken)
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if seen == nil {
		t.Fatal("expected user in context")
	}
	if seen.Role != entity.RoleHost {
		t.Errorf("expected host role, got %q", seen.Role)
	}
}

func TestRequireAuth_TestInjectedUser_ShortCircuitsMiddleware(t *testing.T) {
	env := newAuthTestEnv(t)
	var seen *entity.User
	wrapped := RequireAuth(env.auth)(func(w http.ResponseWriter, r *http.Request) {
		seen = UserFromCtx(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	// A pre-injected user (via WithUserForTest) should short-circuit
	// the bearer-token check. The test mirrors how integration tests
	// drive the mux without a real session.
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req = WithUserForTest(req, env.adminUser)
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if seen == nil || seen.Role != entity.RoleAdmin {
		t.Errorf("expected admin user from injected context, got %+v", seen)
	}
}

func TestRequireRole_NoAllowed_AllowsAnyAuthed(t *testing.T) {
	env := newAuthTestEnv(t)
	wrapped := RequireAuth(env.auth)(RequireRole()(okHandler))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer "+env.guestToken)
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (RequireRole() with no roles is a no-op), got %d", rr.Code)
	}
}

func TestRequireRole_AllowedForMatchingRole(t *testing.T) {
	env := newAuthTestEnv(t)
	wrapped := RequireAuth(env.auth)(RequireRole(entity.RoleHost, entity.RoleAdmin)(okHandler))

	for _, tok := range []string{env.hostToken, env.adminToken} {
		req := httptest.NewRequest(http.MethodGet, "/anything", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		wrapped(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for token %q, got %d", tok, rr.Code)
		}
	}
}

func TestRequireRole_ForbiddenForOtherRole(t *testing.T) {
	env := newAuthTestEnv(t)
	wrapped := RequireAuth(env.auth)(RequireRole(entity.RoleHost, entity.RoleAdmin)(okHandler))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer "+env.guestToken)
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireRole_NoUserInContext_Returns401(t *testing.T) {
	// RequireRole called without RequireAuth first — no user in context.
	wrapped := RequireRole(entity.RoleHost)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// okHandler is a stand-in handler used to verify that the middleware
// forwards the request after auth succeeds.
func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, "ok"); err != nil {
		tlog("okHandler write: %v", err)
	}
}

func tlog(format string, args ...interface{}) {
	// Helper for okHandler which cannot use t directly.
	// We intentionally swallow the error here because okHandler is a
	// stand-in; tests that care about side effects should provide their
	// own handler.
	_ = format
	_ = args
}

// --- Stub interactor (avoids requiring a real UserRepository) ---

// stubAuthenticator implements the http.Authenticator interface used by
// RequireAuth, so middleware tests can run without a real PostgreSQL
// UserRepository. The session validation still uses the real InMemoryStore
// so expired/invalid tokens behave authentically.
type stubAuthenticator struct {
	store auth.SessionStore
	users map[string]*entity.User
	err   error
}

func newStubAuthenticator(store auth.SessionStore, users map[string]*entity.User) *stubAuthenticator {
	return &stubAuthenticator{
		store: store,
		users: users,
	}
}

func newStubAuthenticatorWithError(store auth.SessionStore, err error) *stubAuthenticator {
	return &stubAuthenticator{
		store: store,
		err:   err,
	}
}

func (s *stubAuthenticator) ResolveSession(ctx context.Context, token string) (*entity.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	// Synthetic test tokens (e.g. "guest-token") map directly to users
	// without round-tripping the session store. This keeps the test
	// self-contained and decoupled from session creation order.
	if u, ok := s.users[token]; ok {
		return u, nil
	}
	// Any other token must validate against the real session store
	// (covers the "invalid token" and "expired token" cases).
	if _, err := s.store.Resolve(ctx, token); err != nil {
		return nil, err
	}
	return nil, auth.ErrSessionInvalid
}

var _ Authenticator = (*stubAuthenticator)(nil)
