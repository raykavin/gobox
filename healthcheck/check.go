package healthcheck

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"runtime"
	"sync"
	"time"
)

const (
	statusHealthy   = "healthy"
	statusUnhealthy = "unhealthy"
)

// Pinger is the minimal database surface the health check needs. It is
// satisfied by *sql.DB out of the box, but any type implementing it (such as a
// test double) can be registered.
type Pinger interface {
	PingContext(ctx context.Context) error
	Stats() sql.DBStats
}

// DBEntry holds a named database connection to be monitored. Driver is an
// optional human-readable label (e.g. "postgres", "mysql") reported back in
// the diagnostics; it is purely informational.
type DBEntry struct {
	Name   string
	Driver string
	DB     Pinger
}

// dbConn pairs a connection with its driver label inside the service.
type dbConn struct {
	driver string
	db     Pinger
}

// DBReport contains non-sensitive diagnostics for a single database connection.
type DBReport struct {
	Status          string        `json:"status"`
	Driver          string        `json:"driver,omitempty"`
	Error           string        `json:"error,omitempty"`
	OpenConnections int           `json:"open_connections"`
	InUse           int           `json:"in_use"`
	Idle            int           `json:"idle"`
	WaitCount       int64         `json:"wait_count"`
	WaitDuration    time.Duration `json:"wait_duration"`
}

// RuntimeReport contains non-sensitive Go runtime diagnostics.
type RuntimeReport struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	Uptime       string `json:"uptime"`

	// Memory (all values in bytes)
	MemAllocated  uint64 `json:"mem_allocated"`   // heap currently allocated
	MemTotalAlloc uint64 `json:"mem_total_alloc"` // cumulative heap allocated
	MemSys        uint64 `json:"mem_sys"`         // total memory from OS
	MemNumGC      uint32 `json:"mem_num_gc"`      // number of GC cycles completed
	MemLastGC     string `json:"mem_last_gc"`     // time of last GC
}

// HealthReport is the full snapshot returned by the service.
type HealthReport struct {
	Runtime   RuntimeReport       `json:"runtime"`
	Databases map[string]DBReport `json:"databases"`
}

// defaultPingTimeout bounds each connection ping so a single stalled database
// cannot block the whole report.
const defaultPingTimeout = 5 * time.Second

// Check probes registered database
// connections and collects Go runtime diagnostics.
type Check struct {
	mu          sync.RWMutex
	databases   map[string]dbConn
	startedAt   time.Time
	pingTimeout time.Duration
}

// New creates a Check pre-loaded with the given DB entries.
func New(entries []DBEntry) *Check {
	dbs := make(map[string]dbConn, len(entries))
	for _, e := range entries {
		dbs[e.Name] = dbConn{driver: e.Driver, db: e.DB}
	}
	return &Check{
		databases:   dbs,
		startedAt:   time.Now(),
		pingTimeout: defaultPingTimeout,
	}
}

// SetPingTimeout overrides the per-connection ping timeout. A non-positive
// value resets it to the default.
func (s *Check) SetPingTimeout(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d <= 0 {
		d = defaultPingTimeout
	}
	s.pingTimeout = d
}

// AddDB registers or replaces a named connection. driver is an optional label
// reported in diagnostics.
func (s *Check) AddDB(name, driver string, db Pinger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.databases[name] = dbConn{driver: driver, db: db}
}

// RemoveDB removes a named connection.
func (s *Check) RemoveDB(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.databases, name)
}

// CheckDB probes a single connection by name.
// Returns an error if the name is not registered.
func (s *Check) CheckDB(ctx context.Context, name string) (DBReport, error) {
	s.mu.RLock()
	conn, ok := s.databases[name]
	timeout := s.pingTimeout
	s.mu.RUnlock()

	if !ok {
		return DBReport{}, fmt.Errorf("database %q not registered", name)
	}

	return probeDB(ctx, conn, timeout), nil
}

// Report returns a full health snapshot: runtime info plus all DB probes
// executed concurrently.
func (s *Check) Report(ctx context.Context) HealthReport {
	return HealthReport{
		Runtime:   s.runtimeReport(),
		Databases: s.probeAllDBs(ctx),
	}
}

// runtimeReport collects current Go runtime metrics.
func (s *Check) runtimeReport() RuntimeReport {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	lastGC := "never"
	if mem.LastGC > 0 {
		lastGC = time.Unix(0, int64(mem.LastGC)).UTC().Format(time.RFC3339)
	}

	return RuntimeReport{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		NumCPU:        runtime.NumCPU(),
		NumGoroutine:  runtime.NumGoroutine(),
		MemAllocated:  mem.Alloc,
		MemTotalAlloc: mem.TotalAlloc,
		MemSys:        mem.Sys,
		MemNumGC:      mem.NumGC,
		MemLastGC:     lastGC,
		Uptime: time.Since(s.startedAt).
			Round(time.Second).String(),
	}
}

// probeAllDBs probes every registered connection concurrently.
func (s *Check) probeAllDBs(ctx context.Context) map[string]DBReport {
	s.mu.RLock()
	snapshot := make(map[string]dbConn, len(s.databases))
	maps.Copy(snapshot, s.databases)
	timeout := s.pingTimeout
	s.mu.RUnlock()

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]DBReport, len(snapshot))
	)

	for name, conn := range snapshot {
		wg.Add(1)
		go func(n string, c dbConn) {
			defer wg.Done()
			report := probeDB(ctx, c, timeout)
			mu.Lock()
			results[n] = report
			mu.Unlock()
		}(name, conn)
	}

	wg.Wait()
	return results
}

// probeDB pings the connection and collects its pool stats.
func probeDB(ctx context.Context, conn dbConn, timeout time.Duration) DBReport {
	report := DBReport{Driver: conn.driver}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := conn.db.PingContext(pingCtx); err != nil {
		report.Status = statusUnhealthy
		report.Error = err.Error()
		return report
	}

	stats := conn.db.Stats()
	report.Status = statusHealthy
	report.OpenConnections = stats.OpenConnections
	report.InUse = stats.InUse
	report.Idle = stats.Idle
	report.WaitCount = stats.WaitCount
	report.WaitDuration = stats.WaitDuration

	return report
}
