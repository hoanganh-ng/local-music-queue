package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence/roomcutover"
)

// cliFlags holds the parsed flag values for one subcommand invocation. Only
// the fields registered for that subcommand are ever set.
type cliFlags struct {
	roomSlug   string
	roomName   string
	hostUserID int64
	dryRun     bool
	postgres   string
	reportFile string
}

// newFlagSet builds the per-subcommand ContinueOnError flag set that prints
// to stderr, so a parse failure lets run() exit with code 2. Each subcommand
// registers ONLY the flags it accepts:
//
//   - plan:   --room-slug --room-name --host-user-id --postgres --report-file
//   - up:     plan's flags plus --dry-run
//   - verify: --postgres --report-file (identity is derived from the marker)
//
// An unregistered flag (e.g. --dry-run on plan, or --room-slug on verify) is
// a parse error and exits 2.
func newFlagSet(mode string) (*flag.FlagSet, *cliFlags) {
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := &cliFlags{}
	fs.StringVar(&f.postgres, "postgres", "", "PostgreSQL DSN override (defaults to DATABASE_URL or MIGRATE_DATABASE_URL)")
	fs.StringVar(&f.reportFile, "report-file", "", "write JSON copy of the integrity report to this path (mode 0600)")
	if mode == "plan" || mode == "up" {
		fs.StringVar(&f.roomSlug, "room-slug", "", "target room slug (required)")
		fs.StringVar(&f.roomName, "room-name", "", "target room display name (required)")
		fs.Int64Var(&f.hostUserID, "host-user-id", 0, "existing user id to make the sole room host (required)")
	}
	if mode == "up" {
		fs.BoolVar(&f.dryRun, "dry-run", false, "run without writing")
	}
	return fs, f
}

// validate enforces flag presence and semantic slug validity (exit 2 on any
// failure) and returns the roomcutover options. verify carries no identity:
// the package derives it from the durable marker.
func (f *cliFlags) validate(mode string) roomcutover.Options {
	if mode == "verify" {
		return roomcutover.Options{}
	}
	slug := strings.TrimSpace(f.roomSlug)
	if slug == "" {
		fmt.Fprintln(os.Stderr, "error: --room-slug is required")
		os.Exit(2)
	}
	if !entity.IsValidSlug(slug) {
		fmt.Fprintf(os.Stderr, "error: invalid room slug %q\n", slug)
		os.Exit(2)
	}
	if entity.IsReservedSlug(slug) {
		fmt.Fprintf(os.Stderr, "error: reserved room slug %q\n", slug)
		os.Exit(2)
	}
	name := strings.TrimSpace(f.roomName)
	if name == "" {
		fmt.Fprintln(os.Stderr, "error: --room-name is required")
		os.Exit(2)
	}
	if f.hostUserID <= 0 {
		fmt.Fprintln(os.Stderr, "error: --host-user-id must be a positive integer")
		os.Exit(2)
	}
	return roomcutover.Options{
		RoomSlug:   slug,
		RoomName:   name,
		HostUserID: f.hostUserID,
		DryRun:     f.dryRun,
	}
}

func usage(w *os.File) {
	fmt.Fprintln(w, "usage: room-cutover <plan|up|verify> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  plan    read-only preview: validate readiness, hash sources, report expected target evidence")
	fmt.Fprintln(w, "          flags: --room-slug --room-name --host-user-id --postgres --report-file")
	fmt.Fprintln(w, "  up      execute the cutover in one locked transaction (idempotent)")
	fmt.Fprintln(w, "          flags: --room-slug --room-name --host-user-id --dry-run --postgres --report-file")
	fmt.Fprintln(w, "  verify  re-verify the committed cutover; identity is derived from the durable marker")
	fmt.Fprintln(w, "          flags: --postgres --report-file")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "flags:")
	fmt.Fprintln(w, "  --room-slug     target room slug (required for plan/up)")
	fmt.Fprintln(w, "  --room-name     target room display name (required for plan/up)")
	fmt.Fprintln(w, "  --host-user-id  existing user id to make the sole room host (required for plan/up)")
	fmt.Fprintln(w, "  --dry-run       up only: run without writing")
	fmt.Fprintln(w, "  --postgres      PostgreSQL DSN; defaults to $DATABASE_URL or $MIGRATE_DATABASE_URL")
	fmt.Fprintln(w, "  --report-file   write JSON copy of the integrity report to this path (mode 0600)")
}

func usageAndExit() {
	usage(os.Stderr)
	os.Exit(2)
}
