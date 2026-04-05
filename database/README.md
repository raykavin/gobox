# database

The `database` package provides two reusable approaches for data access:

- a lightweight typed wrapper around `database/sql`
- a configurable GORM bootstrap with connection-pool and logging helpers

This split allows consumers to choose the abstraction level they need while keeping both options documented in a single package.

## Import

```go
import "github.com/raykavin/gokit/database"
```

## What it provides

- `SQLConfig` for plain `database/sql` usage
- `GormConfig` for GORM initialization and pool configuration
- a generic `Connector[T]` for typed query results with `database/sql`
- caller-defined row mapping through `ScanFunc[T]`
- GORM connection bootstrap with retry support
- connection pool updates and connection statistics helpers for GORM
- sentinel errors for GORM configuration and bootstrap failures

## Main types

- `SQLConfig`: driver name and DSN for `database/sql`
- `GormConfig`: dialector, DSN, retry, pool, logging, and optional `*gorm.Config` override
- `ScanFunc[T]`: maps a single `sql.Rows` record into `T`
- `Connector[T]`: executes typed queries and returns `[]T`

## SQL usage

Use `SQLConfig` with `New()` when you want a small wrapper over `database/sql` and full control over query execution and row scanning.

Example:

```go
package main

import (
	"context"
	"database/sql"
	"log"

	_ "<your-driver>"

	"github.com/raykavin/gokit/database"
)

type User struct {
	ID   int
	Name string
}

func main() {
	conn, err := database.New(database.SQLConfig{
		Driver: "postgres",
		DSN:    "postgres://user:pass@localhost:5432/app?sslmode=disable",
	}, func(rows *sql.Rows) (User, error) {
		var user User
		err := rows.Scan(&user.ID, &user.Name)
		return user, err
	})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	users, err := conn.Query(context.Background(), "select id, name from users")
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("users found: %d", len(users))
}
```

## GORM usage

Use `GormConfig` with `NewGorm()` when you want a ready-to-use `*gorm.DB` with shared defaults for retry, logging, and connection pool settings.

Supported dialectors:

- `postgres`
- `mysql`
- `mariadb`
- `sqlite`
- `sqlserver`
- `mssql`

Example:

```go
package main

import (
	"log"
	"time"

	"github.com/raykavin/gokit/database"
)

type User struct {
	ID   uint
	Name string
}

func main() {
	cfg := database.DefaultGormConfig()
	cfg.Dialector = "postgres"
	cfg.DSN = "postgres://user:pass@localhost:5432/app?sslmode=disable"
	cfg.LogLevel = "info"
	cfg.SlowThreshold = 250 * time.Millisecond

	db, err := database.NewGorm(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.AutoMigrate(&User{}); err != nil {
		log.Fatal(err)
	}
}
```

## GORM notes

- `DefaultGormConfig()` returns sensible defaults for pooling, retries, and log behavior
- `NewGorm()` validates the config, resolves the dialector, retries the initial connection, and configures the underlying `sql.DB` pool
- `GormConfig.GormConfig` can be used to pass a custom `*gorm.Config` override
- `UpdateConnectionPool()` reapplies pool settings on an existing `*gorm.DB`
- `GetConnectionStats()` returns `sql.DBStats` for an active GORM connection
- `ParseLoggerLevel()` maps strings such as `silent`, `info`, `warn`, and `error` to GORM log levels

## Sentinel errors

The GORM helper exposes sentinel errors that can be checked with `errors.Is`:

- `ErrInvalidDatabaseConfig`
- `ErrDatabaseDSNRequired`
- `ErrDatabaseDialectorRequired`
- `ErrUnsupportedDialector`
- `ErrDatabaseConnectionFailed`
- `ErrDatabasePoolAccessFailed`

## SQL notes

- `New()` opens the database connection, validates the input, and pings the database before returning a connector
- `Query()` executes a query with optional arguments and maps each row using the provided scan function
- `Close()` releases the underlying connection pool
