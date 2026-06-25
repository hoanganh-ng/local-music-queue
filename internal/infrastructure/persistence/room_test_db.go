package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// NewRoomTestDB returns a per-test *sql.DB scoped to a throwaway PG schema
// with migrations applied. Used by the room usecase tests; exported here
// to avoid cyclic test packages. Skips when PG is unreachable.
func NewRoomTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}

	// Probe for reachability first; skip (not fail) when the DB is absent.
	probe, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("postgres unavailable (open): %v", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	pingErr := probe.PingContext(pingCtx)
	cancel()
	_ = probe.Close()
	if pingErr != nil {
		t.Skipf("postgres unavailable (ping): %v", pingErr)
	}

	schema := fmt.Sprintf("lmq_room_test_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())

	// Open with search_path scoped to the test schema so every query —
	// including golang-migrate's schema_migrations bookkeeping — is scoped.
	scoped, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	openCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := scoped.PingContext(openCtx); err != nil {
		_ = scoped.Close()
		t.Fatalf("ping postgres: %v", err)
	}

	if _, err := scoped.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = scoped.Close()
		t.Fatalf("create schema: %v", err)
	}

	if err := RunEmbeddedMigrationsUp(scoped); err != nil {
		_ = scoped.Close()
		t.Fatalf("migrate up: %v", err)
	}

	t.Cleanup(func() {
		drop, err := sql.Open("pgx", dsn)
		if err == nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
		_ = scoped.Close()
	})
	return scoped, func() { _ = scoped.Close() }
}
