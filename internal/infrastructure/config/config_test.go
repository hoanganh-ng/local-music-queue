package config

import (
	"os"
	"strings"
	"testing"
)

func TestRedactDSN(t *testing.T) {
	t.Run("PostgreSQL URL with password is redacted", func(t *testing.T) {
		in := "postgres://lmq:supersecret@postgres:5432/lmq?sslmode=disable"
		out := RedactDSN(in)
		if strings.Contains(out, "supersecret") {
			t.Fatalf("password leaked: %s", out)
		}
		// url.URL.String() may percent-encode the marker; accept either form.
		if !strings.Contains(out, "***") && !strings.Contains(out, "%2A%2A%2A") {
			t.Fatalf("expected redaction marker in: %s", out)
		}
	})

	t.Run("URL without password is returned verbatim", func(t *testing.T) {
		in := "postgres://lmq@postgres:5432/lmq?sslmode=disable"
		out := RedactDSN(in)
		if out != in {
			t.Fatalf("expected unchanged URL, got %s", out)
		}
	})

	t.Run("Non-URL with password= token is redacted", func(t *testing.T) {
		in := "host=postgres user=lmq password=supersecret dbname=lmq"
		out := RedactDSN(in)
		if strings.Contains(out, "supersecret") {
			t.Fatalf("password leaked: %s", out)
		}
		if !strings.Contains(out, "password=***") {
			t.Fatalf("expected 'password=***' in redacted output, got %s", out)
		}
	})

	t.Run("Empty input returns empty", func(t *testing.T) {
		if got := RedactDSN(""); got != "" {
			t.Fatalf("expected empty output, got %s", got)
		}
	})
}

func TestLoad_DatabaseURLSelection(t *testing.T) {
	// envKeys are the variables each subtest must control. Each subtest
	// snapshots the outer value via t.Setenv / os.Unsetenv and restores on
	// cleanup, so subtests cannot pollute one another or the surrounding
	// test process.
	envKeys := []string{
		"DATABASE_URL",
		"POSTGRES_HOST",
		"POSTGRES_PORT",
		"POSTGRES_USER",
		"POSTGRES_PASSWORD",
		"POSTGRES_DB",
		"POSTGRES_SSLMODE",
		"DB_PATH",
		"PORT",
		"CLIENT_PIN",
		"HOST_PIN",
		"ADMIN_PIN",
		"YTDLP_PATH",
	}

	// isolateEnv clears every envKeys entry inside the calling subtest and
	// installs a t.Cleanup hook that restores the values the subtest's
	// parent observed.
	isolateEnv := func(t *testing.T) {
		t.Helper()
		// Skip dotenv loading so tests don't pick up the local .env file.
		t.Setenv("APP_ENV", "test")
		saved := map[string]string{}
		for _, k := range envKeys {
			saved[k] = os.Getenv(k)
			os.Unsetenv(k)
		}
		t.Cleanup(func() {
			for k, v := range saved {
				if v != "" {
					os.Setenv(k, v)
				} else {
					os.Unsetenv(k)
				}
			}
		})
	}

	t.Run("DATABASE_URL takes precedence", func(t *testing.T) {
		isolateEnv(t)
		os.Setenv("DATABASE_URL", "postgres://lmq:devpassword@localhost:5432/lmq?sslmode=disable")
		os.Setenv("POSTGRES_HOST", "should-be-ignored")
		os.Setenv("POSTGRES_PASSWORD", "should-be-ignored")

		cfg := Load()
		if cfg.DatabaseURL == "" {
			t.Fatal("expected DATABASE_URL to be populated")
		}
		if !strings.HasPrefix(cfg.DatabaseURL, "postgres://lmq:") {
			t.Fatalf("expected DATABASE_URL to start with postgres://lmq:, got %s", cfg.DatabaseURL)
		}
		if strings.Contains(cfg.DatabaseURL, "should-be-ignored") {
			t.Fatal("POSTGRES_* overrides should not override DATABASE_URL")
		}
	})

	t.Run("POSTGRES_HOST builds DSN when DATABASE_URL is unset", func(t *testing.T) {
		isolateEnv(t)
		// Defensive: explicitly assert DATABASE_URL is unset so this case
		// cannot pass by accident if a prior subtest leaked the var.
		if v := os.Getenv("DATABASE_URL"); v != "" {
			t.Fatalf("subtest precondition violated: DATABASE_URL=%q", v)
		}
		os.Setenv("POSTGRES_HOST", "localhost")
		os.Setenv("POSTGRES_PORT", "5432")
		os.Setenv("POSTGRES_USER", "lmq")
		os.Setenv("POSTGRES_PASSWORD", "devpassword")
		os.Setenv("POSTGRES_DB", "lmq")
		os.Setenv("POSTGRES_SSLMODE", "disable")

		cfg := Load()
		if cfg.DatabaseURL == "" {
			t.Fatal("expected DatabaseURL to be built from POSTGRES_* overrides")
		}
		if !strings.Contains(cfg.DatabaseURL, "localhost:5432") {
			t.Fatalf("expected host:port in DSN, got %s", cfg.DatabaseURL)
		}
		if !strings.Contains(cfg.DatabaseURL, "sslmode=disable") {
			t.Fatalf("expected sslmode=disable in DSN, got %s", cfg.DatabaseURL)
		}
	})

	t.Run("Empty DATABASE_URL and POSTGRES_HOST signals SQLite fallback", func(t *testing.T) {
		isolateEnv(t)
		cfg := Load()
		if cfg.DatabaseURL != "" {
			t.Fatalf("expected empty DatabaseURL (SQLite fallback), got %s", cfg.DatabaseURL)
		}
	})
}