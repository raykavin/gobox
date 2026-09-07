# healthcheck

The `healthcheck` package probes registered database connections and collects Go runtime diagnostics. It is intended for services that expose a `/health` or `/readiness` endpoint and need connection pool statistics, runtime memory metrics, and uptime without coupling to a specific HTTP framework.

## Import

```go
import "github.com/raykavin/gobox/healthcheck"
```

## What it provides

- `Check` for managing named database connections and producing health snapshots
- `Report()` for probing all connections concurrently and collecting runtime metrics
- `CheckDB()` for probing a single named connection
- `AddDB()` and `RemoveDB()` for dynamically registering connections at runtime
- `SetPingTimeout()` for overriding the per-connection ping deadline (default: 5s)
- `Pinger` interface satisfied by `*sql.DB` out of the box, accepting any compatible type

## Main types

- `Check`: the main service; safe for concurrent use
- `DBEntry`: named connection passed to `New`
- `HealthReport`: full snapshot with `Runtime` and `Databases` fields
- `RuntimeReport`: OS, arch, CPU count, goroutine count, uptime, and memory stats
- `DBReport`: per-connection status, error, and pool counters

## Example

```go
package main

import (
    "database/sql"
    "encoding/json"
    "log"
    "net/http"

    "github.com/raykavin/gobox/healthcheck"
    _ "github.com/lib/pq"
)

func main() {
    db, err := sql.Open("postgres", "postgres://user:pass@localhost/mydb?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }

    svc := healthcheck.New([]healthcheck.DBEntry{
        {Name: "primary", Driver: "postgres", DB: db},
    })

    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        report := svc.Report(r.Context())

        // HealthReport carries no overall verdict, so derive one.
        status := http.StatusOK
        for _, db := range report.Databases {
            if db.Status != "healthy" {
                status = http.StatusServiceUnavailable
                break
            }
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(report)
    })

    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

## Example response

```json
{
  "runtime": {
    "os": "linux",
    "arch": "amd64",
    "num_cpu": 4,
    "num_goroutine": 8,
    "uptime": "2m35s",
    "mem_allocated": 1245184,
    "mem_total_alloc": 3801088,
    "mem_sys": 10485760,
    "mem_num_gc": 3,
    "mem_last_gc": "2026-06-28T14:00:00Z"
  },
  "databases": {
    "primary": {
      "status": "healthy",
      "driver": "postgres",
      "open_connections": 5,
      "in_use": 1,
      "idle": 4,
      "wait_count": 0,
      "wait_duration": 0
    }
  }
}
```

## Notes

- all registered connections are probed concurrently inside `Report`; one slow connection does not delay others
- each ping is bounded by `pingTimeout` (default 5s); use `SetPingTimeout` to adjust
- `AddDB` and `RemoveDB` are safe to call while `Report` is in progress
- `Pinger` is satisfied by `*sql.DB` directly; no adapter is needed for standard Go database connections
- `DBReport.Error` is only populated when `status` is `"unhealthy"`; the two status strings are `"healthy"` and `"unhealthy"`
- `HealthReport` has no aggregate status field and `Report` never returns an error, so a readiness endpoint must decide its own HTTP status by inspecting `Databases`
- `AddDB` replaces any existing entry registered under the same name
- `SetPingTimeout` resets to the 5s default when given a non-positive duration
- `CheckDB` returns an error only when the name is not registered; a failed ping is reported through `DBReport.Status` instead
