# terminal

The `terminal` package provides lightweight CLI output helpers for banners, headers, colored text, and concurrent progress rendering. It is intended for command-line applications that need a more expressive terminal UX while staying close to simple standard output and ANSI-based rendering.

## Import

```go
import "github.com/raykavin/gokit/terminal"
```

## What it provides

- `PrintBanner()` for colored ASCII-art application banners
- `PrintHeader()` for runtime and host summary output
- `PrintText()` for highlighted terminal messages
- a goroutine-safe `Progress` type for fixed-slot concurrent progress display
- spinner-based status updates with running, done, and failed states
- slot acquisition and release that can also act as a concurrency gate for workers

## Main types

- `Progress`: manages a fixed set of terminal lines for concurrent work display

## Output helpers example

```go
package main

import (
	"log"

	"github.com/raykavin/gokit/terminal"
)

func main() {
	if err := terminal.PrintBanner("billing-api"); err != nil {
		log.Fatal(err)
	}

	terminal.PrintHeader("Service bootstrap")
	terminal.PrintText("Connecting to upstream services...")
}
```

## Progress example

```go
package main

import (
	"sync"
	"time"

	"github.com/raykavin/gokit/terminal"
)

func main() {
	progress := terminal.New(2)
	progress.Start()
	defer progress.Stop()

	var wg sync.WaitGroup
	tasks := []string{"users", "orders", "payments"}

	for _, task := range tasks {
		slot := progress.Acquire(task)
		wg.Add(1)

		go func(slot int, task string) {
			defer wg.Done()
			defer progress.Release(slot)

			progress.Update(slot, "Fetching data")
			time.Sleep(500 * time.Millisecond)
			progress.Done(slot, "Completed")
		}(slot, task)
	}

	wg.Wait()
}
```

## Notes

- `PrintBanner()` chooses a font from the package font list and returns `ErrEmptyFontsList` when no fonts are available
- `PrintHeader()` prints OS, architecture, and CPU information, then adds distribution, hostname, and kernel details when they can be detected
- `Progress` writes to standard error and all exported methods are goroutine-safe
- `Acquire()` blocks until a slot is free, so the number passed to `New()` should usually match your desired maximum worker concurrency
- call `Start()` before workers begin updating slots and `Stop()` after all work has finished so the final terminal state is rendered cleanly
