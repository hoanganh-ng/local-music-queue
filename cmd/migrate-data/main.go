// Command migrate-data is the operator-driven CLI for the offline
// SQLite-to-PostgreSQL data migration described in ADR 002 §11.
//
// Subcommands:
//
//	up [--sqlite <path>] [--postgres <dsn>] [--report-file <path>] [--chunk-size <n>] [--dry-run]
//	            Copy data from SQLite to PostgreSQL inside one transaction.
//	            Idempotent: re-running on an already-migrated target prints
//	            "already migrated; no-op" and exits 0.
//
// The CLI mirrors the conventions of cmd/migrate-schema:
//
//   - Exit code 0 = success (including no-op re-run)
//   - Exit code 1 = error
//   - Exit code 2 = usage error
//   - All DSN strings printed to stdout/stderr are run through
//     config.RedactDSN so credentials never appear in operator logs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"local-music-queue/internal/infrastructure/persistence/migratedata"
)

func main() {
	if len(os.Args) < 2 {
		usageAndExit()
	}

	subcommand := os.Args[1]
	switch subcommand {
	case "up":
		if err := runUp(os.Args[2:]); err != nil {
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

func runUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		sqlitePath = fs.String("sqlite", "", "path to SQLite source file (required)")
		pgOverride = fs.String("postgres", "", "PostgreSQL DSN override (defaults to DATABASE_URL or MIGRATE_DATABASE_URL)")
		reportFile = fs.String("report-file", "", "write JSON integrity report to this path")
		chunkStr   = fs.String("chunk-size", "", "rows per INSERT batch (default 1000)")
		dryRun     = fs.Bool("dry-run", false, "run verification and print report without writing")
	)
	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already printed; exit with code 2.
		os.Exit(2)
	}

	if strings.TrimSpace(*sqlitePath) == "" {
		fmt.Fprintln(os.Stderr, "error: --sqlite <path> is required")
		os.Exit(2)
	}

	dsn := *pgOverride
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = os.Getenv("MIGRATE_DATABASE_URL")
	}
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "error: --postgres <dsn> (or DATABASE_URL / MIGRATE_DATABASE_URL env) is required")
		os.Exit(2)
	}

	chunkSize := 0
	if *chunkStr != "" {
		n, err := strconv.Atoi(*chunkStr)
		if err != nil || n <= 0 {
			fmt.Fprintln(os.Stderr, "error: --chunk-size must be a positive integer")
			os.Exit(2)
		}
		chunkSize = n
	}

	opts := migratedata.Options{
		SQLitePath: *sqlitePath,
		PGDSN:      dsn,
		ReportFile: *reportFile,
		ChunkSize:  chunkSize,
		DryRun:     *dryRun,
	}

	report, err := migratedata.Run(context.Background(), opts)
	if err != nil {
		return err
	}

	if err := report.WriteText(os.Stdout); err != nil {
		return fmt.Errorf("write text report: %w", err)
	}
	if err := report.WriteToFile(*reportFile); err != nil {
		return fmt.Errorf("write JSON report: %w", err)
	}
	return nil
}

func usage(w *os.File) {
	fmt.Fprintln(w, "usage: migrate-data up [--sqlite <path>] [--postgres <dsn>] [--report-file <path>] [--chunk-size <n>] [--dry-run]")
	fmt.Fprintln(w, "  --sqlite       path to the SQLite source file (required)")
	fmt.Fprintln(w, "  --postgres     PostgreSQL DSN; defaults to $DATABASE_URL or $MIGRATE_DATABASE_URL")
	fmt.Fprintln(w, "  --report-file  write JSON copy of the integrity report to this path")
	fmt.Fprintln(w, "  --chunk-size   rows per INSERT batch (default 1000)")
	fmt.Fprintln(w, "  --dry-run      run verification without writing data")
}

func usageAndExit() {
	usage(os.Stderr)
	os.Exit(2)
}