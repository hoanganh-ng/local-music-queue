package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"local-music-queue/internal/infrastructure/persistence/roomcutover"
)

// cliFlags holds the parsed flag values shared by every subcommand.
type cliFlags struct {
	roomSlug   string
	roomName   string
	hostUserID int64
	dryRun     bool
	postgres   string
	reportFile string
}

// newFlagSet builds a ContinueOnError flag set that prints to stderr, so a
// parse failure lets run() exit with code 2.
func newFlagSet(mode string) *flag.FlagSet {
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// bindFlags registers the common flags on fs and returns the destination
// struct. --dry-run is only meaningful for `up`; plan and verify are always
// read-only.
func bindFlags(fs *flag.FlagSet) *cliFlags {
	f := &cliFlags{}
	fs.StringVar(&f.roomSlug, "room-slug", "", "target room slug (required)")
	fs.StringVar(&f.roomName, "room-name", "", "target room display name (required for plan/up)")
	fs.Int64Var(&f.hostUserID, "host-user-id", 0, "existing user id to make the sole room host (required)")
	fs.BoolVar(&f.dryRun, "dry-run", false, "up only: run without writing")
	fs.StringVar(&f.postgres, "postgres", "", "PostgreSQL DSN override (defaults to DATABASE_URL or MIGRATE_DATABASE_URL)")
	fs.StringVar(&f.reportFile, "report-file", "", "write JSON copy of the integrity report to this path")
	return f
}

// validate enforces flag presence (exit 2 on failure) and returns the
// roomcutover options. Semantic validation of the slug (pattern / reserved) is
// left to the package and surfaces as an exit-1 runtime error.
func (f *cliFlags) validate(requireName bool) roomcutover.Options {
	slug := strings.TrimSpace(f.roomSlug)
	if slug == "" {
		fmt.Fprintln(os.Stderr, "error: --room-slug is required")
		os.Exit(2)
	}
	name := strings.TrimSpace(f.roomName)
	if requireName && name == "" {
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
	fmt.Fprintln(w, "usage: room-cutover <plan|up|verify> --room-slug <s> --host-user-id <id> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  plan    read-only preview: validate, hash sources, count rows")
	fmt.Fprintln(w, "  up      execute the cutover in one locked transaction (idempotent)")
	fmt.Fprintln(w, "  verify  re-hash source + target and assert they match the marker")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "flags:")
	fmt.Fprintln(w, "  --room-slug     target room slug (required)")
	fmt.Fprintln(w, "  --room-name     target room display name (required for plan/up)")
	fmt.Fprintln(w, "  --host-user-id  existing user id to make the sole room host (required)")
	fmt.Fprintln(w, "  --dry-run       up only: run without writing")
	fmt.Fprintln(w, "  --postgres      PostgreSQL DSN; defaults to $DATABASE_URL or $MIGRATE_DATABASE_URL")
	fmt.Fprintln(w, "  --report-file   write JSON copy of the integrity report to this path")
}

func usageAndExit() {
	usage(os.Stderr)
	os.Exit(2)
}
