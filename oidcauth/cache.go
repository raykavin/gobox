package oidcauth

import (
	"context"
	"sync"
	"time"
)

// DefaultCacheDuration is the TTL used when no custom duration is provided.
const DefaultCacheDuration = 5 * time.Minute

// Cache is the interface for token caching backends. Implement it to plug in
// Redis, Memcached, or any other store.
type Cache interface {
	Get(key string, now time.Time) (Claims, bool)
	Set(key string, claims Claims, now time.Time)
}

type memoryCacheEntry struct {
	Claims    Claims
	ExpiresAt time.Time
}

// MemoryCache is a thread-safe in-memory Cache with TTL-based eviction.
// Create one with NewMemoryCache and attach it to a verifier via WithCache.
type MemoryCache struct {
	duration time.Duration
	mu       sync.RWMutex
	entries  map[string]memoryCacheEntry
	stop     chan struct{}
	once     sync.Once
}

// NewMemoryCache returns a MemoryCache that caps each entry's TTL at duration
// and runs a background eviction goroutine until ctx is cancelled or Close is
// called.
func NewMemoryCache(ctx context.Context, duration time.Duration) *MemoryCache {
	c := &MemoryCache{
		duration: duration,
		entries:  make(map[string]memoryCacheEntry),
		stop:     make(chan struct{}),
	}
	go c.runCleanup(ctx)
	return c
}

// Get returns the cached claims for key if present and not yet expired.
func (c *MemoryCache) Get(key string, now time.Time) (Claims, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || !now.Before(entry.ExpiresAt) {
		return Claims{}, false
	}
	return entry.Claims, true
}

// Set stores claims under key, expiring at min(token.exp, now+duration).
func (c *MemoryCache) Set(key string, claims Claims, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = memoryCacheEntry{
		Claims:    claims,
		ExpiresAt: c.expiry(claims, now),
	}
}

// Close stops the background eviction goroutine. Safe to call multiple times.
func (c *MemoryCache) Close() {
	c.once.Do(func() { close(c.stop) })
}

func (c *MemoryCache) expiry(claims Claims, now time.Time) time.Time {
	configured := now.Add(c.duration)
	if claims.Exp <= 0 {
		return configured
	}
	tokenExp := time.Unix(int64(claims.Exp), 0)
	if tokenExp.Before(configured) {
		return tokenExp
	}
	return configured
}

func (c *MemoryCache) runCleanup(ctx context.Context) {
	ticker := time.NewTicker(c.duration)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.evict(time.Now())
		case <-c.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *MemoryCache) evict(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if !now.Before(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}
