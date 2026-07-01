package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// requirePostgresDSN returns a DSN suitable for integration tests.
//
// The DSN is read from the LMQ_TEST_DATABASE_URL env var when set, otherwise
// falls back to the local-dev default. If the database is not reachable the
// helper skips the test (not a failure) so the suite can run without a live
// PostgreSQL container — but the build and unit-level tests still pass.
//
// Each test that calls newPostgresDB runs inside its own throwaway schema
// so concurrent test invocations do not collide.
func requirePostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("LMQ_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("postgres unavailable (open): %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("postgres unavailable (ping): %v", err)
	}
	return dsn
}

// newPostgresDB opens a per-test *sql.DB whose search_path is set to a
// unique schema. The schema is dropped on test cleanup. Migrations are
// applied into the schema before the test body runs.
func newPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := requirePostgresDSN(t)

	schema := fmt.Sprintf("lmq_test_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping postgres: %v", err)
	}

	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}

	// Re-open with search_path scoped to the test schema so every query,
	// including golang-migrate's schema_migrations bookkeeping, is scoped.
	scoped, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		_ = db.Close()
		t.Fatalf("reopen with search_path: %v", err)
	}
	_ = db.Close()
	// Cap per-test pool so concurrent tests do not exhaust PG
	// `max_connections` (mirrors the limit applied in
	// room_test_db.NewRoomTestDB). Set high enough that the
	// golang-migrate driver can hold a dedicated `sql.Conn` plus run
	// concurrent Ping/Query calls without deadlocking under the
	// per-test pool.
	scoped.SetMaxOpenConns(8)
	scoped.SetMaxIdleConns(2)

	t.Cleanup(func() {
		drop, err := sql.Open("pgx", dsn)
		if err == nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
		_ = scoped.Close()
	})
	return scoped
}

// schemaMigratedUp runs the embedded migrations against the given *sql.DB
// (which must already have its search_path scoped to the target schema).
func schemaMigratedUp(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
}

// testPostgresMigrationsDir exposes the embedded dir name for tests that
// want to inspect the migration list without importing it twice.
func testPostgresMigrationsDir() string { return PostgresMigrationsDir }

// compile-time assertion: keep filepath import used so the file is not
// accidentally emptied.
var _ = filepath.Separator