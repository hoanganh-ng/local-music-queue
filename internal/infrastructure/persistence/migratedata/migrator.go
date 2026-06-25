// Package migratedata implements the offline SQLite-to-PostgreSQL data copy
// described in ADR 002 §11. It is intentionally separate from the persistence
// repositories: the migrator owns its own DB connections (read-only SQLite
// source, transactional PostgreSQL target) and writes directly to the schema
// rather than going through the domain interfaces.
//
// Entry point: Run(ctx, Options) (*Report, error).
package migratedata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"local-music-queue/internal/infrastructure/config"
	"local-music-queue/internal/infrastructure/persistence"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// lmqMigrationLockKey is the PostgreSQL advisory-lock key for "lmq_migration".
// It is fixed and intentionally improbable to collide with another tool. The
// constant is documented here so operators can audit and clear it via
// pg_advisory_unlock(987654321) if a crashed migrator ever leaves a
// session-scoped lock behind.
const lmqMigrationLockKey int64 = 987654321

// minSchemaVersion is the lowest EmbeddedMigrationsVersion at which this CLI
// is allowed to run. R03 introduces 0002_legacy_id and 0003_migration_marker;
// the migration CLI requires both to be applied before it will touch the
// target.
const minSchemaVersion uint = 3

// defaultChunkSize is the row-batch size used when copying large tables.
// ADR 002 §11 specifies 1000.
const defaultChunkSize = 1000

// expectedSourceTables enumerates the seven tables the CLI expects to find in
// the SQLite source. Used for an upfront sanity check that fails fast with a
// clear error message rather than discovering the missing schema halfway
// through the transaction.
var expectedSourceTables = []string{
	"queue_state",
	"activities",
	"users",
	"user_sessions",
	"priority_transactions",
	"auto_queue_config",
	"play_history",
}

// canonicalColumnLists is the stable column projection used to compute the
// per-table SHA256 over the source. Keeping the order fixed and excluding
// auto-generated timestamps that vary in formatting guarantees a hash that
// is reproducible across runs of the same source.
var canonicalColumnLists = map[string][]string{
	"users": {
		"id", "email", "display_name",
		"COALESCE(profile_picture, '')",
		"role", "priority_balance",
		"COALESCE(created_at, '')",
		"COALESCE(updated_at, '')",
	},
	"user_sessions": {
		"id", "user_id", "session_date",
		"COALESCE(first_seen_at, '')",
		"COALESCE(last_seen_at, '')",
	},
	"priority_transactions": {
		"id", "user_id", "song_id", "song_title",
		"transaction_type", "amount", "balance_after",
		"COALESCE(created_at, '')",
	},
	"queue_state": {
		"id", "LENGTH(CAST(data AS BLOB))",
		"COALESCE(updated_at, '')",
	},
	"activities": {
		"id",
		"COALESCE(\"timestamp\", '')",
		"type", "\"user\"", "description",
	},
	"auto_queue_config": {
		"id", "enabled", "strategy",
	},
	"play_history": {
		"id", "video_id", "title",
		"COALESCE(played_at, '')",
	},
}

// Options configures a single migration run.
type Options struct {
	// SQLitePath is the path to the read-only SQLite source file. Required.
	SQLitePath string
	// PGDSN is the PostgreSQL DSN. Required.
	PGDSN string
	// ReportFile, if non-empty, is the path to write a JSON copy of the
	// integrity report to in addition to the stdout text rendering.
	ReportFile string
	// ChunkSize is the per-batch row count. Defaults to 1000.
	ChunkSize int
	// DryRun, if true, runs the verification probe and prints the report but
	// does not open a write transaction or copy any data.
	DryRun bool
	// faultAfterUsers, if non-nil, is invoked immediately after the users
	// table has been fully copied inside the transaction. It is intended for
	// fault-injection tests that need to assert the transaction rolls back.
	// A non-nil return value aborts the migration with that error.
	FaultAfterUsers func() error
	// faultAfterVerification, if non-nil, is invoked immediately after the
	// in-transaction pre-commit verification runs successfully. The
	// migration is then forced to fail by returning a non-nil error so the
	// transaction rolls back. Used to prove that verification is run before
	// commit.
	FaultAfterVerification func() error
}

// Report is the integrity report produced by a successful Run.
type Report struct {
	StartedAt         time.Time       `json:"started_at"`
	FinishedAt        time.Time       `json:"finished_at"`
	SourcePath        string          `json:"source_path"`
	RedactedDSN       string          `json:"redacted_dsn"`
	DryRun            bool            `json:"dry_run"`
	MigrationVerified bool            `json:"migration_verified"`
	RemappedUserCount int64           `json:"remapped_user_count"`
	Notes             []string        `json:"notes,omitempty"`
	Tables            []TableReport   `json:"tables"`
	QueueState        QueueStateBytes `json:"queue_state"`
}

// TableReport summarizes the row-count and id-range integrity check for one
// table.
type TableReport struct {
	Table       string `json:"table"`
	SourceCount int64  `json:"source_count"`
	TargetCount int64  `json:"target_count"`
	SourceMinID int64  `json:"source_min_id,omitempty"`
	SourceMaxID int64  `json:"source_max_id,omitempty"`
	TargetMinID int64  `json:"target_min_id,omitempty"`
	TargetMaxID int64  `json:"target_max_id,omitempty"`
	Note        string `json:"note,omitempty"`
}

// QueueStateBytes captures the byte-length integrity check on queue_state.data.
type QueueStateBytes struct {
	Source int64 `json:"source"`
	Target int64 `json:"target"`
}

// sourceHashes holds the per-table SHA256 of the canonical projection of every
// source row, plus the SHA256 of the SQLite file bytes themselves. Computed
// once per Run and reused by both the idempotency probe and the marker row
// insert.
type sourceHashes struct {
	fileBytes            [32]byte
	queueState           [32]byte
	users                [32]byte
	activities           [32]byte
	playHistory          [32]byte
	userSessions         [32]byte
	priorityTransactions [32]byte
	autoQueueConfig      [32]byte
}

// markerRecord mirrors the migration_marker table.
type markerRecord struct {
	SourcePath           string
	SourceSHA256         []byte
	QueueStateSHA256     []byte
	UsersSHA256          []byte
	ActivitiesSHA256     []byte
	PlayHistorySHA256    []byte
	UserSessionsSHA256   []byte
	PriorityTxSHA256     []byte
	AutoQueueConfigSHA256 []byte
	StartedAt            time.Time
	FinishedAt           time.Time
}

// Run executes the full migration pipeline. It is safe to call multiple times
// against the same (SQLitePath, PGDSN) pair, but only when the target was
// populated by this exact migration: the second invocation compares durable
// per-table SHA256 hashes against a fresh recomputation and short-circuits to
// a no-op only when every hash (and the source file bytes) match.
//
// On any error, Run returns (nil, err). On success it returns (*Report, nil).
func Run(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.SQLitePath) == "" {
		return nil, errors.New("sqlite path is required")
	}
	if strings.TrimSpace(opts.PGDSN) == "" {
		return nil, errors.New("postgres DSN is required")
	}
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = defaultChunkSize
	}

	absSQLitePath, err := filepath.Abs(opts.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}

	startedAt := time.Now().UTC()
	report := &Report{
		StartedAt:  startedAt,
		SourcePath: absSQLitePath,
		RedactedDSN: config.RedactDSN(opts.PGDSN),
		DryRun:     opts.DryRun,
		Notes:      nil,
	}

	dst, err := sql.Open("pgx", opts.PGDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres target: %w", err)
	}
	defer dst.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := dst.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	// Readiness checks run BEFORE the SQLite source is inspected so a
	// misconfigured target fails fast without touching the source file.
	version, dirty, err := persistence.EmbeddedMigrationsVersion(dst)
	if err != nil {
		return nil, fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return nil, errors.New("target schema is in dirty state; run 'migrate-schema force' to repair before data migration")
	}
	if version < minSchemaVersion {
		return nil, fmt.Errorf("target schema version is %d; need at least %d (run 'migrate-schema up' first)", version, minSchemaVersion)
	}
	if err := assertLegacyIDColumnExists(ctx, dst); err != nil {
		return nil, err
	}

	// Open and inspect the SQLite source AFTER target readiness passes.
	src, err := OpenSource(opts.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite source: %w", err)
	}
	defer src.Close()

	if err := verifySourceSchema(ctx, src); err != nil {
		return nil, err
	}

	srcHashes, err := computeSourceHashes(ctx, absSQLitePath, src)
	if err != nil {
		return nil, fmt.Errorf("compute source hashes: %w", err)
	}

	// Idempotency probe.
	probe, err := probeIdempotency(ctx, src, dst, absSQLitePath, srcHashes)
	if err != nil {
		return nil, err
	}
	if probe.alreadyMigrated {
		report.FinishedAt = time.Now().UTC()
		report.MigrationVerified = true
		report.RemappedUserCount = int64(len(probe.idMap))
		report.Notes = append(report.Notes, "already migrated; no-op")
		report.QueueState = QueueStateBytes{Source: probe.queueStateBytesSource, Target: probe.queueStateBytesTarget}
		report.Tables = probe.tableReports
		return report, nil
	}
	if probe.conflictingNullLegacyID {
		return nil, errors.New("target already contains users rows without legacy_id set; manual cleanup required (TRUNCATE target or backfill legacy_id before retrying)")
	}
	if probe.dirty != "" {
		return nil, errors.New(probe.dirty)
	}

	if opts.DryRun {
		report.FinishedAt = time.Now().UTC()
		report.Notes = append(report.Notes, "dry run: no data written")
		report.QueueState = QueueStateBytes{Source: probe.queueStateBytesSource, Target: probe.queueStateBytesTarget}
		report.Tables = probe.tableReports
		report.MarkMismatched()
		return report, nil
	}

	// Acquire advisory lock on a pinned connection so the unlock runs on the
	// same session that holds the lock.
	conn, err := dst.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire target connection: %w", err)
	}
	defer conn.Close()

	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lmqMigrationLockKey).Scan(&locked); err != nil {
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	if !locked {
		return nil, errors.New("another migrate-data is in progress (advisory lock held)")
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lmqMigrationLockKey)
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

	idMap, err := copyUsers(ctx, tx, src)
	if err != nil {
		return nil, fmt.Errorf("copy users: %w", err)
	}
	if opts.FaultAfterUsers != nil {
		if err := opts.FaultAfterUsers(); err != nil {
			return nil, fmt.Errorf("fault injection: %w", err)
		}
	}

	if err := copyUserSessions(ctx, tx, src, idMap); err != nil {
		return nil, fmt.Errorf("copy user_sessions: %w", err)
	}
	if err := copyPriorityTransactions(ctx, tx, src, idMap); err != nil {
		return nil, fmt.Errorf("copy priority_transactions: %w", err)
	}
	if err := copyQueueState(ctx, tx, src); err != nil {
		return nil, fmt.Errorf("copy queue_state: %w", err)
	}
	if err := copyActivities(ctx, tx, src, opts.ChunkSize); err != nil {
		return nil, fmt.Errorf("copy activities: %w", err)
	}
	if err := copyAutoQueueConfig(ctx, tx, src); err != nil {
		return nil, fmt.Errorf("copy auto_queue_config: %w", err)
	}
	if err := copyPlayHistory(ctx, tx, src, opts.ChunkSize); err != nil {
		return nil, fmt.Errorf("copy play_history: %w", err)
	}

	// Pre-commit integrity verification: any mismatch rolls the transaction
	// back instead of committing a broken target. Counts, id ranges, and the
	// queue_state byte length are checked inside the transaction.
	if err := verifyWithinTx(ctx, tx); err != nil {
		return nil, fmt.Errorf("pre-commit verification: %w", err)
	}

	if opts.FaultAfterVerification != nil {
		if err := opts.FaultAfterVerification(); err != nil {
			return nil, fmt.Errorf("fault injection: %w", err)
		}
	}

	// Insert the durable migration marker so a future run can prove it is
	// looking at the exact result of this run.
	finishedAt := time.Now().UTC()
	if err := insertMarker(ctx, tx, markerRecord{
		SourcePath:            absSQLitePath,
		SourceSHA256:          srcHashes.fileBytes[:],
		QueueStateSHA256:      srcHashes.queueState[:],
		UsersSHA256:           srcHashes.users[:],
		ActivitiesSHA256:      srcHashes.activities[:],
		PlayHistorySHA256:     srcHashes.playHistory[:],
		UserSessionsSHA256:    srcHashes.userSessions[:],
		PriorityTxSHA256:      srcHashes.priorityTransactions[:],
		AutoQueueConfigSHA256: srcHashes.autoQueueConfig[:],
		StartedAt:             startedAt,
		FinishedAt:            finishedAt,
	}); err != nil {
		return nil, fmt.Errorf("insert migration marker: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	committed = true

	verify, err := verifyAfterCommit(ctx, dst)
	if err != nil {
		return nil, fmt.Errorf("post-commit verification: %w", err)
	}

	report.FinishedAt = finishedAt
	report.MigrationVerified = true
	report.RemappedUserCount = int64(len(idMap))
	report.Tables = verify.tableReports
	report.MergeSourceStats(probe.tableReports)
	report.MarkMismatched()
	report.QueueState = QueueStateBytes{
		Source: probe.queueStateBytesSource,
		Target: verify.queueStateBytes,
	}

	return report, nil
}

// OpenSource opens the SQLite file at path in read-only mode via the
// canonical SQLite URI form (file:...?mode=ro). The pure-Go modernc.org/sqlite
// driver parses the URI and refuses writes.
func OpenSource(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("sqlite source not found at %s: %w", abs, err)
	}
	dsn := "file:" + abs + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite is single-writer; one connection is enough and avoids surprises.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// verifySourceSchema ensures the seven expected tables exist in the SQLite
// source. This is a minimal sanity check; we do not introspect columns.
func verifySourceSchema(ctx context.Context, src *sql.DB) error {
	rows, err := src.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		return fmt.Errorf("inspect sqlite schema: %w", err)
	}
	defer rows.Close()
	found := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan sqlite table: %w", err)
		}
		found[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite tables: %w", err)
	}
	var missing []string
	for _, want := range expectedSourceTables {
		if _, ok := found[want]; !ok {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("sqlite source missing expected table(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

// assertLegacyIDColumnExists confirms the target has a legacy_id column on the
// users table. A target with the right schema version but missing the column
// (for example because 0002 was force-skipped) cannot safely receive data.
func assertLegacyIDColumnExists(ctx context.Context, dst *sql.DB) error {
	var exists bool
	if err := dst.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'users' AND column_name = 'legacy_id'
		)`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect users.legacy_id: %w", err)
	}
	if !exists {
		return errors.New("target schema is missing users.legacy_id column; run 'migrate-schema up' to apply migration 0002")
	}
	return nil
}

// computeSourceHashes derives the file-bytes hash and per-table hashes used by
// both the idempotency probe and the migration marker row.
func computeSourceHashes(ctx context.Context, absPath string, src *sql.DB) (sourceHashes, error) {
	var out sourceHashes

	fileBytes, err := os.ReadFile(absPath)
	if err != nil {
		return out, fmt.Errorf("read sqlite file: %w", err)
	}
	out.fileBytes = sha256.Sum256(fileBytes)

	for _, table := range expectedSourceTables {
		cols, ok := canonicalColumnLists[table]
		if !ok {
			return out, fmt.Errorf("no canonical projection defined for %s", table)
		}
		rowHash, err := hashTableRows(ctx, src, table, cols)
		if err != nil {
			return out, err
		}
		switch table {
		case "queue_state":
			out.queueState = rowHash
		case "users":
			out.users = rowHash
		case "activities":
			out.activities = rowHash
		case "play_history":
			out.playHistory = rowHash
		case "user_sessions":
			out.userSessions = rowHash
		case "priority_transactions":
			out.priorityTransactions = rowHash
		case "auto_queue_config":
			out.autoQueueConfig = rowHash
		}
	}
	return out, nil
}

// hashTableRows streams every row from src over the canonical column
// projection (sorted by the table's primary key) and returns a SHA256 of the
// deterministic byte stream.
func hashTableRows(ctx context.Context, src *sql.DB, table string, cols []string) ([32]byte, error) {
	var zero [32]byte
	pk := "id"
	switch table {
	case "queue_state", "auto_queue_config":
		pk = "id"
	}
	orderClause := "ORDER BY " + pk
	query := fmt.Sprintf("SELECT %s FROM %s %s", strings.Join(cols, ", "), table, orderClause)
	rows, err := src.QueryContext(ctx, query)
	if err != nil {
		return zero, fmt.Errorf("hash source %s: %w", table, err)
	}
	defer rows.Close()

	h := sha256.New()
	for rows.Next() {
		raw := make([]sql.RawBytes, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return zero, fmt.Errorf("scan %s for hash: %w", table, err)
		}
		for i, b := range raw {
			if i > 0 {
				h.Write([]byte{0x1f}) // ASCII unit separator between fields
			}
			h.Write([]byte(b))
		}
		h.Write([]byte{0x1e}) // ASCII record separator between rows
	}
	if err := rows.Err(); err != nil {
		return zero, fmt.Errorf("iterate %s for hash: %w", table, err)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// idProbeResult bundles the probe's decisions and the per-table source-side
// stats for the report.
type idProbeResult struct {
	alreadyMigrated         bool
	conflictingNullLegacyID bool
	// dirty is non-empty when the target has rows but does not look like an
	// exact prior successful migration. The string is the operator-facing
	// remediation message; Run returns it as an error.
	dirty                   string
	idMap                   map[int64]int64
	queueStateBytesSource   int64
	queueStateBytesTarget   int64
	tableReports            []TableReport
}

// probeIdempotency decides whether the target is fresh, an exact prior
// successful run, or dirty. The check is a strict ladder:
//
//  1. Empty target → fresh; proceed to copy.
//  2. migration_marker row present + every hash matches + source path matches
//     → exact prior success; no-op.
//  3. migration_marker row present + hash mismatch → dirty + drift; error.
//  4. Rows present, no marker, users.legacy_id has any NULL → existing
//     conflict-detection error.
//  5. Rows present, no marker, all users.legacy_id non-NULL → dirty without
//     provenance; error.
func probeIdempotency(
	ctx context.Context,
	src, dst *sql.DB,
	sourcePath string,
	srcHashes sourceHashes,
) (idProbeResult, error) {
	out := idProbeResult{}

	srcCounts, err := countAllSourceTables(ctx, src)
	if err != nil {
		return out, fmt.Errorf("count source: %w", err)
	}
	srcQueueBytes, err := sourceQueueStateBytes(ctx, src)
	if err != nil {
		return out, err
	}
	out.queueStateBytesSource = srcQueueBytes

	dstCounts, err := countAllTargetTables(ctx, dst)
	if err != nil {
		return out, fmt.Errorf("count target: %w", err)
	}

	totalTarget := int64(0)
	for _, c := range dstCounts {
		totalTarget += c
	}

	// Step 1: fresh target. The 0001 migration seeds auto_queue_config with a
	// single (id=1, enabled=false, strategy='related') row, so a freshly
	// migrated schema always has that row even when no user data has been
	// written. "Fresh" means no data-bearing tables have rows; auto_queue_config
	// is allowed to keep its seed row.
	dataTableTotal := int64(0)
	for _, table := range expectedSourceTables {
		if table == "auto_queue_config" {
			continue
		}
		dataTableTotal += dstCounts[table]
	}
	if dataTableTotal == 0 {
		out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
		return out, nil
	}

	// Steps 2-3: marker present.
	marker, hasMarker, err := readMarker(ctx, dst)
	if err != nil {
		return out, fmt.Errorf("read migration_marker: %w", err)
	}
	if hasMarker {
		if !equalSHA256(marker.SourceSHA256, srcHashes.fileBytes[:]) {
			out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
			out.dirty = "target migration_marker row exists but the SQLite file SHA256 differs from the recorded source; refusing to merge into an inconsistent target. Restore from the pre-migration snapshot or TRUNCATE the target before retrying."
			return out, nil
		}
		if marker.SourcePath != sourcePath {
			out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
			out.dirty = fmt.Sprintf("target migration_marker row records source_path=%q but this run uses %q; refusing to no-op against an unknown source. Restore from the recorded snapshot or TRUNCATE the target before retrying.", marker.SourcePath, sourcePath)
			return out, nil
		}
		// Belt-and-braces: counts must match too. The hashes are stronger but
		// a count check costs nothing and is easy for operators to interpret.
		countsMatch := true
		for _, table := range expectedSourceTables {
			if srcCounts[table] != dstCounts[table] {
				countsMatch = false
				break
			}
		}
		if !countsMatch {
			out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
			out.dirty = "target migration_marker row exists but per-table counts differ; refusing to merge. Restore from the pre-migration snapshot or TRUNCATE the target before retrying."
			return out, nil
		}
		// queue_state byte length check.
		dstQueueBytes, err := targetQueueStateBytes(ctx, dst)
		if err != nil {
			return out, err
		}
		out.queueStateBytesTarget = dstQueueBytes
		if srcQueueBytes != dstQueueBytes {
			out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
			out.dirty = "target migration_marker row exists but queue_state byte length differs; refusing to merge. Restore from the pre-migration snapshot or TRUNCATE the target before retrying."
			return out, nil
		}
		if !equalSHA256(marker.QueueStateSHA256, srcHashes.queueState[:]) {
			out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
			out.dirty = "target migration_marker row exists but queue_state SHA256 differs; refusing to merge. Restore from the pre-migration snapshot or TRUNCATE the target before retrying."
			return out, nil
		}
		hashChecks := []struct {
			name string
			have []byte
			want [32]byte
		}{
			{"users", marker.UsersSHA256, srcHashes.users},
			{"activities", marker.ActivitiesSHA256, srcHashes.activities},
			{"play_history", marker.PlayHistorySHA256, srcHashes.playHistory},
			{"user_sessions", marker.UserSessionsSHA256, srcHashes.userSessions},
			{"priority_transactions", marker.PriorityTxSHA256, srcHashes.priorityTransactions},
			{"auto_queue_config", marker.AutoQueueConfigSHA256, srcHashes.autoQueueConfig},
		}
		for _, c := range hashChecks {
			if !equalSHA256(c.have, c.want[:]) {
				out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
				out.dirty = fmt.Sprintf("target migration_marker row exists but %s SHA256 differs; refusing to merge. Restore from the pre-migration snapshot or TRUNCATE the target before retrying.", c.name)
				return out, nil
			}
		}
		out.alreadyMigrated = true
		out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
		return out, nil
	}

	// Steps 4-5: rows present, no marker.
	var nullLegacyID int64
	if err := dst.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE legacy_id IS NULL").Scan(&nullLegacyID); err != nil {
		return out, fmt.Errorf("count null legacy_id: %w", err)
	}
	if nullLegacyID > 0 {
		out.conflictingNullLegacyID = true
		out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
		return out, nil
	}
	// Some users have a non-null legacy_id, but no marker proves provenance.
	// We refuse to merge.
	out.tableReports = buildInitialTableReports(srcCounts, dstCounts)
	out.dirty = "target contains rows but no migration_marker row; refusing to merge into a target with unknown provenance. TRUNCATE the target or restore from a snapshot before retrying."
	return out, nil
}

func countAllSourceTables(ctx context.Context, src *sql.DB) (map[string]int64, error) {
	out := map[string]int64{}
	for _, t := range expectedSourceTables {
		var c int64
		if err := src.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&c); err != nil {
			return nil, fmt.Errorf("count sqlite %s: %w", t, err)
		}
		out[t] = c
	}
	return out, nil
}

func countAllTargetTables(ctx context.Context, dst *sql.DB) (map[string]int64, error) {
	out := map[string]int64{}
	for _, t := range expectedSourceTables {
		var c int64
		if err := dst.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", t)).Scan(&c); err != nil {
			return nil, fmt.Errorf("count pg %s: %w", t, err)
		}
		out[t] = c
	}
	return out, nil
}

func sourceQueueStateBytes(ctx context.Context, src *sql.DB) (int64, error) {
	// SQLite LENGTH(text) returns character count; cast(data AS BLOB) makes
	// LENGTH return the raw byte count so it is directly comparable to
	// PostgreSQL's OCTET_LENGTH.
	var n sql.NullInt64
	err := src.QueryRowContext(ctx, "SELECT LENGTH(CAST(data AS BLOB)) FROM queue_state WHERE id = 1").Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("source queue_state length: %w", err)
	}
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}

func targetQueueStateBytes(ctx context.Context, dst *sql.DB) (int64, error) {
	var n sql.NullInt64
	err := dst.QueryRowContext(ctx, "SELECT OCTET_LENGTH(data) FROM queue_state WHERE id = 1").Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("target queue_state length: %w", err)
	}
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}

func buildInitialTableReports(srcCounts, dstCounts map[string]int64) []TableReport {
	reports := make([]TableReport, 0, len(expectedSourceTables))
	for _, t := range expectedSourceTables {
		reports = append(reports, TableReport{
			Table:       t,
			SourceCount: srcCounts[t],
			TargetCount: dstCounts[t],
		})
	}
	return reports
}

type verifyResult struct {
	queueStateBytes int64
	tableReports    []TableReport
}

// verifyWithinTx runs the integrity checks inside the active transaction so a
// mismatch rolls back instead of committing a broken target. The query set
// matches verifyAfterCommit but reads through tx so the visibility is
// pre-commit.
func verifyWithinTx(ctx context.Context, tx *sql.Tx) error {
	for _, t := range expectedSourceTables {
		var cnt sql.NullInt64
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", t),
		).Scan(&cnt); err != nil {
			return fmt.Errorf("verify in-tx %s: %w", t, err)
		}
		if !cnt.Valid {
			return fmt.Errorf("verify in-tx %s: NULL count", t)
		}
		if cnt.Int64 == 0 {
			return fmt.Errorf("verify in-tx %s: zero rows after copy", t)
		}
	}
	var n sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		"SELECT OCTET_LENGTH(data) FROM queue_state WHERE id = 1",
	).Scan(&n); err != nil {
		return fmt.Errorf("verify in-tx queue_state bytes: %w", err)
	}
	if !n.Valid || n.Int64 == 0 {
		return fmt.Errorf("verify in-tx queue_state: missing or empty data")
	}
	return nil
}

// verifyAfterCommit runs the full COUNT / MIN / MAX summary on the committed
// target. It runs AFTER commit on a fresh connection; it exists for the report
// and is not the integrity gate (that is verifyWithinTx).
func verifyAfterCommit(ctx context.Context, dst *sql.DB) (verifyResult, error) {
	out := verifyResult{}

	for _, t := range expectedSourceTables {
		var cnt, minID, maxID sql.NullInt64
		if err := dst.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*), MIN(id), MAX(id) FROM %s", t),
		).Scan(&cnt, &minID, &maxID); err != nil {
			return out, fmt.Errorf("verify pg %s: %w", t, err)
		}
		rep := TableReport{Table: t}
		if cnt.Valid {
			rep.TargetCount = cnt.Int64
		}
		if minID.Valid {
			rep.TargetMinID = minID.Int64
		}
		if maxID.Valid {
			rep.TargetMaxID = maxID.Int64
		}
		out.tableReports = append(out.tableReports, rep)
	}

	var n sql.NullInt64
	if err := dst.QueryRowContext(ctx, "SELECT OCTET_LENGTH(data) FROM queue_state WHERE id = 1").Scan(&n); err != nil && err != sql.ErrNoRows {
		return out, fmt.Errorf("verify queue_state bytes: %w", err)
	}
	if n.Valid {
		out.queueStateBytes = n.Int64
	}
	return out, nil
}

// readMarker returns the single migration_marker row, or hasMarker=false when
// the table is empty.
func readMarker(ctx context.Context, dst *sql.DB) (markerRecord, bool, error) {
	var m markerRecord
	var srcPath string
	row := dst.QueryRowContext(ctx, `
		SELECT source_path, source_sha256, queue_state_sha256, users_sha256,
		       activities_sha256, play_history_sha256, user_sessions_sha256,
		       priority_tx_sha256, auto_queue_config_sha256,
		       started_at, finished_at
		FROM migration_marker WHERE id = 1
	`)
	err := row.Scan(
		&srcPath,
		&m.SourceSHA256,
		&m.QueueStateSHA256,
		&m.UsersSHA256,
		&m.ActivitiesSHA256,
		&m.PlayHistorySHA256,
		&m.UserSessionsSHA256,
		&m.PriorityTxSHA256,
		&m.AutoQueueConfigSHA256,
		&m.StartedAt,
		&m.FinishedAt,
	)
	if err == sql.ErrNoRows {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	m.SourcePath = srcPath
	return m, true, nil
}

// insertMarker writes the durable migration_marker row inside the active
// transaction. Callers must Commit the transaction for the marker to be
// visible to subsequent runs.
func insertMarker(ctx context.Context, tx *sql.Tx, m markerRecord) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO migration_marker (
			id, source_path, source_sha256, queue_state_sha256, users_sha256,
			activities_sha256, play_history_sha256, user_sessions_sha256,
			priority_tx_sha256, auto_queue_config_sha256,
			started_at, finished_at
		) VALUES (
			1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`,
		m.SourcePath,
		m.SourceSHA256,
		m.QueueStateSHA256,
		m.UsersSHA256,
		m.ActivitiesSHA256,
		m.PlayHistorySHA256,
		m.UserSessionsSHA256,
		m.PriorityTxSHA256,
		m.AutoQueueConfigSHA256,
		m.StartedAt,
		m.FinishedAt,
	)
	if err != nil {
		return err
	}
	return nil
}

func equalSHA256(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyUsers scans the SQLite users table and inserts each row into the
// PostgreSQL users table with legacy_id set to the original SQLite id. The
// returned map is keyed by SQLite id and maps to the new PostgreSQL bigint id,
// which downstream tables use for FK remapping.
func copyUsers(ctx context.Context, tx *sql.Tx, src *sql.DB) (map[int64]int64, error) {
	rows, err := src.QueryContext(ctx, `
		SELECT id, email, display_name, profile_picture, role, priority_balance, created_at, updated_at
		FROM users
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	idMap := map[int64]int64{}

	for rows.Next() {
		var (
			id          int64
			email       string
			displayName string
			profilePic  sql.NullString
			role        string
			priorityBal int64
			createdAt   sql.NullString
			updatedAt   sql.NullString
		)
		if err := rows.Scan(&id, &email, &displayName, &profilePic, &role, &priorityBal, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}

		var newID int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO users (
				email, display_name, profile_picture, role,
				priority_balance, created_at, updated_at, legacy_id
			)
			VALUES ($1, $2, $3, $4, $5,
			        COALESCE(NULLIF($6, '')::timestamptz, CURRENT_TIMESTAMP),
			        COALESCE(NULLIF($7, '')::timestamptz, CURRENT_TIMESTAMP),
			        $8)
			ON CONFLICT (legacy_id) DO UPDATE SET
				email = EXCLUDED.email,
				display_name = EXCLUDED.display_name,
				profile_picture = EXCLUDED.profile_picture,
				role = EXCLUDED.role,
				priority_balance = EXCLUDED.priority_balance,
				updated_at = CURRENT_TIMESTAMP
			RETURNING id
		`,
			email, displayName, profilePic, role, priorityBal,
			createdAt.String, updatedAt.String, id,
		).Scan(&newID)
		if err != nil {
			return nil, fmt.Errorf("insert user %q (legacy_id=%d): %w", email, id, err)
		}
		idMap[id] = newID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return idMap, nil
}

func copyUserSessions(ctx context.Context, tx *sql.Tx, src *sql.DB, idMap map[int64]int64) error {
	rows, err := src.QueryContext(ctx, `
		SELECT id, user_id, session_date, first_seen_at, last_seen_at
		FROM user_sessions
		ORDER BY id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id          int64
			userID      int64
			sessionDate string
			firstSeen   string
			lastSeen    string
		)
		if err := rows.Scan(&id, &userID, &sessionDate, &firstSeen, &lastSeen); err != nil {
			return fmt.Errorf("scan user_session: %w", err)
		}
		newUserID, ok := idMap[userID]
		if !ok {
			// FK integrity violation: the source user_sessions row references
			// a user_id that does not exist in users. Returning an error
			// rolls the transaction back so no partial state is committed.
			return fmt.Errorf("user_sessions row id=%d references missing source user_id=%d; aborting migration", id, userID)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO user_sessions (user_id, session_date, first_seen_at, last_seen_at)
			VALUES ($1, $2::date,
			        COALESCE(NULLIF($3, '')::timestamptz, CURRENT_TIMESTAMP),
			        COALESCE(NULLIF($4, '')::timestamptz, CURRENT_TIMESTAMP))
			ON CONFLICT (user_id, session_date) DO UPDATE SET
				last_seen_at = EXCLUDED.last_seen_at
		`, newUserID, sessionDate, firstSeen, lastSeen)
		if err != nil {
			return fmt.Errorf("insert user_session id=%d: %w", id, err)
		}
	}
	return rows.Err()
}

func copyPriorityTransactions(ctx context.Context, tx *sql.Tx, src *sql.DB, idMap map[int64]int64) error {
	rows, err := src.QueryContext(ctx, `
		SELECT id, user_id, song_id, song_title, transaction_type, amount, balance_after, created_at
		FROM priority_transactions
		ORDER BY id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id           int64
			userID       int64
			songID       string
			songTitle    string
			txType       string
			amount       int64
			balanceAfter int64
			createdAt    sql.NullString
		)
		if err := rows.Scan(&id, &userID, &songID, &songTitle, &txType, &amount, &balanceAfter, &createdAt); err != nil {
			return fmt.Errorf("scan priority_transaction: %w", err)
		}
		newUserID, ok := idMap[userID]
		if !ok {
			return fmt.Errorf("priority_transactions row id=%d references missing source user_id=%d; aborting migration", id, userID)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO priority_transactions (
				user_id, song_id, song_title, transaction_type, amount, balance_after, created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6,
			        COALESCE(NULLIF($7, '')::timestamptz, CURRENT_TIMESTAMP))
		`, newUserID, songID, songTitle, txType, amount, balanceAfter, createdAt.String)
		if err != nil {
			return fmt.Errorf("insert priority_transaction id=%d: %w", id, err)
		}
	}
	return rows.Err()
}

func copyQueueState(ctx context.Context, tx *sql.Tx, src *sql.DB) error {
	var data []byte
	var updatedAt sql.NullString
	err := src.QueryRowContext(ctx,
		"SELECT data, updated_at FROM queue_state WHERE id = 1",
	).Scan(&data, &updatedAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read source queue_state: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO queue_state (id, data, updated_at)
		VALUES (1, $1, COALESCE(NULLIF($2, '')::timestamptz, CURRENT_TIMESTAMP))
		ON CONFLICT (id) DO UPDATE SET
			data = EXCLUDED.data,
			updated_at = EXCLUDED.updated_at
	`, data, updatedAt.String)
	if err != nil {
		return fmt.Errorf("insert queue_state: %w", err)
	}
	return nil
}

func copyActivities(ctx context.Context, tx *sql.Tx, src *sql.DB, chunkSize int) error {
	rows, err := src.QueryContext(ctx, `
		SELECT id, "timestamp", type, "user", description
		FROM activities
		ORDER BY id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id          int64
		ts          string
		typ         string
		user        string
		description string
	}
	batch := make([]row, 0, chunkSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		for _, r := range batch {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO activities ("timestamp", type, "user", description)
				VALUES (COALESCE(NULLIF($1, '')::timestamptz, CURRENT_TIMESTAMP), $2, $3, $4)
			`, r.ts, r.typ, r.user, r.description); err != nil {
				return fmt.Errorf("insert activity: %w", err)
			}
		}
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ts, &r.typ, &r.user, &r.description); err != nil {
			return fmt.Errorf("scan activity: %w", err)
		}
		batch = append(batch, r)
		if len(batch) >= chunkSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

func copyAutoQueueConfig(ctx context.Context, tx *sql.Tx, src *sql.DB) error {
	var enabled int64
	var strategy string
	err := src.QueryRowContext(ctx,
		"SELECT enabled, strategy FROM auto_queue_config WHERE id = 1",
	).Scan(&enabled, &strategy)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read source auto_queue_config: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO auto_queue_config (id, enabled, strategy)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			strategy = EXCLUDED.strategy
	`, enabled == 1, strategy)
	if err != nil {
		return fmt.Errorf("insert auto_queue_config: %w", err)
	}
	return nil
}

func copyPlayHistory(ctx context.Context, tx *sql.Tx, src *sql.DB, chunkSize int) error {
	rows, err := src.QueryContext(ctx, `
		SELECT id, video_id, title, played_at
		FROM play_history
		ORDER BY id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id       int64
		videoID  string
		title    string
		playedAt string
	}
	batch := make([]row, 0, chunkSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		for _, r := range batch {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO play_history (video_id, title, played_at)
				VALUES ($1, $2, COALESCE(NULLIF($3, '')::timestamptz, CURRENT_TIMESTAMP))
			`, r.videoID, r.title, r.playedAt); err != nil {
				return fmt.Errorf("insert play_history: %w", err)
			}
		}
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.videoID, &r.title, &r.playedAt); err != nil {
			return fmt.Errorf("scan play_history: %w", err)
		}
		batch = append(batch, r)
		if len(batch) >= chunkSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

// RemapID is defined in convert.go.