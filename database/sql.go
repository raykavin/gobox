package database

import (
	"context"
	"database/sql"
	"fmt"
)

// SQLConfig holds the parameters needed to open a database connection.
type SQLConfig struct {
	// Driver is the database driver name registered via sql.Register
	// (e.g. "postgres", "mysql", "sqlite3").
	Driver string

	// DSN is the data source name passed verbatim to sql.Open.
	DSN string
}

// ScanFunc maps a single row from sql.Rows into a value of type T.
// It must call rows.Scan internally and must NOT advance the cursor -
// Connector.Query handles the rows.Next loop.
type ScanFunc[T any] func(rows *sql.Rows) (T, error)

// Connector is a generic database source that opens a connection using SQLConfig
// and converts each result row into T via a caller-supplied ScanFunc.
type Connector[T any] struct {
	db   *sql.DB
	scan ScanFunc[T]
}

// New opens and pings the database described by cfg, then returns a Connector
// ready to execute queries. The caller is responsible for calling Close when done.
func New[T any](cfg SQLConfig, scan ScanFunc[T]) (*Connector[T], error) {
	if cfg.Driver == "" {
		return nil, fmt.Errorf("database driver must not be empty")
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("database DSN must not be empty")
	}
	if scan == nil {
		return nil, fmt.Errorf("scan function must not be nil")
	}

	db, err := sql.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("opening database connection: %w", err)
	}

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &Connector[T]{db: db, scan: scan}, nil
}

// Query executes query with the optional args, applies the ScanFunc to each
// row and returns the accumulated results as []T.
func (c *Connector[T]) Query(ctx context.Context, query string, args ...any) ([]T, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	var results []T

	for rows.Next() {
		item, err := c.scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return results, nil
}

// Close releases the underlying database connection pool.
func (c *Connector[T]) Close() error {
	return c.db.Close()
}
