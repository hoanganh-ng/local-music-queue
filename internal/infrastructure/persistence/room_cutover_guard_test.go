package persistence

// R14c fail-closed startup guard tests. PG-gated: they skip when the
// LMQ_TEST_DATABASE_URL / local-dev PostgreSQL is unreachable, and each
// test runs inside its own throwaway schema via newPostgresDB. The
// guard is presence-only, so the tests drive it purely through the
// schema_migrations bookkeeping and the room_cutover_marker row —
// never through the room-cutover CLI.

import (
	"database/sql"
	"strings"
	"testing"
)

// insertTestCutoverMarker writes the single durable marker row the guard
// requires. Values are synthetic test fixtures — no production identity,
// hashes, or build provenance.
func insertTestCutoverMarker(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO room_cutover_marker
			(id, room_cutover_id, target_room_slug, target_room_id, host_user_id,
			 source_hashes, target_hashes, legacy_id_offset, cutover_pre_commit_at, binary_build_sha)
		VALUES
			(1, '00000000-0000-0000-0000-000000000001', 'rehearsal-room', 1, 1,
			 '{}', '{}', 0, NOW(), 'test-build-sha')`)
	if err != nil {
		t.Fatalf("insert test cutover marker: %v", err)
	}
}

func TestVerifyRoomCutoverStartupReadiness_MarkerPresent(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	insertTestCutoverMarker(t, db)

	if err := VerifyRoomCutoverStartupReadiness(db); err != nil {
		t.Fatalf("expected guard to pass with clean schema >= 9 and marker present, got: %v", err)
	}
}

func TestVerifyRoomCutoverStartupReadiness_MissingMarker(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)

	err := VerifyRoomCutoverStartupReadiness(db)
	if err == nil {
		t.Fatal("expected guard to reject a migrated database without room_cutover_marker id=1")
	}
	if !strings.Contains(err.Error(), "room_cutover_marker") {
		t.Errorf("error %q should name the missing marker", err.Error())
	}
}

func TestVerifyRoomCutoverStartupReadiness_DirtyMigrationState(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	insertTestCutoverMarker(t, db)

	if _, err := db.Exec(`UPDATE schema_migrations SET dirty = TRUE`); err != nil {
		t.Fatalf("mark schema_migrations dirty: %v", err)
	}

	err := VerifyRoomCutoverStartupReadiness(db)
	if err == nil {
		t.Fatal("expected guard to reject a dirty migration state even with the marker present")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Errorf("error %q should mention the dirty migration state", err.Error())
	}
}

func TestVerifyRoomCutoverStartupReadiness_SchemaVersionTooLow(t *testing.T) {
	db := newPostgresDB(t)
	schemaMigratedUp(t, db)
	insertTestCutoverMarker(t, db)

	// Simulate a database whose bookkeeping predates 0009 without
	// running down-migrations (the marker table is left in place so the
	// version check is proven to fire before the marker lookup).
	if _, err := db.Exec(`UPDATE schema_migrations SET version = $1, dirty = FALSE`,
		RoomCutoverMinimumSchemaVersion-1); err != nil {
		t.Fatalf("rewind schema_migrations version: %v", err)
	}

	err := VerifyRoomCutoverStartupReadiness(db)
	if err == nil {
		t.Fatal("expected guard to reject a schema version below the minimum")
	}
	if !strings.Contains(err.Error(), "below the required version") {
		t.Errorf("error %q should mention the version requirement", err.Error())
	}
}

func TestVerifyRoomCutoverStartupReadiness_UnmigratedDatabase(t *testing.T) {
	db := newPostgresDB(t) // fresh schema, migrations never applied

	err := VerifyRoomCutoverStartupReadiness(db)
	if err == nil {
		t.Fatal("expected guard to reject a database with no migration bookkeeping at all")
	}
}
