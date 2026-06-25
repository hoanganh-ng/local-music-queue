package migratedata

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"
)

// WriteText renders the report as a human-readable table on w. The format is
// stable enough to grep for `mismatched`/`error` keywords in operator logs.
func (r *Report) WriteText(w io.Writer) error {
	if r == nil {
		return fmt.Errorf("nil report")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Migration report\t\n")
	fmt.Fprintf(tw, "  Started at:\t%s\n", r.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(tw, "  Finished at:\t%s\n", r.FinishedAt.Format(time.RFC3339))
	if r.SourcePath != "" {
		fmt.Fprintf(tw, "  Source path:\t%s\n", r.SourcePath)
	}
	fmt.Fprintf(tw, "  Postgres DSN:\t%s\n", r.RedactedDSN)
	if r.DryRun {
		fmt.Fprintf(tw, "  Mode:\tdry-run (no data written)\n")
	}
	if r.MigrationVerified {
		fmt.Fprintf(tw, "  Migration verified:\ttrue\n")
		fmt.Fprintf(tw, "  Remapped users:\t%d\n", r.RemappedUserCount)
	}
	if len(r.Notes) > 0 {
		fmt.Fprintf(tw, "  Notes:\n")
		for _, n := range r.Notes {
			fmt.Fprintf(tw, "    - %s\n", n)
		}
	}

	fmt.Fprintf(tw, "\n  Table\tSource\tTarget\tSourceMin\tSourceMax\tTargetMin\tTargetMax\n")
	for _, t := range r.Tables {
		fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\t%d\t%d\t%d\n",
			t.Table, t.SourceCount, t.TargetCount,
			t.SourceMinID, t.SourceMaxID,
			t.TargetMinID, t.TargetMaxID,
		)
		if t.Note != "" {
			fmt.Fprintf(tw, "      note: %s\n", t.Note)
		}
	}
	fmt.Fprintf(tw, "\n  queue_state.data byte length: source=%d target=%d\n",
		r.QueueState.Source, r.QueueState.Target)
	return tw.Flush()
}

// WriteJSON serializes the report to w.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteToFile writes the report as JSON to path. Existing files are
// truncated.
func (r *Report) WriteToFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()
	return r.WriteJSON(f)
}

// MergeSourceStats fills in SourceCount/SourceMinID/SourceMaxID on the
// post-commit TableReports from the pre-copy probe. The probe runs before
// the transaction starts; without this merge the post-commit report would
// only show target-side stats and operators would have to cross-reference
// the source counts manually.
func (r *Report) MergeSourceStats(probe []TableReport) {
	if r == nil || len(probe) == 0 {
		return
	}
	srcByTable := make(map[string]TableReport, len(probe))
	for _, p := range probe {
		srcByTable[p.Table] = p
	}
	for i := range r.Tables {
		p, ok := srcByTable[r.Tables[i].Table]
		if !ok {
			continue
		}
		r.Tables[i].SourceCount = p.SourceCount
		r.Tables[i].SourceMinID = p.SourceMinID
		r.Tables[i].SourceMaxID = p.SourceMaxID
	}
}

// AllTablesMatch returns true when every table's source count matches the
// target count. This is the operator-facing integrity predicate.
func (r *Report) AllTablesMatch() bool {
	if r == nil {
		return false
	}
	for _, t := range r.Tables {
		if t.SourceCount != t.TargetCount {
			return false
		}
	}
	return true
}

// MarkMismatched annotates every table whose source/target counts differ so
// the text report can flag them. Returns true if any mismatch was found.
func (r *Report) MarkMismatched() bool {
	if r == nil {
		return false
	}
	found := false
	for i := range r.Tables {
		if r.Tables[i].SourceCount != r.Tables[i].TargetCount {
			r.Tables[i].Note = "count mismatch"
			found = true
		}
	}
	return found
}

// AddNote appends a note to the report. Notes are operator-facing strings
// surfaced in both the text and JSON output.
func (r *Report) AddNote(note string) {
	if r == nil || note == "" {
		return
	}
	r.Notes = append(r.Notes, note)
}