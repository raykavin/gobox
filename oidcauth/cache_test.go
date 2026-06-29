package oidcauth

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryCache_GetMissOnEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, 5*time.Minute)
	defer c.Close()

	if _, ok := c.Get("missing", time.Now()); ok {
		t.Error("expected miss on empty cache")
	}
}

func TestMemoryCache_SetAndGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, 5*time.Minute)
	defer c.Close()

	now := time.Now()
	want := Claims{Sub: "user-123", Email: "user@example.com"}
	c.Set("k1", want, now)

	got, ok := c.Get("k1", now.Add(time.Second))
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if got.Sub != want.Sub || got.Email != want.Email {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestMemoryCache_ExpiryByDuration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, time.Minute)
	defer c.Close()

	now := time.Now()
	c.Set("k1", Claims{Sub: "u"}, now)

	if _, ok := c.Get("k1", now.Add(30*time.Second)); !ok {
		t.Error("expected hit before duration expires")
	}
	if _, ok := c.Get("k1", now.Add(2*time.Minute)); ok {
		t.Error("expected miss after duration expires")
	}
}

func TestMemoryCache_ExpiryByTokenExp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, time.Hour) // long configured duration
	defer c.Close()

	now := time.Now()
	tokenExp := now.Add(30 * time.Second)
	c.Set("k1", Claims{Sub: "u", Exp: float64(tokenExp.Unix())}, now)

	if _, ok := c.Get("k1", now.Add(20*time.Second)); !ok {
		t.Error("expected hit before token exp")
	}
	if _, ok := c.Get("k1", now.Add(45*time.Second)); ok {
		t.Error("expected miss after token exp (shorter than configured duration)")
	}
}

func TestMemoryCache_GetExactExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, time.Minute)
	defer c.Close()

	now := time.Now()
	c.Set("k1", Claims{Sub: "u"}, now)

	// Get at exactly the expiry time should miss (not strictly before)
	if _, ok := c.Get("k1", now.Add(time.Minute)); ok {
		t.Error("expected miss at exact expiry boundary")
	}
}

func TestMemoryCache_MultipleKeys(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, 5*time.Minute)
	defer c.Close()

	now := time.Now()
	c.Set("a", Claims{Sub: "alice"}, now)
	c.Set("b", Claims{Sub: "bob"}, now)

	if got, ok := c.Get("a", now.Add(time.Second)); !ok || got.Sub != "alice" {
		t.Error("expected alice from key a")
	}
	if got, ok := c.Get("b", now.Add(time.Second)); !ok || got.Sub != "bob" {
		t.Error("expected bob from key b")
	}
}

func TestMemoryCache_EvictExpired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, time.Minute)
	defer c.Close()

	now := time.Now()
	c.Set("live", Claims{Sub: "a"}, now)
	// Short token exp so this entry expires sooner than the cache duration
	c.Set("dead", Claims{Sub: "b", Exp: float64(now.Add(10 * time.Second).Unix())}, now)

	c.evict(now.Add(30 * time.Second))

	if _, ok := c.Get("live", now.Add(30*time.Second)); !ok {
		t.Error("expected 'live' to survive eviction")
	}
	if _, ok := c.Get("dead", now.Add(30*time.Second)); ok {
		t.Error("expected 'dead' to be evicted")
	}
}

func TestMemoryCache_CloseIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, time.Minute)
	c.Close()
	c.Close() // must not panic
}

func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewMemoryCache(ctx, time.Minute)
	defer c.Close()

	const goroutines = 50
	now := time.Now()
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i)
			c.Set(key, Claims{Sub: key}, now)
			c.Get(key, now.Add(time.Second))
		}(i)
	}
	wg.Wait()
}
