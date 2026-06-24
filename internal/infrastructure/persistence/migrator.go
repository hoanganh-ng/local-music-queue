package persistence

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// RunEmbeddedMigrationsUp applies all pending schema migrations against the
// given *sql.DB using the embedded PostgreSQL migration files.
//
// It is the single shared code path for:
//   - backend process startup (defensive)
//   - cmd/migrate-schema CLI (authoritative in non-Compose)
//
// ErrNoChange is intentionally treated as success: a no-op indicates an
// already-applied schema, which is the expected state on every backend
// restart in production.
func RunEmbeddedMigrationsUp(db *sql.DB) error {
	return withMigrator(db, func(m *migrate.Migrate) error {
		return m.Up()
	})
}

// RunEmbeddedMigrationsDown steps the schema down by `steps` versions.
// A negative value migrates down; positive migrates up.
func RunEmbeddedMigrationsDown(db *sql.DB, steps int) error {
	return withMigrator(db, func(m *migrate.Migrate) error {
		return m.Steps(-steps)
	})
}

// RunEmbeddedMigrationsForce sets the schema_migrations version without
// running any migrations. Use only when recovering from a dirty state.
func RunEmbeddedMigrationsForce(db *sql.DB, version int) error {
	return withMigrator(db, func(m *migrate.Migrate) error {
		return m.Force(version)
	})
}

// EmbeddedMigrationsVersion returns the current applied schema version and
// dirty flag. When no migrations have been applied yet it returns
// (0, false, nil).
func EmbeddedMigrationsVersion(db *sql.DB) (version uint, dirty bool, err error) {
	m, closer, err := newMigrator(db)
	if err != nil {
		return 0, false, err
	}
	defer closer()
	v, d, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, err
	}
	return v, d, nil
}

func withMigrator(db *sql.DB, op func(*migrate.Migrate) error) error {
	m, closer, err := newMigrator(db)
	if err != nil {
		return err
	}
	defer closer()
	if err := op(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// newMigrator builds a migrate.Migrate instance and returns a closer that
// is safe to call. Note that migrate.Migrate.Close() closes the underlying
// database driver — which can close the *sql.DB the caller passed in.
// Callers that need to keep using the *sql.DB after migration should pass a
// dedicated *sql.DB (one that will not be used by the rest of the program)
// OR simply not close the returned migrator.
func newMigrator(db *sql.DB) (*migrate.Migrate, func(), error) {
	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to init pgx migrate driver: %w", err)
	}
	src, err := iofs.New(postgresMigrationsFS, PostgresMigrationsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open embedded migrations: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to construct migrate: %w", err)
	}
	// The migrator takes ownership of the driver; closing it would close the
	// supplied *sql.DB. The caller is expected to manage the *sql.DB
	// lifecycle directly, so we return a no-op closer.
	return m, func() {}, nil
}