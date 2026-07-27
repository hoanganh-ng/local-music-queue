package roomcutover

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"local-music-queue/internal/infrastructure/persistence"
)

func TestMain(m *testing.M) {
	// Inject a non-placeholder build SHA so the production marker-insert guard
	// is satisfied in tests.
	BuildSHA = "testsha-0123456789abcdef"
	os.Exit(m.Run())
}

func TestUpHappyPath(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	report, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !report.Verified {
		t.Fatal("expected report.Verified")
	}
	if report.AlreadyCutOver {
		t.Fatal("first run should not be AlreadyCutOver")
	}
	if report.RoomCutoverID == "" {
		t.Fatal("expected a room cutover id")
	}
	if report.TargetRoomID == 0 {
		t.Fatal("expected a target room id")
	}
	if !hashesEqual(report.SourceHashes, report.TargetHashes) {
		t.Fatalf("source and target hashes differ:\n src=%v\n dst=%v", report.SourceHashes, report.TargetHashes)
	}
	for _, k := range []string{hashKeyQueueState, hashKeyActivities, hashKeyAutoQueueConfig, hashKeyPlayHistory} {
		if len(report.SourceHashes[k]) != 64 {
			t.Fatalf("expected 64-hex hash for %s, got %q", k, report.SourceHashes[k])
		}
	}

	// Row-level assertions.
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms WHERE slug = 'legacy-room'`); got != 1 {
		t.Fatalf("rooms=%d, want 1", got)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_members WHERE room_id = $1 AND role = 'host'`, report.TargetRoomID); got != 1 {
		t.Fatalf("host memberships=%d, want 1", got)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_queue_state WHERE room_id = $1`, report.TargetRoomID); got != 1 {
		t.Fatalf("room_queue_state=%d, want 1", got)
	}
	if src, dst := countInt(t, db, `SELECT COUNT(*) FROM activities`), countInt(t, db, `SELECT COUNT(*) FROM room_activities WHERE room_id = $1`, report.TargetRoomID); src != dst {
		t.Fatalf("activities copied %d != source %d", dst, src)
	}
	if src, dst := countInt(t, db, `SELECT COUNT(*) FROM play_history`), countInt(t, db, `SELECT COUNT(*) FROM room_play_history WHERE room_id = $1`, report.TargetRoomID); src != dst {
		t.Fatalf("play_history copied %d != source %d", dst, src)
	}
	var enabled bool
	var strategy string
	if err := db.QueryRowContext(ctx, `SELECT enabled, strategy FROM room_auto_queue_config WHERE room_id = $1`, report.TargetRoomID).Scan(&enabled, &strategy); err != nil {
		t.Fatalf("read room_auto_queue_config: %v", err)
	}
	if !enabled || strategy != "related" {
		t.Fatalf("auto-queue config copied wrong: enabled=%v strategy=%q", enabled, strategy)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 1 {
		t.Fatalf("marker rows=%d, want 1", got)
	}

	// Activity ids are preserved verbatim.
	if diff := countInt(t, db, `
		SELECT COUNT(*) FROM activities a
		WHERE NOT EXISTS (
			SELECT 1 FROM room_activities r
			WHERE r.id = a.id AND r.room_id = $1
		)`, report.TargetRoomID); diff != 0 {
		t.Fatalf("%d activity ids not preserved", diff)
	}
}

func TestUpIsIdempotent(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	first, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("first Up: %v", err)
	}
	second, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if !second.AlreadyCutOver {
		t.Fatal("second run should report AlreadyCutOver")
	}
	if !second.Verified {
		t.Fatal("second run should report Verified")
	}
	if second.TargetRoomID != first.TargetRoomID {
		t.Fatalf("no-op changed target room id: %d -> %d", first.TargetRoomID, second.TargetRoomID)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms WHERE slug = 'legacy-room'`); got != 1 {
		t.Fatalf("idempotent re-run created duplicate rooms: %d", got)
	}
}

func TestUpConflictingHostFails(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	otherID := seedUser(t, db, "other@example.test")

	if _, err := Up(ctx, db, baseOptions(hostID)); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	opts := baseOptions(otherID) // same slug, different host
	_, err := Up(ctx, db, opts)
	if err == nil || !strings.Contains(err.Error(), "host user id") {
		t.Fatalf("expected host mismatch error, got %v", err)
	}
}

func TestUpFailsWhenSlugTaken(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	// A different room already owns the slug (created via runtime path).
	if _, err := db.ExecContext(ctx, `INSERT INTO rooms (slug, name) VALUES ('legacy-room', 'Taken')`); err != nil {
		t.Fatalf("pre-insert room: %v", err)
	}
	_, err := Up(ctx, db, baseOptions(hostID))
	if err == nil {
		t.Fatal("expected failure when slug already taken")
	}
	// The failed transaction rolled back: no marker, no host membership.
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 0 {
		t.Fatalf("marker should not exist after rollback, got %d", got)
	}
}

func TestPlanReadOnly(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	report, err := Plan(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !report.DryRun {
		t.Fatal("plan report should be DryRun")
	}
	if len(report.SourceHashes) != 4 {
		t.Fatalf("expected 4 source hashes, got %d", len(report.SourceHashes))
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms`); got != 0 {
		t.Fatalf("plan wrote a room: %d", got)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 0 {
		t.Fatalf("plan wrote a marker: %d", got)
	}
}

func TestUpDryRunWritesNothing(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	opts := baseOptions(hostID)
	opts.DryRun = true
	report, err := Up(ctx, db, opts)
	if err != nil {
		t.Fatalf("dry-run Up: %v", err)
	}
	if !report.DryRun {
		t.Fatal("expected DryRun report")
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms`); got != 0 {
		t.Fatalf("dry-run wrote a room: %d", got)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 0 {
		t.Fatalf("dry-run wrote a marker: %d", got)
	}
}

func TestVerifyHappyPath(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	up, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Verify is marker-only: no room slug, name, or host id supplied.
	report, err := Verify(ctx, db, verifyOptions())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.Verified {
		t.Fatal("expected Verified")
	}
	// The identity in the report is derived solely from the marker.
	if report.TargetRoomSlug != "legacy-room" {
		t.Fatalf("marker-derived slug=%q, want legacy-room", report.TargetRoomSlug)
	}
	if report.TargetRoomID != up.TargetRoomID {
		t.Fatalf("marker-derived room id=%d, want %d", report.TargetRoomID, up.TargetRoomID)
	}
	if report.HostUserID != hostID {
		t.Fatalf("marker-derived host id=%d, want %d", report.HostUserID, hostID)
	}
}

func TestVerifyRequiresMarker(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	seedTypicalLegacyState(t, db)
	_, err := Verify(ctx, db, verifyOptions())
	if err == nil || !strings.Contains(err.Error(), "no cutover marker") {
		t.Fatalf("expected missing marker error, got %v", err)
	}
}

func TestVerifyDetectsSourceDrift(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	if _, err := Up(ctx, db, baseOptions(hostID)); err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Mutate the legacy source after cutover.
	if _, err := db.ExecContext(ctx, `UPDATE activities SET description = 'tampered' WHERE id = (SELECT MIN(id) FROM activities)`); err != nil {
		t.Fatalf("mutate source: %v", err)
	}
	_, err := Verify(ctx, db, verifyOptions())
	if err == nil || !strings.Contains(err.Error(), "source hash drift") {
		t.Fatalf("expected source drift error, got %v", err)
	}
}

func TestVerifyDetectsTargetDrift(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	report, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Mutate the copied target rows after cutover.
	if _, err := db.ExecContext(ctx, `UPDATE room_activities SET description = 'tampered' WHERE room_id = $1 AND id = (SELECT MIN(id) FROM room_activities WHERE room_id = $1)`, report.TargetRoomID); err != nil {
		t.Fatalf("mutate target: %v", err)
	}
	_, err = Verify(ctx, db, verifyOptions())
	if err == nil || !strings.Contains(err.Error(), "target hash drift") {
		t.Fatalf("expected target drift error, got %v", err)
	}
}

func TestPlayHistoryOffsetApplied(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	offset := seedForeignRoomPlayHistory(t, db, 3)
	if offset <= 0 {
		t.Fatalf("expected positive offset, got %d", offset)
	}

	report, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if report.LegacyIDOffset != offset {
		t.Fatalf("report offset=%d, want %d", report.LegacyIDOffset, offset)
	}
	// Every copied row id must exceed the offset.
	if bad := countInt(t, db, `SELECT COUNT(*) FROM room_play_history WHERE room_id = $1 AND id <= $2`, report.TargetRoomID, offset); bad != 0 {
		t.Fatalf("%d copied play_history ids did not clear the offset", bad)
	}
	if !hashesEqual(report.SourceHashes, report.TargetHashes) {
		t.Fatal("offset applied but hashes diverged")
	}
}

func TestHostUserNotFound(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	seedTypicalLegacyState(t, db)
	opts := baseOptions(999999)
	_, err := Up(ctx, db, opts)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected host-not-found error, got %v", err)
	}
}

func TestSchemaVersionGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("below required", func(t *testing.T) {
		db := newCutoverTestDB(t)
		hostID := seedTypicalLegacyState(t, db)
		if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET version = 8, dirty = false`); err != nil {
			t.Fatalf("force version: %v", err)
		}
		_, err := Up(ctx, db, baseOptions(hostID))
		if err == nil || !strings.Contains(err.Error(), "below the required") {
			t.Fatalf("expected below-version error, got %v", err)
		}
	})

	t.Run("above supported", func(t *testing.T) {
		db := newCutoverTestDB(t)
		hostID := seedTypicalLegacyState(t, db)
		if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET version = 10, dirty = false`); err != nil {
			t.Fatalf("force version: %v", err)
		}
		_, err := Up(ctx, db, baseOptions(hostID))
		if err == nil || !strings.Contains(err.Error(), "above the supported") {
			t.Fatalf("expected above-version error, got %v", err)
		}
	})

	t.Run("dirty", func(t *testing.T) {
		db := newCutoverTestDB(t)
		hostID := seedTypicalLegacyState(t, db)
		if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET dirty = true`); err != nil {
			t.Fatalf("force dirty: %v", err)
		}
		_, err := Up(ctx, db, baseOptions(hostID))
		if err == nil || !strings.Contains(err.Error(), "dirty") {
			t.Fatalf("expected dirty error, got %v", err)
		}
	})
}

func TestAdvisoryLockContention(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	// Hold the advisory lock on a separate session.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&locked); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if !locked {
		t.Fatal("expected to acquire the lock")
	}
	defer conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey)

	_, err = Up(ctx, db, baseOptions(hostID))
	if err == nil || !strings.Contains(err.Error(), "advisory lock held") {
		t.Fatalf("expected lock-contention error, got %v", err)
	}
}

func TestFaultBeforeCommitRollsBackAndReleasesLock(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	opts := baseOptions(hostID)
	opts.FaultBeforeCommit = func() error { return errors.New("injected pre-commit fault") }
	if _, err := Up(ctx, db, opts); err == nil {
		t.Fatal("expected fault error")
	}
	// Rolled back: nothing written.
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms WHERE slug = 'legacy-room'`); got != 0 {
		t.Fatalf("fault left a room behind: %d", got)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 0 {
		t.Fatalf("fault left a marker behind: %d", got)
	}
	// Lock released: a clean retry succeeds.
	report, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("retry after fault: %v", err)
	}
	if !report.Verified {
		t.Fatal("retry should verify")
	}
}

func TestFaultBeforeMarkerRollsBack(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	opts := baseOptions(hostID)
	opts.FaultBeforeMarker = func() error { return errors.New("injected pre-marker fault") }
	if _, err := Up(ctx, db, opts); err == nil {
		t.Fatal("expected fault error")
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms WHERE slug = 'legacy-room'`); got != 0 {
		t.Fatalf("fault left a room behind: %d", got)
	}
}

func TestPlaceholderBuildSHARejected(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	orig := BuildSHA
	BuildSHA = "dev"
	defer func() { BuildSHA = orig }()

	_, err := Up(ctx, db, baseOptions(hostID))
	if err == nil || !strings.Contains(err.Error(), "placeholder build sha") {
		t.Fatalf("expected placeholder rejection, got %v", err)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms`); got != 0 {
		t.Fatalf("placeholder run must not write: %d rooms", got)
	}
}

func TestInvalidSlugRejected(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	opts := baseOptions(hostID)
	opts.RoomSlug = "API" // uppercase fails the slug pattern
	if _, err := Up(ctx, db, opts); err == nil || !strings.Contains(err.Error(), "invalid room slug") {
		t.Fatalf("expected invalid slug error, got %v", err)
	}

	opts = baseOptions(hostID)
	opts.RoomSlug = "api" // reserved
	if _, err := Up(ctx, db, opts); err == nil || !strings.Contains(err.Error(), "reserved room slug") {
		t.Fatalf("expected reserved slug error, got %v", err)
	}
}

// Guard against accidental drift in the version constant.
func TestRequiredSchemaVersionMatchesEmbedded(t *testing.T) {
	db := newCutoverTestDB(t)
	version, dirty, err := persistence.EmbeddedMigrationsVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if dirty {
		t.Fatal("fresh test DB should not be dirty")
	}
	if version != requiredSchemaVersion {
		t.Fatalf("fresh test DB at version %d but requiredSchemaVersion=%d", version, requiredSchemaVersion)
	}
}

// --- corrective-pass regressions ------------------------------------------

// TestUpPostCommitVerificationDetectsTamper proves Up runs the full
// marker-derived verification against the COMMITTED state and refuses to
// claim success when that state no longer matches the marker.
func TestUpPostCommitVerificationDetectsTamper(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	opts := baseOptions(hostID)
	opts.TamperAfterCommit = func() error {
		// Mutate a committed target row between COMMIT and the post-commit
		// verification pass. Return nil so the run proceeds to verification.
		_, err := db.ExecContext(ctx, `
			UPDATE room_activities SET description = 'tampered-after-commit'
			WHERE id = (SELECT MIN(id) FROM room_activities)`)
		return err
	}
	_, err := Up(ctx, db, opts)
	if err == nil || !strings.Contains(err.Error(), "post-commit verification failed") {
		t.Fatalf("expected post-commit verification failure, got %v", err)
	}
	// The transaction did commit: the marker is durable, so the failure is
	// attributable to the post-commit pass, not a rollback.
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 1 {
		t.Fatalf("marker rows=%d, want 1 (commit happened)", got)
	}
}

// TestUpRerunDetectsRoomNameDrift: a no-op rerun supplies the room name, so
// a renamed target room must fail the rerun — while marker-only Verify
// (which carries no name expectation) still passes on the intact data.
func TestUpRerunDetectsRoomNameDrift(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	if _, err := Up(ctx, db, baseOptions(hostID)); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE rooms SET name = 'Renamed Room' WHERE slug = 'legacy-room'`); err != nil {
		t.Fatalf("rename room: %v", err)
	}
	if _, err := Up(ctx, db, baseOptions(hostID)); err == nil || !strings.Contains(err.Error(), "room name drift") {
		t.Fatalf("expected room name drift error, got %v", err)
	}
	if _, err := Plan(ctx, db, baseOptions(hostID)); err == nil || !strings.Contains(err.Error(), "room name drift") {
		t.Fatalf("expected room name drift error from plan, got %v", err)
	}
	if _, err := Verify(ctx, db, verifyOptions()); err != nil {
		t.Fatalf("marker-only Verify should not check the name: %v", err)
	}
}

// TestVerifyDetectsMissingRoom: the marker's audit room id must resolve.
func TestVerifyDetectsMissingRoom(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	report, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM rooms WHERE id = $1`, report.TargetRoomID); err != nil {
		t.Fatalf("delete room: %v", err)
	}
	_, err = Verify(ctx, db, verifyOptions())
	if err == nil || !strings.Contains(err.Error(), "does not resolve to a room") {
		t.Fatalf("expected unresolved room error, got %v", err)
	}
}

// TestVerifyDetectsRoomSlugRename: the resolved room's current slug must
// still match the marker's recorded slug.
func TestVerifyDetectsRoomSlugRename(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	report, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE rooms SET slug = 'hijacked-room' WHERE id = $1`, report.TargetRoomID); err != nil {
		t.Fatalf("rename slug: %v", err)
	}
	_, err = Verify(ctx, db, verifyOptions())
	if err == nil || !strings.Contains(err.Error(), "now has slug") {
		t.Fatalf("expected slug rename error, got %v", err)
	}
}

// TestVerifyDetectsHostMembershipDrift: the marker's host must still hold
// the room's host membership — a demoted or deleted membership fails.
func TestVerifyDetectsHostMembershipDrift(t *testing.T) {
	t.Run("demoted", func(t *testing.T) {
		db := newCutoverTestDB(t)
		ctx := context.Background()
		hostID := seedTypicalLegacyState(t, db)
		report, err := Up(ctx, db, baseOptions(hostID))
		if err != nil {
			t.Fatalf("Up: %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE room_members SET role = 'admin' WHERE room_id = $1 AND user_id = $2`, report.TargetRoomID, hostID); err != nil {
			t.Fatalf("demote host: %v", err)
		}
		_, err = Verify(ctx, db, verifyOptions())
		if err == nil || !strings.Contains(err.Error(), "no longer holds the host membership") {
			t.Fatalf("expected host membership error, got %v", err)
		}
	})
	t.Run("deleted", func(t *testing.T) {
		db := newCutoverTestDB(t)
		ctx := context.Background()
		hostID := seedTypicalLegacyState(t, db)
		report, err := Up(ctx, db, baseOptions(hostID))
		if err != nil {
			t.Fatalf("Up: %v", err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, report.TargetRoomID, hostID); err != nil {
			t.Fatalf("delete membership: %v", err)
		}
		_, err = Verify(ctx, db, verifyOptions())
		if err == nil || !strings.Contains(err.Error(), "no longer holds the host membership") {
			t.Fatalf("expected host membership error, got %v", err)
		}
	})
}

// TestVerifyDetectsPlayedAtDrift proves played_at participates in both the
// source and target play-history hash projections.
func TestVerifyDetectsPlayedAtDrift(t *testing.T) {
	t.Run("target", func(t *testing.T) {
		db := newCutoverTestDB(t)
		ctx := context.Background()
		hostID := seedTypicalLegacyState(t, db)
		report, err := Up(ctx, db, baseOptions(hostID))
		if err != nil {
			t.Fatalf("Up: %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE room_play_history SET played_at = played_at + INTERVAL '1 hour' WHERE room_id = $1`, report.TargetRoomID); err != nil {
			t.Fatalf("shift target played_at: %v", err)
		}
		_, err = Verify(ctx, db, verifyOptions())
		if err == nil || !strings.Contains(err.Error(), "target hash drift") {
			t.Fatalf("expected target hash drift, got %v", err)
		}
	})
	t.Run("source", func(t *testing.T) {
		db := newCutoverTestDB(t)
		ctx := context.Background()
		hostID := seedTypicalLegacyState(t, db)
		if _, err := Up(ctx, db, baseOptions(hostID)); err != nil {
			t.Fatalf("Up: %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE play_history SET played_at = played_at + INTERVAL '1 hour' WHERE id = (SELECT MIN(id) FROM play_history)`); err != nil {
			t.Fatalf("shift source played_at: %v", err)
		}
		_, err := Verify(ctx, db, verifyOptions())
		if err == nil || !strings.Contains(err.Error(), "source hash drift") {
			t.Fatalf("expected source hash drift, got %v", err)
		}
	})
}

// TestFirstCutoverRejectsPreexistingRoomActivities: legacy activity ids are
// preserved verbatim, so plan, dry-run, and up must all refuse while ANY
// room_activities row exists anywhere.
func TestFirstCutoverRejectsPreexistingRoomActivities(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	seedForeignRoomActivity(t, db)

	if _, err := Plan(ctx, db, baseOptions(hostID)); err == nil || !strings.Contains(err.Error(), "room_activities already contains") {
		t.Fatalf("plan: expected room_activities readiness error, got %v", err)
	}
	dry := baseOptions(hostID)
	dry.DryRun = true
	if _, err := Up(ctx, db, dry); err == nil || !strings.Contains(err.Error(), "room_activities already contains") {
		t.Fatalf("dry-run: expected room_activities readiness error, got %v", err)
	}
	if _, err := Up(ctx, db, baseOptions(hostID)); err == nil || !strings.Contains(err.Error(), "room_activities already contains") {
		t.Fatalf("up: expected room_activities readiness error, got %v", err)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 0 {
		t.Fatalf("marker rows=%d, want 0", got)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms WHERE slug = 'legacy-room'`); got != 0 {
		t.Fatalf("target room created despite readiness failure")
	}
}

// TestPlanRejectsTakenSlug: plan must surface a slug conflict instead of
// reporting readiness.
func TestPlanRejectsTakenSlug(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	if _, err := db.ExecContext(ctx, `INSERT INTO rooms (slug, name) VALUES ('legacy-room', 'Taken')`); err != nil {
		t.Fatalf("pre-insert room: %v", err)
	}
	_, err := Plan(ctx, db, baseOptions(hostID))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected slug conflict error, got %v", err)
	}
}

// TestPlanReportsExpectedTargetEvidence: plan (and dry-run) must report the
// expected target hashes and per-table counts a correct cutover would
// produce, derived read-only from the source.
func TestPlanReportsExpectedTargetEvidence(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	offset := seedForeignRoomPlayHistory(t, db, 3)

	report, err := Plan(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(report.TargetHashes) != 4 {
		t.Fatalf("expected 4 expected-target hashes, got %d", len(report.TargetHashes))
	}
	if !hashesEqual(report.SourceHashes, report.TargetHashes) {
		t.Fatalf("expected target hashes to equal source hashes:\n src=%v\n dst=%v", report.SourceHashes, report.TargetHashes)
	}
	if report.LegacyIDOffset != offset {
		t.Fatalf("plan offset=%d, want %d", report.LegacyIDOffset, offset)
	}
	if len(report.Tables) != 4 {
		t.Fatalf("expected 4 table reports, got %d", len(report.Tables))
	}
	for _, tr := range report.Tables {
		if tr.SourceCount == 0 && tr.Table != hashKeyQueueState {
			t.Fatalf("table %s has zero source count in a seeded fixture", tr.Table)
		}
		if tr.TargetCount != tr.SourceCount {
			t.Fatalf("table %s expected target count %d != source count %d", tr.Table, tr.TargetCount, tr.SourceCount)
		}
	}

	// And the real cutover must then produce exactly this evidence.
	up, err := Up(ctx, db, baseOptions(hostID))
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !hashesEqual(up.TargetHashes, report.TargetHashes) {
		t.Fatalf("actual target hashes diverge from plan's expected hashes")
	}
}

// TestPlanAndDryRunLeaveSequencesUntouched: read-only modes must not
// consume or resync the BIGSERIAL sequences of the target tables.
func TestPlanAndDryRunLeaveSequencesUntouched(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)

	actLast, actCalled := readSequenceState(t, db, "room_activities")
	phLast, phCalled := readSequenceState(t, db, "room_play_history")

	if _, err := Plan(ctx, db, baseOptions(hostID)); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	dry := baseOptions(hostID)
	dry.DryRun = true
	if _, err := Up(ctx, db, dry); err != nil {
		t.Fatalf("dry-run Up: %v", err)
	}

	if last, called := readSequenceState(t, db, "room_activities"); last != actLast || called != actCalled {
		t.Fatalf("room_activities sequence changed: (%d,%v) -> (%d,%v)", actLast, actCalled, last, called)
	}
	if last, called := readSequenceState(t, db, "room_play_history"); last != phLast || called != phCalled {
		t.Fatalf("room_play_history sequence changed: (%d,%v) -> (%d,%v)", phLast, phCalled, last, called)
	}
}

// TestRejectsPlayHistoryOverflow: when MAX(play_history.id) + offset would
// exceed BIGINT, plan and up must refuse before any DML.
func TestRejectsPlayHistoryOverflow(t *testing.T) {
	db := newCutoverTestDB(t)
	ctx := context.Background()
	hostID := seedTypicalLegacyState(t, db)
	// Highest legacy id leaves headroom of only 7; an offset of 8 overflows.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO play_history (id, video_id, title, played_at)
		VALUES (9223372036854775800, 'huge', 'Huge', now())
	`); err != nil {
		t.Fatalf("seed huge play_history id: %v", err)
	}
	if off := seedForeignRoomPlayHistory(t, db, 8); off < 8 {
		t.Fatalf("expected offset >= 8, got %d", off)
	}

	if _, err := Plan(ctx, db, baseOptions(hostID)); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("plan: expected overflow error, got %v", err)
	}
	if _, err := Up(ctx, db, baseOptions(hostID)); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("up: expected overflow error, got %v", err)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM rooms WHERE slug = 'legacy-room'`); got != 0 {
		t.Fatalf("overflow run must not write: %d rooms", got)
	}
	if got := countInt(t, db, `SELECT COUNT(*) FROM room_cutover_marker`); got != 0 {
		t.Fatalf("overflow run must not write a marker: %d", got)
	}
}
