package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/infrastructure/persistence"
)

func setupPostgresForTest(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("postgres unavailable (open): %v", err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := root.PingContext(ctx); err != nil {
		t.Skipf("postgres unavailable (ping): %v", err)
	}
	schema := fmt.Sprintf("lmq_setup_test_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	scoped, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer scoped.Close()
	if err := persistence.RunEmbeddedMigrationsUp(scoped); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	t.Cleanup(func() {
		drop, _ := sql.Open("pgx", dsn)
		if drop != nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
	})
	return dsn + "&search_path=" + schema
}

func TestSetupApp(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)

	// Configure for PostgreSQL path. YTDLP_PATH must point at an executable
	// for cfg.Validate() to pass; fall back to /bin/true if unset.
	if os.Getenv("YTDLP_PATH") == "" {
		os.Setenv("YTDLP_PATH", "/bin/true")
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	defer func() {
		os.Unsetenv("DATABASE_URL")
	}()

	mux, cfg, _, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp failed: %v", err)
	}
	defer cleanup()

	if mux == nil {
		t.Fatal("Expected mux to be non-nil")
	}
	if cfg.DatabaseURL == "" {
		t.Error("Expected DatabaseURL to be set")
	}
}

// TestSetupApp_RegistersRoomWSRoute pins the R07b wiring contract:
// /ws/rooms/{slug} is registered on the same mux. A plain GET without
// the required query params returns 401 from the handler before the
// upgrade is attempted, which proves the route is wired.
func TestSetupApp_RegistersRoomWSRoute(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	if os.Getenv("YTDLP_PATH") == "" {
		os.Setenv("YTDLP_PATH", "/bin/true")
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	defer os.Unsetenv("DATABASE_URL")

	mux, _, _, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/ws/rooms/anything", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected /ws/rooms/{slug} to be registered (401 without session token), got %d", rr.Code)
	}
}