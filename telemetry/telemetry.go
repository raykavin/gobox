package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Config holds the runtime settings for the telemetry subsystem.
type Config struct {
	// ServiceName identifies this service in traces and metrics. Required.
	ServiceName string

	// OTLPEndpoint is the OTLP/gRPC collector address (host:port). When empty,
	// the exporter's default endpoint is used.
	OTLPEndpoint string

	// MetricsPort is the TCP port for the Prometheus /metrics HTTP server.
	MetricsPort uint16

	// Collectors are optional pluggable metric sources. Each is registered and
	// started during New, after the global providers are installed. A
	// collector whose RegisterMetrics fails is skipped without aborting the
	// rest of the subsystem.
	Collectors []Collector
}

// New initialises the global TracerProvider and MeterProvider, then starts
// a dedicated HTTP server on cfg.MetricsPort exposing /metrics for Prometheus.
// Any collectors supplied in cfg.Collectors are registered and started.
//
// The returned shutdown function must be called on application exit to flush
// pending spans and stop the metrics server. It is safe to call even if New
// returned an error (it becomes a no-op).
func New(ctx context.Context, cfg Config) (shutdown func(context.Context), err error) {
	noop := func(context.Context) {}

	tp, err := newTracerProvider(ctx, cfg)
	if err != nil {
		return noop, fmt.Errorf("telemetry: tracer provider: %w", err)
	}

	mp, metricsHandler, err := newMeterProvider()
	if err != nil {
		_ = tp.Shutdown(ctx)
		return noop, fmt.Errorf("telemetry: meter provider: %w", err)
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Register and start pluggable collectors. A collector that fails to
	// register is skipped; it must not bring down the whole subsystem.
	for _, collector := range cfg.Collectors {
		if collector == nil {
			continue
		}
		if err := collector.RegisterMetrics(); err != nil {
			continue
		}
		collector.Start(ctx)
	}

	addr := ":" + strconv.Itoa(int(cfg.MetricsPort))
	metricsSrv := &http.Server{Addr: addr, Handler: metricsHandler}
	go func() { _ = metricsSrv.ListenAndServe() }()

	return func(shutCtx context.Context) {
		_ = metricsSrv.Shutdown(shutCtx)
		_ = mp.Shutdown(shutCtx)
		_ = tp.Shutdown(shutCtx)
	}, nil
}
