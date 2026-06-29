# database/gorm

The `gorm` package provides a GORM database connection factory. It is intended for services that need a consistent way to open a database, configure connection pooling, set log levels, and retry connections on startup without writing boilerplate for each project.

## Import

```go
import "github.com/raykavin/gobox/database/gorm"
```

## What it provides

- `New()` for opening a GORM connection with pooling, logging, and retry
- `DefaultGormConfig()` for a sensible starting configuration
- support for `postgres`, `mysql`, `mariadb`, `sqlite`, `sqlserver`, and `mssql`
- `ParseLoggerLevel()` for mapping level strings to GORM log levels
- `UpdateConnectionPool()` for adjusting pool settings on an existing connection
- `GetConnectionStats()` for reading pool metrics at runtime

## Main types

- `GormConfig`: connection and pool settings
- `GormConfig.GormConfig`: optional override for the underlying `*gorm.Config`

## Example

```go
package main

import (
    "log"
    "os"
    "time"

    gormkit "github.com/raykavin/gobox/database/gorm"
)

func main() {
    cfg := gormkit.DefaultGormConfig()
    cfg.DSN       = os.Getenv("DATABASE_URL")
    cfg.Dialector = "postgres"
    cfg.LogLevel  = "warn"

    db, err := gormkit.New(cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer func() {
        sqlDB, _ := db.DB()
        _ = sqlDB.Close()
    }()

    // use db as a normal *gorm.DB
    _ = db
}
```

## Default values

| Field | Default |
|---|---|
| `MaxOpenConns` | 50 |
| `MaxIdleConns` | 50 |
| `ConnMaxLifetime` | 1h |
| `ConnMaxIdleTime` | 30m |
| `LogLevel` | `"info"` |
| `SlowThreshold` | 200ms |
| `SkipDefaultTx` | true |
| `PrepareStmt` | true |
| `RetryAttempts` | 3 |
| `RetryDelay` | 1s |

## Errors

| Sentinel | Cause |
|---|---|
| `ErrInvalidDatabaseConfig` | `nil` config passed to `New` |
| `ErrDatabaseDSNRequired` | `DSN` is empty |
| `ErrDatabaseDialectorRequired` | `Dialector` is empty |
| `ErrUnsupportedDialector` | `Dialector` is not a known driver |
| `ErrDatabaseConnectionFailed` | `gorm.Open` failed after all retries |
| `ErrDatabasePoolAccessFailed` | could not retrieve the underlying `*sql.DB` |

## Notes

- retry applies only to the initial connection; queries are not retried automatically
- set `GormConfig.GormConfig` to a custom `*gorm.Config` to bypass the built-in logger and configuration entirely
- `DryRun: true` generates SQL without executing it, useful for testing
