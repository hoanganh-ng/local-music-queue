// Command migrate-schema is the operator-driven CLI for the PostgreSQL
// schema migration set embedded in the backend binary.
//
// Subcommands:
//
//	up              Apply all pending migrations.
//	down <steps>    Step down by <steps> versions (positive integer).
//	force <version> Set schema_migrations version without running migrations.
//	                Use only to recover from a dirty state.
//	version         Print the current applied schema version.
//
// The CLI reuses the same embedded FS as the backend process startup
// (see internal/infrastructure/persistence/migrations_postgres.go) so
// process startup and operator-driven migrations cannot drift.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"

	"local-music-queue/internal/infrastructure/persistence"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if len(os.Args) < 2 {
		usageAndExit()
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("MIGRATE_DATABASE_URL")
	}
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "error: DATABASE_URL (or MIGRATE_DATABASE_URL) is required")
		os.Exit(2)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: ping: %v\n", err)
		os.Exit(1)
	}

	subcommand := os.Args[1]
	switch subcommand {
	case "up":
		if err := persistence.RunEmbeddedMigrationsUp(db); err != nil {
			fmt.Fprintf(os.Stderr, "error: up: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("migrations: up to date")

	case "down":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: down requires <steps>")
			os.Exit(2)
		}
		steps, err := strconv.Atoi(os.Args[2])
		if err != nil || steps < 1 {
			fmt.Fprintln(os.Stderr, "error: <steps> must be a positive integer")
			os.Exit(2)
		}
		if err := persistence.RunEmbeddedMigrationsDown(db, steps); err != nil {
			fmt.Fprintf(os.Stderr, "error: down: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("migrations: stepped down %d version(s)\n", steps)

	case "force":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: force requires <version>")
			os.Exit(2)
		}
		v, err := strconv.Atoi(os.Args[2])
		if err != nil || v < 0 {
			fmt.Fprintln(os.Stderr, "error: <version> must be a non-negative integer")
			os.Exit(2)
		}
		if err := persistence.RunEmbeddedMigrationsForce(db, v); err != nil {
			fmt.Fprintf(os.Stderr, "error: force: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("migrations: forced version=%d\n", v)

	case "version":
		v, dirty, err := persistence.EmbeddedMigrationsVersion(db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: version: %v\n", err)
			os.Exit(1)
		}
		if dirty {
			fmt.Printf("version=%d dirty=true\n", v)
			os.Exit(1)
		}
		fmt.Printf("version=%d\n", v)

	default:
		usageAndExit()
	}
}

func usageAndExit() {
	fmt.Fprintln(os.Stderr, "usage: migrate-schema <up|down <steps>|force <version>|version>")
	fmt.Fprintln(os.Stderr, "  DATABASE_URL must be set.")
	os.Exit(2)
}