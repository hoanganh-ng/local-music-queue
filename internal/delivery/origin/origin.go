// Package origin implements the shared allowed-origin policy for HTTP CORS
// and WebSocket upgrades. It is the single source of truth for both paths:
// HTTP responses echo the requested Origin only when it is on the allow
// list, and WebSocket CheckOrigin delegates here.
//
// ALLOWED_ORIGINS is a comma-separated list. Empty Origin (server-to-server
// requests, CLI tooling, health probes) is always permitted for WebSocket
// upgrades but does NOT imply authentication — callers must still resolve
// the user via session_token. In non-local environments the policy fails
// fast when the allow list is empty so a misconfigured deploy cannot
// accidentally accept every origin.
package origin

import (
	"fmt"
	"net/http"
	"strings"
)

// localDefaults is the loopback allow list used when APP_ENV=local and no
// explicit ALLOWED_ORIGINS is provided. These cover the in-repo defaults:
// backend on :1111, Vite dev server on :5173.
var localDefaults = []string{
	"http://localhost:1111",
	"http://localhost:5173",
	"http://127.0.0.1:1111",
	"http://127.0.0.1:5173",
}

// Policy classifies origins for both HTTP CORS and WebSocket upgrades.
type Policy struct {
	// Allowed is the parsed allow list (preserving order, no whitespace).
	Allowed []string
	// IsLocal is true when APP_ENV=local. Documented so logging can mark
	// dev-mode policy decisions explicitly.
	IsLocal bool
	// allowedSet is Allowed cached as a map for O(1) membership tests.
	allowedSet map[string]struct{}
}

// Parse builds a Policy from a process environment snapshot. The map shape
// mirrors what callers receive from os.Getenv-style lookups; tests pass
// literals directly.
func Parse(env map[string]string) (*Policy, error) {
	raw := strings.TrimSpace(env["ALLOWED_ORIGINS"])
	isLocal := strings.EqualFold(strings.TrimSpace(env["APP_ENV"]), "local")

	var allowed []string
	if raw != "" {
		for _, part := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			allowed = append(allowed, trimmed)
		}
	}
	if len(allowed) == 0 {
		if !isLocal {
			return nil, fmt.Errorf("ALLOWED_ORIGINS must be set when APP_ENV != \"local\"")
		}
		allowed = append([]string(nil), localDefaults...)
	}

	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[o] = struct{}{}
	}
	return &Policy{Allowed: allowed, IsLocal: isLocal, allowedSet: set}, nil
}

// IsBrowser reports whether the request carries an Origin header. Browser
// requests always set Origin; server-to-server requests, curl probes, and
// health checks do not.
func (p *Policy) IsBrowser(r *http.Request) bool {
	_, ok := r.Header["Origin"]
	return ok
}

// AllowOriginHeader returns the value to set on Access-Control-Allow-Origin
// for a request. Empty string means "do not set the header" — neither
// wildcard nor the requested origin is echoed. The caller is responsible
// for also setting Vary: Origin when this returns a non-empty value.
func (p *Policy) AllowOriginHeader(r *http.Request) string {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return ""
	}
	if _, ok := p.allowedSet[origin]; ok {
		return origin
	}
	return ""
}

// AllowWebSocket reports whether a WebSocket upgrade with this Origin may
// proceed. Empty Origin (non-browser) is always allowed so server-to-server
// tooling can connect.
func (p *Policy) AllowWebSocket(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	_, ok := p.allowedSet[origin]
	return ok
}
