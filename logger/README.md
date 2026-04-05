# logger

The `logger` package provides a reusable logging layer built on top of `zerolog`. It is intended for shared service logging where applications need consistent formatting, contextual fields, caller metadata, and helper methods for common operational events.

## Import

```go
import "github.com/raykavin/gokit/logger"
```

## What it provides

- configurable log level and timestamp layout
- optional colored output
- configurable format mode through `Config`
- contextual fields for structured logs
- helper methods for success, failure, benchmark, and API request logging
- caller metadata in log output
- formatted error rendering for nested error details

## Main types

- `Config`: logger settings such as level, colors, timestamp format, and output mode
- `Zerolog`: wraps `zerolog.Logger` with additional helpers
- `Logger`: adds `Print` and `Printf` style convenience methods on top of `Zerolog`

## Example

```go
package main

import (
	"log"
	"time"

	"github.com/raykavin/gokit/logger"
)

func main() {
	appLogger, err := logger.New(&logger.Config{
		Level:          "debug",
		DateTimeLayout: time.RFC3339,
		Colored:        true,
		JSONFormat:     false,
		UseEmoji:       false,
	})
	if err != nil {
		log.Fatal(err)
	}

	appLogger.Info().
		Str("service", "billing").
		Msg("service started")

	appLogger.WithContext(map[string]any{
		"request_id": "req-123",
		"component":  "http",
	}).API("GET", "/health", "127.0.0.1", 200, 42*time.Millisecond)
}
```

## Notes

- `New()` creates a logger with defaults when `nil` config is provided
- `WithContext()` returns a new logger enriched with structured fields
- `API()` logs HTTP request metadata with a level derived from the status code
- `Benchmark()`, `Success()`, and `Failure()` provide convenience helpers for common operational logs
- `ErrInvalidLogLevel` is returned when the configured level cannot be parsed
