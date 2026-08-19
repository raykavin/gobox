package oidcauth

import (
	"context"
	"time"
)

// defaultCleanupTimeout bounds a single CleanupExpired call when the caller
// does not specify one.
const defaultCleanupTimeout = 30 * time.Second

// CleanupResult reports the outcome of one RunSessionCleanup pass.
type CleanupResult struct {
	Removed  int64
	Duration time.Duration
	Err      error
}

// RunSessionCleanup runs SessionManager.CleanupExpired on a ticker until ctx
// is cancelled, reporting each pass's outcome (including failures) through
// onResult. It never logs anything itself no session data ever passes
// through this function so callers can plug in their own logger via
// onResult without RunSessionCleanup needing to know about it.
//
// Safe to run redundantly from multiple instances of the same application:
// the DELETE it issues is idempotent, so at most it does a little
// unnecessary work when two instances race, never incorrect work.
//
// interval <= 0 defaults to one hour; timeout <= 0 defaults to 30s per pass.
func RunSessionCleanup(ctx context.Context, m *SessionManager, interval, timeout time.Duration, onResult func(CleanupResult)) {
	if interval <= 0 {
		interval = time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			runCleanupOnce(ctx, m, timeout, onResult)
		case <-ctx.Done():
			return
		}
	}
}

func runCleanupOnce(ctx context.Context, m *SessionManager, timeout time.Duration, onResult func(CleanupResult)) {
	if timeout <= 0 {
		timeout = defaultCleanupTimeout
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	removed, err := m.CleanupExpired(cctx, time.Now())

	if onResult != nil {
		onResult(CleanupResult{Removed: removed, Duration: time.Since(start), Err: err})
	}
}
