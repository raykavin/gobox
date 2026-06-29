# database/sql

The `sql` package provides a generic database connector built on top of `database/sql`. It is intended for cases where a lightweight query helper is preferred over a full ORM: open a connection, describe how to scan a row into a type, and call `Query` anywhere.

## Import

```go
import "github.com/raykavin/gobox/database/sql"
```

## What it provides

- `Connector[T]` for executing queries and mapping each row to a caller-defined type
- `ScanFunc[T]` for describing how a single `*sql.Rows` cursor maps to `T`
- `NewSQL` for opening, pinging, and wrapping a database connection

## Main types

- `SQLConfig`: driver name and DSN
- `ScanFunc[T]`: `func(rows *sql.Rows) (T, error)` called once per row
- `Connector[T]`: holds the connection and scan function, exposes `Query` and `Close`

## Example

```go
package main

import (
    "context"
    stdsql "database/sql"
    "log"

    kitql "github.com/raykavin/gobox/database/sql"
    _ "github.com/lib/pq"
)

type User struct {
    ID   int
    Name string
}

func main() {
    conn, err := kitql.NewSQL(kitql.SQLConfig{
        Driver: "postgres",
        DSN:    "postgres://user:pass@localhost/mydb?sslmode=disable",
    }, func(rows *stdsql.Rows) (User, error) {
        var u User
        return u, rows.Scan(&u.ID, &u.Name)
    })
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    users, err := conn.Query(context.Background(),
        "SELECT id, name FROM users WHERE active = $1", true)
    if err != nil {
        log.Fatal(err)
    }

    for _, u := range users {
        log.Printf("%d: %s", u.ID, u.Name)
    }
}
```

## Notes

- the database driver must be imported separately with a blank import (e.g. `_ "github.com/lib/pq"`)
- `NewSQL` calls `PingContext` immediately; a connection failure returns an error before the `Connector` is returned
- `ScanFunc` must call `rows.Scan` internally and must not advance the cursor; `Query` handles the `rows.Next` loop
- `Query` returns `nil, nil` (not an error) when the result set is empty
- `Close` releases the underlying connection pool; it should be called when the `Connector` is no longer needed
