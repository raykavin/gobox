# Gobox

> A curated collection of reusable Go packages for common infrastructure concerns.

[![Go Reference](https://pkg.go.dev/badge/github.com/raykavin/gobox.svg)](https://pkg.go.dev/github.com/raykavin/gobox)
[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8?logo=go&logoColor=white)](https://golang.org/dl/)
[![Go Report Card](https://goreportcard.com/badge/github.com/raykavin/gobox)](https://goreportcard.com/report/github.com/raykavin/gobox)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Gobox is a Go module that centralizes shared libraries reused across multiple projects. The goal is to keep common building blocks in one place so teams can reduce code duplication, standardize recurring infrastructure concerns, and move faster when starting or evolving services.

Each package is independently importable, has zero knowledge of the others, and ships with its own README and godoc.

## Installation

```sh
go get github.com/raykavin/gobox
```

Then import only the packages you need:

```go
import "github.com/raykavin/gobox/log"
import "github.com/raykavin/gobox/retry"
```

## Packages

### Observability

| Package | Description |
|---|---|
| [`log`](./log/README.md) | Structured logger built on zerolog with colored console output, JSON mode, and HTTP request logging |
| [`telemetry`](./telemetry/README.md) | OpenTelemetry bootstrap: OTLP tracing, Prometheus metrics, and pluggable custom collectors |
| [`healthcheck`](./healthcheck/README.md) | Health snapshot with concurrent database probes and Go runtime diagnostics |

### HTTP

| Package | Description |
|---|---|
| [`httpclient`](./httpclient/README.md) | Thin HTTP client wrapper with header presets, query params, and response decompression (gzip, br, zstd) |
| [`httpserver`](./httpserver/README.md) | Gin-based HTTP server with TLS, HTTP/2, timeouts, payload limits, and graceful shutdown |

### Database

| Package | Description |
|---|---|
| [`database/gorm`](./database/gorm/README.md) | GORM connection factory with pooling, structured logging, and retry support |
| [`database/migrate`](./database/migrate/README.md) | Schema migration and seed execution via golang-migrate (postgres, mysql, sqlite3) |
| [`database/sql`](./database/sql/README.md) | Generic `database/sql` connector with a caller-supplied row scanner |

### Configuration and resilience

| Package | Description |
|---|---|
| [`config`](./config/README.md) | Configuration loading with Viper: env expansion, validation, hot-reload, and typed change events |
| [`retry`](./retry/README.md) | Context-aware retry with exponential backoff and caller-defined retry policy |

### Workflows

| Package | Description |
|---|---|
| [`temporal`](./temporal/README.md) | Temporal client factory and activity options builder with sensible retry defaults |

### Security

| Package | Description |
|---|---|
| [`oidcauth`](./oidcauth/README.md) | OIDC token verification with optional in-memory cache and Keycloak role helpers |

### Utilities

| Package | Description |
|---|---|
| [`spreadsheet`](./spreadsheet/README.md) | CSV and XLSX writer with multi-sheet support and configurable header styling |
| [`cli`](./cli/README.md) | Terminal helpers: ASCII art banner, colored system header, and concurrent progress display |

### Integrations

| Package | Description |
|---|---|
| [`integration/prest`](./integration/prest/README.md) | Generic OAuth2-authenticated HTTP client for pREST APIs with typed JSON responses |

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
