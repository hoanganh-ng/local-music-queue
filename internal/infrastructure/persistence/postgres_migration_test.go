package persistence

import (
	"context"
	"testing"
)

func TestPostgresMigration_CleanSchema(t *testing.T) {
	db := newPostgresDB(t)

	// Apply migrations to a clean schema.
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("migrate up failed: %v", err)
	}

	// Re-running migrate.Up must be a no-op (ErrNoChange).
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("second migrate up should be no-op: %v", err)
	}

	// Version subcommand must report 6 (0001_initial + 0002_legacy_id + 0003_migration_marker + 0004_rooms + 0005_player_leases + 0006_room_queue_state).
	v, dirty, err := EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if dirty {
		t.Fatalf("schema unexpectedly dirty")
	}
	if v != 6 {
		t.Fatalf("expected version=6 after first migration, got %d", v)
	}

	// Verify all twelve tables exist.
	want := []string{
		"queue_state",
		"activities",
		"users",
		"user_sessions",
		"priority_transactions",
		"auto_queue_config",
		"play_history",
		"rooms",
		"room_members",
		"room_invites",
		"player_leases",
		"room_queue_state",
	}
	for _, table := range want {
		var exists bool
		err := db.QueryRowContext(context.Background(),
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema() AND table_name = $1
			)`,
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist after migration", table)
		}
	}

	// The play_history index must exist. Scoped to current_schema() so a
	// same-named index from a sibling test schema cannot satisfy this check.
	var hasIndex bool
	err = db.QueryRowContext(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = 'idx_play_history_played_at'
		)`,
	).Scan(&hasIndex)
	if err != nil {
		t.Fatalf("query index: %v", err)
	}
	if !hasIndex {
		t.Errorf("expected idx_play_history_played_at index to exist")
	}

	// The default auto_queue_config row must exist with the documented shape.
	var enabled bool
	var strategy string
	err = db.QueryRowContext(context.Background(),
		`SELECT enabled, strategy FROM auto_queue_config WHERE id = 1`,
	).Scan(&enabled, &strategy)
	if err != nil {
		t.Fatalf("query auto_queue_config: %v", err)
	}
	if enabled {
		t.Errorf("expected enabled=false by default")
	}
	if strategy != "related" {
		t.Errorf("expected strategy=related by default, got %s", strategy)
	}
}

func TestPostgresMigration_DownThenUp(t *testing.T) {
	db := newPostgresDB(t)

	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Step down two versions. We are now at v6 (0001..0006); stepping down 2
	// reverses 0006 + 0005, landing at v4.
	if err := RunEmbeddedMigrationsDown(db, 2); err != nil {
		t.Fatalf("migrate down 2: %v", err)
	}

	v, _, err := EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 4 {
		t.Errorf("expected version=4 after down 2, got %d", v)
	}

	// Step down four more to fully revert all six migrations. queue_state
	// is dropped by 0001_initial.down.sql.
	if err := RunEmbeddedMigrationsDown(db, 4); err != nil {
		t.Fatalf("migrate down 4 (final): %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 0 {
		t.Errorf("expected version=0 after down 6, got %d", v)
	}

	// queue_state should be gone. Scoped to the test schema so the query
	// does not pick up queue_state left behind by concurrent test schemas
	// or earlier failed runs that did not clean up.
	var exists bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = 'queue_state'
		)`,
	).Scan(&exists); err != nil {
		t.Fatalf("query queue_state: %v", err)
	}
	if exists {
		t.Errorf("expected queue_state dropped after down")
	}

	// Re-apply: the schema must come back to version 6.
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("re-migrate up: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 6 {
		t.Errorf("expected version=6 after re-up, got %d", v)
	}
}

func TestPostgresMigration_DownThenUp_Rooms(t *testing.T) {
	db := newPostgresDB(t)

	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	v, _, err := EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 6 {
		t.Fatalf("expected version=6, got %d", v)
	}

	// Step down 3 — 0006_room_queue_state + 0005_player_leases + 0004_rooms
	// reverse together, leaving v3.
	if err := RunEmbeddedMigrationsDown(db, 3); err != nil {
		t.Fatalf("migrate down 3: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version after down: %v", err)
	}
	if v != 3 {
		t.Fatalf("expected version=3 after stepping down 0006+0005+0004, got %d", v)
	}

	// Verify the four room tables + room_queue_state are gone.
	for _, table := range []string{"rooms", "room_members", "room_invites", "player_leases", "room_queue_state"} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`, table).Scan(&exists); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if exists {
			t.Errorf("expected table %q to be dropped after 0006+0005+0004 down", table)
		}
	}

	// Re-apply — must come back to v6.
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("re-migrate up: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version after re-up: %v", err)
	}
	if v != 6 {
		t.Fatalf("expected version=6 after re-up, got %d", v)
	}
}