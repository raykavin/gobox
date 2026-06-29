package gorm

import "errors"

var (
	ErrInvalidDatabaseConfig     = errors.New("invalid database configuration")
	ErrDatabaseDSNRequired       = errors.New("database dsn is required")
	ErrDatabaseDialectorRequired = errors.New("database dialector is required")
	ErrUnsupportedDialector      = errors.New("unsupported database dialector")
	ErrDatabaseConnectionFailed  = errors.New("failed to open database connection")
	ErrDatabasePoolAccessFailed  = errors.New("failed to access database connection pool")
)
