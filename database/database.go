package database

import "errors"

var (
	ErrUnsupportedDialect       = errors.New("unsupported database dialect")
	ErrInvalidConfig            = errors.New("invalid configuration")
	ErrInvalidMigrationsPath    = errors.New("migrations path does not exist")
	ErrDatabaseConnectionFailed = errors.New("failed to open database connection")
	ErrDatabasePingFailed       = errors.New("failed to ping database")
	ErrAbsolutePathFailed       = errors.New("failed to resolve absolute path")
	ErrMigrateInstanceFailed    = errors.New("failed to create migrate instance")
	ErrGetVersionFailed         = errors.New("failed to get current migration version")
	ErrDatabaseDirtyState       = errors.New("database is in a dirty state, manual intervention required")
	ErrMigrationFailed          = errors.New("failed to apply migrations")
	ErrGetNewVersionFailed      = errors.New("failed to get new migration version after applying")
	ErrReadPopulationDirectory  = errors.New("failed to read population directory")
	ErrReadPopulateFile         = errors.New("failed to read seed file")
	ErrPopulateExecutionFailed  = errors.New("failed to execute seed file")

	ErrDSNRequired            = errors.New("dsn is required")
	ErrDialectorRequired      = errors.New("dialector is required")
	ErrMigrationsPathRequired = errors.New("migrations path is required")

	ErrInvalidDatabaseConfig     = errors.New("invalid database configuration")
	ErrDatabaseDSNRequired       = errors.New("database dsn is required")
	ErrDatabaseDialectorRequired = errors.New("database dialector is required")
	ErrUnsupportedDialector      = errors.New("unsupported database dialector")
	ErrDatabasePoolAccessFailed  = errors.New("failed to access database connection pool")
)
