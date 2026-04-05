package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	db "github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// MigrateConfig holds the database and migration settings.
type MigrateConfig struct {
	DSN            string
	Dialector      string
	MigrationsPath string
	PopulationPath string
}

// Migrator applies schema migrations and seed data using golang-migrate.
type Migrator struct {
	db      *sql.DB
	migrate *migrate.Migrate
	config  MigrateConfig
}

// New creates a new Migrator, opening and validating the database connection.
func New(config MigrateConfig) (*Migrator, error) {
	if err := validateMigrateConfig(config); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	db, err := sql.Open(config.Dialector, config.DSN)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseConnectionFailed, err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: %w", ErrDatabasePingFailed, err)
	}

	m := &Migrator{db: db, config: config}

	if err := m.initMigrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return m, nil
}

// initMigrate initializes the underlying migrate instance.
func (m *Migrator) initMigrate() error {
	driver, err := m.createDatabaseDriver()
	if err != nil {
		return err
	}

	absPath, err := filepath.Abs(m.config.MigrationsPath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAbsolutePathFailed, err)
	}

	m.migrate, err = migrate.NewWithDatabaseInstance(
		"file://"+absPath,
		m.config.Dialector,
		driver,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMigrateInstanceFailed, err)
	}

	return nil
}

// createDatabaseDriver returns the golang-migrate driver for the configured dialect.
func (m *Migrator) createDatabaseDriver() (db.Driver, error) {
	switch m.config.Dialector {
	case "postgres":
		return postgres.WithInstance(m.db, &postgres.Config{})
	case "mysql":
		return mysql.WithInstance(m.db, &mysql.Config{})
	case "sqlite3":
		return sqlite3.WithInstance(m.db, &sqlite3.Config{})
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDialect, m.config.Dialector)
	}
}

// Migrate applies all pending migrations.
func (m *Migrator) Migrate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("migration cancelled before start: %w", err)
	}

	version, dirty, err := m.migrate.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("%w: %w", ErrGetVersionFailed, err)
	}

	if dirty {
		return fmt.Errorf("%w (version %d)", ErrDatabaseDirtyState, version)
	}

	if err := m.migrate.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return fmt.Errorf("%w: %w", ErrMigrationFailed, err)
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled after migration completed: %w", err)
	}

	return nil
}

// Populate executes all .sql files in the configured PopulationPath.
func (m *Migrator) Populate(ctx context.Context) error {
	if m.config.PopulationPath == "" {
		return nil
	}

	files, err := os.ReadDir(m.config.PopulationPath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReadPopulationDirectory, err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".sql" {
			continue
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("populate cancelled: %w", err)
		}

		path := filepath.Join(m.config.PopulationPath, file.Name())

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%w %q: %w", ErrReadPopulateFile, path, err)
		}

		if _, err := m.db.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("%w %q: %w", ErrPopulateExecutionFailed, path, err)
		}
	}

	return nil
}

// validateMigrateConfig checks that all required fields are present and valid.
func validateMigrateConfig(config MigrateConfig) error {
	if config.DSN == "" {
		return ErrDSNRequired
	}
	if config.Dialector == "" {
		return ErrDialectorRequired
	}
	switch config.Dialector {
	case "postgres", "mysql", "sqlite3":
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedDialect, config.Dialector)
	}
	if config.MigrationsPath == "" {
		return ErrMigrationsPathRequired
	}
	if _, err := os.Stat(config.MigrationsPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %q", ErrInvalidMigrationsPath, config.MigrationsPath)
	}
	return nil
}
