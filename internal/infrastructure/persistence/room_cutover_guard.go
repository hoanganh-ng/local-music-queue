package persistence

// R14c fail-closed startup guard for the authoritative room runtime.
//
// When cmd/server is started with --room-cutover-authoritative=true it must
// refuse to serve unless the migration state is clean, the schema version is
// at least 9 (migration 0009_room_cutover_support), and the durable cutover
// marker row room_cutover_marker.id = 1 exists. The guard runs before any
// defensive startup migration and before listeners open, so an older or
// un-cut-over database can never be silently migrated into — or served by —
// the authoritative runtime.
//
// The guard is presence-only by design: it never creates schema, never
// creates or edits the marker, never invokes the room-cutover CLI, never
// repairs a dirty migration, and does not revalidate the marker's mutable
// target hashes on every startup (that deep re-verification belongs to
// `room-cutover verify`). Error messages carry no DSNs, credentials, marker
// hashes, email addresses, or session material.

import (
	"database/sql"
	"errors"
	"fmt"
)

// RoomCutoverMinimumSchemaVersion is the lowest embedded schema version that
// contains the room_activities and room_cutover_marker tables (migration
// 0009_room_cutover_support).
const RoomCutoverMinimumSchemaVersion = 9

// VerifyRoomCutoverStartupReadiness enforces the R14c authoritative-mode
// startup contract against an already-opened-and-pinged *sql.DB:
//
//  1. the golang-migrate state must not be dirty;
//  2. the applied schema version must be >= RoomCutoverMinimumSchemaVersion;
//  3. room_cutover_marker.id = 1 must exist.
//
// Any violation returns a non-nil error and the caller must refuse to serve.
func VerifyRoomCutoverStartupReadiness(db *sql.DB) error {
	version, dirty, err := EmbeddedMigrationsVersion(db)
	if err != nil {
		return fmt.Errorf("room-cutover startup guard: read schema migration state: %w", err)
	}
	if dirty {
		return fmt.Errorf("room-cutover startup guard: migration state is dirty at version %d; refusing to serve in authoritative mode (resolve the dirty migration with the operator tooling first)", version)
	}
	if version < RoomCutoverMinimumSchemaVersion {
		return fmt.Errorf("room-cutover startup guard: schema version %d is below the required version %d; refusing to serve in authoritative mode", version, RoomCutoverMinimumSchemaVersion)
	}

	var id int
	err = db.QueryRow(`SELECT id FROM room_cutover_marker WHERE id = 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("room-cutover startup guard: room_cutover_marker id=1 not found; run `room-cutover up` and `room-cutover verify` before starting in authoritative mode")
	}
	if err != nil {
		return fmt.Errorf("room-cutover startup guard: read room_cutover_marker: %w", err)
	}
	return nil
}
