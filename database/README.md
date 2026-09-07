# database

`database` is not a Go package. It is a directory grouping three independent packages for relational database work, each importable on its own:

| Package | Import path | Use it for |
|---|---|---|
| [`database/sql`](./sql/README.md) | `github.com/raykavin/gobox/database/sql` | A lightweight typed wrapper over `database/sql` with caller-supplied row scanning |
| [`database/gorm`](./gorm/README.md) | `github.com/raykavin/gobox/database/gorm` | A GORM connection factory with pooling, logging, and startup retry |
| [`database/migrate`](./migrate/README.md) | `github.com/raykavin/gobox/database/migrate` | Versioned schema migrations and seed execution via golang-migrate |

There is no `github.com/raykavin/gobox/database` package to import, and the three subpackages know nothing about each other. Pick the one that matches the abstraction level you want, or combine them: `migrate` at startup, then `gorm` or `sql` for queries.

## Choosing between them

- **`database/sql`** keeps you in control of every query and every scan. Reach for it when the queries are few and hand-written, or when an ORM would only get in the way.
- **`database/gorm`** gives you a configured `*gorm.DB`. Reach for it when you want models, associations, and `AutoMigrate`.
- **`database/migrate`** is orthogonal to both. It applies `.up.sql` files from a directory and optionally runs seed scripts, so it pairs with either of the other two.

## Dialect support

The three packages do not support the same set of drivers, because each delegates to a different underlying library:

| Package | Supported |
|---|---|
| `database/sql` | any driver registered with `sql.Register`, imported by the caller |
| `database/gorm` | `postgres`, `mysql`, `mariadb`, `sqlite`, `sqlserver`, `mssql` |
| `database/migrate` | `postgres`, `mysql`, `sqlite3` |

Note the naming difference: `gorm` expects `sqlite` while `migrate` expects `sqlite3`.

## Quick start

```go
import (
    gormdb "github.com/raykavin/gobox/database/gorm"
    "github.com/raykavin/gobox/database/migrate"
)

// 1. Bring the schema up to date.
m, err := migrate.New(migrate.MigrateConfig{
    DSN:            dsn,
    Dialector:      "postgres",
    MigrationsPath: "./migrations",
})
if err != nil {
    return err
}
if err := m.Migrate(ctx); err != nil {
    return err
}

// 2. Open the connection the application will use.
cfg := gormdb.DefaultGormConfig()
cfg.DSN = dsn
cfg.Dialector = "postgres"

db, err := gormdb.New(cfg)
if err != nil {
    return err
}
```

Each subpackage README carries its own configuration reference, defaults, error sentinels, and examples.
