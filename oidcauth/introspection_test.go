package oidcauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIntrospectionServer is a minimal RFC 7662 endpoint for testing
// Introspector directly, independent of any OIDC discovery/JWT machinery.
type fakeIntrospectionServer struct {
	srv *httptest.Server

	mu           sync.Mutex
	active       bool
	status       int
	exp          float64 // 0 = omit "exp" from the response
	calls        int
	lastToken    string
	lastAuthUser string
	lastAuthPass string
	lastMethod   string
	lastContentT string
	lastHint     string
	blockUntil   chan struct{} // if non-nil, the handler waits on it before responding
}

func newFakeIntrospectionServer(t *testing.T) *fakeIntrospectionServer {
	t.Helper()
	f := &fakeIntrospectionServer{active: true, status: http.StatusOK}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIntrospectionServer) endpoint() string { return f.srv.URL + "/introspect" }

func (f *fakeIntrospectionServer) handle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	user, pass, _ := r.BasicAuth()

	f.mu.Lock()
	f.calls++
	f.lastToken = r.FormValue("token")
	f.lastHint = r.FormValue("token_type_hint")
	f.lastMethod = r.Method
	f.lastContentT = r.Header.Get("Content-Type")
	f.lastAuthUser = user
	f.lastAuthPass = pass
	active := f.active
	status := f.status
	exp := f.exp
	blockUntil := f.blockUntil
	f.mu.Unlock()

	if blockUntil != nil {
		select {
		case <-blockUntil:
		case <-r.Context().Done():
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusOK {
		return
	}
	resp := map[string]any{"active": active}
	if exp > 0 {
		resp["exp"] = exp
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *fakeIntrospectionServer) setActive(v bool) {
	f.mu.Lock()
	f.active = v
	f.mu.Unlock()
}

func (f *fakeIntrospectionServer) setStatus(v int) {
	f.mu.Lock()
	f.status = v
	f.mu.Unlock()
}

func (f *fakeIntrospectionServer) setExp(v float64) {
	f.mu.Lock()
	f.exp = v
	f.mu.Unlock()
}

func (f *fakeIntrospectionServer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeIntrospectionServer) newIntrospector(t *testing.T, cacheTTL time.Duration) *Introspector {
	t.Helper()
	i, err := NewIntrospector(IntrospectorConfig{
		Endpoint:     f.endpoint(),
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		CacheTTL:     cacheTTL,
	})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	t.Cleanup(i.Close)
	return i
}

// ---- request shape ----

func TestIntrospect_ActiveTokenIsAccepted(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, 0)

	claims, err := i.Introspect(context.Background(), "some-access-token", time.Time{})
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	_ = claims
}

func TestIntrospect_InactiveTokenIsRejected(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.setActive(false)
	i := f.newIntrospector(t, 0)

	_, err := i.Introspect(context.Background(), "some-access-token", time.Time{})
	if !errors.Is(err, ErrAccessTokenInactive) {
		t.Fatalf("err = %v, want ErrAccessTokenInactive", err)
	}
}

func TestIntrospect_UsesPOST(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, 0)

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if f.lastMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", f.lastMethod)
	}
}

func TestIntrospect_SetsFormContentType(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, 0)

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if !strings.HasPrefix(f.lastContentT, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", f.lastContentT)
	}
}

func TestIntrospect_SendsTokenTypeHint(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, 0)

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if f.lastHint != "access_token" {
		t.Errorf("token_type_hint = %q, want access_token", f.lastHint)
	}
	if f.lastToken != "tok" {
		t.Errorf("token = %q, want tok", f.lastToken)
	}
}

func TestIntrospect_UsesBasicAuth(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, 0)

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if f.lastAuthUser != "test-client" || f.lastAuthPass != "test-secret" {
		t.Errorf("basic auth = %q:%q, want test-client:test-secret", f.lastAuthUser, f.lastAuthPass)
	}
}

func TestIntrospect_UsesConfiguredEndpoint(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i, err := NewIntrospector(IntrospectorConfig{
		Endpoint:     f.endpoint(),
		ClientID:     "c",
		ClientSecret: "s",
	})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	defer i.Close()

	if i.endpoint != f.endpoint() {
		t.Errorf("endpoint = %q, want %q", i.endpoint, f.endpoint())
	}
}

func TestResolveIntrospectionEndpoint_PrefersDiscovery(t *testing.T) {
	got, err := resolveIntrospectionEndpoint(
		fakeProviderMetadata{"introspection_endpoint": "https://issuer.example.com/discovered/introspect"},
		Config{RealmURL: "https://issuer.example.com/realms/main", IntrospectionEndpoint: "https://issuer.example.com/configured/introspect"},
	)
	if err != nil {
		t.Fatalf("resolveIntrospectionEndpoint: %v", err)
	}
	if got != "https://issuer.example.com/discovered/introspect" {
		t.Errorf("endpoint = %q, want the discovery document's value", got)
	}
}

func TestResolveIntrospectionEndpoint_FallsBackToConfig(t *testing.T) {
	got, err := resolveIntrospectionEndpoint(
		fakeProviderMetadata{},
		Config{RealmURL: "https://issuer.example.com/realms/main", IntrospectionEndpoint: "https://issuer.example.com/configured/introspect"},
	)
	if err != nil {
		t.Fatalf("resolveIntrospectionEndpoint: %v", err)
	}
	if got != "https://issuer.example.com/configured/introspect" {
		t.Errorf("endpoint = %q, want the configured override", got)
	}
}

func TestResolveIntrospectionEndpoint_ErrorsWithoutDiscoveryOrConfig(t *testing.T) {
	_, err := resolveIntrospectionEndpoint(
		fakeProviderMetadata{},
		Config{RealmURL: "https://issuer.example.com/realms/main"},
	)
	if !errors.Is(err, ErrInvalidOIDCConfiguration) {
		t.Fatalf("resolveIntrospectionEndpoint error = %v, want ErrInvalidOIDCConfiguration", err)
	}
}

type fakeProviderMetadata map[string]string

func (f fakeProviderMetadata) Claims(v any) error {
	b, err := json.Marshal(map[string]string(f))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// ---- failure handling ----

func TestIntrospect_UnexpectedStatusReturnsError(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.setStatus(http.StatusInternalServerError)
	i := f.newIntrospector(t, 0)

	_, err := i.Introspect(context.Background(), "tok", time.Time{})
	if !errors.Is(err, ErrIntrospectionFailed) {
		t.Fatalf("err = %v, want ErrIntrospectionFailed", err)
	}
}

func TestIntrospect_InvalidJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-valid-json"))
	}))
	t.Cleanup(srv.Close)

	i, err := NewIntrospector(IntrospectorConfig{Endpoint: srv.URL, ClientID: "c", ClientSecret: "s"})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	defer i.Close()

	_, err = i.Introspect(context.Background(), "tok", time.Time{})
	if !errors.Is(err, ErrInvalidIntrospectionResponse) {
		t.Fatalf("err = %v, want ErrInvalidIntrospectionResponse", err)
	}
	if errors.Is(err, ErrAccessTokenInactive) {
		t.Error("an invalid response must not also match ErrAccessTokenInactive")
	}
}

func TestIntrospect_ContextCancellationPropagates(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	blockUntil := make(chan struct{})
	// The shared (singleflight-coalesced) request deliberately runs on its
	// own background context rather than any single caller's see
	// Introspect's doc comment so it keeps running past this caller's
	// cancellation. Release it once the assertions below are done, instead
	// of leaving httptest.Server to force-close a lingering connection.
	t.Cleanup(func() { close(blockUntil) })
	f.mu.Lock()
	f.blockUntil = blockUntil
	f.mu.Unlock()
	i := f.newIntrospector(t, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := i.Introspect(ctx, "tok", time.Time{})
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Introspect took %v to respect context cancellation, want well under 2s", elapsed)
	}
}

func TestIntrospect_EmptyTokenIsRejected(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, 0)

	if _, err := i.Introspect(context.Background(), "", time.Time{}); !errors.Is(err, ErrIntrospectionFailed) {
		t.Fatalf("err = %v, want ErrIntrospectionFailed", err)
	}
	if f.callCount() != 0 {
		t.Error("an empty token must never reach the network")
	}
}

// ---- caching ----

func TestIntrospect_CacheHitAvoidsNewRequest(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, time.Minute)

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("first Introspect: %v", err)
	}
	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("second Introspect: %v", err)
	}
	if f.callCount() != 1 {
		t.Errorf("calls = %d, want 1 (second call should hit the cache)", f.callCount())
	}
}

func TestIntrospect_CacheMissCallsSSO(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, time.Minute)

	if _, err := i.Introspect(context.Background(), "tok-a", time.Time{}); err != nil {
		t.Fatalf("Introspect tok-a: %v", err)
	}
	if _, err := i.Introspect(context.Background(), "tok-b", time.Time{}); err != nil {
		t.Fatalf("Introspect tok-b: %v", err)
	}
	if f.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (distinct tokens must not share a cache entry)", f.callCount())
	}
}

func TestIntrospect_CacheDisabledAlwaysCallsSSO(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, 0) // CacheTTL 0: caching disabled

	for n := 1; n <= 3; n++ {
		if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
			t.Fatalf("Introspect #%d: %v", n, err)
		}
	}
	if f.callCount() != 3 {
		t.Errorf("calls = %d, want 3 (cache disabled must never short-circuit)", f.callCount())
	}
}

func TestIntrospect_InactiveResultNotCached(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.setActive(false)
	i := f.newIntrospector(t, time.Minute)

	for n := 1; n <= 2; n++ {
		if _, err := i.Introspect(context.Background(), "tok", time.Time{}); !errors.Is(err, ErrAccessTokenInactive) {
			t.Fatalf("Introspect #%d: err = %v, want ErrAccessTokenInactive", n, err)
		}
	}
	if f.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (an inactive result must never be cached)", f.callCount())
	}
}

func TestIntrospect_TokenIsNotUsedAsCacheKey(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, time.Minute)

	token := "super-secret-access-token-value"
	if _, err := i.Introspect(context.Background(), token, time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	i.mu.RLock()
	defer i.mu.RUnlock()
	for k := range i.entries {
		if string(k[:]) == token {
			t.Fatal("cache key equals the raw token bytes")
		}
	}
	if len(i.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(i.entries))
	}
	wantKey := newTokenCacheKey(token)
	if _, ok := i.entries[wantKey]; !ok {
		t.Fatal("expected the SHA-256 digest of the token to be the cache key")
	}
}

func TestEffectiveExpiry_RespectsConfiguredTTL(t *testing.T) {
	i := &Introspector{cacheTTL: 5 * time.Minute, now: fixedNow(t)}
	got := i.effectiveExpiry(time.Time{}, 0)
	want := i.now().Add(5 * time.Minute)
	if !got.Equal(want) {
		t.Errorf("expiresAt = %v, want %v", got, want)
	}
}

func TestEffectiveExpiry_CappedByKnownExpiry(t *testing.T) {
	i := &Introspector{cacheTTL: time.Hour, now: fixedNow(t)}
	knownExpiry := i.now().Add(2 * time.Minute) // shorter than the configured TTL
	got := i.effectiveExpiry(knownExpiry, 0)
	if !got.Equal(knownExpiry) {
		t.Errorf("expiresAt = %v, want the shorter knownExpiry %v", got, knownExpiry)
	}
}

func TestEffectiveExpiry_CappedByIntrospectedExp(t *testing.T) {
	i := &Introspector{cacheTTL: time.Hour, now: fixedNow(t)}
	introspectedExp := float64(i.now().Add(90 * time.Second).Unix())
	got := i.effectiveExpiry(time.Time{}, introspectedExp)
	want := time.Unix(int64(introspectedExp), 0)
	if !got.Equal(want) {
		t.Errorf("expiresAt = %v, want %v", got, want)
	}
}

func TestEffectiveExpiry_AlreadyExpiredIsNotCacheable(t *testing.T) {
	i := &Introspector{cacheTTL: time.Hour, now: fixedNow(t)}
	past := i.now().Add(-time.Minute)
	got := i.effectiveExpiry(past, 0)
	if !got.IsZero() {
		t.Errorf("expiresAt = %v, want zero (already-expired token must not be cached)", got)
	}
}

func TestEffectiveExpiry_CacheDisabledReturnsZero(t *testing.T) {
	i := &Introspector{cacheTTL: 0, now: fixedNow(t)}
	got := i.effectiveExpiry(i.now().Add(time.Hour), 0)
	if !got.IsZero() {
		t.Errorf("expiresAt = %v, want zero when caching is disabled", got)
	}
}

func TestIntrospect_TokenExpiringBeforeIntrospectionIsNotCached(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.setExp(float64(time.Now().Add(-time.Hour).Unix())) // issuer reports an already-past exp
	i := f.newIntrospector(t, time.Minute)

	for n := 1; n <= 2; n++ {
		if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
			t.Fatalf("Introspect #%d: %v", n, err)
		}
	}
	if f.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (an already-expired token must not be cached)", f.callCount())
	}
}

func fixedNow(t *testing.T) func() time.Time {
	t.Helper()
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return fixed }
}

// ---- configuration validation ----

func TestNewIntrospector_RequiresEndpoint(t *testing.T) {
	_, err := NewIntrospector(IntrospectorConfig{ClientID: "c", ClientSecret: "s"})
	if !errors.Is(err, ErrInvalidOIDCConfiguration) {
		t.Fatalf("err = %v, want ErrInvalidOIDCConfiguration", err)
	}
}

func TestNewIntrospector_RejectsNegativeCacheTTL(t *testing.T) {
	_, err := NewIntrospector(IntrospectorConfig{Endpoint: "https://issuer.example.com/introspect", CacheTTL: -time.Second})
	if !errors.Is(err, ErrInvalidOIDCConfiguration) {
		t.Fatalf("err = %v, want ErrInvalidOIDCConfiguration", err)
	}
}

func TestNewIntrospector_RejectsNegativeHTTPTimeout(t *testing.T) {
	_, err := NewIntrospector(IntrospectorConfig{Endpoint: "https://issuer.example.com/introspect", HTTPTimeout: -time.Second})
	if !errors.Is(err, ErrInvalidOIDCConfiguration) {
		t.Fatalf("err = %v, want ErrInvalidOIDCConfiguration", err)
	}
}

func TestNewIntrospector_ZeroCacheTTLDoesNotStartCleanupGoroutine(t *testing.T) {
	i, err := NewIntrospector(IntrospectorConfig{Endpoint: "https://issuer.example.com/introspect"})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	// Close must return immediately: if a cleanup goroutine had been
	// started despite CacheTTL being 0, wg.Wait() inside Close would only
	// return once that (never-started, in this case absent) goroutine
	// exits, so this also indirectly proves no such goroutine is running.
	done := make(chan struct{})
	go func() { i.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return promptly; a cleanup goroutine may be running despite CacheTTL=0")
	}
}

// ---- cleanup / lifecycle ----

func TestIntrospector_CleanupRemovesExpiredEntries(t *testing.T) {
	i, err := NewIntrospector(IntrospectorConfig{Endpoint: "https://issuer.example.com/introspect", CacheTTL: time.Hour})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	defer i.Close()

	now := time.Now()
	i.store(newTokenCacheKey("expired"), IntrospectionCacheEntry{ExpiresAt: now.Add(-time.Minute)})
	i.store(newTokenCacheKey("valid"), IntrospectionCacheEntry{ExpiresAt: now.Add(time.Hour)})

	i.evict(now)

	if _, ok := i.lookup(newTokenCacheKey("expired")); ok {
		t.Error("expired entry was not evicted")
	}
	if _, ok := i.lookup(newTokenCacheKey("valid")); !ok {
		t.Error("valid entry was incorrectly evicted")
	}
}

func TestIntrospector_ClearCache(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	i := f.newIntrospector(t, time.Hour)

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	i.ClearCache()

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect after ClearCache: %v", err)
	}
	if f.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (ClearCache must force a fresh lookup)", f.callCount())
	}
}

func TestIntrospector_CloseIsIdempotent(t *testing.T) {
	i, err := NewIntrospector(IntrospectorConfig{Endpoint: "https://issuer.example.com/introspect", CacheTTL: time.Minute})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	i.Close()
	i.Close()
	i.Close()
}

func TestIntrospector_CloseStopsCleanupGoroutine(t *testing.T) {
	i, err := NewIntrospector(IntrospectorConfig{Endpoint: "https://issuer.example.com/introspect", CacheTTL: time.Minute})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}

	done := make(chan struct{})
	go func() { i.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return; cleanup goroutine may have leaked")
	}
}

// ---- concurrency ----

func TestIntrospect_ConcurrentCallsForSameTokenAreCoalesced(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.mu.Lock()
	f.blockUntil = make(chan struct{})
	f.mu.Unlock()
	i := f.newIntrospector(t, time.Minute)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for k := 0; k < n; k++ {
		go func() {
			defer wg.Done()
			if _, err := i.Introspect(context.Background(), "same-token", time.Time{}); err != nil {
				t.Errorf("Introspect: %v", err)
			}
		}()
	}

	// Give every goroutine a chance to reach the (blocked) request before
	// releasing it, so they are genuinely concurrent, not sequential.
	time.Sleep(50 * time.Millisecond)
	close(f.blockUntil)
	wg.Wait()

	if got := f.callCount(); got != 1 {
		t.Fatalf("calls = %d, want exactly 1 (concurrent calls for the same token must be coalesced)", got)
	}
}

func TestIntrospect_DifferentTokensAreNotSerializedByAGlobalLock(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.mu.Lock()
	f.blockUntil = make(chan struct{})
	f.mu.Unlock()
	i := f.newIntrospector(t, 0)

	const n = 5
	var wg sync.WaitGroup
	wg.Add(n)
	for k := 0; k < n; k++ {
		token := "token-" + string(rune('a'+k))
		go func(tok string) {
			defer wg.Done()
			if _, err := i.Introspect(context.Background(), tok, time.Time{}); err != nil {
				t.Errorf("Introspect(%s): %v", tok, err)
			}
		}(token)
	}

	// If a distinct-token call were serialized behind a single global
	// lock, none of them could reach the (blocked) handler concurrently,
	// and this deadline would trip before the handler is released.
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-ticker.C:
			if f.callCount() == n {
				break loop
			}
		case <-deadline:
			t.Fatalf("only %d/%d distinct-token requests reached the server; a global lock may be serializing them", f.callCount(), n)
		}
	}

	close(f.blockUntil)
	wg.Wait()
}

// ---- error content ----

func TestIntrospect_ErrorsDoNotLeakTheToken(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.setStatus(http.StatusInternalServerError)
	i := f.newIntrospector(t, 0)

	const secretToken = "sk_super_secret_value_12345"
	_, err := i.Introspect(context.Background(), secretToken, time.Time{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("error message leaks the raw token: %v", err)
	}
}

func TestIntrospect_InactiveErrorDoesNotLeakToken(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	f.setActive(false)
	i := f.newIntrospector(t, 0)

	const secretToken = "sk_super_secret_value_67890"
	_, err := i.Introspect(context.Background(), secretToken, time.Time{})
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("error message leaks the raw token: %v", err)
	}
}

// ---- events ----

func TestIntrospector_EmitsCacheHitAndMissEvents(t *testing.T) {
	f := newFakeIntrospectionServer(t)
	var kinds []IntrospectionEventKind
	var mu sync.Mutex
	i, err := NewIntrospector(IntrospectorConfig{
		Endpoint: f.endpoint(), ClientID: "c", ClientSecret: "s", CacheTTL: time.Minute,
		OnEvent: func(e IntrospectionEvent) {
			mu.Lock()
			kinds = append(kinds, e.Kind)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	defer i.Close()

	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if _, err := i.Introspect(context.Background(), "tok", time.Time{}); err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(kinds) < 2 {
		t.Fatalf("kinds = %v, want at least a miss then a hit", kinds)
	}
	if kinds[0] != EventCacheMiss {
		t.Errorf("first event = %v, want EventCacheMiss", kinds[0])
	}
	found := false
	for _, k := range kinds {
		if k == EventCacheHit {
			found = true
		}
	}
	if !found {
		t.Errorf("kinds = %v, want an EventCacheHit for the second call", kinds)
	}
}
