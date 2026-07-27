package roomcutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

// advisoryLockKey is the PostgreSQL advisory-lock key guarding the cutover
// write transaction. It is intentionally the same key the R03 schema/data
// migrators use ("lmq_migration") so a cutover can never race a schema
// migration or a second cutover: whoever holds it first wins, the loser fails
// fast. A crashed holder's lock is released when its session ends.
const advisoryLockKey int64 = 987654321

// requiredSchemaVersion is the exact schema version the cutover runs against.
// Below it the operator must migrate up first; above it the two legacy tables
// the cutover reads may have been dropped by a later migration, so this binary
// is stale and refuses to run.
const requiredSchemaVersion uint = 9

// BuildSHA is the binary build revision recorded in the cutover marker. It is
// overridden at link time (-ldflags "-X .../roomcutover.BuildSHA=<rev>") in
// production builds and injected directly by tests. When empty it falls back
// to the VCS revision embedded by the Go toolchain. A production `up` refuses
// to write a marker carrying a placeholder value.
var BuildSHA string

// Options configures a cutover run. RoomSlug and HostUserID identify the
// target for plan/up; RoomName is additionally required to create the room
// in plan/up. Verify ignores the identity fields entirely and derives the
// cutover identity from the durable marker. The zero value is only usable
// for Verify.
type Options struct {
	RoomSlug   string
	RoomName   string
	HostUserID int64
	DryRun     bool

	// RedactedDSN is copied verbatim into the report. The caller is
	// responsible for redacting credentials (the package never sees the raw
	// DSN; it receives an already-open *sql.DB).
	RedactedDSN string

	// Now supplies the pre-commit clock. Nil defaults to time.Now().UTC().
	// Tests inject a fixed clock for deterministic marker timestamps.
	Now func() time.Time

	// FaultBeforeMarker and FaultBeforeCommit are test-only hooks. When
	// non-nil and returning an error the run aborts at that point so the
	// rollback/lock-release behavior can be asserted. Production callers
	// leave them nil.
	FaultBeforeMarker func() error
	FaultBeforeCommit func() error

	// TamperAfterCommit is a test-only hook that runs after COMMIT and
	// before the post-commit verification pass, so tests can mutate the
	// committed state and assert that post-commit verification catches it.
	// A returned error aborts the run. Production callers leave it nil.
	TamperAfterCommit func() error
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

// validateIdentity checks the fields plan/up need: a valid, non-reserved
// slug and a positive host user id.
func (o Options) validateIdentity() error {
	if !entity.IsValidSlug(o.RoomSlug) {
		return fmt.Errorf("invalid room slug %q", o.RoomSlug)
	}
	if entity.IsReservedSlug(o.RoomSlug) {
		return fmt.Errorf("reserved room slug %q", o.RoomSlug)
	}
	if o.HostUserID <= 0 {
		return errors.New("host user id must be a positive integer")
	}
	return nil
}

// validate additionally requires a non-empty room name (plan/up create the
// room).
func (o Options) validate() error {
	if err := o.validateIdentity(); err != nil {
		return err
	}
	if strings.TrimSpace(o.RoomName) == "" {
		return errors.New("room name is required")
	}
	return nil
}

// markerRecord mirrors the room_cutover_marker single row.
type markerRecord struct {
	RoomCutoverID  string
	TargetRoomSlug string
	TargetRoomID   int64
	HostUserID     int64
	SourceHashes   map[string]string
	TargetHashes   map[string]string
	LegacyIDOffset int64
	PreCommitAt    time.Time
	BuildSHA       string
}

// markerExpectation carries the caller-supplied identity a plan/up rerun
// must additionally match against the marker and the resolved target room.
// Verify passes nil: its identity is derived solely from the marker.
type markerExpectation struct {
	RoomSlug   string
	RoomName   string // empty = not supplied, not checked
	HostUserID int64
}

// Plan performs a read-only preview: it validates inputs, asserts schema
// readiness, host existence, and first-cutover readiness (target slug
// absent, room_activities empty, no play-history id overflow), honors an
// existing matching marker (reporting a fully verified no-op), and otherwise
// computes the source hashes plus the expected target hashes/counts a
// correct cutover would produce. It never writes.
func Plan(ctx context.Context, db *sql.DB, opts Options) (*Report, error) {
	report := newReport("plan", opts)
	report.DryRun = true
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if err := checkSchemaReady(db); err != nil {
		return nil, err
	}
	if err := resolveHostUser(ctx, db, opts.HostUserID); err != nil {
		return nil, err
	}

	if done, err := applyExistingMarker(ctx, db, opts, report); err != nil {
		return nil, err
	} else if done {
		report.Verified = true
		report.AddNote("already cut over; up would be a no-op")
		report.FinishedAt = opts.now()
		return report, nil
	}

	if err := checkFirstCutoverReadiness(ctx, db, opts.RoomSlug); err != nil {
		return nil, err
	}

	if err := fillExpectedEvidence(ctx, db, report); err != nil {
		return nil, err
	}
	report.AddNote("plan only: no data written")
	report.AddNote("target hashes/counts are the expected values derived from the validated source projection")
	report.FinishedAt = opts.now()
	return report, nil
}

// fillExpectedEvidence populates a read-only (plan / dry-run) report with the
// source hashes, the play-history offset, and the expected target evidence: a
// lossless cutover copies the canonical logical projection verbatim, so the
// expected target hashes equal the source hashes and the expected target
// counts equal the source counts.
func fillExpectedEvidence(ctx context.Context, q rowQueryer, report *Report) error {
	src, err := computeSourceHashes(ctx, q)
	if err != nil {
		return err
	}
	report.SourceHashes = src
	expected := make(map[string]string, len(src))
	for k, v := range src {
		expected[k] = v
	}
	report.TargetHashes = expected

	offset, err := currentPlayHistoryOffset(ctx, q)
	if err != nil {
		return err
	}
	report.LegacyIDOffset = offset

	tables, err := buildExpectedTableReports(ctx, q)
	if err != nil {
		return err
	}
	report.Tables = tables
	return nil
}

// Up executes the cutover. A dry run behaves like Plan but reports Mode "up".
// The write path holds the advisory lock on a pinned connection for the whole
// single transaction, following the ADR 003 §8 order: re-assert readiness,
// create room, create sole host membership, copy the four tables, insert the
// marker (timestamped immediately before insertion) before commit, verify
// within the transaction, commit (or roll back), release the lock — and then
// re-verify the committed state through the marker-derived verification
// routine before claiming Verified. A matching prior marker short-circuits to
// a fully verified no-op.
func Up(ctx context.Context, db *sql.DB, opts Options) (*Report, error) {
	report := newReport("up", opts)
	report.DryRun = opts.DryRun
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if err := checkSchemaReady(db); err != nil {
		return nil, err
	}
	if err := resolveHostUser(ctx, db, opts.HostUserID); err != nil {
		return nil, err
	}

	if done, err := applyExistingMarker(ctx, db, opts, report); err != nil {
		return nil, err
	} else if done {
		report.Verified = true
		report.AddNote("already cut over; no-op")
		report.FinishedAt = opts.now()
		return report, nil
	}

	if err := checkFirstCutoverReadiness(ctx, db, opts.RoomSlug); err != nil {
		return nil, err
	}

	buildSHA := resolveBuildSHA()
	report.BuildSHA = buildSHA

	if opts.DryRun {
		if err := fillExpectedEvidence(ctx, db, report); err != nil {
			return nil, err
		}
		report.AddNote("dry run: no data written")
		report.AddNote("target hashes/counts are the expected values derived from the validated source projection")
		report.FinishedAt = opts.now()
		return report, nil
	}

	if isPlaceholderBuildSHA(buildSHA) {
		return nil, fmt.Errorf("refusing to write cutover marker with placeholder build sha %q; build with -ldflags \"-X local-music-queue/internal/infrastructure/persistence/roomcutover.BuildSHA=<rev>\"", buildSHA)
	}

	// Write path: advisory lock on a pinned connection so the unlock runs on
	// the same session that acquired the lock.
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()

	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&locked); err != nil {
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	if !locked {
		return nil, errors.New("another cutover or migration is in progress (advisory lock held)")
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Re-assert first-cutover readiness inside the locked transaction: the
	// preflight check above ran before the lock was held, so a concurrent
	// writer could have taken the slug or written room_activities since.
	if err := checkFirstCutoverReadiness(ctx, tx, opts.RoomSlug); err != nil {
		return nil, err
	}

	createdAt := opts.now()
	cutoverID := uuid.NewString()

	roomID, err := insertRoom(ctx, tx, opts, createdAt)
	if err != nil {
		return nil, err
	}
	if err := insertHostMembership(ctx, tx, roomID, opts.HostUserID, createdAt); err != nil {
		return nil, err
	}

	// Compute the offset inside the tx so it is consistent with the rows
	// actually copied (any concurrent writer is excluded by the lock), and
	// reject bigint overflow before any play-history DML runs.
	offset, err := currentPlayHistoryOffset(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := checkPlayHistoryOverflow(ctx, tx, offset); err != nil {
		return nil, err
	}

	if err := copyQueueState(ctx, tx, roomID); err != nil {
		return nil, err
	}
	if err := copyActivities(ctx, tx, roomID); err != nil {
		return nil, err
	}
	if err := copyAutoQueueConfig(ctx, tx, roomID); err != nil {
		return nil, err
	}
	if err := copyPlayHistory(ctx, tx, roomID, offset); err != nil {
		return nil, err
	}
	if err := resyncSequences(ctx, tx); err != nil {
		return nil, err
	}

	if opts.FaultBeforeMarker != nil {
		if err := opts.FaultBeforeMarker(); err != nil {
			return nil, fmt.Errorf("fault before marker: %w", err)
		}
	}

	// Recompute both source and target hashes from the same in-transaction
	// snapshot so the pre-commit check compares a single consistent view.
	srcHashes, err := computeSourceHashes(ctx, tx)
	if err != nil {
		return nil, err
	}
	tgtHashes, err := computeTargetHashes(ctx, tx, roomID, offset)
	if err != nil {
		return nil, err
	}
	report.SourceHashes = srcHashes
	report.TargetHashes = tgtHashes

	// The marker audit timestamp is captured immediately before the marker
	// row is inserted, per the accepted audit contract.
	preCommitAt := opts.now()
	if err := insertMarker(ctx, tx, markerRecord{
		RoomCutoverID:  cutoverID,
		TargetRoomSlug: opts.RoomSlug,
		TargetRoomID:   roomID,
		HostUserID:     opts.HostUserID,
		SourceHashes:   srcHashes,
		TargetHashes:   tgtHashes,
		LegacyIDOffset: offset,
		PreCommitAt:    preCommitAt,
		BuildSHA:       buildSHA,
	}); err != nil {
		return nil, err
	}

	// Pre-commit verification within the same tx: any drift rolls back.
	if !hashesEqual(srcHashes, tgtHashes) {
		return nil, errors.New("pre-commit verification failed: source and target hashes differ")
	}

	if opts.FaultBeforeCommit != nil {
		if err := opts.FaultBeforeCommit(); err != nil {
			return nil, fmt.Errorf("fault before commit: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit cutover: %w", err)
	}
	committed = true

	if opts.TamperAfterCommit != nil {
		if err := opts.TamperAfterCommit(); err != nil {
			return nil, fmt.Errorf("tamper after commit: %w", err)
		}
	}

	// Post-commit verification: reread the durable marker from the committed
	// database and run the full marker-derived verification routine (room
	// resolution, slug/name/host identity, membership role, and recomputed
	// hash equality against the recorded values). Verified is only claimed
	// after this pass succeeds.
	m, err := readMarker(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("post-commit verification: %w", err)
	}
	if m == nil {
		return nil, errors.New("post-commit verification: cutover committed but no marker row is present")
	}
	srcCommitted, tgtCommitted, err := verifyMarker(ctx, db, m, &markerExpectation{
		RoomSlug:   opts.RoomSlug,
		RoomName:   opts.RoomName,
		HostUserID: opts.HostUserID,
	})
	report.SourceHashes = srcCommitted
	report.TargetHashes = tgtCommitted
	if err != nil {
		return nil, fmt.Errorf("cutover committed but post-commit verification failed: %w", err)
	}

	report.RoomCutoverID = cutoverID
	report.TargetRoomID = roomID
	report.LegacyIDOffset = offset
	report.Verified = true

	tables, err := buildTableReports(ctx, db, roomID)
	if err != nil {
		return nil, err
	}
	report.Tables = tables
	report.AddNote("cutover committed")
	report.AddNote("post-commit verification passed")
	report.FinishedAt = opts.now()
	return report, nil
}

// Verify re-reads the durable marker and runs the full marker-derived
// verification routine against the committed state. All identity is derived
// solely from the marker — the caller supplies no room slug, name, or host
// id. It requires a marker to be present and never writes.
func Verify(ctx context.Context, db *sql.DB, opts Options) (*Report, error) {
	report := newReport("verify", opts)
	report.DryRun = true
	if err := checkSchemaReady(db); err != nil {
		return nil, err
	}

	m, err := readMarker(ctx, db)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("no cutover marker present; nothing to verify")
	}

	report.RoomCutoverID = m.RoomCutoverID
	report.TargetRoomSlug = m.TargetRoomSlug
	report.TargetRoomID = m.TargetRoomID
	report.HostUserID = m.HostUserID
	report.LegacyIDOffset = m.LegacyIDOffset
	report.BuildSHA = m.BuildSHA
	report.AlreadyCutOver = true

	src, dst, err := verifyMarker(ctx, db, m, nil)
	report.SourceHashes = src
	report.TargetHashes = dst
	if err != nil {
		return nil, err
	}
	report.Verified = true

	tables, err := buildTableReports(ctx, db, m.TargetRoomID)
	if err != nil {
		return nil, err
	}
	report.Tables = tables
	report.AddNote("verify complete")
	report.FinishedAt = opts.now()
	return report, nil
}

// newReport seeds a Report with the fields common to every mode.
func newReport(mode string, opts Options) *Report {
	return &Report{
		Mode:           mode,
		StartedAt:      opts.now(),
		RedactedDSN:    opts.RedactedDSN,
		TargetRoomSlug: opts.RoomSlug,
		HostUserID:     opts.HostUserID,
	}
}

// applyExistingMarker checks for a durable marker. When one exists it must
// pass the full marker-derived verification routine — including the
// caller-supplied slug/name/host expectation and the freshly recomputed
// hashes — before the run short-circuits to a no-op (done=true). Any
// identity, membership, or hash drift returns an error. No marker returns
// done=false with no error.
func applyExistingMarker(ctx context.Context, db *sql.DB, opts Options, report *Report) (bool, error) {
	m, err := readMarker(ctx, db)
	if err != nil {
		return false, err
	}
	if m == nil {
		return false, nil
	}
	src, dst, err := verifyMarker(ctx, db, m, &markerExpectation{
		RoomSlug:   opts.RoomSlug,
		RoomName:   opts.RoomName,
		HostUserID: opts.HostUserID,
	})
	if err != nil {
		return false, err
	}
	report.AlreadyCutOver = true
	report.RoomCutoverID = m.RoomCutoverID
	report.TargetRoomID = m.TargetRoomID
	report.LegacyIDOffset = m.LegacyIDOffset
	report.BuildSHA = m.BuildSHA
	report.SourceHashes = src
	report.TargetHashes = dst
	return true, nil
}

// verifyMarker is the single marker-derived verification routine shared by
// Verify, the plan/up no-op path, and Up's post-commit pass. It validates:
//
//  1. the marker row's shape (uuid, slug, ids, hash maps, timestamp, sha);
//  2. that the marker's target_room_id resolves to a room whose current slug
//     still matches the marker;
//  3. the caller-supplied expectation, when present (plan/up reruns): slug,
//     host id, and — when supplied — the current room name;
//  4. that the marker's host user still holds the room's sole host
//     membership;
//  5. freshly recomputed source and target hashes against the recorded
//     marker hashes;
//  6. that source and target row counts still agree per table.
//
// It returns the recomputed hashes for reporting even on failure.
func verifyMarker(ctx context.Context, q rowQueryer, m *markerRecord, expect *markerExpectation) (src, dst map[string]string, err error) {
	if err := validateMarkerShape(m); err != nil {
		return nil, nil, err
	}

	// Resolve the target room from the marker's audit id.
	var roomSlug, roomName string
	rowErr := q.QueryRowContext(ctx,
		`SELECT slug, name FROM rooms WHERE id = $1`, m.TargetRoomID,
	).Scan(&roomSlug, &roomName)
	if rowErr == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("cutover marker target room id %d does not resolve to a room", m.TargetRoomID)
	}
	if rowErr != nil {
		return nil, nil, fmt.Errorf("resolve marker target room: %w", rowErr)
	}
	if roomSlug != m.TargetRoomSlug {
		return nil, nil, fmt.Errorf("cutover marker records room slug %q but room id %d now has slug %q", m.TargetRoomSlug, m.TargetRoomID, roomSlug)
	}

	if expect != nil {
		if m.TargetRoomSlug != expect.RoomSlug {
			return nil, nil, fmt.Errorf("cutover marker records room slug %q but %q was requested", m.TargetRoomSlug, expect.RoomSlug)
		}
		if m.HostUserID != expect.HostUserID {
			return nil, nil, fmt.Errorf("cutover marker records host user id %d but %d was requested", m.HostUserID, expect.HostUserID)
		}
		if expect.RoomName != "" && roomName != expect.RoomName {
			return nil, nil, fmt.Errorf("target room name is now %q but %q was requested (room name drift)", roomName, expect.RoomName)
		}
	}

	// The recorded host must still hold the room's sole host membership.
	var isHost bool
	if err := q.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM room_members
			WHERE room_id = $1 AND user_id = $2 AND role = 'host'
		)`, m.TargetRoomID, m.HostUserID).Scan(&isHost); err != nil {
		return nil, nil, fmt.Errorf("look up marker host membership: %w", err)
	}
	if !isHost {
		return nil, nil, fmt.Errorf("cutover marker host user id %d no longer holds the host membership of room %d", m.HostUserID, m.TargetRoomID)
	}

	src, err = computeSourceHashes(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	dst, err = computeTargetHashes(ctx, q, m.TargetRoomID, m.LegacyIDOffset)
	if err != nil {
		return src, nil, err
	}
	if !hashesEqual(src, m.SourceHashes) {
		return src, dst, errors.New("legacy source data changed since the recorded cutover (source hash drift)")
	}
	if !hashesEqual(dst, m.TargetHashes) {
		return src, dst, errors.New("target room data changed since the recorded cutover (target hash drift)")
	}

	tables, err := buildTableReports(ctx, q, m.TargetRoomID)
	if err != nil {
		return src, dst, err
	}
	for _, t := range tables {
		if t.SourceCount != t.TargetCount {
			return src, dst, fmt.Errorf("table %s count mismatch: source=%d target=%d", t.Table, t.SourceCount, t.TargetCount)
		}
	}
	return src, dst, nil
}

// validateMarkerShape asserts the durable marker row itself is well-formed
// before any of its fields drive verification.
func validateMarkerShape(m *markerRecord) error {
	if _, err := uuid.Parse(m.RoomCutoverID); err != nil {
		return fmt.Errorf("cutover marker has invalid room_cutover_id %q: %w", m.RoomCutoverID, err)
	}
	if !entity.IsValidSlug(m.TargetRoomSlug) {
		return fmt.Errorf("cutover marker has invalid target room slug %q", m.TargetRoomSlug)
	}
	if m.TargetRoomID <= 0 {
		return fmt.Errorf("cutover marker has non-positive target room id %d", m.TargetRoomID)
	}
	if m.HostUserID <= 0 {
		return fmt.Errorf("cutover marker has non-positive host user id %d", m.HostUserID)
	}
	if m.LegacyIDOffset < 0 {
		return fmt.Errorf("cutover marker has negative legacy id offset %d", m.LegacyIDOffset)
	}
	if m.PreCommitAt.IsZero() {
		return errors.New("cutover marker has a zero cutover_pre_commit_at")
	}
	if isPlaceholderBuildSHA(m.BuildSHA) {
		return fmt.Errorf("cutover marker records placeholder build sha %q", m.BuildSHA)
	}
	for _, hashes := range []map[string]string{m.SourceHashes, m.TargetHashes} {
		if len(hashes) != len(hashOrder) {
			return fmt.Errorf("cutover marker hash map has %d entries, want %d", len(hashes), len(hashOrder))
		}
		for _, k := range hashOrder {
			if len(hashes[k]) != 64 {
				return fmt.Errorf("cutover marker hash %q is malformed", k)
			}
		}
	}
	return nil
}

// checkFirstCutoverReadiness asserts the preconditions a first cutover (no
// marker yet) requires: the target slug must be unclaimed, the entire
// room_activities table must be empty (the copy preserves legacy activity
// ids verbatim, so any preexisting row could collide or interleave), and the
// play-history id shift must not overflow BIGINT. It is run by plan,
// dry-run, the up preflight, and again inside the locked transaction.
func checkFirstCutoverReadiness(ctx context.Context, q rowQueryer, slug string) error {
	var slugTaken bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM rooms WHERE slug = $1)`, slug,
	).Scan(&slugTaken); err != nil {
		return fmt.Errorf("check target slug: %w", err)
	}
	if slugTaken {
		return fmt.Errorf("target room slug %q already exists; choose an unused slug", slug)
	}

	activityRows, err := countRows(ctx, q, `SELECT COUNT(*) FROM room_activities`)
	if err != nil {
		return fmt.Errorf("check room_activities emptiness: %w", err)
	}
	if activityRows != 0 {
		return fmt.Errorf("room_activities already contains %d row(s); the first cutover requires an empty table so legacy activity ids are preserved without collision", activityRows)
	}

	offset, err := currentPlayHistoryOffset(ctx, q)
	if err != nil {
		return err
	}
	return checkPlayHistoryOverflow(ctx, q, offset)
}

// checkPlayHistoryOverflow rejects the cutover before any DML when shifting
// the highest legacy play_history id by offset would exceed BIGINT range.
func checkPlayHistoryOverflow(ctx context.Context, q rowQueryer, offset int64) error {
	var maxID sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(id) FROM play_history`).Scan(&maxID); err != nil {
		return fmt.Errorf("read max play_history id: %w", err)
	}
	if maxID.Valid && offset > math.MaxInt64-maxID.Int64 {
		return fmt.Errorf("play_history id %d plus legacy id offset %d would overflow BIGINT", maxID.Int64, offset)
	}
	return nil
}

// checkSchemaReady asserts the DB is reachable, non-dirty, and at exactly
// requiredSchemaVersion.
func checkSchemaReady(db *sql.DB) error {
	version, dirty, err := persistence.EmbeddedMigrationsVersion(db)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("schema is dirty at version %d; resolve the failed migration before cutover", version)
	}
	if version < requiredSchemaVersion {
		return fmt.Errorf("schema version %d is below the required %d; run migrations up first", version, requiredSchemaVersion)
	}
	if version > requiredSchemaVersion {
		return fmt.Errorf("schema version %d is above the supported %d; this cutover binary is stale", version, requiredSchemaVersion)
	}
	return nil
}

// resolveHostUser confirms the host user exists by primary key.
func resolveHostUser(ctx context.Context, q rowQueryer, userID int64) error {
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("look up host user: %w", err)
	}
	if !exists {
		return fmt.Errorf("host user id %d not found", userID)
	}
	return nil
}

// resolveBuildSHA returns the explicit BuildSHA override or falls back to the
// toolchain-embedded VCS revision.
func resolveBuildSHA() string {
	if BuildSHA != "" {
		return BuildSHA
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return ""
}

// isPlaceholderBuildSHA reports whether sha is empty or a well-known
// non-release placeholder that must never be recorded in a production marker.
func isPlaceholderBuildSHA(sha string) bool {
	switch strings.ToLower(strings.TrimSpace(sha)) {
	case "", "unknown", "dev", "devel":
		return true
	}
	return false
}

// readMarker returns the single room_cutover_marker row, or (nil, nil) when no
// marker has been written.
func readMarker(ctx context.Context, q rowQueryer) (*markerRecord, error) {
	var (
		m       markerRecord
		srcJSON []byte
		dstJSON []byte
	)
	err := q.QueryRowContext(ctx, `
		SELECT room_cutover_id::text, target_room_slug, target_room_id, host_user_id,
		       source_hashes, target_hashes, legacy_id_offset,
		       cutover_pre_commit_at, binary_build_sha
		FROM room_cutover_marker
		WHERE id = 1
	`).Scan(
		&m.RoomCutoverID, &m.TargetRoomSlug, &m.TargetRoomID, &m.HostUserID,
		&srcJSON, &dstJSON, &m.LegacyIDOffset, &m.PreCommitAt, &m.BuildSHA,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cutover marker: %w", err)
	}
	if err := json.Unmarshal(srcJSON, &m.SourceHashes); err != nil {
		return nil, fmt.Errorf("decode marker source_hashes: %w", err)
	}
	if err := json.Unmarshal(dstJSON, &m.TargetHashes); err != nil {
		return nil, fmt.Errorf("decode marker target_hashes: %w", err)
	}
	return &m, nil
}

// insertMarker writes the single-row idempotency marker inside the cutover
// transaction, before commit.
func insertMarker(ctx context.Context, tx *sql.Tx, m markerRecord) error {
	srcJSON, err := json.Marshal(m.SourceHashes)
	if err != nil {
		return fmt.Errorf("encode source_hashes: %w", err)
	}
	dstJSON, err := json.Marshal(m.TargetHashes)
	if err != nil {
		return fmt.Errorf("encode target_hashes: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO room_cutover_marker (
			id, room_cutover_id, target_room_slug, target_room_id, host_user_id,
			source_hashes, target_hashes, legacy_id_offset,
			cutover_pre_commit_at, binary_build_sha
		)
		VALUES (1, $1::uuid, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9)
	`,
		m.RoomCutoverID, m.TargetRoomSlug, m.TargetRoomID, m.HostUserID,
		srcJSON, dstJSON, m.LegacyIDOffset, m.PreCommitAt, m.BuildSHA,
	)
	if err != nil {
		return fmt.Errorf("insert cutover marker: %w", err)
	}
	return nil
}

// insertRoom creates the target room and returns its generated id.
func insertRoom(ctx context.Context, tx *sql.Tx, opts Options, at time.Time) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO rooms (slug, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		RETURNING id
	`, opts.RoomSlug, opts.RoomName, string(entity.RoomStatusActive), at).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert room %q: %w", opts.RoomSlug, err)
	}
	return id, nil
}

// insertHostMembership records the sole host membership. The one-host-per-room
// partial unique index enforces the invariant at the database level.
func insertHostMembership(ctx context.Context, tx *sql.Tx, roomID, hostUserID int64, at time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO room_members (room_id, user_id, role, joined_at)
		VALUES ($1, $2, $3, $4)
	`, roomID, hostUserID, string(entity.RoomRoleHost), at)
	if err != nil {
		return fmt.Errorf("insert host membership: %w", err)
	}
	return nil
}

// copyQueueState canonicalizes the legacy queue and writes it as the room's
// JSONB queue state. Storing the canonical projection means the target rehash
// equals the source hash.
func copyQueueState(ctx context.Context, tx *sql.Tx, roomID int64) error {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT data::text FROM queue_state WHERE id = 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return errors.New("legacy queue_state row (id=1) not found; nothing to cut over")
	}
	if err != nil {
		return fmt.Errorf("read source queue_state: %w", err)
	}
	canon, err := canonicalizeQueue(raw)
	if err != nil {
		return fmt.Errorf("canonicalize legacy queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_queue_state (room_id, data, updated_at)
		VALUES ($1, $2::jsonb, CURRENT_TIMESTAMP)
	`, roomID, canon); err != nil {
		return fmt.Errorf("insert room_queue_state: %w", err)
	}
	return nil
}

// copyActivities copies the legacy activity feed into the room, preserving the
// legacy id verbatim. room_activities is introduced in 0009 and has no runtime
// writer at schema 9, so it is empty and the explicit ids cannot collide.
func copyActivities(ctx context.Context, tx *sql.Tx, roomID int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_activities (id, room_id, "timestamp", type, "user", description)
		SELECT id, $1, "timestamp", type, "user", description
		FROM activities
		ORDER BY id
	`, roomID); err != nil {
		return fmt.Errorf("copy activities: %w", err)
	}
	return nil
}

// copyAutoQueueConfig copies the single legacy auto-queue config row into the
// room. The legacy row is seeded by 0001 and always present.
func copyAutoQueueConfig(ctx context.Context, tx *sql.Tx, roomID int64) error {
	var (
		enabled  bool
		strategy string
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT enabled, strategy FROM auto_queue_config WHERE id = 1`,
	).Scan(&enabled, &strategy); err != nil {
		return fmt.Errorf("read source auto_queue_config: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_auto_queue_config (room_id, enabled, strategy, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
	`, roomID, enabled, strategy); err != nil {
		return fmt.Errorf("insert room_auto_queue_config: %w", err)
	}
	return nil
}

// copyPlayHistory copies the legacy play history into the room, shifting each
// id by offset so it never collides with rows other rooms already own.
func copyPlayHistory(ctx context.Context, tx *sql.Tx, roomID, offset int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_play_history (id, room_id, video_id, title, played_at)
		SELECT id + $2::bigint, $1, video_id, title, played_at
		FROM play_history
		ORDER BY id
	`, roomID, offset); err != nil {
		return fmt.Errorf("copy play_history: %w", err)
	}
	return nil
}

// resyncSequences aligns the BIGSERIAL sequences of the two room tables the
// cutover inserts explicit ids into with the highest id actually written, so a
// later runtime INSERT does not collide. Empty tables are skipped. Uses
// setval(seq, max, is_called=true): the next nextval() yields max+1.
func resyncSequences(ctx context.Context, tx *sql.Tx) error {
	for _, t := range []string{"room_activities", "room_play_history"} {
		var maxID sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT MAX(id) FROM "+t).Scan(&maxID); err != nil {
			return fmt.Errorf("read max %s: %w", t, err)
		}
		if !maxID.Valid {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`SELECT setval(pg_get_serial_sequence($1, 'id'), $2, true)`,
			t, maxID.Int64,
		); err != nil {
			return fmt.Errorf("setval %s: %w", t, err)
		}
	}
	return nil
}
