// Package telemetry initialises OpenTelemetry tracing and metrics for a
// service in a single call, exposing a Prometheus /metrics endpoint and
// sending traces to an OTLP/gRPC collector.
//
// # Initialisation
//
// Call New once at startup. It installs the global TracerProvider and
// MeterProvider, starts the Prometheus HTTP server, and returns a shutdown
// function that must be called on exit to flush pending spans.
//
//	shutdown, err := telemetry.New(ctx, telemetry.Config{
//	    ServiceName:  "my-service",
//	    OTLPEndpoint: "otel-collector:4317",
//	    MetricsPort:  9090,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer shutdown(ctx)
//
// # Tracing
//
// After New returns, use the global tracer from anywhere in the application.
//
//	tracer := otel.Tracer("my-service/orders")
//	ctx, span := tracer.Start(ctx, "process-order")
//	defer span.End()
//
// # Metrics
//
// After New returns, use the global meter from anywhere in the application.
//
//	meter := otel.Meter("my-service/orders")
//	counter, _ := meter.Int64Counter("orders.processed")
//	counter.Add(ctx, 1)
//
// # Pluggable collectors
//
// Custom metric sources implement the Collector interface and are passed in
// Config.Collectors. Each collector registers its instruments once and
// optionally runs a background polling loop.
//
//	shutdown, err := telemetry.New(ctx, telemetry.Config{
//	    ServiceName: "my-service",
//	    MetricsPort: 9090,
//	    Collectors:  []telemetry.Collector{myCollector},
//	})
//
// A collector that fails RegisterMetrics is skipped without stopping the
// rest of the subsystem.
package telemetry
