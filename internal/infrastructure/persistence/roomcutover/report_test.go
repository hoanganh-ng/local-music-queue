package roomcutover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteToFileCreatesWithMode0600: a freshly created report file must be
// 0600 regardless of the process umask.
func TestWriteToFileCreatesWithMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	r := &Report{Mode: "plan"}
	if err := r.WriteToFile(path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat report: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("new report file mode = %04o, want 0600", perm)
	}
}

// TestWriteToFileForcesModeOnExistingFile: O_CREATE's mode argument applies
// only to newly created files, so truncating a preexisting 0644 report must
// still end at 0600 — the corrective Chmod is the regression target.
func TestWriteToFileForcesModeOnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte("stale world-readable report"), 0o644); err != nil {
		t.Fatalf("seed 0644 report: %v", err)
	}
	// Force the mode explicitly so a restrictive process umask cannot
	// invalidate the 0644 precondition.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod seeded report: %v", err)
	}
	pre, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat seeded report: %v", err)
	}
	if perm := pre.Mode().Perm(); perm != 0o644 {
		t.Fatalf("precondition: seeded file mode = %04o, want 0644", perm)
	}

	r := &Report{Mode: "verify", Verified: true}
	if err := r.WriteToFile(path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat report: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("existing report file mode = %04o, want 0600", perm)
	}
	// The stale content was truncated and replaced with valid report JSON.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var got Report
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("report content is not valid JSON: %v", err)
	}
	if got.Mode != "verify" || !got.Verified {
		t.Fatalf("report content mismatch: %+v", got)
	}
}
