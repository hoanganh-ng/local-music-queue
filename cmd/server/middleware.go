package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"local-music-queue/internal/delivery/origin"
)

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.written {
		return
	}
	r.statusCode = code
	r.written = true
	r.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker for WebSocket support.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// requestLogger is a middleware that logs [timestamp - endpoint - handle_time] for every request.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		ts := start.Format("2006-01-02 15:04:05")
		log.Printf("[%s] %-4s %s → %d (%s)", ts, r.Method, r.URL.Path, rec.statusCode, formatDuration(duration))
	})
}

// formatDuration rounds duration to the nearest millisecond for clean log output.
func formatDuration(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1 {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", ms)
}

// enableCORS applies the shared origin policy to every response. Allowed
// browser origins are echoed back with Vary: Origin so caches do not
// conflate them. Disallowed browser origins on preflight (OPTIONS) are
// rejected with 403 — non-preflight requests from disallowed origins fall
// through and the request continues, matching the production CORS model
// where the browser is responsible for blocking responses. Empty Origin
// (server-to-server, curl, health probes) is allowed and emits no
// Access-Control-Allow-Origin header.
func enableCORS(p *origin.Policy, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if originHeader := p.AllowOriginHeader(r); originHeader != "" {
			w.Header().Set("Access-Control-Allow-Origin", originHeader)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Add("Vary", "Origin")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
		}

		if r.Method == http.MethodOptions {
			if p.IsBrowser(r) && p.AllowOriginHeader(r) == "" {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
