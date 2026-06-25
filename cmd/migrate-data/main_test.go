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

// seedEmptySQLite creates a SQLite source whose seven tables are present but
// empty (the auto_queue_config seed row from sqliteDDL is the only row).
// Migrations of an empty source must succeed: the migrator must not require
// every table to be non-empty.
func seedEmptySQLite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(sqliteDDL); err != nil {
		t.Fatalf("sqlite ddl: %v", err)
	}
	return path
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
	// Apply the full migration set (0001 + 0002 + 0003) but then force the
	// schema_migrations row back to version 1 so the migrator sees a
	// "too-old" target even though 0002/0003's columns and tables are present.
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

// ---- R03 fix-pass regression tests ----

// TestMigrateData_DirtyTarget_SameCounts_DifferentRows_Fails covers the case
// where the target users table has the same row count as the source but the
// rows are completely different (no legacy_id correlation, no marker row).
// The migration must refuse to merge and must NOT treat it as a no-op.
func TestMigrateData_DirtyTarget_SameCounts_DifferentRows_Fails(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	// Insert 3 unrelated user rows with NULL legacy_id; same count as the
	// source fixture. No migration_marker row.
	if _, err := db.Exec(`
		INSERT INTO users (email, display_name, role, priority_balance) VALUES
		  ('unrelated1@example.com', 'Unrelated1', 'guest', 0),
		  ('unrelated2@example.com', 'Unrelated2', 'guest', 0),
		  ('unrelated3@example.com', 'Unrelated3', 'guest', 0)
	`); err != nil {
		t.Fatalf("seed unrelated users: %v", err)
	}

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	_, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected dirty-target error, got nil")
	}
	// Must surface a NULL-legacy_id conflict OR a no-marker dirty error.
	got := err.Error()
	if !contains(got, "manual cleanup required") && !contains(got, "unknown provenance") {
		t.Errorf("expected NULL-legacy_id or no-marker error, got: %q", got)
	}
	// Target must not have been modified by the failed run.
	if got := mustCount(t, db, "users"); got != 3 {
		t.Errorf("users count after failed migration = %d, want 3 (untouched)", got)
	}
	var markerCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM migration_marker`).Scan(&markerCount); err != nil {
		t.Fatalf("count marker: %v", err)
	}
	if markerCount != 0 {
		t.Errorf("migration_marker should not exist after failed run; got count=%d", markerCount)
	}
}

// TestMigrateData_DirtyTarget_NonNullLegacyID_PartialDependents covers the
// "rows present, no marker, legacy_id is set but other tables are empty"
// branch: a partial previous copy. Must fail; must not be a no-op.
func TestMigrateData_DirtyTarget_NonNullLegacyID_PartialDependents(t *testing.T) {
	path, ids := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	// Seed users with non-null legacy_id matching the source IDs, but leave
	// every other table empty. No migration_marker.
	for i, uid := range ids.userLegacyIDs {
		if _, err := db.Exec(`
			INSERT INTO users (email, display_name, role, priority_balance, legacy_id)
			VALUES ($1, $2, 'guest', 0, $3)
		`, fmt.Sprintf("partial%d@example.com", i), fmt.Sprintf("Partial%d", i), uid); err != nil {
			t.Fatalf("seed partial user %d: %v", uid, err)
		}
	}

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	_, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected dirty-target error, got nil")
	}
	if got := err.Error(); !contains(got, "unknown provenance") {
		t.Errorf("expected 'unknown provenance' error, got: %q", got)
	}
	// user_sessions must remain empty (no merge).
	if got := mustCount(t, db, "user_sessions"); got != 0 {
		t.Errorf("user_sessions should still be 0 after failed merge; got %d", got)
	}
}

// TestMigrateData_OrphanUserSession_FailsAndRollsBack covers the FK orphan
// case: source has a user_sessions row referencing a user_id that does not
// exist in users. The migration must return an error AND roll back so all 7
// tables are empty on the target.
func TestMigrateData_OrphanUserSession_FailsAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "orphan-sessions.sqlite")
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(sqliteDDL); err != nil {
		t.Fatalf("sqlite ddl: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO users (email, display_name, role, priority_balance)
		VALUES ('alice@example.com', 'Alice', 'host', 5)
	`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// Insert an orphan user_sessions row referencing user_id=999 which
	// does not exist.
	if _, err := db.Exec(`
		INSERT INTO user_sessions (user_id, session_date, first_seen_at, last_seen_at)
		VALUES (999, '2026-01-05', '2026-01-05 09:00:00', '2026-01-05 17:00:00')
	`); err != nil {
		t.Fatalf("insert orphan session: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	pg, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, pg)

	opts := migratedata.Options{SQLitePath: srcPath, PGDSN: scopedDSN}
	_, err = migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected FK orphan error, got nil")
	}
	if got := err.Error(); !contains(got, "user_sessions row id=") || !contains(got, "missing source user_id=999") {
		t.Errorf("unexpected error: %q", got)
	}
	// All data-bearing tables must be empty after rollback. The
	// auto_queue_config row from migration 0001 (id=1, enabled=false) is
	// expected to remain because it is seeded by the schema, not the data
	// copy.
	for _, table := range []string{"users", "user_sessions", "priority_transactions", "queue_state", "activities", "play_history"} {
		if got := mustCount(t, pg, table); got != 0 {
			t.Errorf("after orphan rollback: %s count = %d, want 0", table, got)
		}
	}
}

// TestMigrateData_OrphanPriorityTransaction_FailsAndRollsBack is the same
// scenario for priority_transactions.
func TestMigrateData_OrphanPriorityTransaction_FailsAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "orphan-ptx.sqlite")
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(sqliteDDL); err != nil {
		t.Fatalf("sqlite ddl: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO users (email, display_name, role, priority_balance)
		VALUES ('alice@example.com', 'Alice', 'host', 5)
	`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO priority_transactions (user_id, song_id, song_title, transaction_type, amount, balance_after)
		VALUES (999, 'video-x', 'Orphan', 'spend', -1, 0)
	`); err != nil {
		t.Fatalf("insert orphan pt: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	pg, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, pg)

	opts := migratedata.Options{SQLitePath: srcPath, PGDSN: scopedDSN}
	_, err = migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected FK orphan error, got nil")
	}
	if got := err.Error(); !contains(got, "priority_transactions row id=") || !contains(got, "missing source user_id=999") {
		t.Errorf("unexpected error: %q", got)
	}
	for _, table := range []string{"users", "user_sessions", "priority_transactions", "queue_state", "activities", "play_history"} {
		if got := mustCount(t, pg, table); got != 0 {
			t.Errorf("after orphan rollback: %s count = %d, want 0", table, got)
		}
	}
}

// TestMigrateData_ForcedVerificationMismatch_RollsBack uses the new
// FaultAfterVerification hook to prove the verification runs BEFORE commit:
// a forced error after verification must roll back every inserted row.
func TestMigrateData_ForcedVerificationMismatch_RollsBack(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{
		SQLitePath:            path,
		PGDSN:                 scopedDSN,
		FaultAfterVerification: func() error { return errors.New("injected fault after verification") },
	}
	_, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected fault-after-verification error, got nil")
	}
	if got := err.Error(); !contains(got, "fault injection") {
		t.Errorf("unexpected error: %q", got)
	}
	// Rollback must leave every target table empty. auto_queue_config's seed
	// row from migration 0001 is allowed to remain.
	for _, table := range []string{"users", "user_sessions", "priority_transactions", "queue_state", "activities", "play_history"} {
		if got := mustCount(t, db, table); got != 0 {
			t.Errorf("after post-verify fault rollback: %s count = %d, want 0", table, got)
		}
	}
	// No marker row should exist.
	var markerCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM migration_marker`).Scan(&markerCount); err != nil {
		t.Fatalf("count marker: %v", err)
	}
	if markerCount != 0 {
		t.Errorf("migration_marker should not exist after post-verify rollback; got count=%d", markerCount)
	}
}

// TestMigrateData_SecondRunNoOpOnlyAfterExactPriorSuccess proves idempotency
// requires an exact prior migration marker: any drift in the marker hash
// triggers a dirty-target error instead of a silent no-op.
func TestMigrateData_SecondRunNoOpOnlyAfterExactPriorSuccess(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !r1.MigrationVerified {
		t.Error("expected first run to set MigrationVerified=true")
	}

	// Tamper with queue_state.data on the target: append a single byte.
	// This breaks the queue_state SHA256 without changing row counts.
	if _, err := db.Exec(`
		UPDATE queue_state
		SET data = data || ' '
		WHERE id = 1
	`); err != nil {
		t.Fatalf("tamper queue_state: %v", err)
	}

	r2, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected dirty-target error after tampering, got nil")
	}
	if got := err.Error(); !contains(got, "queue_state") {
		t.Errorf("expected queue_state-related dirty error, got: %q", got)
	}
	_ = r2 // error path; report is nil

	// Restore the byte: now the second run should succeed as a no-op.
	if _, err := db.Exec(`
		UPDATE queue_state
		SET data = substring(data from 1 for length(data) - 1)
		WHERE id = 1
	`); err != nil {
		t.Fatalf("restore queue_state: %v", err)
	}
	r3, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("third Run (restore + re-run): %v", err)
	}
	if len(r3.Notes) == 0 || r3.Notes[0] != "already migrated; no-op" {
		t.Errorf("third run Notes: %v", r3.Notes)
	}
}

// TestMigrateData_IntegrationCoverage exercises the full data path including
// the durable marker row and the post-copy verification.
func TestMigrateData_IntegrationCoverage(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.MigrationVerified {
		t.Error("expected MigrationVerified=true")
	}
	if r.SourcePath == "" {
		t.Error("expected SourcePath to be populated")
	}
	if r.RemappedUserCount != 3 {
		t.Errorf("RemappedUserCount = %d, want 3", r.RemappedUserCount)
	}

	// Marker row must exist on target.
	var markerCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM migration_marker`).Scan(&markerCount); err != nil {
		t.Fatalf("count marker: %v", err)
	}
	if markerCount != 1 {
		t.Errorf("migration_marker row count = %d, want 1", markerCount)
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

// ---- R03 dirty-detection and empty-source tests ----

// TestMigrateData_EmptySQLiteSource_MigratesSuccessfully proves an empty
// source (all seven tables present, six of them empty, auto_queue_config
// holding only the schema seed) migrates to a verified target with a marker
// row written.
func TestMigrateData_EmptySQLiteSource_MigratesSuccessfully(t *testing.T) {
	path := seedEmptySQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run (empty source): %v", err)
	}
	if !r.MigrationVerified {
		t.Errorf("expected MigrationVerified=true; got false")
	}
	if r.RemappedUserCount != 0 {
		t.Errorf("RemappedUserCount = %d, want 0", r.RemappedUserCount)
	}

	// auto_queue_config schema seed must survive; every other table must be
	// empty.
	expected := map[string]int64{
		"queue_state":          0,
		"activities":           0,
		"users":                0,
		"user_sessions":        0,
		"priority_transactions": 0,
		"auto_queue_config":    1,
		"play_history":         0,
	}
	for table, want := range expected {
		if got := mustCount(t, db, table); got != want {
			t.Errorf("target %s count = %d, want %d", table, got, want)
		}
	}

	// Marker row exists.
	var markerCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM migration_marker`).Scan(&markerCount); err != nil {
		t.Fatalf("count marker: %v", err)
	}
	if markerCount != 1 {
		t.Errorf("migration_marker row count = %d, want 1", markerCount)
	}
}

// TestMigrateData_EmptySQLiteSource_SecondRunNoOp proves the marker-based
// no-op path also works for empty sources.
func TestMigrateData_EmptySQLiteSource_SecondRunNoOp(t *testing.T) {
	path := seedEmptySQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !r1.MigrationVerified {
		t.Fatalf("first run: MigrationVerified=false")
	}

	r2, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(r2.Notes) == 0 || r2.Notes[0] != "already migrated; no-op" {
		t.Errorf("second run Notes: %v", r2.Notes)
	}
}

// TestMigrateData_TamperedUserEmail_DirtyDetection proves the second-run
// no-op gate checks live target content. The first run writes a row; we
// UPDATE the email column in place; the second run must classify the target
// as dirty (target content drifted from source).
func TestMigrateData_TamperedUserEmail_DirtyDetection(t *testing.T) {
	path, ids := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !r1.MigrationVerified {
		t.Fatal("expected first run MigrationVerified=true")
	}

	// Tamper with one user's email on the target. Row count is unchanged.
	targetID := mustInt64(t, db, `SELECT id FROM users WHERE legacy_id = $1`, ids.userLegacyIDs[0])
	if _, err := db.Exec(`UPDATE users SET email = 'tampered@example.com' WHERE id = $1`, targetID); err != nil {
		t.Fatalf("tamper users: %v", err)
	}

	r2, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected dirty error after tampering users.email, got nil")
	}
	got := err.Error()
	if !contains(got, "users") || !contains(got, "drifted") {
		t.Errorf("expected users dirty-drift error, got: %q", got)
	}
	_ = r2 // error path; report is nil
}

// TestMigrateData_TamperedActivityDescription_DirtyDetection mirrors the user
// tamper test for the activities table.
func TestMigrateData_TamperedActivityDescription_DirtyDetection(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !r1.MigrationVerified {
		t.Fatal("expected first run MigrationVerified=true")
	}

	// Tamper with one activity description. Row count is unchanged.
	if _, err := db.Exec(`UPDATE activities SET description = 'tampered' WHERE id = (SELECT MIN(id) FROM activities)`); err != nil {
		t.Fatalf("tamper activities: %v", err)
	}

	r2, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected dirty error after tampering activities.description, got nil")
	}
	got := err.Error()
	if !contains(got, "activities") || !contains(got, "drifted") {
		t.Errorf("expected activities dirty-drift error, got: %q", got)
	}
	_ = r2 // error path; report is nil
}

// TestMigrateData_TamperedQueueStateSameLength_DirtyDetection proves a
// same-length queue_state.data mutation is detected as dirty. The existing
// "append a byte" tamper test would be caught by the queue_state byte-length
// check; this test exercises the SHA256 projection itself by mutating
// content while preserving the byte count.
func TestMigrateData_TamperedQueueStateSameLength_DirtyDetection(t *testing.T) {
	path, ids := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if r1.QueueState.Source != int64(len(ids.queueData)) {
		t.Fatalf("source queue_state bytes = %d, want %d", r1.QueueState.Source, len(ids.queueData))
	}

	// Replace queue_state.data with a same-length JSON-shaped payload of
	// spaces. The byte length matches the original, defeating the
	// queue_state byte-length gate; only the SHA256 projection will catch
	// the drift.
	original := ids.queueData
	replacement := make([]byte, len(original))
	for i := range replacement {
		replacement[i] = ' '
	}
	if _, err := db.Exec(`UPDATE queue_state SET data = $1 WHERE id = 1`, replacement); err != nil {
		t.Fatalf("tamper queue_state: %v", err)
	}

	r2, err := migratedata.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected dirty error after same-length queue_state tamper, got nil")
	}
	got := err.Error()
	if !contains(got, "queue_state") || !contains(got, "drifted") {
		t.Errorf("expected queue_state dirty-drift error, got: %q", got)
	}
	_ = r2 // error path; report is nil

	// Restore the original byte length AND content so the next assertion can
	// confirm a clean source matches a clean target on re-run.
	if _, err := db.Exec(`UPDATE queue_state SET data = $1 WHERE id = 1`, original); err != nil {
		t.Fatalf("restore queue_state: %v", err)
	}
	r3, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("restore+rerun: %v", err)
	}
	if len(r3.Notes) == 0 || r3.Notes[0] != "already migrated; no-op" {
		t.Errorf("restored run Notes: %v", r3.Notes)
	}
}

// kept to ensure imports are used
var _ = mustInt64

// ---- R03 idempotency-proof fix tests ----

// seedNonContiguousSQLite writes a SQLite source whose users,
// user_sessions, priority_transactions, activities, and play_history all
// have non-contiguous ids (gaps in the integer range). The migrator must
// preserve these ids on the target, and a second run must be a no-op
// despite the gaps.
func seedNonContiguousSQLite(t *testing.T) (path string, ids seedIDs) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "gaps.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(sqliteDDL); err != nil {
		t.Fatalf("sqlite ddl: %v", err)
	}

	// Insert users with explicit non-contiguous ids: 1, 50, 5000.
	for i, rawID := range []int64{1, 50, 5000} {
		if _, err := db.Exec(`
			INSERT INTO users (id, email, display_name, role, priority_balance)
			VALUES (?, ?, ?, ?, ?)
		`, rawID, fmt.Sprintf("user%d@example.com", i), fmt.Sprintf("User%d", i), "guest", 0); err != nil {
			t.Fatalf("insert user id=%d: %v", rawID, err)
		}
		ids.userLegacyIDs = append(ids.userLegacyIDs, rawID)
	}

	// user_sessions with non-contiguous ids referencing legacy users.
	sessions := []struct {
		id     int64
		userID int64
		day    string
	}{
		{7, 1, "2026-01-05"},
		{42, 50, "2026-01-06"},
		{999, 5000, "2026-01-07"},
	}
	for _, s := range sessions {
		if _, err := db.Exec(`
			INSERT INTO user_sessions (id, user_id, session_date, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?)
		`, s.id, s.userID, s.day, "2026-01-05 09:00:00", "2026-01-05 17:00:00"); err != nil {
			t.Fatalf("insert session id=%d: %v", s.id, err)
		}
	}

	// priority_transactions with non-contiguous ids.
	txs := []struct {
		id     int64
		userID int64
		songID string
		title  string
	}{
		{13, 1, "v-1", "Song 1"},
		{100, 50, "v-2", "Song 2"},
		{7777, 5000, "v-3", "Song 3"},
	}
	for _, tx := range txs {
		if _, err := db.Exec(`
			INSERT INTO priority_transactions (id, user_id, song_id, song_title, transaction_type, amount, balance_after)
			VALUES (?, ?, ?, ?, 'spend', -1, 0)
		`, tx.id, tx.userID, tx.songID, tx.title); err != nil {
			t.Fatalf("insert ptx id=%d: %v", tx.id, err)
		}
	}

	// queue_state: small fixed payload.
	ids.queueData = []byte(`{"x":1}`)
	if _, err := db.Exec(`INSERT INTO queue_state (id, data, updated_at) VALUES (1, ?, '2026-01-05 09:00:00')`, string(ids.queueData)); err != nil {
		t.Fatalf("insert queue_state: %v", err)
	}

	// activities with non-contiguous ids.
	for _, aid := range []int64{3, 11, 222} {
		if _, err := db.Exec(`
			INSERT INTO activities (id, "timestamp", type, "user", description)
			VALUES (?, ?, ?, ?, ?)
		`, aid, "2026-01-05 09:00:00", "song_added", "User0", fmt.Sprintf("act-%d", aid)); err != nil {
			t.Fatalf("insert activity id=%d: %v", aid, err)
		}
	}

	// play_history with non-contiguous ids.
	for _, pid := range []int64{2, 88, 4444} {
		if _, err := db.Exec(`
			INSERT INTO play_history (id, video_id, title, played_at)
			VALUES (?, ?, ?, ?)
		`, pid, fmt.Sprintf("video-%d", pid), fmt.Sprintf("Title %d", pid), "2026-01-05 09:00:00"); err != nil {
			t.Fatalf("insert play_history id=%d: %v", pid, err)
		}
	}

	return path, ids
}

// TestMigrateData_NonContiguousIDs_MigratesAndNoOps verifies the
// canonical-target-projection fix: a source with gaps in the
// id sequences for users, user_sessions, priority_transactions,
// activities, and play_history migrates successfully AND a second run
// against the same target is a clean no-op. The fix replaced the
// PostgreSQL-generated-id comparison with a users.legacy_id
// comparison, so gaps no longer break idempotency.
func TestMigrateData_NonContiguousIDs_MigratesAndNoOps(t *testing.T) {
	path, ids := seedNonContiguousSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !r1.AllTablesMatch() {
		t.Fatalf("first run: %+v", r1.Tables)
	}

	// Verify the target preserved the explicit source ids on the four
	// id-preserved tables.
	expectedRows := map[string]struct {
		count     int64
		idsOnTgt  []int64
	}{
		"user_sessions":        {3, []int64{7, 42, 999}},
		"priority_transactions": {3, []int64{13, 100, 7777}},
		"activities":           {3, []int64{3, 11, 222}},
		"play_history":         {3, []int64{2, 88, 4444}},
	}
	for table, want := range expectedRows {
		if got := mustCount(t, db, table); got != want.count {
			t.Errorf("%s count = %d, want %d", table, got, want.count)
		}
		for _, id := range want.idsOnTgt {
			var c int64
			if err := db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE id = $1", id).Scan(&c); err != nil {
				t.Fatalf("query %s id=%d: %v", table, id, err)
			}
			if c != 1 {
				t.Errorf("%s id=%d not preserved on target (count=%d)", table, id, c)
			}
		}
	}

	// Second run is a clean no-op despite the gaps.
	r2, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(r2.Notes) == 0 || r2.Notes[0] != "already migrated; no-op" {
		t.Errorf("second run Notes: %v", r2.Notes)
	}

	// users.legacy_id holds the source ids (1, 50, 5000).
	for _, legacyID := range ids.userLegacyIDs {
		var c int64
		if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE legacy_id = $1", legacyID).Scan(&c); err != nil {
			t.Fatalf("query users legacy_id=%d: %v", legacyID, err)
		}
		if c != 1 {
			t.Errorf("users legacy_id=%d missing (count=%d)", legacyID, c)
		}
	}
}

// TestMigrateData_FaultAfterUsers_AdvancesSequence_NoSecondOpDrift covers
// the rolled-back-attempt case from the R03 brief:
//
//  1. Run with FaultAfterUsers returning an error after the users copy.
//     The transaction rolls back but the PostgreSQL users_id_seq
//     (BIGSERIAL backing sequence) has advanced. The users table is
//     empty, no marker, but the sequence counter is past 1.
//  2. A second clean run completes successfully. It must insert explicit
//     ids via the migratedata path (it does; copyUsers returns the new
//     ids and the rest of the pipeline runs).
//  3. A third run is a clean no-op. This is the regression: with the
//     old target-hash implementation that compared
//     PostgreSQL-generated ids, the users hash on the second run
//     diverged from the source and the third run was wrongly classified
//     as dirty.
func TestMigrateData_FaultAfterUsers_AdvancesSequence_NoSecondOpDrift(t *testing.T) {
	path, _ := seedSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	// Step 1: faulted run. The advisory lock requires a clean
	// connection per Run, so use a separate Run that fails after the
	// users copy inside its own transaction.
	faultedOpts := migratedata.Options{
		SQLitePath:      path,
		PGDSN:           scopedDSN,
		FaultAfterUsers: func() error { return errors.New("forced fault after users") },
	}
	if _, err := migratedata.Run(context.Background(), faultedOpts); err == nil {
		t.Fatal("expected faulted run to fail, got nil")
	}

	// After the faulted run, the users table must be empty (rollback
	// cleaned up the inserted rows). The sequence, however, may have
	// advanced; record its value.
	if got := mustCount(t, db, "users"); got != 0 {
		t.Fatalf("after faulted run: users count = %d, want 0", got)
	}
	var seqAfterFault int64
	if err := db.QueryRow("SELECT last_value FROM users_id_seq").Scan(&seqAfterFault); err != nil {
		t.Fatalf("read users_id_seq: %v", err)
	}
	// The fixture inserts 3 users, so the sequence has advanced at least
	// past 3 (rolled-back inserts still consume sequence values).
	if seqAfterFault < 3 {
		t.Fatalf("users_id_seq last_value = %d, want >= 3 (sequence should have advanced during faulted run)", seqAfterFault)
	}

	// Step 2: clean run completes successfully despite the advanced
	// sequence.
	cleanOpts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	r1, err := migratedata.Run(context.Background(), cleanOpts)
	if err != nil {
		t.Fatalf("clean Run after fault: %v", err)
	}
	if !r1.MigrationVerified {
		t.Errorf("expected clean run MigrationVerified=true; got false")
	}

	// Step 3: second clean run is a no-op. The previous implementation
	// failed here because the target users hash compared
	// PostgreSQL-generated ids (now much larger than the source ids)
	// against the source, producing different SHA256s.
	r2, err := migratedata.Run(context.Background(), cleanOpts)
	if err != nil {
		t.Fatalf("second clean Run: %v", err)
	}
	if len(r2.Notes) == 0 || r2.Notes[0] != "already migrated; no-op" {
		t.Errorf("second clean run Notes: %v", r2.Notes)
	}
}

// TestMigrateData_SequenceResync_DefaultInsertGreaterThanMaxID covers the
// R03 sequence-resync fix. After migrating a source with non-contiguous
// ids, each of the four id-preserved tables must accept a normal
// default-id insert whose generated id is strictly greater than the
// migrated MAX(id). Without the is_called=true fix, the next nextval()
// would return the migrated max itself and collide with the migrated
// row, surfacing as a unique-constraint violation.
func TestMigrateData_SequenceResync_DefaultInsertGreaterThanMaxID(t *testing.T) {
	path, ids := seedNonContiguousSQLite(t)
	db, scopedDSN := newPostgresDB(t)
	schemaMigratedUp(t, db)

	opts := migratedata.Options{SQLitePath: path, PGDSN: scopedDSN}
	if _, err := migratedata.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Record the migrated MAX(id) per id-preserved table. The fixture
	// guarantees a non-contiguous range so MAX(id) is meaningfully larger
	// than the row count.
	type maxRow struct {
		table string
		max   int64
	}
	var maxRows []maxRow
	for _, table := range []string{"user_sessions", "priority_transactions", "activities", "play_history"} {
		maxRows = append(maxRows, maxRow{
			table: table,
			max:   mustInt64(t, db, "SELECT MAX(id) FROM "+table),
		})
	}

	// Pick a migrated user id (NOT a legacy_id) for the FK-dependent
	// inserts into user_sessions and priority_transactions.
	anyUserID := mustInt64(t, db, `SELECT id FROM users WHERE legacy_id = $1`, ids.userLegacyIDs[0])

	// Insert one row per table using default-id and verify the generated
	// id exceeds the migrated MAX(id). Use a single transaction so a
	// failure on any table surfaces the regression here.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	inserts := []struct {
		table string
		query string
		args  []any
	}{
		{
			table: "user_sessions",
			query: `INSERT INTO user_sessions (user_id, session_date, first_seen_at, last_seen_at)
			        VALUES ($1, '2026-02-15', '2026-02-15 09:00:00', '2026-02-15 17:00:00')
			        RETURNING id`,
			args: []any{anyUserID},
		},
		{
			table: "priority_transactions",
			query: `INSERT INTO priority_transactions (user_id, song_id, song_title, transaction_type, amount, balance_after)
			        VALUES ($1, 'v-new', 'New Song', 'spend', -1, 0)
			        RETURNING id`,
			args: []any{anyUserID},
		},
		{
			table: "activities",
			query: `INSERT INTO activities ("timestamp", type, "user", description)
			        VALUES ('2026-02-15 09:00:00', 'song_added', 'DefaultInsert', 'post-migration default id')
			        RETURNING id`,
		},
		{
			table: "play_history",
			query: `INSERT INTO play_history (video_id, title, played_at)
			        VALUES ('v-default', 'Default Insert', '2026-02-15 09:00:00')
			        RETURNING id`,
		},
	}
	got := make(map[string]int64, len(inserts))
	for _, ins := range inserts {
		var newID int64
		if err := tx.QueryRow(ins.query, ins.args...).Scan(&newID); err != nil {
			t.Fatalf("default-id insert into %s: %v", ins.table, err)
		}
		got[ins.table] = newID
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for _, mr := range maxRows {
		newID, ok := got[mr.table]
		if !ok {
			t.Errorf("missing default-id insert for %s", mr.table)
			continue
		}
		if newID <= mr.max {
			t.Errorf("%s: default-id insert = %d, want > migrated MAX(id) = %d", mr.table, newID, mr.max)
		}
	}
}