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

	// Version subcommand must report 9 (0001_initial + 0002_legacy_id + 0003_migration_marker + 0004_rooms + 0005_player_leases + 0006_room_queue_state + 0007_room_auto_queue + 0008_room_chat_messages + 0009_room_cutover_support).
	v, dirty, err := EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if dirty {
		t.Fatalf("schema unexpectedly dirty")
	}
	if v != 9 {
		t.Fatalf("expected version=9 after first migration, got %d", v)
	}

	// Verify all seventeen tables exist.
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
		"room_auto_queue_config",
		"room_play_history",
		"room_chat_messages",
		"room_activities",
		"room_cutover_marker",
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

	// Step down two versions. We are now at v9 (0001..0009); stepping down 2
	// reverses 0009 + 0008, landing at v7.
	if err := RunEmbeddedMigrationsDown(db, 2); err != nil {
		t.Fatalf("migrate down 2: %v", err)
	}

	v, _, err := EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 7 {
		t.Errorf("expected version=7 after down 2, got %d", v)
	}

	// Step down seven more to fully revert all nine migrations. queue_state
	// is dropped by 0001_initial.down.sql.
	if err := RunEmbeddedMigrationsDown(db, 7); err != nil {
		t.Fatalf("migrate down 7 (final): %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 0 {
		t.Errorf("expected version=0 after down 9, got %d", v)
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

	// Re-apply: the schema must come back to version 9.
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("re-migrate up: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 9 {
		t.Errorf("expected version=9 after re-up, got %d", v)
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
	if v != 9 {
		t.Fatalf("expected version=9, got %d", v)
	}

	// Step down 5 — 0009_room_cutover_support + 0008_room_chat_messages + 0007_room_auto_queue + 0006_room_queue_state + 0005_player_leases
	// reverse together, leaving v4.
	if err := RunEmbeddedMigrationsDown(db, 5); err != nil {
		t.Fatalf("migrate down 5: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version after down: %v", err)
	}
	if v != 4 {
		t.Fatalf("expected version=4 after stepping down 0009+0008+0007+0006+0005, got %d", v)
	}

	// Verify the per-room queue + auto-queue tables are gone. The
	// rooms / room_members / room_invites tables persist because
	// 0004 is not yet reverted.
	for _, table := range []string{"player_leases", "room_queue_state", "room_auto_queue_config", "room_play_history"} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`, table).Scan(&exists); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if exists {
			t.Errorf("expected table %q to be dropped after 0009+0008+0007+0006+0005 down", table)
		}
	}

	// Re-apply — must come back to v9.
	if err := RunEmbeddedMigrationsUp(db); err != nil {
		t.Fatalf("re-migrate up: %v", err)
	}
	v, _, err = EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version after re-up: %v", err)
	}
	if v != 9 {
		t.Fatalf("expected version=9 after re-up, got %d", v)
	}
}