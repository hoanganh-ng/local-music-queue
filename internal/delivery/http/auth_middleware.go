package http

import (
	"context"
	"errors"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/usecase/auth"
	"net/http"
)

// userKey is the private context key under which RequireAuth stores the
// resolved *entity.User so downstream handlers can read it via UserFromCtx.
// It is intentionally unexported and unique to this package so it cannot
// collide with keys defined elsewhere (notably the actorKey used by main.go
// for the room API, which only stores the user id).
type userKey struct{}

// Authenticator is the minimal surface the auth middleware needs from the
// auth package. *auth.Interactor satisfies it. Defined as an interface so
// the middleware can be unit-tested without a real UserRepository.
type Authenticator interface {
	ResolveSession(ctx context.Context, token string) (*entity.User, error)
}

// RequireAuth returns a middleware that resolves the bearer token via the
// auth interactor. Unauthenticated callers get 401; unexpected resolve errors
// collapse to 500 with no leakage of the underlying error text. The resolved
// *entity.User is injected into the request context so handlers can read it
// via UserFromCtx. The handler signature is the same as http.HandlerFunc so
// existing wiring stays readable.
//
// Test-only path: if a user was pre-injected via WithUserForTest, the
// middleware trusts it and skips the bearer token round-trip. This lets
// mux-level integration tests inject a user without standing up the
// session round-trip. Production code paths always carry a bearer token
// from a real client.
//
// This middleware is the single source of truth for backend identity for the
// non-room REST endpoints. The room API uses an analogous wrapper defined
// in cmd/server; both share ExtractToken semantics.
func RequireAuth(a Authenticator) func(http.HandlerFunc) http.HandlerFunc {
	return func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Test injection short-circuit. When a user is pre-injected
			// into the context (only possible from a Go test that calls
			// WithUserForTest), the middleware treats the request as
			// authenticated. Production callers cannot reach this branch
			// because no public API injects a user into a real request.
			if pre := UserFromCtx(r.Context()); pre != nil {
				fn(w, r)
				return
			}

			token := ExtractToken(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			user, err := a.ResolveSession(r.Context(), token)
			if err != nil {
				if errors.Is(err, auth.ErrSessionInvalid) || errors.Is(err, auth.ErrSessionExpired) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				// Operational failure (DB down, etc.) — return 500 without
				// leaking the underlying error text.
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			ctx := context.WithValue(r.Context(), userKey{}, user)
			fn(w, r.WithContext(ctx))
		}
	}
}

// RequireRole wraps a handler (already wrapped by RequireAuth) and rejects
// callers whose resolved role is not in allowedRoles with 403. The role
// check is server-side; client-supplied user_role fields in the JSON body
// are not consulted.
//
// A 0 user (no actor in context, which should not happen if RequireAuth ran
// first) also returns 401 for safety.
func RequireRole(allowedRoles ...entity.Role) func(http.HandlerFunc) http.HandlerFunc {
	return func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user := UserFromCtx(r.Context())
			if user == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !roleAllowed(user.Role, allowedRoles) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			fn(w, r)
		}
	}
}

// roleAllowed reports whether role is in the allowed set. An empty allowed
// set is treated as "any authenticated user" so RequireRole() with no args
// is a no-op gate (used for endpoints that only require authentication).
func roleAllowed(role entity.Role, allowed []entity.Role) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, r := range allowed {
		if role == r {
			return true
		}
	}
	return false
}

// UserFromCtx extracts the *entity.User stashed in the request context by
// RequireAuth. Returns nil when the context was not produced by the
// middleware.
func UserFromCtx(ctx context.Context) *entity.User {
	u, _ := ctx.Value(userKey{}).(*entity.User)
	return u
}

// WithUserForTest is a test-only helper that injects a resolved user into
// the request context. Production code must use RequireAuth; this exists
// so unit tests that drive handlers directly (without the middleware
// round-trip) can still satisfy the auth check.
func WithUserForTest(r *http.Request, user *entity.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userKey{}, user))
}
