// Package retry provides a small helper for retrying operations that may fail
// transiently, with exponential backoff and context-aware waiting.
//
// # Usage
//
//	err := retry.Do(
//	    ctx,
//	    5,
//	    200*time.Millisecond,
//	    2*time.Second,
//	    func(attempt int, err error) bool {
//	        return errors.Is(err, ErrTemporary)
//	    },
//	    func() error {
//	        return callExternalService()
//	    },
//	)
//
// Do executes fn up to maxAttempts times. On each failure it calls shouldRetry
// to decide whether to continue, then waits for an exponentially increasing
// duration between waitMin and waitMax before the next attempt. If ctx is
// cancelled while waiting, Do returns ctx.Err() immediately.
package retry
