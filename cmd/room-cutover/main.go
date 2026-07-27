// Command room-cutover is the operator-driven CLI for the offline
// legacy-global-state → room cutover described in ADR 003 (R14a contract) and
// Sprint 026 (R14b). It copies the single legacy global queue/activity/
// auto-queue/play-history state into one newly created room, records a durable
// idempotency marker, and verifies integrity via canonical SHA-256 hashes.
//
// Subcommands:
//
//	plan   --room-slug <s> --room-name <n> --host-user-id <id> [flags]
//	         Read-only preview: validate inputs, assert readiness, compute
//	         source hashes and per-table counts. Never writes.
//	up     --room-slug <s> --room-name <n> --host-user-id <id> [--dry-run] [flags]
//	         Execute the cutover inside one locked transaction. Idempotent:
//	         re-running with the same identity on an already-cut-over target
//	         prints "already cut over; no-op" and exits 0.
//	verify --room-slug <s> --host-user-id <id> [flags]
//	         Re-read the marker and re-hash source + target, asserting they
//	         still match. Never writes.
//
// Conventions (shared with cmd/migrate-schema and cmd/migrate-data):
//
//   - Exit code 0 = success (including an already-cut-over no-op)
//   - Exit code 1 = runtime / consistency / verification failure
//   - Exit code 2 = usage / flag error
//   - Every DSN printed to stdout/stderr is run through config.RedactDSN so
//     credentials never appear in operator logs.
//
// There is deliberately no abort/force/reset/allow-hash-drift subcommand: any
// drift fails explicitly and the operator investigates rather than overriding.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"local-music-queue/internal/infrastructure/config"
	"local-music-queue/internal/infrastructure/persistence/roomcutover"
)

func main() {
	if len(os.Args) < 2 {
		usageAndExit()
	}

	subcommand := os.Args[1]
	switch subcommand {
	case "plan", "up", "verify":
		if err := run(subcommand, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown subcommand %q\n", subcommand)
		usageAndExit()
	}
}

// run parses the flags for one subcommand, opens the target DB, dispatches to
// the matching roomcutover entry point, and renders the report. Usage/flag
// problems exit 2 directly; runtime failures are returned for the exit-1 path.
func run(mode string, args []string) error {
	fs := newFlagSet(mode)
	flags := bindFlags(fs)
	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already printed the error.
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n", fs.Arg(0))
		os.Exit(2)
	}

	requireName := mode != "verify"
	opts := flags.validate(requireName)

	dsn := resolveDSN(flags.postgres)
	opts.RedactedDSN = config.RedactDSN(dsn)

	db, err := openDB(dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()
	var report *roomcutover.Report
	switch mode {
	case "plan":
		report, err = roomcutover.Plan(ctx, db, opts)
	case "up":
		report, err = roomcutover.Up(ctx, db, opts)
	case "verify":
		report, err = roomcutover.Verify(ctx, db, opts)
	}
	if err != nil {
		return err
	}

	if werr := report.WriteText(os.Stdout); werr != nil {
		return fmt.Errorf("write text report: %w", werr)
	}
	if werr := report.WriteToFile(flags.reportFile); werr != nil {
		return fmt.Errorf("write JSON report: %w", werr)
	}
	return nil
}

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect postgres (%s): %w", config.RedactDSN(dsn), err)
	}
	return db, nil
}

func resolveDSN(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return os.Getenv("MIGRATE_DATABASE_URL")
}
