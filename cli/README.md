# cli

The `cli` package provides terminal output helpers for CLI applications. It is intended for tools and services that need a startup banner with ASCII art, a colored system information header, and a live progress display for tracking concurrent workers.

## Import

```go
import "github.com/raykavin/gobox/cli"
```

## What it provides

- `PrintBanner()` for rendering an ASCII art banner in a randomly chosen font
- `PrintHeader()` for printing a colored header with OS, architecture, CPU count, hostname, and kernel version
- `PrintText()` for printing a single bold cyan line
- `Progress` for managing a fixed pool of animated terminal lines during concurrent operations

## Main types

- `Progress`: a fixed-width multi-line display with spinner animation, concurrency gating via `Acquire`/`Release`, and per-slot status updates

## Banner example

```go
package main

import (
    "log"

    "github.com/raykavin/gobox/cli"
)

func main() {
    if err := cli.PrintBanner("myapp"); err != nil {
        log.Fatal(err)
    }
    cli.PrintHeader("v1.0.0")
}
```

## Progress example

`New(numSlots)` sets the maximum number of workers displayed at once. `Acquire` blocks until a slot is free, making it the concurrency gate.

```go
package main

import (
    "sync"

    "github.com/raykavin/gobox/cli"
)

func main() {
    items := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    p := cli.New(3) // at most 3 lines visible at once
    p.Start()
    defer p.Stop()

    var wg sync.WaitGroup
    for _, item := range items {
        wg.Add(1)
        go func(name string) {
            defer wg.Done()
            idx := p.Acquire(name)
            defer p.Release(idx)

            p.Update(idx, "processing...")
            if err := doWork(name); err != nil {
                p.Fail(idx, err.Error())
                return
            }
            p.Done(idx, "done")
        }(item)
    }
    wg.Wait()
}
```

## Notes

- `PrintBanner` picks a font at random from a built-in list; it returns `ErrEmptyFontsList` only if the list is somehow empty
- `PrintHeader` silently skips fields it cannot retrieve (distribution, hostname, kernel version) rather than returning an error
- the `numSlots` argument to `New` also caps concurrent workers since `Acquire` blocks when all slots are taken
- the render loop runs at 100 ms intervals; call `Stop` to flush a final frame and release the goroutine
- `Start` and `Stop` are safe to call from multiple goroutines and to call more than once
- calling `Stop` before `Start` consumes the internal `sync.Once`, so a later `Start` will not launch the render loop; always `Start` first

## Reference

### Functions

| Function                          | Description                                                                                  |
| --------------------------------- | -------------------------------------------------------------------------------------------- |
| `PrintBanner(appName string) error` | Renders `appName` as cyan ASCII art in a randomly chosen font, followed by a separator       |
| `PrintHeader(content string)`       | Prints a separator, then `content` and the detected system information                       |
| `PrintText(text string)`            | Prints a single bold cyan line                                                               |

### Progress

| Method                        | Description                                                                        |
| ----------------------------- | ------------------------------------------------------------------------------------ |
| `New(numSlots int) *Progress` | Creates a display with `numSlots` visible lines                                     |
| `Acquire(desc string) int`    | Blocks until a slot is free, then claims it and returns its index                   |
| `Release(idx int)`            | Returns the slot to the pool so a waiting `Acquire` can proceed                     |
| `Update(idx int, msg string)` | Replaces the message on a slot, keeping the spinner running                         |
| `Done(idx int, msg string)`   | Marks the slot as succeeded                                                         |
| `Fail(idx int, msg string)`   | Marks the slot as failed                                                            |
| `Start()`                     | Begins the 100 ms background render loop                                            |
| `Stop()`                      | Halts the render loop, waits for it to exit, then performs a final render           |

### Errors

| Error                     | Returned when                                                             |
| ------------------------- | --------------------------------------------------------------------------- |
| `ErrEmptyFontsList`       | `PrintBanner` finds the built-in font list empty                           |
| `ErrOSReleaseNotFound`    | `/etc/os-release` is missing while reading the distribution name           |
| `ErrReadOSReleaseFailed`  | `/etc/os-release` exists but cannot be read                                |
| `ErrDistributionNotFound` | `/etc/os-release` contains no distribution name                            |
| `ErrHostnameFailed`       | The hostname cannot be determined                                          |
| `ErrKernelVersionFailed`  | The kernel version cannot be determined                                    |

These errors are surfaced by `PrintBanner` and by the internal system-information lookups. `PrintHeader` itself returns nothing and omits any field whose lookup failed.
