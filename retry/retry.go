package retry

import (
	"context"
	"time"
)

// Do executes fn up to maxAttempts times, backing off exponentially between
// attempts. It stops early if ctx is cancelled or fn returns a nil error.
// shouldRetry is called with the attempt index (1-based) and the last error to
// decide whether to retry.
func Do(
	ctx context.Context,
	maxAttempts int,
	waitMin, waitMax time.Duration,
	shouldRetry func(attempt int, err error) bool,
	fn func() error,
) error {
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if attempt == maxAttempts {
			break
		}
		if !shouldRetry(attempt, err) {
			break
		}
		wait := backoff(attempt, waitMin, waitMax)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

// backoff calculates an exponential backoff duration for the given attempt.
// delay = min * 2^(attempt-1), capped at max.
func backoff(attempt int, min, max time.Duration) time.Duration {
	d := min
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > max {
			return max
		}
	}
	return d
}
