# database/migrate

The `migrate` package provides schema migration and seed data execution. It is built on top of [golang-migrate](https://github.com/golang-migrate/migrate) and is intended for services that need to apply versioned SQL migrations and optional seed files at startup.

## Import

```go
import "github.com/raykavin/gobox/database/migrate"
```

## What it provides

- `Migrator` for applying pending schema migrations from a local directory
- `Populate` for executing seed `.sql` files in directory order
- support for `postgres`, `mysql`, and `sqlite3`
- dirty-state detection before applying migrations
- context-aware execution with cancellation checks

## Main types

- `MigrateConfig`: connection and path settings
- `Migrator`: runs migrations and population against the configured database

## Example

```go
package main

import (
    "context"
    "log"

    "github.com/raykavin/gobox/database/migrate"
)

func main() {
    ctx := context.Background()

    m, err := migrate.New(migrate.MigrateConfig{
        DSN:            "postgres://user:pass@localhost/mydb?sslmode=disable",
        Dialector:      "postgres",
        MigrationsPath: "./migrations",
        PopulationPath: "./seeds",
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := m.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    if err := m.Populate(ctx); err != nil {
        log.Fatal(err)
    }
}
```

## Migration file naming

Files follow the golang-migrate convention:

```
migrations/
  000001_create_users.up.sql
  000001_create_users.down.sql
  000002_add_email_index.up.sql
  000002_add_email_index.down.sql
```

## Errors

All sentinels can be matched with `errors.Is`. Validation failures are wrapped as `ErrInvalidConfig` joined with the specific cause, so both match.

### Configuration

| Sentinel | Cause |
|---|---|
| `ErrInvalidConfig` | wraps every validation failure below |
| `ErrDSNRequired` | `DSN` is empty |
| `ErrDialectorRequired` | `Dialector` is empty |
| `ErrUnsupportedDialect` | `Dialector` is not `postgres`, `mysql`, or `sqlite3` |
| `ErrMigrationsPathRequired` | `MigrationsPath` is empty |
| `ErrInvalidMigrationsPath` | `MigrationsPath` does not exist on disk |

### Setup

| Sentinel | Cause |
|---|---|
| `ErrDatabaseConnectionFailed` | `sql.Open` failed |
| `ErrDatabasePingFailed` | database unreachable after open |
| `ErrAbsolutePathFailed` | `MigrationsPath` could not be resolved to an absolute path |
| `ErrMigrateInstanceFailed` | the underlying golang-migrate instance could not be created |

### Migration

| Sentinel | Cause |
|---|---|
| `ErrGetVersionFailed` | the current migration version could not be read |
| `ErrDatabaseDirtyState` | a previous migration left the database in a dirty state |
| `ErrMigrationFailed` | `migrate.Up()` returned an unexpected error |
| `ErrGetNewVersionFailed` | the version could not be re-read after applying |

### Population

| Sentinel | Cause |
|---|---|
| `ErrReadPopulationDirectory` | `PopulationPath` could not be read |
| `ErrReadPopulateFile` | a seed file could not be read |
| `ErrPopulateExecutionFailed` | a seed file failed to execute |

## Notes

- `Migrate` is a no-op when there are no pending migrations
- `Populate` is a no-op when `PopulationPath` is empty
- seed files are executed in the order returned by `os.ReadDir`, which is alphabetical
- if the database is in a dirty state, manual intervention is required before migrations can proceed
- `MigrationsPath` must exist when `New` is called; `PopulationPath` is only read when `Populate` runs
- the drivers for all three supported dialects are linked in by this package, so callers do not need their own blank imports
