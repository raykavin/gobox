package oidcauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Sentinel errors specific to introspection. All wrap-compose with %w, so
// callers can distinguish cases with errors.Is/errors.As regardless of any
// outer wrapping Verify itself adds.
var (
	// ErrAccessTokenInactive is returned when the issuer reports the access
	// token as inactive (revoked, expired, never existed, or wrong client)
	// RFC 7662 collapses all of these into a single "active": false and does
	// not distinguish which applies, so neither does this error.
	ErrAccessTokenInactive = errors.New("oidcauth: access token is inactive")

	// ErrTokenRevoked is a deprecated alias of ErrAccessTokenInactive (the
	// same error value under both names), kept so existing callers using
	// errors.Is(err, ErrTokenRevoked) keep working unchanged.
	//
	// Deprecated: use ErrAccessTokenInactive.
	ErrTokenRevoked = ErrAccessTokenInactive

	// ErrInvalidIntrospectionResponse is returned when the issuer's
	// introspection response cannot be parsed as the expected JSON shape.
	// Distinct from ErrIntrospectionFailed, which covers transport/HTTP
	// level failures (network error, unexpected status, timeout).
	ErrInvalidIntrospectionResponse = errors.New("oidcauth: invalid introspection response")

	// ErrInvalidOIDCConfiguration is returned by New/NewIntrospector for a
	// configuration value that cannot be honored (e.g. a negative TTL or
	// timeout, or a missing introspection endpoint).
	ErrInvalidOIDCConfiguration = errors.New("oidcauth: invalid OIDC configuration")
)

const (
	// defaultIntrospectionHTTPTimeout is applied when
	// Config.IntrospectionHTTPTimeout / IntrospectorConfig.HTTPTimeout is
	// zero.
	defaultIntrospectionHTTPTimeout = 10 * time.Second

	// minCleanupInterval and maxCleanupInterval bound the cache-eviction
	// ticker: never so tight it busy-spins on a very short TTL, never so
	// loose that a long TTL leaves abandoned entries around for hours.
	minCleanupInterval = time.Second
	maxCleanupInterval = 5 * time.Minute

	// maxIntrospectResponseBytes caps how much of the issuer's response
	// body is read: an introspection response is a small JSON object, so
	// anything past this is either a misbehaving issuer or not JSON at all.
	maxIntrospectResponseBytes = 1 << 20 // 1 MiB
)

// IntrospectionCacheEntry is a cached, positive (active=true) introspection
// result: the claims the issuer returned, and when the entry stops being
// usable.
type IntrospectionCacheEntry struct {
	Claims    Claims
	ExpiresAt time.Time
}

// tokenCacheKey is a SHA-256 digest of an access token. Used as the cache
// map's key (and as the singleflight dedup key, hex-encoded) so the raw
// token is never held in memory as a key, logged, or visible in a profiler
// dump of map internals.
type tokenCacheKey [sha256.Size]byte

func newTokenCacheKey(token string) tokenCacheKey {
	return sha256.Sum256([]byte(token))
}

// IntrospectionEventKind identifies what an IntrospectorConfig.OnEvent
// callback is reporting.
type IntrospectionEventKind int

const (
	EventCacheHit IntrospectionEventKind = iota
	EventCacheMiss
	EventIntrospectionSucceeded
	EventTokenInactive
	EventIntrospectionFailed
	EventCacheEvicted
)

// IntrospectionEvent is passed to IntrospectorConfig.OnEvent. It never
// carries the access token, its hash, or any claim value only the kind of
// event and, where relevant, a latency or count so it is always safe to
// log or export as a metric verbatim.
type IntrospectionEvent struct {
	Kind IntrospectionEventKind

	// Duration is set for EventIntrospectionSucceeded and
	// EventIntrospectionFailed: the HTTP round trip's latency.
	Duration time.Duration

	// Count is set for EventCacheEvicted: how many entries were removed.
	Count int
}

// IntrospectorConfig configures an Introspector.
type IntrospectorConfig struct {
	// Endpoint is the RFC 7662 introspection endpoint URL. Required.
	Endpoint string

	// ClientID and ClientSecret authenticate the introspection request via
	// HTTP Basic auth, the client authentication method Keycloak (and most
	// providers) expect for a confidential client's introspection calls.
	ClientID     string
	ClientSecret string

	// HTTPTimeout bounds each introspection request. Zero defaults to 10s;
	// negative is rejected by NewIntrospector.
	HTTPTimeout time.Duration

	// CacheTTL bounds how long a positive result may be reused. Zero
	// disables caching entirely (every Introspect call hits the network);
	// negative is rejected by NewIntrospector. See Introspector's doc
	// comment for the full effective-TTL calculation.
	CacheTTL time.Duration

	// OnEvent, if non-nil, is invoked for cache hits/misses, successful and
	// failed introspections, inactive tokens, and cache evictions see
	// IntrospectionEvent. Called synchronously but never while holding the
	// Introspector's internal lock; keep it fast and non-blocking (e.g. an
	// atomic counter increment or metrics-library call), not a network call
	// of its own.
	OnEvent func(IntrospectionEvent)
}

// Introspector performs RFC 7662 token introspection against an OIDC
// provider's introspection endpoint, with an optional in-memory cache of
// positive results.
//
// # Caching
//
// A zero CacheTTL disables caching: every Introspect call performs a fresh
// HTTP round trip. Otherwise, a positive (active=true) result is cached
// under the SHA-256 digest of the access token (never the token itself) for
// however long is shortest of:
//
//  1. now + CacheTTL;
//  2. knownExpiry, the caller-supplied expiry of the token itself (e.g. from
//     locally parsing a JWT access token) zero if unknown;
//  3. the "exp" the issuer's introspection response reports, if any.
//
// If that shortest value is not after "now" (the token is already expired,
// or expires immediately), the result is not cached at all. A negative or
// failed result (inactive, or the request itself failed) is never cached,
// so a revoked token is never remembered as briefly-still-good and a
// transient issuer outage is retried on the very next call rather than
// remembered as a hard failure.
//
// CacheTTL is therefore the maximum window in which a server-side
// revocation can go unnoticed by a process holding a cached result choose
// it as the acceptable trade-off between that window and introspection
// request volume.
//
// # Concurrency
//
// Concurrent Introspect calls for the *same* access token are coalesced
// into a single HTTP request via singleflight; calls for different tokens
// never block each other, and the Introspector's internal lock is never
// held across the HTTP call itself. Each caller's own ctx cancellation is
// honored independently even while sharing an in-flight request with
// others (see Introspect's doc comment) one caller cancelling does not
// abort the shared request for the rest.
//
// # Lifecycle
//
// Call Close when the Introspector is no longer needed. It stops the
// background cache-eviction goroutine (started only when CacheTTL > 0) and
// is idempotent safe to call more than once, and safe to call even when
// CacheTTL is 0 (nothing to stop).
type Introspector struct {
	endpoint     string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	cacheTTL     time.Duration
	onEvent      func(IntrospectionEvent)

	mu      sync.RWMutex
	entries map[tokenCacheKey]IntrospectionCacheEntry

	sf singleflight.Group

	now func() time.Time

	stop      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewIntrospector builds an Introspector. Returns an error wrapping
// ErrInvalidOIDCConfiguration if cfg.Endpoint is empty or either duration is
// negative.
func NewIntrospector(cfg IntrospectorConfig) (*Introspector, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("%w: introspection endpoint is required", ErrInvalidOIDCConfiguration)
	}
	if cfg.CacheTTL < 0 {
		return nil, fmt.Errorf("%w: introspection cache TTL cannot be negative", ErrInvalidOIDCConfiguration)
	}
	if cfg.HTTPTimeout < 0 {
		return nil, fmt.Errorf("%w: introspection HTTP timeout cannot be negative", ErrInvalidOIDCConfiguration)
	}

	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = defaultIntrospectionHTTPTimeout
	}

	i := &Introspector{
		endpoint:     cfg.Endpoint,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		httpClient:   &http.Client{Timeout: timeout},
		cacheTTL:     cfg.CacheTTL,
		onEvent:      cfg.OnEvent,
		entries:      make(map[tokenCacheKey]IntrospectionCacheEntry),
		now:          time.Now,
		stop:         make(chan struct{}),
	}

	if i.cacheTTL > 0 {
		i.wg.Add(1)
		go i.runCleanup()
	}

	return i, nil
}

// Introspect reports whether token is active on the issuer, returning the
// claims from the introspection response on success. knownExpiry is the
// token's own expiry if the caller already knows it (e.g. from locally
// parsing a JWT access token's exp claim); pass the zero time when unknown
// (e.g. the token is opaque) it only ever tightens the cache TTL, never
// loosens it.
//
// A positive result may be served from cache; every inactive or failed
// lookup always performs a fresh request (see the Introspector doc comment
// for the full caching rule).
//
// Returns an error wrapping:
//   - ErrAccessTokenInactive if the issuer reports the token inactive (or
//     omits "active" RFC 7662 treats a missing field as inactive);
//   - ErrInvalidIntrospectionResponse if the response body cannot be parsed;
//   - ErrIntrospectionFailed for any other failure: network error,
//     unexpected HTTP status, or the request timing out.
//
// ctx cancellation is honored even when this call's request is coalesced
// with a concurrent caller's identical one via singleflight: this call
// returns ctx.Err() without waiting for (or aborting) the shared request.
func (i *Introspector) Introspect(ctx context.Context, token string, knownExpiry time.Time) (Claims, error) {
	if token == "" {
		return Claims{}, fmt.Errorf("%w: empty token", ErrIntrospectionFailed)
	}

	key := newTokenCacheKey(token)

	if i.cacheTTL > 0 {
		if entry, ok := i.lookup(key); ok {
			i.emit(IntrospectionEvent{Kind: EventCacheHit})
			return entry.Claims, nil
		}
		i.emit(IntrospectionEvent{Kind: EventCacheMiss})
	}

	// The shared request runs against its own background context, not any
	// single caller's ctx: multiple callers may be waiting on it, and one
	// of them cancelling must not abort the request for the others. Each
	// caller still honors its own ctx via the select below.
	ch := i.sf.DoChan(hex.EncodeToString(key[:]), func() (any, error) {
		return i.doIntrospect(context.Background(), token, knownExpiry)
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return Claims{}, res.Err
		}
		result := res.Val.(introspectionResult)
		if i.cacheTTL > 0 && !result.expiresAt.IsZero() {
			i.store(key, IntrospectionCacheEntry{Claims: result.claims, ExpiresAt: result.expiresAt})
		}
		return result.claims, nil
	case <-ctx.Done():
		return Claims{}, ctx.Err()
	}
}

// ClearCache removes every cached entry immediately.
func (i *Introspector) ClearCache() {
	i.mu.Lock()
	n := len(i.entries)
	i.entries = make(map[tokenCacheKey]IntrospectionCacheEntry)
	i.mu.Unlock()
	if n > 0 {
		i.emit(IntrospectionEvent{Kind: EventCacheEvicted, Count: n})
	}
}

// Close stops the background cache-eviction goroutine, if one was started
// (only when CacheTTL > 0). Idempotent and safe to call even when no
// goroutine was ever started.
func (i *Introspector) Close() {
	i.closeOnce.Do(func() {
		close(i.stop)
	})
	i.wg.Wait()
}

type introspectionResult struct {
	claims    Claims
	expiresAt time.Time // zero means "do not cache this result"
}

// doIntrospect performs the actual RFC 7662 request. It is only ever
// invoked once per in-flight token via Introspect's singleflight call.
func (i *Introspector) doIntrospect(ctx context.Context, token string, knownExpiry time.Time) (introspectionResult, error) {
	start := i.now()

	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		i.emit(IntrospectionEvent{Kind: EventIntrospectionFailed, Duration: i.now().Sub(start)})
		return introspectionResult{}, fmt.Errorf("%w: build request: %w", ErrIntrospectionFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(i.clientID, i.clientSecret)

	resp, err := i.httpClient.Do(req)
	if err != nil {
		i.emit(IntrospectionEvent{Kind: EventIntrospectionFailed, Duration: i.now().Sub(start)})
		return introspectionResult{}, fmt.Errorf("%w: %w", ErrIntrospectionFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		i.emit(IntrospectionEvent{Kind: EventIntrospectionFailed, Duration: i.now().Sub(start)})
		return introspectionResult{}, fmt.Errorf("%w: unexpected HTTP status %d", ErrIntrospectionFailed, resp.StatusCode)
	}

	var raw Introspection
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxIntrospectResponseBytes)).Decode(&raw); err != nil {
		i.emit(IntrospectionEvent{Kind: EventIntrospectionFailed, Duration: i.now().Sub(start)})
		return introspectionResult{}, fmt.Errorf("%w: decode response: %w", ErrInvalidIntrospectionResponse, err)
	}

	duration := i.now().Sub(start)
	if !raw.Active {
		i.emit(IntrospectionEvent{Kind: EventTokenInactive, Duration: duration})
		return introspectionResult{}, ErrAccessTokenInactive
	}
	i.emit(IntrospectionEvent{Kind: EventIntrospectionSucceeded, Duration: duration})

	return introspectionResult{
		claims:    raw.Claims,
		expiresAt: i.effectiveExpiry(knownExpiry, raw.Claims.Exp),
	}, nil
}

// effectiveExpiry computes the shortest of now+CacheTTL, knownExpiry (zero
// if unknown), and the introspection response's own exp claim (<= 0 if
// absent) the single TTL rule documented on Introspector. Returns the
// zero time if the result should not be cached at all (already expired, or
// CacheTTL is disabled).
func (i *Introspector) effectiveExpiry(knownExpiry time.Time, introspectedExp float64) time.Time {
	if i.cacheTTL <= 0 {
		return time.Time{}
	}

	now := i.now()
	expiresAt := now.Add(i.cacheTTL)

	if !knownExpiry.IsZero() && knownExpiry.Before(expiresAt) {
		expiresAt = knownExpiry
	}
	if introspectedExp > 0 {
		if tokenExp := time.Unix(int64(introspectedExp), 0); tokenExp.Before(expiresAt) {
			expiresAt = tokenExp
		}
	}

	if !expiresAt.After(now) {
		return time.Time{}
	}
	return expiresAt
}

func (i *Introspector) lookup(key tokenCacheKey) (IntrospectionCacheEntry, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	entry, ok := i.entries[key]
	if !ok || !i.now().Before(entry.ExpiresAt) {
		return IntrospectionCacheEntry{}, false
	}
	return entry, true
}

func (i *Introspector) store(key tokenCacheKey, entry IntrospectionCacheEntry) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.entries[key] = entry
}

func (i *Introspector) evict(now time.Time) {
	i.mu.Lock()
	var removed int
	for k, e := range i.entries {
		if !now.Before(e.ExpiresAt) {
			delete(i.entries, k)
			removed++
		}
	}
	i.mu.Unlock()
	if removed > 0 {
		i.emit(IntrospectionEvent{Kind: EventCacheEvicted, Count: removed})
	}
}

func (i *Introspector) runCleanup() {
	defer i.wg.Done()
	ticker := time.NewTicker(clampCleanupInterval(i.cacheTTL))
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			i.evict(i.now())
		case <-i.stop:
			return
		}
	}
}

func (i *Introspector) emit(e IntrospectionEvent) {
	if i.onEvent != nil {
		i.onEvent(e)
	}
}

// clampCleanupInterval bounds the cache-eviction ticker to
// [minCleanupInterval, maxCleanupInterval] regardless of CacheTTL, so a very
// short TTL cannot spin a ticker and a very long one still sweeps
// abandoned entries reasonably often.
func clampCleanupInterval(ttl time.Duration) time.Duration {
	switch {
	case ttl < minCleanupInterval:
		return minCleanupInterval
	case ttl > maxCleanupInterval:
		return maxCleanupInterval
	default:
		return ttl
	}
}

// looksLikeJWT reports whether token has the three dot-separated, non-empty
// segments a JWT (header.payload.signature) requires. It is a format check
// only no cryptographic verification used solely to decide whether
// local JWT validation is even applicable: a genuinely opaque access token
// (which Keycloak, and most providers, can also issue) has no JWT structure
// to check locally and must skip straight to introspection instead of
// failing local validation.
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

// resolveIntrospectionEndpoint prefers the discovery document's standard
// "introspection_endpoint" field, then config.IntrospectionEndpoint. Errors
// wrapping ErrInvalidOIDCConfiguration if neither supplies one.
func resolveIntrospectionEndpoint(provider providerMetadataSource, config Config) (string, error) {
	var discovery struct {
		IntrospectionEndpoint string `json:"introspection_endpoint"`
	}
	if err := provider.Claims(&discovery); err == nil && discovery.IntrospectionEndpoint != "" {
		return discovery.IntrospectionEndpoint, nil
	}
	if config.IntrospectionEndpoint != "" {
		return config.IntrospectionEndpoint, nil
	}
	return "", fmt.Errorf("%w: no introspection endpoint in discovery document or config", ErrInvalidOIDCConfiguration)
}

// providerMetadataSource is satisfied by *oidc.Provider; declared here so
// resolveIntrospectionEndpoint can be unit-tested without a live discovery
// document.
type providerMetadataSource interface {
	Claims(v any) error
}
