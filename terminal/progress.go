package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tj/go-spin"
)

type lineStatus int

const (
	statusIdle lineStatus = iota
	statusRunning
	statusDone
	statusFailed
)

// ANSI escape sequences and render format strings.
const (
	ansiCursorUp  = "\033[%dA"
	ansiClearLine = "\r\033[K"

	renderIdle    = ansiClearLine + "    -\n"
	renderDone    = ansiClearLine + "[✓] %s - %s\n"
	renderFailed  = ansiClearLine + "[✗] %s - %s\n"
	renderRunning = ansiClearLine + "[%s] %s - %s\n"
)

type slot struct {
	spinner *spin.Spinner
	desc    string
	msg     string
	status  lineStatus
}

// Progress manages a fixed pool of terminal lines, one per concurrent worker.
//
// Acquire blocks the caller until a display slot is free, acting as the
// concurrency gate (replaces a semaphore). Release returns the slot to the
// pool when the worker finishes. The terminal always shows exactly numSlots
// lines regardless of how many total subscribers exist.
//
// All exported methods are goroutine-safe.
type Progress struct {
	mu      sync.Mutex
	slots   []*slot
	freeCh  chan int
	once    sync.Once // guards Start
	stopCh  chan struct{}
	doneCh  chan struct{} // closed when the render goroutine exits
	started bool
	out     io.Writer
}

// New creates a Progress with numSlots display lines (set this to maxWorkers).
func New(numSlots int) *Progress {
	if numSlots <= 0 {
		numSlots = 1
	}
	slots := make([]*slot, numSlots)
	freeCh := make(chan int, numSlots)
	for i := range slots {
		s := spin.New()
		s.Set(spin.Box1)
		slots[i] = &slot{spinner: s, status: statusIdle}
		freeCh <- i
	}
	return &Progress{
		slots:  slots,
		freeCh: freeCh,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		out:    os.Stderr,
	}
}

// Acquire blocks until a display slot is free, initialises it with desc, and
// returns its index. Call this before launching a worker.
func (p *Progress) Acquire(desc string) int {
	idx := <-p.freeCh
	p.mu.Lock()
	s := p.slots[idx]
	s.desc = desc
	s.msg = "Starting..."
	s.status = statusRunning
	p.mu.Unlock()
	return idx
}

// Release marks the slot as idle and returns it to the pool.
// Call this at the end of the worker goroutine (typically via defer).
func (p *Progress) Release(idx int) {
	p.mu.Lock()
	p.slots[idx].status = statusIdle
	p.mu.Unlock()
	p.freeCh <- idx
}

// Update sets the current status message for slot idx.
func (p *Progress) Update(idx int, msg string) {
	p.mu.Lock()
	p.slots[idx].msg = msg
	p.mu.Unlock()
}

// Done marks slot idx as successfully completed.
func (p *Progress) Done(idx int, msg string) {
	p.mu.Lock()
	s := p.slots[idx]
	s.msg = msg
	s.status = statusDone
	p.mu.Unlock()
}

// Fail marks slot idx as failed.
func (p *Progress) Fail(idx int, msg string) {
	p.mu.Lock()
	s := p.slots[idx]
	s.msg = msg
	s.status = statusFailed
	p.mu.Unlock()
}

// Start begins the 100 ms background render loop.
// Safe to call multiple times — only the first call has effect.
func (p *Progress) Start() {
	p.once.Do(func() {
		_, _ = fmt.Fprintln(p.out)
		go func() {
			defer close(p.doneCh)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					p.mu.Lock()
					p.render()
					p.mu.Unlock()
				case <-p.stopCh:
					return
				}
			}
		}()
	})
}

// Stop halts the render loop, waits for it to exit, then performs a final render.
// Safe to call multiple times — only the first call has effect.
func (p *Progress) Stop() {
	p.once.Do(func() {}) // ensure Start's once is consumed if Stop is called first
	select {
	case <-p.stopCh:
		// already stopped
	default:
		close(p.stopCh)
	}
	<-p.doneCh // wait for the goroutine to exit before the final render
	p.mu.Lock()
	p.render()
	p.mu.Unlock()
	_, _ = fmt.Fprintln(p.out)
}

// render rewrites all slots in-place using ANSI escape codes.
// Caller must hold p.mu.
func (p *Progress) render() {
	n := len(p.slots)
	if n == 0 {
		return
	}

	var sb strings.Builder

	if p.started {
		_, _ = fmt.Fprintf(&sb, ansiCursorUp, n)
	}

	for _, s := range p.slots {
		switch s.status {
		case statusIdle:
			_, _ = fmt.Fprint(&sb, renderIdle)
		case statusDone:
			_, _ = fmt.Fprintf(&sb, renderDone, s.desc, s.msg)
		case statusFailed:
			_, _ = fmt.Fprintf(&sb, renderFailed, s.desc, s.msg)
		default:
			_, _ = fmt.Fprintf(&sb, renderRunning, s.spinner.Next(), s.desc, s.msg)
		}
	}

	_, _ = fmt.Fprint(p.out, sb.String())
	p.started = true
}
