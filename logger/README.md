# logger

The `logger` package provides a structured logger built on top of [zerolog](https://github.com/rs/zerolog). It is intended for services that need colored console output during development, JSON output for production log aggregators, HTTP request logging, and structured field attachment without importing a heavier logging framework.

## Import

```go
import "github.com/raykavin/gobox/logger"
```

## What it provides

- `Zerolog` for structured logging with colored console or JSON output
- `Logger` wrapping `Zerolog` with `Print`, `Debug`, `Info`, `Warn`, `Error`, `Fatal`, and `Panic` families, each with an `f` formatting variant
- `WithField`, `WithFields`, and `WithError` for attaching context to log entries
- `WithContext` for attaching a map of arbitrary key-value pairs to a logger instance
- `API` for logging HTTP request details at the appropriate level based on status code
- `Benchmark` for recording named duration measurements
- `Success` and `Failure` as semantic aliases for info and error
- `SetLevel` and `Level` for reading and changing the minimum emitted level at runtime

## Main types

- `Config`: log level, timestamp format, color toggle, JSON toggle, and emoji toggle
- `Zerolog`: the core logger, embedding `*zerolog.Logger`; safe for concurrent use
- `Logger`: embeds `*Zerolog` and satisfies common logging interfaces

## Example

```go
package main

import (
    "time"

    "github.com/raykavin/gobox/logger"
)

func main() {
    zl, err := logger.New(&logger.Config{
        Level:          "debug",
        DateTimeLayout: time.RFC3339,
        Colored:        true,
        JSONFormat:     false,
    })
    if err != nil {
        panic(err)
    }

    log := &logger.Logger{Zerolog: zl}

    log.Info("application started")
    log.WithField("port", 8080).Info("listening")
    log.WithError(err).Error("startup failed")
}
```

Passing a `nil` `*Config` to `New` uses the defaults listed below.

## API request logging example

```go
start := time.Now()
// ... handle request ...
zl.API(r.Method, r.URL.Path, r.RemoteAddr, w.Status(), time.Since(start))
```

The log level is chosen automatically: `info` for 2xx and 3xx, `warn` for 4xx, and `error` for 5xx. `API` takes an optional trailing `skipFrameCount` argument to correct the reported caller when it is invoked from a wrapper. Calls with an empty method or path are rejected and logged as an error.

## Runtime level control

```go
if err := logger.SetLevel("debug"); err != nil {
    // ErrInvalidLogLevel joined with the parse error
}

current := logger.Level() // "debug"
```

`SetLevel` writes zerolog's package-level atomic threshold, so it is safe to call while other goroutines are logging and takes effect on the next record. This is what lets a process raise verbosity during a live incident without a restart.

## Config reference

| Field | Default | Description |
|---|---|---|
| `Level` | `"info"` | Minimum log level (trace, debug, info, warn, error, fatal, panic) |
| `DateTimeLayout` | `time.RFC3339` | Timestamp format for console output |
| `Colored` | `true` | Enable ANSI colors in console mode |
| `JSONFormat` | `false` | Emit JSON lines instead of formatted console output |
| `UseEmoji` | `false` | Prefix unknown log levels with an emoji |

Defaults apply only when `New` is called with a `nil` config; a non-nil `Config` is used exactly as provided, so its zero values are not filled in.

## Errors

| Error | Returned when |
|---|---|
| `ErrInvalidLogLevel` | `New` or `SetLevel` receives a level zerolog cannot parse; joined with the underlying parse error |

## Notes

- `New` sets the global zerolog level, which affects all zerolog loggers in the process
- caller information (file and line number) is included automatically with a skip frame count of 3
- `WithField`, `WithFields`, and `WithError` return new `Logger` instances; the original is not modified
- in JSON mode the output still goes through zerolog's `ConsoleWriter`; for true JSON lines, wire zerolog directly to `os.Stdout` using the embedded `*zerolog.Logger`
- `logger/zerolog` is a legacy copy of this package kept for existing imports; new code should use `logger`, which is the only one exposing `SetLevel` and `Level`
