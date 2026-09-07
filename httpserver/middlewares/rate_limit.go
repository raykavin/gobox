package middlewares

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/httpserver/respond"
	"golang.org/x/time/rate"
)

// rateLimitIdleTimeout bounds how long a per-client limiter is kept once that
// client stops sending requests, so the map does not grow without bound over
// the life of the process.
const rateLimitIdleTimeout = 10 * time.Minute

// RateLimitOptions describes one token-bucket policy.
//
// A non-positive field is a programming error rather than "unlimited": the
// caller decides the policy, and silently disabling a limit is never the
// safer reading. Use RateLimitOptions.WithDefaults to fill gaps explicitly.
type RateLimitOptions struct {
	// RequestsPerMinute is the sustained refill rate per client.
	RequestsPerMinute int

	// Burst is the bucket capacity: how many requests may arrive
	// back-to-back before the sustained rate applies.
	Burst int
}

// WithDefaults returns o with any non-positive field replaced by the given
// fallback, so a caller can express "this policy must never end up off".
func (o RateLimitOptions) WithDefaults(requestsPerMinute, burst int) RateLimitOptions {
	if o.RequestsPerMinute <= 0 {
		o.RequestsPerMinute = requestsPerMinute
	}
	if o.Burst <= 0 {
		o.Burst = burst
	}
	return o
}

// ipRateLimiter tracks one token-bucket limiter per client key, so a burst
// from one caller cannot exhaust the capacity shared by every other client.
// Idle entries are evicted opportunistically (see evictIfDue) rather than by
// a background goroutine, so this needs no lifecycle wiring of its own.
type ipRateLimiter struct {
	mu        sync.Mutex
	limiters  map[string]*ipLimiterEntry
	rps       rate.Limit
	burst     int
	lastEvict time.Time
}

type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(requestsPerMinute, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters:  make(map[string]*ipLimiterEntry),
		rps:       rate.Limit(float64(requestsPerMinute) / 60),
		burst:     burst,
		lastEvict: time.Now(),
	}
}

// retune adopts opts when they differ from the policy in force, applying the
// new rate to every live bucket. A no-op in the common case, which is why the
// comparison is two integer reads under the same lock allow already takes.
func (l *ipRateLimiter) retune(opts RateLimitOptions) {
	rps := rate.Limit(float64(opts.RequestsPerMinute) / 60)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rps == rps && l.burst == opts.Burst {
		return
	}

	l.rps = rps
	l.burst = opts.Burst

	for _, entry := range l.limiters {
		entry.limiter.SetLimit(rps)
		entry.limiter.SetBurst(opts.Burst)
	}
}

func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.evictIfDue()

	entry, ok := l.limiters[key]
	if !ok {
		entry = &ipLimiterEntry{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.limiters[key] = entry
	}
	entry.lastSeen = time.Now()

	return entry.limiter.Allow()
}

// evictIfDue removes limiters idle for longer than rateLimitIdleTimeout,
// checked at most once per rateLimitIdleTimeout so the sweep stays off the
// common hot path. Caller must hold l.mu.
func (l *ipRateLimiter) evictIfDue() {
	now := time.Now()
	if now.Sub(l.lastEvict) < rateLimitIdleTimeout {
		return
	}
	l.lastEvict = now

	for key, entry := range l.limiters {
		if now.Sub(entry.lastSeen) > rateLimitIdleTimeout {
			delete(l.limiters, key)
		}
	}
}

// RateLimitByIP returns a middleware enforcing opts as a per-client-IP
// token-bucket limit, answering 429 Too Many Requests once exceeded.
//
// The client is identified by gin.Context.ClientIP(), which resolves
// X-Forwarded-For / X-Real-IP only for peers listed in the engine's trusted
// proxies. That is what makes this safe behind a reverse proxy: an untrusted
// caller cannot forge a header to get its own bucket. Conversely, deploying
// behind a proxy WITHOUT configuring trusted proxies collapses every client
// into the proxy's single bucket.
func RateLimitByIP(opts RateLimitOptions) gin.HandlerFunc {
	return newIPRateLimiter(opts.RequestsPerMinute, opts.Burst).middleware(nil)
}

// RateLimitByIPProvider is RateLimitByIP against a policy that may change.
//
// provider is called once per request and must be cheap: the intended source
// is an atomic pointer load. When the returned options differ from the ones in
// force, existing buckets are retuned in place rather than discarded, so
// raising a limit does not hand every client a fresh full bucket and lowering
// one takes effect without waiting for idle eviction.
func RateLimitByIPProvider(provider func() RateLimitOptions) gin.HandlerFunc {
	return newIPRateLimiter(0, 0).middleware(provider)
}

// middleware serves both constructors. A nil provider is the static case and
// skips retune entirely: a caller with a fixed policy must not pay a second
// lock acquisition per request for a reload it never performs.
func (l *ipRateLimiter) middleware(provider func() RateLimitOptions) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if provider != nil {
			l.retune(provider())
		}

		if !l.allow(ctx.ClientIP()) {
			respond.TooManyRequests(ctx)
			return
		}
		ctx.Next()
	}
}
