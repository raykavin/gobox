# retry

The `retry` package provides a small helper for retrying operations that may fail transiently. It is intended for shared infrastructure code that needs context-aware retries with exponential backoff and caller-controlled retry decisions without introducing a larger resilience framework.

## Import

```go
import "github.com/raykavin/gobox/retry"
```

## What it provides

- `Do()` for retrying an operation up to a fixed number of attempts
- exponential backoff starting at `waitMin` and capped at `waitMax`
- context-aware waiting between attempts
- caller-defined retry policy through `shouldRetry(attempt, err)`

## Main function

- `Do()`: executes `fn`, stops on success, cancellation, max attempts, or when `shouldRetry` returns `false`

## Example

```go
package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/raykavin/gobox/retry"
)

var errTemporary = errors.New("temporary upstream failure")

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	attempts := 0

	err := retry.Do(
		ctx,
		5,
		200*time.Millisecond,
		2*time.Second,
		func(attempt int, err error) bool {
			log.Printf("attempt %d failed: %v", attempt, err)
			return errors.Is(err, errTemporary)
		},
		func() error {
			attempts++
			if attempts < 3 {
				return errTemporary
			}
			return nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("operation succeeded after %d attempts", attempts)
}
```

## Notes

- the `attempt` passed to `shouldRetry` is 1-based and refers to the failed attempt that just completed
- the backoff progression is `waitMin`, `waitMin*2`, `waitMin*4`, and so on, capped at `waitMax`
- if `ctx` is cancelled while waiting between attempts, `Do()` returns `ctx.Err()`
- when the final attempt fails or retries stop early, `Do()` returns the last error returned by `fn`
- `shouldRetry` is not consulted after the final attempt, since there is nothing left to decide; with `maxAttempts` of 1 it is never called at all
- `ctx` is only checked while waiting between attempts, so the first call to `fn` runs even if `ctx` is already cancelled
- `Do()` does not validate its inputs, so callers should pass `maxAttempts >= 1`, `waitMin <= waitMax`, and non-nil `shouldRetry` and `fn`

## Reference

```go
func Do(
    ctx context.Context,
    maxAttempts int,
    waitMin, waitMax time.Duration,
    shouldRetry func(attempt int, err error) bool,
    fn func() error,
) error
```

| Parameter     | Description                                                                              |
| ------------- | ------------------------------------------------------------------------------------------ |
| `ctx`         | Cancels the wait between attempts; `Do` returns `ctx.Err()` if it fires                   |
| `maxAttempts` | Total number of calls to `fn`, including the first                                        |
| `waitMin`     | Delay before the second attempt, and the base of the exponential progression              |
| `waitMax`     | Upper bound on any single delay                                                           |
| `shouldRetry` | Called with the 1-based failed attempt and its error; returning `false` stops immediately |
| `fn`          | The operation to run                                                                      |

`Do` returns `nil` on the first success, `ctx.Err()` if the context is cancelled during a wait, and otherwise the error from the last call to `fn`.
