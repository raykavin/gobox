package telemetry

import "context"

// Collector is a pluggable source of custom metrics. Implementations register
// their instruments against the global MeterProvider (already configured by
// New) and optionally run a background loop to feed them.
//
// The lifecycle is:
//
//  1. RegisterMetrics is called once, after the MeterProvider is installed but
//     before Start. Returning an error aborts the collector (Start is skipped)
//     without affecting the rest of the telemetry subsystem.
//  2. Start is called with a context tied to the telemetry lifetime; the
//     collector should return promptly, spawning any background work as a
//     goroutine that exits when ctx is cancelled.
//
// A typical implementation polls an external system on a ticker and exposes
// the latest snapshot through OTel observable instruments.
type Collector interface {
	// RegisterMetrics registers the collector's instruments. It is called
	// once during New, after the MeterProvider is set globally.
	RegisterMetrics() error

	// Start launches the collector's background work, if any. It must not
	// block; long-running work belongs in a goroutine bound to ctx.
	Start(ctx context.Context)
}