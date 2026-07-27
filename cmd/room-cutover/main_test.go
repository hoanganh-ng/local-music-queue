package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/infrastructure/persistence"
)

// TestResolveDSN pins the DSN precedence: explicit --postgres override wins,
// then $DATABASE_URL, then $MIGRATE_DATABASE_URL, else empty.
func TestResolveDSN(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://env/database")
		t.Setenv("MIGRATE_DATABASE_URL", "postgres://env/migrate")
		if got := resolveDSN("postgres://flag/override"); got != "postgres://flag/override" {
			t.Fatalf("override precedence: got %q", got)
		}
	})
	t.Run("database_url next", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://env/database")
		t.Setenv("MIGRATE_DATABASE_URL", "postgres://env/migrate")
		if got := resolveDSN(""); got != "postgres://env/database" {
			t.Fatalf("DATABASE_URL precedence: got %q", got)
		}
	})
	t.Run("migrate_database_url fallback", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("MIGRATE_DATABASE_URL", "postgres://env/migrate")
		if got := resolveDSN(""); got != "postgres://env/migrate" {
			t.Fatalf("MIGRATE_DATABASE_URL fallback: got %q", got)
		}
	})
	t.Run("empty when unset", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("MIGRATE_DATABASE_URL", "")
		if got := resolveDSN(""); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})
}

// --- subprocess-based end-to-end CLI tests -------------------------------

var (
	buildOnce sync.Once
	buildBin  string
	buildErr  error
)

// binaryPath builds the room-cutover binary once per test run with an injected
// non-placeholder BuildSHA so the `up` production guard is satisfied.
func binaryPath(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		// Place the binary in the OS temp root (not t.TempDir, which is cleaned
		// when the first caller's test ends) so it survives across sub-tests;
		// TestMain removes it.
		bin := filepath.Join(os.TempDir(), fmt.Sprintf("room-cutover-clitest-%d", os.Getpid()))
		ld := "-X local-music-queue/internal/infrastructure/persistence/roomcutover.BuildSHA=clitest-0123456789ab"
		cmd := exec.Command("go", "build", "-ldflags", ld, "-o", bin, ".")
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
			return
		}
		buildBin = bin
	})
	if buildErr != nil {
		t.Fatalf("build binary: %v", buildErr)
	}
	return buildBin
}

func TestMain(m *testing.M) {
	code := m.Run()
	if buildBin != "" {
		_ = os.Remove(buildBin)
	}
	os.Exit(code)
}

// runCLI executes the built binary with args and env, returning combined
// output and the exit code.
func runCLI(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath(t), args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("run CLI: %v\n%s", err, out)
	return "", -1
}

// scopedCLIDSN provisions a throwaway schema with migrations applied, seeds a
// representative legacy state, and returns the connectable DSN (search_path
// scoped) plus the seeded host user id. Skips when PG is unreachable.
func scopedCLIDSN(t *testing.T) (dsn string, hostID int64) {
	t.Helper()
	base := os.Getenv("LMQ_TEST_DATABASE_URL")
	if base == "" {
		base = "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable"
	}

	probe, err := sql.Open("pgx", base)
	if err != nil {
		t.Skipf("postgres unavailable (open): %v", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	pingErr := probe.PingContext(pingCtx)
	cancel()
	_ = probe.Close()
	if pingErr != nil {
		t.Skipf("postgres unavailable (ping): %v", pingErr)
	}

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	schema := fmt.Sprintf("lmq_cli_test_%d_%d", time.Now().UnixNano(), runtime.NumCPU()*1000+os.Getpid())
	dsn = base + sep + "search_path=" + schema

	scoped, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open scoped: %v", err)
	}
	if _, err := scoped.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = scoped.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		drop, err := sql.Open("pgx", base)
		if err == nil {
			_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
			_ = drop.Close()
		}
		_ = scoped.Close()
	})
	if err := persistence.RunEmbeddedMigrationsUp(scoped); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	ctx := context.Background()
	if err := scoped.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name, role, priority_balance)
		VALUES ('cli-host@example.test', 'CLI Host', 'user', 0)
		RETURNING id
	`).Scan(&hostID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := scoped.ExecContext(ctx, `
		INSERT INTO queue_state (id, data) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data
	`, `{"songs":[],"current_index":-1,"status":"idle","elapsed":0,"history":[]}`); err != nil {
		t.Fatalf("seed queue_state: %v", err)
	}
	if _, err := scoped.ExecContext(ctx, `
		INSERT INTO activities ("timestamp", type, "user", description)
		VALUES (now(), 'song_added', 'alice', 'added one')
	`); err != nil {
		t.Fatalf("seed activity: %v", err)
	}
	if _, err := scoped.ExecContext(ctx, `UPDATE auto_queue_config SET enabled = true, strategy = 'related' WHERE id = 1`); err != nil {
		t.Fatalf("seed auto_queue_config: %v", err)
	}
	if _, err := scoped.ExecContext(ctx, `
		INSERT INTO play_history (video_id, title, played_at) VALUES ('vid1', 'Song One', now())
	`); err != nil {
		t.Fatalf("seed play_history: %v", err)
	}
	return dsn, hostID
}

func TestCLIUsageAndFlagErrors(t *testing.T) {
	bin := binaryPath(t)

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no subcommand", nil, 2},
		{"unknown subcommand", []string{"frobnicate"}, 2},
		{"missing slug", []string{"plan", "--host-user-id", "1", "--room-name", "N"}, 2},
		{"missing name for up", []string{"up", "--room-slug", "s", "--host-user-id", "1"}, 2},
		{"nonpositive host", []string{"plan", "--room-slug", "s", "--room-name", "N", "--host-user-id", "0"}, 2},
		{"unexpected positional", []string{"plan", "--room-slug", "s", "--room-name", "N", "--host-user-id", "1", "extra"}, 2},
		{"unknown flag", []string{"plan", "--nope"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, tc.args...)
			// Ensure no ambient DSN leaks a real connection attempt.
			cmd.Env = append(os.Environ(), "DATABASE_URL=", "MIGRATE_DATABASE_URL=")
			out, err := cmd.CombinedOutput()
			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			if code != tc.want {
				t.Fatalf("exit=%d want=%d\n%s", code, tc.want, out)
			}
		})
	}
}

func TestCLIHelpExitsZero(t *testing.T) {
	out, code := runCLI(t, nil, "help")
	if code != 0 {
		t.Fatalf("help exit=%d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "usage: room-cutover") {
		t.Fatalf("help output missing usage banner:\n%s", out)
	}
}

func TestCLIPlanUpVerifyRoundTrip(t *testing.T) {
	dsn, hostID := scopedCLIDSN(t)
	slug := "cli-legacy"
	name := "CLI Legacy"
	host := fmt.Sprintf("%d", hostID)

	// plan: read-only preview, exit 0, writes nothing durable.
	out, code := runCLI(t, nil, "plan",
		"--room-slug", slug, "--room-name", name, "--host-user-id", host, "--postgres", dsn)
	if code != 0 {
		t.Fatalf("plan exit=%d\n%s", code, out)
	}

	// up: perform the cutover, exit 0.
	out, code = runCLI(t, nil, "up",
		"--room-slug", slug, "--room-name", name, "--host-user-id", host, "--postgres", dsn)
	if code != 0 {
		t.Fatalf("up exit=%d\n%s", code, out)
	}

	// A durable marker now exists.
	{
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("open verify db: %v", err)
		}
		defer db.Close()
		var markers, rooms int64
		if err := db.QueryRow(`SELECT COUNT(*) FROM room_cutover_marker`).Scan(&markers); err != nil {
			t.Fatalf("count markers: %v", err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM rooms WHERE slug = $1`, slug).Scan(&rooms); err != nil {
			t.Fatalf("count rooms: %v", err)
		}
		if markers != 1 || rooms != 1 {
			t.Fatalf("post-up state: markers=%d rooms=%d", markers, rooms)
		}
	}

	// up again: idempotent no-op, still exit 0.
	out, code = runCLI(t, nil, "up",
		"--room-slug", slug, "--room-name", name, "--host-user-id", host, "--postgres", dsn)
	if code != 0 {
		t.Fatalf("idempotent up exit=%d\n%s", code, out)
	}

	// verify (room-name optional): exit 0.
	out, code = runCLI(t, nil, "verify",
		"--room-slug", slug, "--host-user-id", host, "--postgres", dsn)
	if code != 0 {
		t.Fatalf("verify exit=%d\n%s", code, out)
	}
}

func TestCLIVerifyWithoutMarkerFails(t *testing.T) {
	dsn, hostID := scopedCLIDSN(t)
	out, code := runCLI(t, nil, "verify",
		"--room-slug", "cli-legacy", "--host-user-id", fmt.Sprintf("%d", hostID), "--postgres", dsn)
	if code != 1 {
		t.Fatalf("verify without marker exit=%d, want 1\n%s", code, out)
	}
}

func TestCLIReportFileWritten(t *testing.T) {
	dsn, hostID := scopedCLIDSN(t)
	reportPath := filepath.Join(t.TempDir(), "report.json")
	out, code := runCLI(t, nil, "plan",
		"--room-slug", "cli-legacy", "--room-name", "CLI Legacy",
		"--host-user-id", fmt.Sprintf("%d", hostID), "--postgres", dsn,
		"--report-file", reportPath)
	if code != 0 {
		t.Fatalf("plan exit=%d\n%s", code, out)
	}
	info, err := os.Stat(reportPath)
	if err != nil {
		t.Fatalf("report file not written: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("report file is empty")
	}
}
