package postgres

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers "pgx5" database driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"contractpro/document-management/internal/domain/port"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator runs SQL schema migrations embedded in the binary.
//
// It uses golang-migrate/v4 with the iofs source driver (reading from
// embed.FS) and the pgx5 database driver (connecting via DSN).
type Migrator struct {
	m *migrate.Migrate
}

// NewMigrator creates a Migrator for the given PostgreSQL DSN.
//
// The DSN should be a standard PostgreSQL connection string, e.g.
// "postgres://user:pass@host:5432/dbname?sslmode=disable".
// golang-migrate will manage the schema_migrations table automatically.
func NewMigrator(dsn string) (*Migrator, error) {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, port.NewDatabaseError("create migration source", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, pgxDSN(dsn))
	if err != nil {
		return nil, port.NewDatabaseError("create migrator", err)
	}

	return &Migrator{m: m}, nil
}

// pgxDSN converts a standard postgres:// DSN to the pgx5:// scheme expected
// by the golang-migrate pgx5 database driver.
//
// If the DSN already starts with "pgx5://", it is returned unchanged.
// If it starts with "postgres://" or "postgresql://", the scheme is replaced.
func pgxDSN(dsn string) string {
	const (
		pgx5Scheme       = "pgx5://"
		postgresScheme   = "postgres://"
		postgresqlScheme = "postgresql://"
	)

	if len(dsn) >= len(pgx5Scheme) && dsn[:len(pgx5Scheme)] == pgx5Scheme {
		return dsn
	}
	if len(dsn) >= len(postgresScheme) && dsn[:len(postgresScheme)] == postgresScheme {
		return pgx5Scheme + dsn[len(postgresScheme):]
	}
	if len(dsn) >= len(postgresqlScheme) && dsn[:len(postgresqlScheme)] == postgresqlScheme {
		return pgx5Scheme + dsn[len(postgresqlScheme):]
	}

	// Fallback: prepend pgx5:// (may not be valid, but let migrate handle the error).
	return pgx5Scheme + dsn
}

// Up applies all pending migrations.
//
// Returns nil if the schema is already up to date (migrate.ErrNoChange is
// suppressed). Any other error is returned as a *port.DomainError.
func (mg *Migrator) Up() error {
	if err := mg.m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return port.NewDatabaseError("apply migrations (up)", err)
	}
	return nil
}

// Down rolls back all applied migrations (use with caution).
//
// Returns nil if there are no migrations to roll back.
func (mg *Migrator) Down() error {
	if err := mg.m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return port.NewDatabaseError("rollback migrations (down)", err)
	}
	return nil
}

// MigrateToVersion migrates up or down to the specified version number.
//
// This is useful for targeted rollbacks in operational scenarios.
func (mg *Migrator) MigrateToVersion(version uint) error {
	if err := mg.m.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return port.NewDatabaseError(
			fmt.Sprintf("migrate to version %d", version), err,
		)
	}
	return nil
}

// Version returns the current migration version and dirty flag.
//
// Returns (0, false, nil) if no migrations have been applied yet
// (migrate.ErrNilVersion is suppressed).
func (mg *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = mg.m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, port.NewDatabaseError("get migration version", err)
	}
	return version, dirty, nil
}

// Close releases the migrator resources (source and database handles).
// Safe to call after migrations are done.
func (mg *Migrator) Close() error {
	srcErr, dbErr := mg.m.Close()
	if err := errors.Join(srcErr, dbErr); err != nil {
		return port.NewDatabaseError("close migrator", err)
	}
	return nil
}
