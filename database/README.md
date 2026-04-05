# database

The `database` package provides three reusable approaches for relational database work:

- a lightweight typed wrapper around `database/sql`
- a configurable GORM bootstrap with connection-pool and logging helpers
- a schema migrator built on top of `golang-migrate`

This split allows consumers to choose the abstraction level they need while keeping querying, ORM setup, and migration tooling documented in a single package.

## Import

```go
import "github.com/raykavin/gokit/database"
```

## What it provides

- `SQLConfig` for plain `database/sql` usage
- `GormConfig` for GORM initialization and pool configuration
- `MigrateConfig` for schema migration and seed execution
- a generic `Connector[T]` for typed query results with `database/sql`
- caller-defined row mapping through `ScanFunc[T]`
- GORM connection bootstrap with retry support
- connection pool updates and connection statistics helpers for GORM
- a `Migrator` that applies pending migrations and optional seed files
- sentinel errors for validation, bootstrap, and migration failures

## Main types

- `SQLConfig`: driver name and DSN for `database/sql`
- `GormConfig`: dialector, DSN, retry, pool, logging, and optional `*gorm.Config` override
- `MigrateConfig`: DSN, dialector, migration path, and optional population path
- `ScanFunc[T]`: maps a single `sql.Rows` record into `T`
- `Connector[T]`: executes typed queries and returns `[]T`
- `Migrator`: validates the database connection, applies migrations, and runs seed files

## SQL usage

Use `SQLConfig` with `NewSQL()` when you want a small wrapper over `database/sql` and full control over query execution and row scanning.

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
	conn, err := database.NewSQL(database.SQLConfig{
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

## Migrator usage

Use `MigrateConfig` with `New()` when you want to apply filesystem-based SQL migrations and optional population scripts through `golang-migrate`.

Supported migrator dialects:

- `postgres`
- `mysql`
- `sqlite3`

Example:

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/raykavin/gokit/database"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	migrator, err := database.New(database.MigrateConfig{
		DSN:            "postgres://user:pass@localhost:5432/app?sslmode=disable",
		Dialector:      "postgres",
		MigrationsPath: "./migrations",
		PopulationPath: "./populate",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := migrator.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	if err := migrator.Populate(ctx); err != nil {
		log.Fatal(err)
	}
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

## Migrator notes

- `New()` validates the migration config, opens the database connection, pings it, and initializes the underlying `golang-migrate` instance
- `Migrate()` applies all pending migrations and returns `nil` when there are no changes to apply
- `Populate()` is optional and executes every `.sql` file found in `PopulationPath`
- `Populate()` returns immediately when `PopulationPath` is empty
- `Migrate()` checks for dirty migration state before applying new migrations
- the migrations path must exist when the migrator is created

## Sentinel errors

The package exposes sentinel errors for both GORM configuration and migration workflows. These can be checked with `errors.Is`:

GORM bootstrap:

- `ErrInvalidDatabaseConfig`
- `ErrDatabaseDSNRequired`
- `ErrDatabaseDialectorRequired`
- `ErrUnsupportedDialector`
- `ErrDatabaseConnectionFailed`
- `ErrDatabasePoolAccessFailed`

Migrator:

- `ErrInvalidConfig`
- `ErrDSNRequired`
- `ErrDialectorRequired`
- `ErrUnsupportedDialect`
- `ErrMigrationsPathRequired`
- `ErrInvalidMigrationsPath`
- `ErrDatabasePingFailed`
- `ErrAbsolutePathFailed`
- `ErrMigrateInstanceFailed`
- `ErrGetVersionFailed`
- `ErrDatabaseDirtyState`
- `ErrMigrationFailed`
- `ErrGetNewVersionFailed`
- `ErrReadPopulationDirectory`
- `ErrReadPopulateFile`
- `ErrPopulateExecutionFailed`

## SQL notes

- `NewSQL()` opens the database connection, validates the input, and pings the database before returning a connector
- `Query()` executes a query with optional arguments and maps each row using the provided scan function
- `Close()` releases the underlying connection pool
