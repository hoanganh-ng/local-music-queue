// Package roomcutover implements the offline legacy-global-state → room
// cutover described in ADR 003 (R14a contract) and Sprint 026 (R14b). It is
// intentionally separate from the runtime persistence repositories: the
// mechanism owns its own transactional PostgreSQL connection and writes
// directly to the schema rather than going through the domain interfaces.
//
// Entry points: Plan, Up, Verify. The CLI wrapper is cmd/room-cutover.
//
// This package draws on the R03 migratedata patterns (advisory lock on a
// pinned connection, single-transaction copy + pre-commit verification,
// durable idempotency marker, canonical SHA-256 integrity hashes) but does
// NOT reuse R03's cmd/migrate-data, migratedata package, or migration_marker
// table — those are preserved untouched.
package roomcutover

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"
)

// TableReport summarizes the row-count copy for one source→target table
// pair. It is intentionally PII-free: counts only, never row content.
type TableReport struct {
	Table       string `json:"table"`
	SourceCount int64  `json:"source_count"`
	TargetCount int64  `json:"target_count"`
}

// Report is the PII-safe integrity report produced by Plan / Up / Verify.
// It carries only opaque hashes, counts, identity fields, and the redacted
// DSN — never song titles, user display names, activity descriptions, video
// titles, or DSN credentials.
type Report struct {
	Mode           string            `json:"mode"` // plan | up | verify
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     time.Time         `json:"finished_at"`
	RedactedDSN    string            `json:"redacted_dsn"`
	DryRun         bool              `json:"dry_run"`
	AlreadyCutOver bool              `json:"already_cut_over"`
	Verified       bool              `json:"verified"`
	RoomCutoverID  string            `json:"room_cutover_id,omitempty"`
	TargetRoomSlug string            `json:"target_room_slug,omitempty"`
	TargetRoomID   int64             `json:"target_room_id,omitempty"`
	HostUserID     int64             `json:"host_user_id,omitempty"`
	LegacyIDOffset int64             `json:"legacy_id_offset"`
	BuildSHA       string            `json:"binary_build_sha,omitempty"`
	SourceHashes   map[string]string `json:"source_hashes,omitempty"`
	TargetHashes   map[string]string `json:"target_hashes,omitempty"`
	Tables         []TableReport     `json:"tables,omitempty"`
	Notes          []string          `json:"notes,omitempty"`
}

// AddNote appends an operator-facing note surfaced in both the text and JSON
// renderings. Empty notes are ignored.
func (r *Report) AddNote(note string) {
	if r == nil || note == "" {
		return
	}
	r.Notes = append(r.Notes, note)
}

// hashOrder is the stable key order used when rendering the hash maps so the
// text output is deterministic.
var hashOrder = []string{
	hashKeyQueueState,
	hashKeyActivities,
	hashKeyAutoQueueConfig,
	hashKeyPlayHistory,
}

// WriteText renders the report as a human-readable table on w. The format is
// stable enough to grep for keywords ("already cut over", "verified",
// "mismatch") in operator logs.
func (r *Report) WriteText(w io.Writer) error {
	if r == nil {
		return fmt.Errorf("nil report")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Room-cutover report\t\n")
	fmt.Fprintf(tw, "  Mode:\t%s\n", r.Mode)
	fmt.Fprintf(tw, "  Started at:\t%s\n", r.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(tw, "  Finished at:\t%s\n", r.FinishedAt.Format(time.RFC3339))
	fmt.Fprintf(tw, "  Postgres DSN:\t%s\n", r.RedactedDSN)
	if r.DryRun {
		fmt.Fprintf(tw, "  Mode detail:\tdry-run (no data written)\n")
	}
	if r.AlreadyCutOver {
		fmt.Fprintf(tw, "  Already cut over:\ttrue\n")
	}
	if r.Verified {
		fmt.Fprintf(tw, "  Verified:\ttrue\n")
	}
	if r.TargetRoomSlug != "" {
		fmt.Fprintf(tw, "  Target room slug:\t%s\n", r.TargetRoomSlug)
	}
	if r.TargetRoomID != 0 {
		fmt.Fprintf(tw, "  Target room id:\t%d\n", r.TargetRoomID)
	}
	if r.HostUserID != 0 {
		fmt.Fprintf(tw, "  Host user id:\t%d\n", r.HostUserID)
	}
	if r.RoomCutoverID != "" {
		fmt.Fprintf(tw, "  Room cutover id:\t%s\n", r.RoomCutoverID)
	}
	fmt.Fprintf(tw, "  Legacy id offset:\t%d\n", r.LegacyIDOffset)
	if r.BuildSHA != "" {
		fmt.Fprintf(tw, "  Binary build sha:\t%s\n", r.BuildSHA)
	}
	if len(r.Notes) > 0 {
		fmt.Fprintf(tw, "  Notes:\n")
		for _, n := range r.Notes {
			fmt.Fprintf(tw, "    - %s\n", n)
		}
	}

	if len(r.Tables) > 0 {
		fmt.Fprintf(tw, "\n  Table\tSource\tTarget\n")
		for _, t := range r.Tables {
			note := ""
			if t.SourceCount != t.TargetCount {
				note = "\t(count mismatch)"
			}
			fmt.Fprintf(tw, "  %s\t%d\t%d%s\n", t.Table, t.SourceCount, t.TargetCount, note)
		}
	}

	if len(r.SourceHashes) > 0 || len(r.TargetHashes) > 0 {
		fmt.Fprintf(tw, "\n  Hash\tSource\tTarget\n")
		for _, k := range hashOrder {
			src := r.SourceHashes[k]
			dst := r.TargetHashes[k]
			match := ""
			if src != "" && dst != "" && src != dst {
				match = "\t(mismatch)"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s%s\n", k, shortHash(src), shortHash(dst), match)
		}
	}
	return tw.Flush()
}

// shortHash renders a hex hash compactly for the text table (first 12 chars),
// keeping the full value in the JSON output. Empty stays empty.
func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12] + "…"
}

// WriteJSON serializes the full report (full-length hashes) to w.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteToFile writes the report as JSON to path. Existing files are
// truncated. The file mode is forced to 0600 explicitly (not left to the
// process umask): the report carries integrity hashes and identity fields
// operators may not want group/world readable. O_CREATE's mode argument
// applies only when the file is newly created — truncating a preexisting
// file keeps its old permissions — so the mode is additionally enforced via
// Chmod on the opened handle. An empty path is a no-op.
func (r *Report) WriteToFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("set report file permissions: %w", err)
	}
	return r.WriteJSON(f)
}
