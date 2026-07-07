# log

The `log` package provides a structured logger built on top of [zerolog](https://github.com/rs/zerolog). It is intended for services that need colored console output during development, JSON output for production log aggregators, HTTP request logging, and structured field attachment without importing a heavier logging framework.

## Import

```go
import "github.com/raykavin/gobox/log"
```

## What it provides

- `Zerolog` for structured logging with colored console or JSON output
- `Logger` wrapping `Zerolog` with `Print`, `Debug`, `Info`, `Warn`, `Error`, `Fatal`, and `Panic` families
- `WithField`, `WithFields`, and `WithError` for attaching context to log entries
- `API` for logging HTTP request details at the appropriate level based on status code
- `Benchmark` for recording named duration measurements
- `Success` and `Failure` as semantic aliases for info and error
- `WithContext` for attaching a map of arbitrary key-value pairs to a logger instance

## Main types

- `Config`: log level, timestamp format, color toggle, JSON toggle, and emoji toggle
- `Zerolog`: the core logger; safe for concurrent use
- `Logger`: embeds `Zerolog` and satisfies common logging interfaces

## Example

```go
package main

import (
    "time"
    "github.com/raykavin/gobox/log"
)

func main() {
    zl, err := log.New(&log.Config{
        Level:          "debug",
        DateTimeLayout: time.RFC3339,
        Colored:        true,
        JSONFormat:     false,
    })
    if err != nil {
        panic(err)
    }

    logger := &log.Logger{Zerolog: zl}

    logger.Info("application started")
    logger.WithField("port", 8080).Info("listening")
    logger.WithError(err).Error("startup failed")
}
```

## API request logging example

```go
start := time.Now()
// ... handle request ...
zl.API(r.Method, r.URL.Path, r.RemoteAddr, w.Status(), time.Since(start))
```

The log level is chosen automatically: `info` for 2xx and 3xx, `warn` for 4xx, and `error` for 5xx.

## Config reference

| Field | Default | Description |
|---|---|---|
| `Level` | `"info"` | Minimum log level (trace, debug, info, warn, error, fatal, panic) |
| `DateTimeLayout` | `time.RFC3339` | Timestamp format for console output |
| `Colored` | `true` | Enable ANSI colors in console mode |
| `JSONFormat` | `false` | Emit JSON lines instead of formatted console output |
| `UseEmoji` | `false` | Prefix unknown log levels with an emoji |

## Notes

- `New` sets the global zerolog level, which affects all zerolog loggers in the process
- caller information (file and line number) is included automatically with a skip frame count of 3
- `WithField`, `WithFields`, and `WithError` return new `Logger` instances; the original is not modified
- in JSON mode the output format still goes through zerolog's `ConsoleWriter`; for true JSON lines, wire zerolog directly to `os.Stdout` using the underlying `*zerolog.Logger`
