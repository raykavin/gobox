// Package cli provides terminal output helpers for CLI applications.
//
// It includes a banner printer with ASCII art, a colored header with system
// information, plain colored text output, and a Progress display for tracking
// concurrent workers in the terminal.
//
// # Banner and header
//
//	if err := cli.PrintBanner("myapp"); err != nil {
//	    log.Fatal(err)
//	}
//	cli.PrintHeader("v1.0.0")
//
// # Progress display
//
// Progress manages a fixed pool of terminal lines for tracking concurrent
// operations. Acquire blocks until a slot is free, acting as both a display
// allocator and a concurrency gate.
//
//	p := cli.New(4)
//	p.Start()
//	defer p.Stop()
//
//	var wg sync.WaitGroup
//	for _, item := range items {
//	    wg.Add(1)
//	    go func(item string) {
//	        defer wg.Done()
//	        idx := p.Acquire(item)
//	        defer p.Release(idx)
//
//	        p.Update(idx, "processing...")
//	        if err := process(item); err != nil {
//	            p.Fail(idx, err.Error())
//	            return
//	        }
//	        p.Done(idx, "ok")
//	    }(item)
//	}
//	wg.Wait()
package cli
