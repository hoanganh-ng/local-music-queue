package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/infrastructure/persistence/migratedata"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// ---- postgres test helpers (mirrors internal/infrastructure/persistence/testutil_postgres_test.go) ----

func requireDSN(t *testing.T) string {
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

func newPostgresDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := requireDSN(t)
	schema := fmt.Sprintf("lmq_migrate_data_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = root.Close()
		t.Fatalf("create schema: %v", err)
	}
	scopedDSN := dsn + "&search_path=" + schema
	scoped, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		_ = root.Close()
		t.Fatalf("reopen with search_path: %v", err)
	}
	_ = root.Close()
	t.Cleanup(func() {
		drop, err := sql.Open("pgx", dsn)
		if err == nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
		_ = scoped.Close()
	})
	return scoped, scopedDSN
}

func schemaMigratedUp(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := persistence.RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("RunEmbeddedMigrationsUp: %v", err)
	}
}

// ---- sqlite fixture helpers ----

const sqliteDDL = `
CREATE TABLE IF NOT EXISTS queue_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    data TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL,
    type TEXT NOT NULL,
    user TEXT NOT NULL,
    description TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    profile_picture TEXT,
    role TEXT NOT NULL,
    priority_balance INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS user_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    session_date DATE NOT NULL,
    first_seen_at DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    UNIQUE(user_id, session_date)
);
CREATE TABLE IF NOT EXISTS priority_transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    song_id TEXT NOT NULL,
    song_title TEXT NOT NULL,
    transaction_type TEXT NOT NULL,
    amount INTEGER NOT NULL,
    balance_after INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS auto_queue_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled INTEGER NOT NULL DEFAULT 0,
    strategy TEXT NOT NULL DEFAULT 'related'
);
INSERT OR IGNORE INTO auto_queue_config (id, enabled, strategy) VALUES (1, 0, 'related');
CREATE TABLE IF NOT EXISTS play_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id TEXT NOT NULL,
    title TEXT NOT NULL,
    played_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

type seedIDs struct {
	userLegacyIDs []int64
	queueData     []byte
}

func seedSQLite(t *testing.T) (path string, ids seedIDs) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "fixture.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(sqliteDDL); err != nil {
		t.Fatalf("sqlite ddl: %v", err)
	}

	// Users
	users := []struct {
		email    string
		display  string
		role     string
		priority int
	}{
		{"alice@example.com", "Alice", "host", 5},
		{"bob@example.com", "Bob", "guest", 3},
		{"carol@example.com", "Carol", "guest", 0},
	}
	for _, u := range users {
		if _, err := db.Exec(`
			INSERT INTO users (email, display_name, role, priority_balance)
			VALUES (?, ?, ?, ?)
		`, u.email, u.display, u.role, u.priority); err != nil {
			t.Fatalf("insert user %s: %v", u.email, err)
		}
	}
	rows, err := db.Query(`SELECT id FROM users ORDER BY id`)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan user id: %v", err)
		}
		ids.userLegacyIDs = append(ids.userLegacyIDs, id)
	}
	rows.Close()

	// user_sessions
	for i, uid := range ids.userLegacyIDs {
		day := fmt.Sprintf("2026-01-%02d", 5+i)
		if _, err := db.Exec(`
			INSERT INTO user_sessions (user_id, session_date, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, ?)
		`, uid, day, "2026-01-05 09:00:00", "2026-01-05 17:00:00"); err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}

	// priority_transactions
	for i, uid := range ids.userLegacyIDs {
		for j := 0; j < 2; j++ {
			if _, err := db.Exec(`
				INSERT INTO priority_transactions (user_id, song_id, song_title, transaction_type, amount, balance_after)
				VALUES (?, ?, ?, ?, ?, ?)
			`, uid, fmt.Sprintf("video-%d-%d", i, j), fmt.Sprintf("Song %d-%d", i, j), "spend", -1, 5-int64(j)); err != nil {
				t.Fatalf("insert priority tx: %v", err)
			}
		}
	}

	// queue_state — use non-trivial JSON with unicode + escaping to exercise byte preservation
	ids.queueData = []byte(`{"songs":[{"id":"v-1","title":"Hello世界","duration":180}],"currentIndex":0,"status":"playing","elapsed":42}`)
	if _, err := db.Exec(`INSERT INTO queue_state (id, data, updated_at) VALUES (1, ?, '2026-01-05 09:00:00')`, string(ids.queueData)); err != nil {
		t.Fatalf("insert queue_state: %v", err)
	}

	// activities
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(`
			INSERT INTO activities ("timestamp", type, "user", description)
			VALUES (?, ?, ?, ?)
		`, fmt.Sprintf("2026-01-05 09:%02d:00", i), "song_added", "Alice", fmt.Sprintf("Added song %d", i)); err != nil {
			t.Fatalf("insert activity: %v", err)
		}
	}

	// play_history
	for i := 0; i < 7; i++ {
		if _, err := db.Exec(`
			INSERT INTO play_history (video_id, title, played_at)
			VALUES (?, ?, ?)
		`, fmt.Sprintf("video-%d", i), fmt.Sprintf("Title %d", i), fmt.Sprintf("2026-01-05 09:%02d:00", i)); err != nil {
			t.Fatalf("insert play_history: %v", err)
		}
	}

	return path, ids
}

func mustCount(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()
	var c int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&c); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return c
}

func mustInt64(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return v
}

// ---- tests ----

func TestMigrateData_HappyPath(t *testing.T) {
	path, ids := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.AllTablesMatch() {
		t.Errorf("counts mismatch:\n%+v", r.Tables)
	}
	if r.QueueState.Source != int64(len(ids.queueData)) {
		t.Errorf("source queue_state bytes = %d, want %d", r.QueueState.Source, len(ids.queueData))
	}
	if r.QueueState.Source != r.QueueState.Target {
		t.Errorf("queue_state byte mismatch: source=%d target=%d", r.QueueState.Source, r.QueueState.Target)
	}

	// legacy_id populated for every user.
	var nullLegacy int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE legacy_id IS NULL`).Scan(&nullLegacy); err != nil {
		t.Fatalf("count null legacy_id: %v", err)
	}
	if nullLegacy != 0 {
		t.Errorf("expected 0 null legacy_id rows; got %d", nullLegacy)
	}

	// FK remap resolves for every user_session and priority_transaction.
	var orphanSessions, orphanTx int64
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM user_sessions us
		WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = us.user_id)
	`).Scan(&orphanSessions); err != nil {
		t.Fatalf("orphan sessions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM priority_transactions pt
		WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = pt.user_id)
	`).Scan(&orphanTx); err != nil {
		t.Fatalf("orphan priority_transactions: %v", err)
	}
	if orphanSessions != 0 {
		t.Errorf("orphan user_sessions rows: %d", orphanSessions)
	}
	if orphanTx != 0 {
		t.Errorf("orphan priority_transactions rows: %d", orphanTx)
	}

	// Counts per table.
	expected := map[string]int64{
		"queue_state":          1,
		"activities":           5,
		"users":                3,
		"user_sessions":        3,
		"priority_transactions": 6,
		"auto_queue_config":    1,
		"play_history":         7,
	}
	for table, want := range expected {
		if got := mustCount(t, db, table); got != want {
			t.Errorf("target %s count = %d, want %d", table, got, want)
		}
	}

	// auto_queue_config.enabled boolean round-trip.
	var enabled bool
	var strategy string
	if err := db.QueryRow(`SELECT enabled, strategy FROM auto_queue_config WHERE id = 1`).Scan(&enabled, &strategy); err != nil {
		t.Fatalf("read auto_queue_config: %v", err)
	}
	if enabled != false {
		t.Errorf("expected enabled=false (fixture had 0)")
	}
	if strategy != "related" {
		t.Errorf("strategy = %q, want \"related\"", strategy)
	}
}

func TestMigrateData_Idempotent(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !r1.AllTablesMatch() {
		t.Fatalf("first run mismatch: %+v", r1.Tables)
	}

	r2, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(r2.Notes) == 0 || r2.Notes[0] != "already migrated; no-op" {
		t.Errorf("second run Notes: %v", r2.Notes)
	}
	// Counts unchanged.
	expected := map[string]int64{
		"queue_state":          1,
		"activities":           5,
		"users":                3,
		"user_sessions":        3,
		"priority_transactions": 6,
		"auto_queue_config":    1,
		"play_history":         7,
	}
	for table, want := range expected {
		if got := mustCount(t, db, table); got != want {
			t.Errorf("idempotent re-run: %s count = %d, want %d", table, got, want)
		}
	}
}

func TestMigrateData_RollbackMidPipeline(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{
		SQLitePath:      path,
		PGDSN:           scopedDSN,
		FaultAfterUsers: func() error { return errors.New("injected fault after users copy") },
	}
	_, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected fault injection error, got nil")
	}
	if got := mustCount(t, db, "users"); got != 0 {
		t.Errorf("after rollback: users count = %d, want 0", got)
	}
	if got := mustCount(t, db, "user_sessions"); got != 0 {
		t.Errorf("after rollback: user_sessions count = %d, want 0", got)
	}
	if got := mustCount(t, db, "priority_transactions"); got != 0 {
		t.Errorf("after rollback: priority_transactions count = %d, want 0", got)
	}
	if got := mustCount(t, db, "queue_state"); got != 0 {
		t.Errorf("after rollback: queue_state count = %d, want 0", got)
	}
	if got := mustCount(t, db, "activities"); got != 0 {
		t.Errorf("after rollback: activities count = %d, want 0", got)
	}
	if got := mustCount(t, db, "play_history"); got != 0 {
		t.Errorf("after rollback: play_history count = %d, want 0", got)
	}
}

func TestMigrateData_AdvisoryLockConflict(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	// Hold the advisory lock on a separate connection.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(context.Background(), "SELECT pg_try_advisory_lock($1)", int64(987654321)).Scan(&locked); err != nil {
		t.Fatalf("try lock: %v", err)
	}
	if !locked {
		t.Skip("could not acquire advisory lock to simulate contention")
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", int64(987654321))
	}()

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	_, err = migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected advisory lock conflict error, got nil")
	}
	if got := err.Error(); !contains(got, "another migrate-data") {
		t.Errorf("unexpected error: %q", got)
	}
}

func TestMigrateData_ExistingRowsNoLegacyID(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	// Insert a row without legacy_id to simulate a pre-existing R02 row.
	if _, err := db.Exec(`
		INSERT INTO users (email, display_name, role, priority_balance)
		VALUES ('preexisting@example.com', 'PreExisting', 'guest', 0)
	`); err != nil {
		t.Fatalf("insert preexisting user: %v", err)
	}

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	_, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if got := err.Error(); !contains(got, "manual cleanup required") {
		t.Errorf("unexpected error: %q", got)
	}
}

func TestMigrateData_NoTablesInSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = db.Exec("CREATE TABLE dummy (x INTEGER)")
	_ = db.Close()
	pgdb, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, pgdb)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	_, err = migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected missing-tables error, got nil")
	}
	if got := err.Error(); !contains(got, "missing expected table") {
		t.Errorf("unexpected error: %q", got)
	}
}

func TestMigrateData_SchemaVersionTooOld(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	// Apply the full migration set (0001 + 0002) but then force the
	// schema_migrations row back to version 1 so the migrator sees a
	// "too-old" target even though 0002's columns are present.
	if err := persistence.RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("RunEmbeddedMigrationsUp: %v", err)
	}
	if err := persistence.RunEmbeddedMigrationsForce(db, 1); err != nil {
		t.Fatalf("RunEmbeddedMigrationsForce: %v", err)
	}

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	_, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected schema-version error, got nil")
	}
	if got := err.Error(); !contains(got, "migrate-schema up") {
		t.Errorf("unexpected error: %q", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// kept to ensure imports are used
var _ = mustInt64