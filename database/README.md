# database

The `database` package provides a lightweight abstraction over `database/sql` for shared data-access helpers. It keeps the module free from driver-specific dependencies while letting each consuming project decide how rows should be scanned into application types.

## Import

```go
import "github.com/raykavin/gokit/database"
```

## What it provides

- a small `Config` type for driver and DSN setup
- a generic `Connector[T]` for typed query results
- caller-defined row mapping through `ScanFunc[T]`
- connection startup validation through `PingContext`

## Main types

- `Config`: database driver name and DSN
- `ScanFunc[T]`: maps a single `sql.Rows` record into `T`
- `Connector[T]`: executes queries and returns `[]T`

## Example

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
	conn, err := database.New(database.Config{
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

## Notes

- `New()` opens the database connection, validates the input, and pings the database before returning a connector
- `Query()` executes a query with optional arguments and maps each row using the provided scan function
- `Close()` releases the underlying connection pool
