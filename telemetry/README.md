# telemetry

The `telemetry` package initialises OpenTelemetry tracing and metrics for a service in a single call. It is intended for services that need distributed tracing via OTLP/gRPC, Prometheus metrics scraping, and a pluggable system for custom metric collectors, without wiring up each OTel component separately in every project.

## Import

```go
import "github.com/raykavin/gobox/telemetry"
```

## What it provides

- `New()` for installing the global `TracerProvider` and `MeterProvider` and starting a Prometheus `/metrics` HTTP server
- `Collector` interface for plugging in custom metric sources (polling loops, external service probes, etc.)
- OTLP/gRPC trace export with `AlwaysSample` and W3C TraceContext + Baggage propagation
- Prometheus metrics with Go runtime and process metrics included by default
- a single `shutdown` function that flushes spans, drains the meter, and stops the metrics server

## Main types

- `Config`: service name, OTLP endpoint, Prometheus port, and optional collectors
- `Collector`: interface with `RegisterMetrics() error` and `Start(ctx context.Context)`

## Quick start

```go
package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"

    "go.opentelemetry.io/otel"
    "github.com/raykavin/gobox/telemetry"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    shutdown, err := telemetry.New(ctx, telemetry.Config{
        ServiceName:  "my-service",
        OTLPEndpoint: "otel-collector:4317",
        MetricsPort:  9090,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer shutdown(ctx)

    // tracing
    tracer := otel.Tracer("my-service/orders")
    ctx2, span := tracer.Start(ctx, "process-order")
    defer span.End()
    _ = ctx2

    // metrics
    meter := otel.Meter("my-service/orders")
    counter, _ := meter.Int64Counter("orders.processed")
    counter.Add(ctx, 1)

    <-ctx.Done()
}
```

## Implementing a custom Collector

A `Collector` registers its OTel instruments once and optionally runs a background loop to feed them. The example below polls an external HTTP endpoint every 15 seconds and exposes the result as an observable gauge.

```go
package mymetrics

import (
    "context"
    "sync/atomic"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

type ServiceCollector struct {
    up atomic.Int64
}

func (c *ServiceCollector) RegisterMetrics() error {
    meter := otel.GetMeterProvider().Meter("my-service/external")
    _, err := meter.Int64ObservableGauge(
        "external.up",
        metric.WithDescription("1 if the external service is reachable, 0 otherwise"),
        metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
            o.Observe(c.up.Load())
            return nil
        }),
    )
    return err
}

func (c *ServiceCollector) Start(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(15 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                c.up.Store(c.probe(ctx))
            }
        }
    }()
}

func (c *ServiceCollector) probe(ctx context.Context) int64 {
    // ... check the external service ...
    return 1
}
```

Register it alongside `New`:

```go
shutdown, err := telemetry.New(ctx, telemetry.Config{
    ServiceName: "my-service",
    MetricsPort: 9090,
    Collectors:  []telemetry.Collector{&mymetrics.ServiceCollector{}},
})
```

## Prometheus endpoint

The `/metrics` endpoint is served on `MetricsPort` and includes:

- Go runtime metrics (goroutines, heap, GC cycles, etc.) via `collectors.NewGoCollector`
- Process metrics (CPU, file descriptors, resident memory) via `collectors.NewProcessCollector`
- All OTel instruments registered through the global `MeterProvider`

## Notes

- `OTLPEndpoint` defaults to the OTel SDK default (`localhost:4317`) when empty
- the trace exporter uses an insecure gRPC connection; add TLS by customising `newTracerProvider` if needed
- a `Collector` that returns an error from `RegisterMetrics` is silently skipped; `Start` is not called for it
- `Start` must return immediately; long-running work must run in a goroutine bound to the supplied context
- the shutdown function is always safe to call, even when `New` returned an error
