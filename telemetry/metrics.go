package telemetry

import (
	"net/http"

	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func newMeterProvider() (*sdkmetric.MeterProvider, http.Handler, error) {
	registry := prometheus.NewRegistry()

	// Standard Go runtime and process metrics (goroutines, heap, GC, uptime…)
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, nil, err
	}

	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)

	return mp, mux, nil
}
