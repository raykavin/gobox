# Gobox

> A curated collection of reusable Go packages for common infrastructure concerns.

[![Go Reference](https://pkg.go.dev/badge/github.com/raykavin/gobox.svg)](https://pkg.go.dev/github.com/raykavin/gobox)
[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8?logo=go&logoColor=white)](https://golang.org/dl/)
[![Go Report Card](https://goreportcard.com/badge/github.com/raykavin/gobox)](https://goreportcard.com/report/github.com/raykavin/gobox)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Gobox is a Go module that centralizes shared libraries reused across multiple projects. The goal is to keep common building blocks in one place so teams can reduce code duplication, standardize recurring infrastructure concerns, and move faster when starting or evolving services.

Each package is independently importable, and ships with its own README and godoc. Packages are deliberately kept free of cross-dependencies, with the few documented exceptions noted below.

## Installation

```sh
go get github.com/raykavin/gobox
```

Then import only the packages you need:

```go
import "github.com/raykavin/gobox/logger"
import "github.com/raykavin/gobox/retry"
```

## Packages

### Observability

| Package | Description |
|---|---|
| [`logger`](./logger/README.md) | Structured logger built on zerolog with colored console output, JSON mode, HTTP request logging, and runtime level control |
| [`telemetry`](./telemetry/README.md) | OpenTelemetry bootstrap: OTLP tracing, Prometheus metrics, and pluggable custom collectors |
| [`healthcheck`](./healthcheck/README.md) | Health snapshot with concurrent database probes and Go runtime diagnostics |

### HTTP

| Package | Description |
|---|---|
| [`httpclient`](./httpclient/README.md) | Thin HTTP client wrapper with header presets, query params, and response decompression (gzip, deflate, br, zstd) |
| [`httpserver`](./httpserver/README.md) | Gin-based HTTP server with TLS, HTTP/2, timeouts, payload limits, and graceful shutdown |
| [`httpserver/middlewares`](./httpserver/middlewares/README.md) | Gin middleware for authorization, roles, CORS, CSRF, per-IP rate limiting, and a full OIDC login flow |
| [`httpserver/respond`](./httpserver/respond/README.md) | A single JSON response envelope, plus sentinel-error to HTTP status mapping |

### Database

| Package | Description |
|---|---|
| [`database/gorm`](./database/gorm/README.md) | GORM connection factory with pooling, structured logging, and startup retry |
| [`database/migrate`](./database/migrate/README.md) | Schema migration and seed execution via golang-migrate (postgres, mysql, sqlite3) |
| [`database/sql`](./database/sql/README.md) | Generic `database/sql` connector with a caller-supplied row scanner |
| [`pagination`](./pagination/README.md) | Offset pagination for GORM with a fluent filter and sort builder, and a generic result envelope |

### Configuration and resilience

| Package | Description |
|---|---|
| [`config`](./config/README.md) | Configuration loading with Viper: env expansion, validation, hot-reload, and typed change events |
| [`retry`](./retry/README.md) | Context-aware retry with exponential backoff and a caller-defined retry policy |

### Workflows

| Package | Description |
|---|---|
| [`temporal`](./temporal/README.md) | Temporal client factory and activity options builder with sensible retry defaults |

### Security

| Package | Description |
|---|---|
| [`oidcauth`](./oidcauth/README.md) | OIDC token verification, RFC 7662 introspection, the Authorization Code + PKCE flow, and server-side sessions |
| [`oauth2`](./oauth2/README.md) | Client-side OAuth 2.0 token manager with per-scope caching for outbound calls |
| [`secure`](./secure/README.md) | Authenticated encryption (AES-256-GCM) for opaque byte payloads |
| [`totp`](./totp/README.md) | RFC 6238 time-based one-time passwords: secrets, codes, validation, and enrollment URIs |

### Utilities

| Package | Description |
|---|---|
| [`spreadsheet`](./spreadsheet/README.md) | CSV and XLSX writer with multi-sheet support and configurable header styling |
| [`cli`](./cli/README.md) | Terminal helpers: ASCII art banner, colored system header, and concurrent progress display |

### Directories that are not packages

[`database`](./database/README.md) groups the three database packages above and holds only their shared README. [`logger/zerolog`](./logger/zerolog/README.md) is a legacy copy of `logger`, kept so existing imports keep compiling; new code should use `logger`.

## Internal dependencies

Packages are independent, with these exceptions:

| Package | Imports | For |
|---|---|---|
| `httpserver/middlewares` | `oidcauth` | Token verification, the login flow, and sessions |
| `httpserver/middlewares` | `httpserver/respond` | Writing error responses in the shared envelope |
| `oauth2` | `httpclient` | Its token requests |

Everything else depends only on the standard library and third-party modules.

## When to add a package

A good rule of thumb: move code here when it appears in more than one service, represents generic infrastructure logic, and carries no business-domain coupling. Keep domain rules in the application.

## Contributing

Contributions to gobox are welcome! Here are some ways you can help:

- **Report bugs and suggest features** by opening issues on GitHub
- **Submit pull requests** with bug fixes or new features
- **Improve documentation** to help other users and developers

---

## License

gobox is distributed under the **MIT License**.  
For complete license terms and conditions, see the [LICENSE](LICENSE) file in the repository.

---

## Contact

For support, collaboration, or questions about gobox:

**Email**: [raykavin.meireles@gmail.com](mailto:raykavin.meireles@gmail.com)  
**GitHub**: [@raykavin](https://github.com/raykavin)
