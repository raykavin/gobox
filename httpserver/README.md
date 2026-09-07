# httpserver

The `httpserver` package provides a Gin-based HTTP server with TLS, HTTP/2, configurable timeouts, trusted proxies, payload size limits, and graceful shutdown. It is intended for services that want a production-ready server setup without writing the same boilerplate in every project.

## Import

```go
import "github.com/raykavin/gobox/httpserver"
```

## What it provides

- `NewGin()` for creating a configured Gin engine
- `DefaultGinConfig()` for a sensible starting configuration
- `Engine.SetupRoutes()`, `Engine.SetupMiddleware()`, and `Engine.Use()` for attaching routes and middleware
- `Engine.Listen()` for starting the server (HTTP or HTTPS)
- `Engine.Shutdown()` for graceful shutdown with a context deadline
- `Engine.Router()` and `Engine.Server()` for reaching the underlying `*gin.Engine` and `*http.Server`
- `DownloadFile()` for writing a byte slice to a response as a downloadable attachment
- TLS 1.2+ with curated cipher suites when `UseSSL` is true
- HTTP/2 support when both `UseSSL` and `EnableHTTP2` are true
- automatic `gin.Recovery()` middleware when `UseRecovery` is true
- configurable `MaxPayloadSize` enforced via `http.MaxBytesReader`

## Main types

- `GinConfig`: all server settings
- `Engine`: wraps `*gin.Engine` and `*http.Server` with lifecycle methods
- `RouteSetup`: `func(*gin.Engine)` passed to `SetupRoutes`
- `MiddlewareSetup`: `func(*gin.Engine)` passed to `SetupMiddleware`

## Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/raykavin/gobox/httpserver"
)

func main() {
    cfg := httpserver.DefaultGinConfig()
    cfg.Port = 8080

    engine, err := httpserver.NewGin(cfg)
    if err != nil {
        log.Fatal(err)
    }

    engine.SetupRoutes(func(r *gin.Engine) {
        r.GET("/ping", func(c *gin.Context) {
            c.JSON(http.StatusOK, gin.H{"message": "pong"})
        })
    })

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        if err := engine.Listen(); err != nil && err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()
    log.Printf("listening on port %d", engine.GetPort())

    <-quit

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := engine.Shutdown(ctx); err != nil {
        log.Fatal(err)
    }
    log.Println("server stopped")
}
```

## Engine methods

| Method | Description |
|---|---|
| `SetupRoutes(RouteSetup)` | Runs the callback against the underlying `*gin.Engine` to register routes |
| `SetupMiddleware(MiddlewareSetup)` | Runs the callback against the underlying `*gin.Engine` to register middleware |
| `Use(...gin.HandlerFunc)` | Attaches middleware directly, without a callback |
| `Listen() error` | Starts the server, choosing HTTP or HTTPS from `UseSSL` |
| `ListenAndServeTLS(certFile, keyFile string) error` | Starts an HTTPS server with an explicit key pair, ignoring `SSLCert` and `SSLKey` |
| `Shutdown(ctx) error` | Graceful shutdown bounded by the context deadline |
| `Router() *gin.Engine` | The underlying Gin engine |
| `Server() *http.Server` | The underlying HTTP server |
| `Addr() string` | The resolved listen address |
| `GetPort() uint16` | The configured port |
| `IsSSLEnabled() bool` | Whether the server was configured for TLS |

## Serving a file download

```go
r.GET("/report.csv", func(c *gin.Context) {
    if err := httpserver.DownloadFile(c.Writer, "report.csv", "text/csv", content); err != nil {
        c.AbortWithStatus(http.StatusInternalServerError)
    }
})
```

## Default values

| Field | Default |
|---|---|
| `Port` | 8080 |
| `ReadTimeout` | 15s |
| `WriteTimeout` | 15s |
| `IdleTimeout` | 60s |
| `MinTLSVersion` | TLS 1.2 |
| `EnableHTTP2` | true |
| `UseRecovery` | true |
| `NoRouteJSON` | true |
| `MaxPayloadSize` | 10 MB |

## Errors

| Sentinel | Cause |
|---|---|
| `ErrInvalidListenAddress` | `Port` is 0 |
| `ErrHostResolutionFailed` | `Host` is set but the address cannot be resolved |
| `ErrInvalidSSLConfig` | `UseSSL` is true but `SSLCert` or `SSLKey` is empty |
| `ErrServerNotInitialized` | `Listen` or `Shutdown` called before `NewGin` succeeded |

## Notes

- HTTP/2 is only active when `UseSSL` is also true; plain HTTP/2 (h2c) is not supported
- when `NoRouteJSON` is true, unmatched routes return a JSON 404 body instead of redirecting
- set `NoRouteTo` to redirect unmatched routes to a fallback path (takes effect only when `NoRouteJSON` is false)
- `DebugMode: true` sets Gin to debug mode and logs all registered routes on startup
- `NewGin(nil)` is valid and falls back to `DefaultGinConfig()`
- `Host` defaults to empty, which binds every interface; set it to restrict the bind address
- `Listen` returns `http.ErrServerClosed` after a normal `Shutdown`, so treat that value as success
- the sibling packages [`httpserver/middlewares`](./middlewares/README.md) and [`httpserver/respond`](./respond/README.md) provide ready-made middleware and response helpers for this engine
