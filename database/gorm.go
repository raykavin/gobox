package database

import (
	"database/sql"
	"errors"
	"fmt"
	stdlog "log"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

// Config holds database configuration
type GormConfig struct {
	DSN             string        // Data Source Name for the database connection
	Dialector       string        // Database dialector (e.g., "postgres", "mysql", "sqlite", "sqlserver")
	LogLevel        string        // Log level for GORM (e.g., "silent", "info", "error", "warning")
	MaxOpenConns    int           // Maximum number of open connections to the database
	MaxIdleConns    int           // Maximum number of connections in the idle connection pool
	SkipDefaultTx   bool          // Skip default transaction for single create, update, delete operations
	PrepareStmt     bool          // Executes the given query in cached statement
	DryRun          bool          // Generate SQL without executing
	ConnMaxLifetime time.Duration // Maximum amount of time a connection may be reused
	ConnMaxIdleTime time.Duration // Maximum amount of time a connection may be idle before being closed
	SlowThreshold   time.Duration // Threshold for logging slow queries

	// Connection retry
	RetryAttempts int           // Number of retry attempts for connection failures
	RetryDelay    time.Duration // Delay between retry attempts

	// GORM Config override
	GormConfig *gorm.Config
}

// DefaultGormConfig returns a Config with sensible defaults
func DefaultGormConfig() *GormConfig {
	return &GormConfig{
		MaxOpenConns:    50,
		MaxIdleConns:    50,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
		LogLevel:        "info",
		SlowThreshold:   200 * time.Millisecond,
		SkipDefaultTx:   true,
		PrepareStmt:     true,
		RetryAttempts:   3,
		RetryDelay:      time.Second,
	}
}

// NewGorm initializes database with detailed configuration
func NewGorm(cfg *GormConfig) (*gorm.DB, error) {
	if cfg == nil {
		return nil, ErrInvalidDatabaseConfig
	}

	// Validate configuration
	if err := validateGormConfig(cfg); err != nil {
		return nil, err
	}

	// Use provided GORM config or build default
	gormConfig := cfg.GormConfig
	if gormConfig == nil {
		gormConfig = buildGormConfig(cfg)
	}

	// Get the GORM dialector function based on the dialector
	dialFn, err := getDriverDialectorFunc(cfg.Dialector)
	if err != nil {
		return nil, err
	}

	// Open the database connection with retry logic
	var conn *gorm.DB
	for i := 0; i <= cfg.RetryAttempts; i++ {
		conn, err = gorm.Open(dialFn(cfg.DSN), gormConfig)
		if err == nil {
			break
		}
		if i < cfg.RetryAttempts {
			time.Sleep(cfg.RetryDelay)
		}
	}
	if err != nil {
		return nil, errors.Join(ErrDatabaseConnectionFailed, err)
	}

	// Configure connection pool
	sqlDB, err := conn.DB()
	if err != nil {
		return nil, errors.Join(ErrDatabasePoolAccessFailed, err)
	}
	configureConnectionPool(sqlDB, cfg)

	return conn, nil
}

// validateGormConfig validates the database configuration
func validateGormConfig(cfg *GormConfig) error {
	if cfg.DSN == "" {
		return ErrDatabaseDSNRequired
	}
	if cfg.Dialector == "" {
		return ErrDatabaseDialectorRequired
	}

	// Validate if dialector is supported
	_, err := getDriverDialectorFunc(cfg.Dialector)
	if err != nil {
		return err
	}

	return nil
}

// buildGormConfig builds GORM configuration from our config
func buildGormConfig(cfg *GormConfig) *gorm.Config {
	// Create default logger with slow threshold
	gormLogger := glog.New(
		stdlog.New(os.Stdout, "\r\n", stdlog.LstdFlags),
		glog.Config{
			SlowThreshold:             cfg.SlowThreshold,
			LogLevel:                  ParseLoggerLevel(cfg.LogLevel),
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	return &gorm.Config{
		Logger:                 gormLogger,
		SkipDefaultTransaction: cfg.SkipDefaultTx,
		PrepareStmt:            cfg.PrepareStmt,
		DryRun:                 cfg.DryRun,
		CreateBatchSize:        1000,
		FullSaveAssociations:   false,
	}
}

// configureConnectionPool configures the database connection pool
func configureConnectionPool(sqlDB *sql.DB, cfg *GormConfig) {
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}
}

// ParseLoggerLevel maps the logger level string to gorm's LogLevel
func ParseLoggerLevel(levelStr string) glog.LogLevel {
	levels := map[string]glog.LogLevel{
		"silent":  glog.Silent,
		"info":    glog.Info,
		"error":   glog.Error,
		"err":     glog.Error,
		"warning": glog.Warn,
		"warn":    glog.Warn,
	}

	if logLevel, found := levels[levelStr]; found {
		return logLevel
	}
	return glog.Info
}

// UpdateConnectionPool updates the connection pool settings for an existing connection
func UpdateConnectionPool(db *gorm.DB, cfg *GormConfig) error {
	sqlDB, err := db.DB()
	if err != nil {
		return errors.Join(ErrDatabasePoolAccessFailed, err)
	}

	configureConnectionPool(sqlDB, cfg)
	return nil
}

// GetConnectionStats returns current connection pool statistics
func GetConnectionStats(db *gorm.DB) (sql.DBStats, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return sql.DBStats{}, errors.Join(ErrDatabasePoolAccessFailed, err)
	}

	return sqlDB.Stats(), nil
}

// getDriverDialectorFunc returns a function that provides the appropriate GORM dialector based on the dialector
func getDriverDialectorFunc(dialector string) (func(string) gorm.Dialector, error) {
	dbDrivers := map[string]func(string) gorm.Dialector{
		"postgres":  postgres.Open,
		"mysql":     mysql.Open,
		"mariadb":   mysql.Open,
		"sqlite":    sqlite.Open,
		"sqlserver": sqlserver.Open,
		"mssql":     sqlserver.Open,
	}

	if dialFn, exists := dbDrivers[dialector]; exists {
		return dialFn, nil
	}

	return nil, errors.Join(ErrUnsupportedDialector, fmt.Errorf("dialector %q", dialector))
}
