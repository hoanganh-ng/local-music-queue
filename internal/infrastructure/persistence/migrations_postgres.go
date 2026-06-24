// Package persistence provides the embedded PostgreSQL migrations FS.
//
// The same embedded FS is used by:
//   - the backend process startup (defensive migrate.Up)
//   - the cmd/migrate-schema CLI
//   - tests
//
// This guarantees one source of truth and prevents drift between
// process startup and operator-driven migrations.
package persistence

import "embed"

//go:embed migrations/postgres/*.sql
var postgresMigrationsFS embed.FS

// PostgresMigrationsDir is the embedded path used by golang-migrate's iofs
// source. It is exported so the CLI and tests can construct a migrate
// instance without duplicating the directory name.
const PostgresMigrationsDir = "migrations/postgres"