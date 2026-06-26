package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	deliveryhttp "local-music-queue/internal/delivery/http"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestSetupApp_PostgresDBStaysOpen guards the DB lifecycle contract:
//
// The PostgreSQL *sql.DB opened by setupApp MUST remain open for the
// lifetime of the returned mux. A deferred Close inside setupApp would
// invalidate every repository handle before main starts ListenAndServe,
// causing every DB-backed request to fail with "sql: database is closed".
//
// The test sets DATABASE_URL to a per-test schema, calls setupApp, then
// POSTs an AddSong through the mux and asserts a 200 (not 500). If the
// lifecycle bug returns, the POST fails with "sql: database is closed"
// and the test fails.
//
// Skips cleanly when no PostgreSQL test DSN is available so the suite can
// run without a live container.
func TestSetupApp_PostgresDBStaysOpen(t *testing.T) {
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}
	probe, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("postgres unavailable (open): %v", err)
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := probe.PingContext(probeCtx); err != nil {
		probeCancel()
		_ = probe.Close()
		t.Skipf("postgres unavailable (ping): %v", err)
	}
	probeCancel()
	_ = probe.Close()

	// Isolate env so other tests cannot pollute DATABASE_URL / YTDLP_PATH.
	t.Setenv("APP_ENV", "test")
	for _, k := range []string{
		"DATABASE_URL",
		"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_USER",
		"POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_SSLMODE",
		"YTDLP_PATH", "PORT",
		"CLIENT_PIN", "HOST_PIN", "ADMIN_PIN",
		"GOOGLE_CLIENT_ID", "HOST_EMAILS", "ADMIN_EMAILS",
	} {
		os.Unsetenv(k)
	}
	t.Setenv("YTDLP_PATH", "/home/vi0l3tsc0rpi0n/linux-softwares/yt-dlp")

	// Create a unique schema and point DATABASE_URL at it. setupApp will
	// run the embedded migrations against this schema via RunEmbeddedMigrationsUp.
	schema := fmt.Sprintf("lmq_setup_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	})

	t.Setenv("DATABASE_URL", dsn+"&search_path="+schema)

	mux, _, _, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp failed: %v", err)
	}
	defer cleanup()
	if mux == nil {
		t.Fatal("expected non-nil mux")
	}

	// Exercise a DB-backed handler. With metadata supplied, AddSong skips
	// the yt-dlp fetch and goes straight to repo.Save. If setupApp closed
	// the DB before returning, this Save fails with "sql: database is closed"
	// and the handler responds 500.
	//
	// R05: the add route is now behind the auth middleware. We mint a
	// session for a guest user via the same path the unit tests use, then
	// inject the user into the request context because we are calling the
	// mux directly (no HTTP server, so the round-trip Google login is
	// not available).
	body, _ := json.Marshal(map[string]interface{}{
		"url":        "https://example.com/watch?v=test",
		"added_by":   "lifecycle-test",
		"added_by_id": 0,
		"metadata": &entity.SearchResult{
			ID:     "test-song-id",
			Title:  "Test Song",
			Artist: "Test Artist",
			URL:    "https://example.com/watch?v=test",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/add", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = deliveryhttp.WithUserForTest(req, &entity.User{ID: 1, Role: entity.RoleHost, DisplayName: "lifecycle-test"})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/queue/add after setupApp; got %d body=%s",
			rr.Code, rr.Body.String())
	}
}
