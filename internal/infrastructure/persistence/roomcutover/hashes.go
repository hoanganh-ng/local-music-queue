package roomcutover

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"local-music-queue/internal/domain/entity"
)

// Hash-map keys. These identify the four source→target table families the
// cutover copies and verifies; they are the exact JSONB object keys stored in
// room_cutover_marker.source_hashes / target_hashes and rendered in the report.
const (
	hashKeyQueueState      = "queue_state"
	hashKeyActivities      = "activities"
	hashKeyAutoQueueConfig = "auto_queue_config"
	hashKeyPlayHistory     = "play_history"
)

// fieldSep and recordSep separate projected columns and rows in the hashed
// byte stream. They mirror the R03 migrator's canonical stream so the hashing
// is order- and content-sensitive but insensitive to SQL text formatting.
const (
	fieldSep  = 0x1f // ASCII unit separator between fields
	recordSep = 0x1e // ASCII record separator between rows
)

// rowQueryer is the read surface shared by *sql.DB, *sql.Tx and *sql.Conn so
// the hash and count helpers run unchanged against either a pre-transaction
// snapshot or the in-transaction target rows.
type rowQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// computeSourceHashes hashes the four legacy global-state tables into the
// canonical map keyed by the hashKey* constants. queue_state is hashed by its
// validated logical projection (see canonicalizeQueue); the other three are
// hashed row-by-row over a deterministic ORDER BY id / single-row projection.
func computeSourceHashes(ctx context.Context, q rowQueryer) (map[string]string, error) {
	out := make(map[string]string, 4)

	var queueRaw []byte
	err := q.QueryRowContext(ctx, `SELECT data::text FROM queue_state WHERE id = 1`).Scan(&queueRaw)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("legacy queue_state row (id=1) not found; nothing to cut over")
	}
	if err != nil {
		return nil, fmt.Errorf("read source queue_state: %w", err)
	}
	if out[hashKeyQueueState], err = hashQueueJSON(queueRaw); err != nil {
		return nil, fmt.Errorf("hash source queue_state: %w", err)
	}

	if out[hashKeyActivities], err = hashRows(ctx, q, `
		SELECT id, "timestamp", type, "user", description
		FROM activities
		ORDER BY id`); err != nil {
		return nil, fmt.Errorf("hash source activities: %w", err)
	}

	if out[hashKeyAutoQueueConfig], err = hashRows(ctx, q, `
		SELECT enabled, strategy FROM auto_queue_config WHERE id = 1`); err != nil {
		return nil, fmt.Errorf("hash source auto_queue_config: %w", err)
	}

	if out[hashKeyPlayHistory], err = hashRows(ctx, q, `
		SELECT id, video_id, title
		FROM play_history
		ORDER BY id`); err != nil {
		return nil, fmt.Errorf("hash source play_history: %w", err)
	}
	return out, nil
}

// computeTargetHashes hashes the copied room-scoped rows for roomID. The
// projections are normalized to re-derive the legacy id space so a correct
// copy produces hashes identical to computeSourceHashes:
//
//   - activities ids are preserved verbatim on copy (room_activities is new
//     and empty at cutover), so no adjustment is needed.
//   - play_history ids are shifted by offset on copy to avoid colliding with
//     rows other rooms may already own, so the projection subtracts offset.
func computeTargetHashes(ctx context.Context, q rowQueryer, roomID, offset int64) (map[string]string, error) {
	out := make(map[string]string, 4)

	var queueRaw []byte
	err := q.QueryRowContext(ctx, `SELECT data::text FROM room_queue_state WHERE room_id = $1`, roomID).Scan(&queueRaw)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("target room_queue_state row (room_id=%d) not found", roomID)
	}
	if err != nil {
		return nil, fmt.Errorf("read target room_queue_state: %w", err)
	}
	if out[hashKeyQueueState], err = hashQueueJSON(queueRaw); err != nil {
		return nil, fmt.Errorf("hash target room_queue_state: %w", err)
	}

	if out[hashKeyActivities], err = hashRows(ctx, q, `
		SELECT id, "timestamp", type, "user", description
		FROM room_activities
		WHERE room_id = $1
		ORDER BY id`, roomID); err != nil {
		return nil, fmt.Errorf("hash target room_activities: %w", err)
	}

	if out[hashKeyAutoQueueConfig], err = hashRows(ctx, q, `
		SELECT enabled, strategy FROM room_auto_queue_config WHERE room_id = $1`, roomID); err != nil {
		return nil, fmt.Errorf("hash target room_auto_queue_config: %w", err)
	}

	if out[hashKeyPlayHistory], err = hashRows(ctx, q, `
		SELECT id - $2::bigint, video_id, title
		FROM room_play_history
		WHERE room_id = $1
		ORDER BY id`, roomID, offset); err != nil {
		return nil, fmt.Errorf("hash target room_play_history: %w", err)
	}
	return out, nil
}

// hashRows streams the projected rows and folds each into a SHA-256 using the
// field/record separators. All projected columns are read as sql.RawBytes so
// the exact stored bytes participate in the hash regardless of Go type. Both
// source and target are the same PostgreSQL session, so timestamptz/boolean
// text encodings are identical across the pair.
func hashRows(ctx context.Context, q rowQueryer, query string, args ...any) (string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for rows.Next() {
		raw := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		for i, b := range raw {
			if i > 0 {
				h.Write([]byte{fieldSep})
			}
			h.Write([]byte(b))
		}
		h.Write([]byte{recordSep})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashQueueJSON canonicalizes raw queue JSON and returns the lowercase hex
// SHA-256 of the canonical bytes.
func hashQueueJSON(raw []byte) (string, error) {
	canon, err := canonicalizeQueue(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalizeQueue decodes raw queue JSON (the source stores it as TEXT, the
// target as JSONB) into an entity.Queue, asserts the domain invariants,
// normalizes History timestamps to UTC, and re-encodes to canonical bytes.
// Because JSONB reorders keys and strips whitespace, a raw byte comparison is
// meaningless; hashing the canonical re-encoding makes source and target
// comparable and rejects a corrupt or invariant-violating legacy queue before
// it lands in a room.
func canonicalizeQueue(raw []byte) ([]byte, error) {
	var q entity.Queue
	if err := json.Unmarshal(raw, &q); err != nil {
		return nil, fmt.Errorf("decode queue json: %w", err)
	}
	if err := validateQueue(&q); err != nil {
		return nil, err
	}
	for i := range q.History {
		q.History[i].Timestamp = q.History[i].Timestamp.UTC()
	}
	canon, err := json.Marshal(&q)
	if err != nil {
		return nil, fmt.Errorf("encode canonical queue: %w", err)
	}
	return canon, nil
}

// validateQueue asserts the entity.Queue invariants the runtime relies on:
// a recognized status, non-negative elapsed, and the empty/non-empty index
// contract (empty ⇒ index -1 / idle / elapsed 0; non-empty ⇒ index in range
// and not idle).
func validateQueue(q *entity.Queue) error {
	switch q.Status {
	case entity.StatusPlaying, entity.StatusPaused, entity.StatusIdle:
	default:
		return fmt.Errorf("queue invariant: invalid status %q", q.Status)
	}
	if q.Elapsed < 0 {
		return fmt.Errorf("queue invariant: negative elapsed %d", q.Elapsed)
	}
	if len(q.Songs) == 0 {
		if q.CurrentIndex != -1 {
			return fmt.Errorf("queue invariant: empty queue must have current_index -1, got %d", q.CurrentIndex)
		}
		if q.Status != entity.StatusIdle {
			return fmt.Errorf("queue invariant: empty queue must be idle, got %q", q.Status)
		}
		if q.Elapsed != 0 {
			return fmt.Errorf("queue invariant: empty queue must have elapsed 0, got %d", q.Elapsed)
		}
		return nil
	}
	if q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
		return fmt.Errorf("queue invariant: current_index %d out of range [0,%d)", q.CurrentIndex, len(q.Songs))
	}
	if q.Status == entity.StatusIdle {
		return fmt.Errorf("queue invariant: non-empty queue must not be idle")
	}
	return nil
}

// hashesEqual reports whether two hash maps hold identical entries.
func hashesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// countRows runs a single-value COUNT(*) query.
func countRows(ctx context.Context, q rowQueryer, query string, args ...any) (int64, error) {
	var n int64
	if err := q.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// buildTableReports produces the PII-free per-table source/target row counts.
// A roomID of 0 (no target room yet, e.g. plan / dry-run) yields target counts
// of 0 without erroring.
func buildTableReports(ctx context.Context, q rowQueryer, roomID int64) ([]TableReport, error) {
	specs := []struct {
		table  string
		srcSQL string
		dstSQL string
	}{
		{hashKeyQueueState, `SELECT COUNT(*) FROM queue_state WHERE id = 1`, `SELECT COUNT(*) FROM room_queue_state WHERE room_id = $1`},
		{hashKeyActivities, `SELECT COUNT(*) FROM activities`, `SELECT COUNT(*) FROM room_activities WHERE room_id = $1`},
		{hashKeyAutoQueueConfig, `SELECT COUNT(*) FROM auto_queue_config WHERE id = 1`, `SELECT COUNT(*) FROM room_auto_queue_config WHERE room_id = $1`},
		{hashKeyPlayHistory, `SELECT COUNT(*) FROM play_history`, `SELECT COUNT(*) FROM room_play_history WHERE room_id = $1`},
	}
	reports := make([]TableReport, 0, len(specs))
	for _, s := range specs {
		src, err := countRows(ctx, q, s.srcSQL)
		if err != nil {
			return nil, fmt.Errorf("count source %s: %w", s.table, err)
		}
		dst, err := countRows(ctx, q, s.dstSQL, roomID)
		if err != nil {
			return nil, fmt.Errorf("count target %s: %w", s.table, err)
		}
		reports = append(reports, TableReport{Table: s.table, SourceCount: src, TargetCount: dst})
	}
	return reports, nil
}

// currentPlayHistoryOffset returns COALESCE(MAX(id), 0) over the whole
// room_play_history table. play_history ids are shifted by this offset on copy
// so a cutover never collides with rows other rooms already own.
func currentPlayHistoryOffset(ctx context.Context, q rowQueryer) (int64, error) {
	var offset int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM room_play_history`).Scan(&offset); err != nil {
		return 0, fmt.Errorf("compute play_history offset: %w", err)
	}
	return offset, nil
}
