// Package sql provides a generic database connector that pairs a standard
// database/sql connection with a caller-supplied row scanner.
//
// Connector[T] opens and pings the database on construction, then exposes a
// single Query method that executes a query and maps each row to T using the
// provided ScanFunc.
//
// # Usage
//
//	type User struct {
//	    ID   int
//	    Name string
//	}
//
//	conn, err := sql.NewSQL(sql.SQLConfig{
//	    Driver: "postgres",
//	    DSN:    "postgres://user:pass@localhost/mydb?sslmode=disable",
//	}, func(rows *stdsql.Rows) (User, error) {
//	    var u User
//	    return u, rows.Scan(&u.ID, &u.Name)
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer conn.Close()
//
//	users, err := conn.Query(ctx, "SELECT id, name FROM users WHERE active = $1", true)
package sql
