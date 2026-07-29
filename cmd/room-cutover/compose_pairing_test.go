package main

// R14c deployment-pairing verification. These tests render the real
// repository docker-compose.yml with `docker compose config` in both
// cutover modes and prove the mode-qualified image references bind the
// already-built frontend artifact to the backend runtime mode:
//
//   - a false render selects local-music-queue-backend:false paired with
//     local-music-queue-frontend:false;
//   - a true render selects the :true pair;
//   - within a single render the backend and frontend image references
//     always carry the SAME mode suffix, so no command can silently
//     produce a true server with a false SPA (or vice versa).
//
// The tests are docker-gated: they skip ONLY when the docker CLI or the
// compose v2 plugin is unavailable, so they never fail on a host without
// Docker. When the tooling IS present, a configuration-rendering failure
// (a broken `docker compose config`) fails the test rather than skipping.
// They only inspect rendered configuration; they never build, pull, run,
// or deploy anything.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRootFromTest resolves the repository root relative to this test
// file (cmd/room-cutover/ -> ../../).
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate repository root")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docker-compose.yml")); err != nil {
		t.Fatalf("docker-compose.yml not found at %s: %v", root, err)
	}
	return root
}

// renderComposeImages runs `docker compose config --format json` at the
// repository root with ROOM_CUTOVER_AUTHORITATIVE=mode and returns the
// rendered backend and frontend image references. It skips (never fails)
// ONLY when Docker or the compose v2 plugin is unavailable; once the
// tooling is present, a configuration-rendering failure FAILS the test.
func renderComposeImages(t *testing.T, mode string) (backend, frontend string) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI unavailable: %v", err)
	}
	root := repoRootFromTest(t)

	// Probe the compose v2 plugin separately from the actual render. A
	// missing/unusable plugin is an environment limitation and skips; a
	// failure of `docker compose config` below is a real config problem
	// and must fail the test.
	if out, err := composeProbe(root); err != nil {
		t.Skipf("docker compose v2 plugin unavailable: %v\n%s", err, out)
	}

	cmd := exec.Command("docker", "compose", "config", "--format", "json")
	cmd.Dir = root
	// Start from the ambient environment (so compose can still read .env)
	// but force the single operator input to the mode under test.
	cmd.Env = append(os.Environ(), "ROOM_CUTOVER_AUTHORITATIVE="+mode)
	// Capture stdout (the JSON document) separately from stderr. Compose
	// writes interpolation warnings to stderr; merging them would corrupt
	// the JSON, and the rendered document may contain local .env values,
	// so it is never printed into the test log.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// The compose plugin is present (probe passed), so a failure here
		// is a genuine configuration-rendering problem and must fail the
		// suite. Report only stderr (variable-name warnings), never the
		// rendered config body.
		t.Fatalf("docker compose config failed (mode=%s): %v\nstderr: %s", mode, err, stderr.String())
	}

	var rendered struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rendered); err != nil {
		// Do NOT echo stdout: the rendered compose config can contain
		// local secrets/identities from .env.
		t.Fatalf("parse compose config JSON (mode=%s): %v", mode, err)
	}
	be, ok := rendered.Services["backend"]
	if !ok {
		t.Fatalf("mode=%s: rendered config has no backend service", mode)
	}
	fe, ok := rendered.Services["frontend"]
	if !ok {
		t.Fatalf("mode=%s: rendered config has no frontend service", mode)
	}
	return be.Image, fe.Image
}

// composeProbe reports whether the docker compose v2 plugin is usable by
// invoking `docker compose version` at the repository root. It returns the
// combined output alongside any error so callers can decide to skip (plugin
// absent) rather than fail. It never renders or reads the compose file.
func composeProbe(root string) ([]byte, error) {
	cmd := exec.Command("docker", "compose", "version")
	cmd.Dir = root
	return cmd.CombinedOutput()
}

// TestComposePairing_ModeQualifiedImages proves each mode selects a
// distinct, mode-matched backend+frontend image pair.
func TestComposePairing_ModeQualifiedImages(t *testing.T) {
	cases := []struct {
		mode         string
		wantBackend  string
		wantFrontend string
	}{
		{"false", "local-music-queue-backend:false", "local-music-queue-frontend:false"},
		{"true", "local-music-queue-backend:true", "local-music-queue-frontend:true"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			backend, frontend := renderComposeImages(t, tc.mode)
			if backend != tc.wantBackend {
				t.Errorf("mode=%s: backend image = %q, want %q", tc.mode, backend, tc.wantBackend)
			}
			if frontend != tc.wantFrontend {
				t.Errorf("mode=%s: frontend image = %q, want %q", tc.mode, frontend, tc.wantFrontend)
			}
			// The whole point of the pairing: both halves carry the same
			// mode tag, so a render can never mix a true server with a
			// false SPA.
			beTag := tagOf(backend)
			feTag := tagOf(frontend)
			if beTag != feTag {
				t.Errorf("mode=%s: backend tag %q != frontend tag %q (mixed pair)", tc.mode, beTag, feTag)
			}
			if beTag != tc.mode {
				t.Errorf("mode=%s: rendered tag %q does not match the operator input", tc.mode, beTag)
			}
		})
	}
}

// TestComposePairing_ModesAreDistinct proves the false and true renders
// select different image references, so building/selecting one mode does
// not silently reuse the other mode's pair.
func TestComposePairing_ModesAreDistinct(t *testing.T) {
	falseBackend, falseFrontend := renderComposeImages(t, "false")
	trueBackend, trueFrontend := renderComposeImages(t, "true")

	if falseBackend == trueBackend {
		t.Errorf("backend image reference is identical across modes (%q); the false rollback image would be unselectable", falseBackend)
	}
	if falseFrontend == trueFrontend {
		t.Errorf("frontend image reference is identical across modes (%q); the false rollback SPA would be unselectable", falseFrontend)
	}
}

// tagOf returns the tag portion of an "name:tag" image reference, or the
// empty string when no tag is present.
func tagOf(image string) string {
	for i := len(image) - 1; i >= 0; i-- {
		switch image[i] {
		case ':':
			return image[i+1:]
		case '/':
			return ""
		}
	}
	return ""
}
