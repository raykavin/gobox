package oidcauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStore is a minimal in-memory SessionStore for SessionManager tests.
type fakeStore struct {
	mu           sync.Mutex
	data         map[string]*Session
	withLockCall int32
}

func newFakeStore() *fakeStore { return &fakeStore{data: make(map[string]*Session)} }

func (s *fakeStore) Create(_ context.Context, sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *sess
	s.data[sess.ID] = &cp
	return nil
}

func (s *fakeStore) Get(_ context.Context, id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	cp := *sess
	return &cp, nil
}

func (s *fakeStore) Touch(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.data[id]; ok {
		sess.LastSeenAt = at
	}
	return nil
}

func (s *fakeStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func (s *fakeStore) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, sess := range s.data {
		if !now.Before(sess.ExpiresAt) {
			delete(s.data, id)
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) WithLock(ctx context.Context, id string, fn func(context.Context, *Session) (*Session, error)) error {
	atomic.AddInt32(&s.withLockCall, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data[id]
	if !ok {
		return ErrSessionNotFound
	}
	cp := *sess
	updated, err := fn(ctx, &cp)
	if err != nil {
		return err
	}
	if updated != nil {
		s.data[id] = updated
	}
	return nil
}

// newRefreshIssuer serves a discovery document and a token endpoint that
// counts calls and returns a canned response (or a failure) so refresh
// behavior can be exercised without a real Keycloak.
func newRefreshIssuer(t *testing.T, tokenResponse map[string]any) (*Flow, *int32) {
	t.Helper()

	var calls int32
	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/auth",
			"token_endpoint":         srv.URL + "/token",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse)
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	flow, err := NewFlow(context.Background(), FlowConfig{
		IssuerURL: srv.URL,
		ClientID:  "test-client",
	})
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}
	return flow, &calls
}

func TestSessionManager_Resolve_ValidSessionTouchesLastSeen(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, map[string]any{"access_token": "unused", "expires_in": 300})
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	_ = store.Create(context.Background(), &Session{
		ID:        id,
		Tokens:    TokenSet{AccessToken: "at", AccessTokenExpiresAt: now.Add(time.Hour)},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	})

	got, err := mgr.Resolve(context.Background(), id)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.LastSeenAt.IsZero() {
		t.Error("LastSeenAt was not touched")
	}
}

// fakeClaimsVerifier lets tests control what Resolve's revalidation call
// returns, without a real OIDC provider.
type fakeClaimsVerifier struct {
	err   error
	calls int32
}

func (f *fakeClaimsVerifier) Verify(context.Context, string) (Claims, error) {
	atomic.AddInt32(&f.calls, 1)
	return Claims{}, f.err
}

// TestSessionManager_Resolve_RevalidatesEvenWhenAccessTokenNotYetExpired is
// the regression test for a session that is not locally expired but whose
// access token the issuer would now reject (the user was disabled/locked,
// the token was revoked, or the SSO session ended): without a configured
// Verifier, the fast path used to trust the locally stored expiry alone and
// never asked the issuer again until the access token's own exp forced a
// refresh so a revoked user kept a working session for however long that
// was. With a Verifier configured, Resolve must call it on every fast-path
// hit and reject the session the moment the issuer says no.
func TestSessionManager_Resolve_RevalidatesEvenWhenAccessTokenNotYetExpired(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	verifier := &fakeClaimsVerifier{err: ErrAccessTokenInactive}
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow, Verifier: verifier})

	now := time.Now()
	id := "30303030-3030-4303-8303-303030303030"
	_ = store.Create(context.Background(), &Session{
		ID:        id,
		Tokens:    TokenSet{AccessToken: "still-locally-valid", AccessTokenExpiresAt: now.Add(time.Hour)},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	})

	if _, err := mgr.Resolve(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound (revalidation should have rejected the session)", err)
	}
	if atomic.LoadInt32(&verifier.calls) != 1 {
		t.Fatalf("verifier called %d times, want 1", verifier.calls)
	}
	if _, ok := store.data[id]; ok {
		t.Fatal("session rejected by revalidation was not deleted")
	}
}

func TestSessionManager_Resolve_SucceedsWhenVerifierAccepts(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	verifier := &fakeClaimsVerifier{err: nil}
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow, Verifier: verifier})

	now := time.Now()
	id := "40404040-4040-4404-8404-404040404040"
	_ = store.Create(context.Background(), &Session{
		ID:        id,
		Tokens:    TokenSet{AccessToken: "still-valid", AccessTokenExpiresAt: now.Add(time.Hour)},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	})

	got, err := mgr.Resolve(context.Background(), id)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != id {
		t.Errorf("ID = %q, want %q", got.ID, id)
	}
	if atomic.LoadInt32(&verifier.calls) != 1 {
		t.Fatalf("verifier called %d times, want 1", verifier.calls)
	}
}

func TestSessionManager_Resolve_NilVerifierSkipsRevalidation(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	// No Verifier configured: preserves the old, purely-local-expiry
	// behavior exactly (documented as a deliberate opt-out).
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	id := "50505050-5050-4505-8505-505050505050"
	_ = store.Create(context.Background(), &Session{
		ID:        id,
		Tokens:    TokenSet{AccessToken: "at", AccessTokenExpiresAt: now.Add(time.Hour)},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	})

	if _, err := mgr.Resolve(context.Background(), id); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestSessionManager_Resolve_MalformedIDIsNotFound(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	if _, err := mgr.Resolve(context.Background(), "not-a-uuid"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionManager_Resolve_UnknownIDIsNotFound(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	id := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := mgr.Resolve(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionManager_Resolve_ExpiredSessionIsDeleted(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	id := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	_ = store.Create(context.Background(), &Session{
		ID:        id,
		Tokens:    TokenSet{AccessToken: "at", AccessTokenExpiresAt: now.Add(time.Hour)},
		ExpiresAt: now.Add(-time.Minute), // already past its ceiling
		CreatedAt: now.Add(-time.Hour),
	})

	if _, err := mgr.Resolve(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if _, ok := store.data[id]; ok {
		t.Fatal("expired session was not deleted")
	}
}

func TestSessionManager_Resolve_IdleTimeoutExpiresSession(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow, IdleTimeout: time.Minute})

	now := time.Now()
	id := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	_ = store.Create(context.Background(), &Session{
		ID:         id,
		Tokens:     TokenSet{AccessToken: "at", AccessTokenExpiresAt: now.Add(time.Hour)},
		ExpiresAt:  now.Add(24 * time.Hour),
		CreatedAt:  now.Add(-2 * time.Hour),
		LastSeenAt: now.Add(-2 * time.Minute), // idle longer than IdleTimeout
	})

	if _, err := mgr.Resolve(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if _, ok := store.data[id]; ok {
		t.Fatal("idle-timed-out session was not deleted")
	}
}

func TestSessionManager_Resolve_RefreshesExpiredAccessToken(t *testing.T) {
	store := newFakeStore()
	flow, calls := newRefreshIssuer(t, map[string]any{
		"access_token":  "new-access",
		"refresh_token": "new-refresh",
		"expires_in":    300,
	})
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	id := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	_ = store.Create(context.Background(), &Session{
		ID: id,
		Tokens: TokenSet{
			AccessToken:          "stale-access",
			RefreshToken:         "old-refresh",
			AccessTokenExpiresAt: now.Add(-time.Minute),
		},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
	})

	got, err := mgr.Resolve(context.Background(), id)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Tokens.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want %q", got.Tokens.AccessToken, "new-access")
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("token endpoint called %d times, want 1", *calls)
	}
}

func TestSessionManager_Resolve_NoRefreshTokenIsNotFoundOnExpiry(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	id := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	_ = store.Create(context.Background(), &Session{
		ID: id,
		Tokens: TokenSet{
			AccessToken:          "stale-access",
			AccessTokenExpiresAt: now.Add(-time.Minute),
			// no refresh token
		},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
	})

	if _, err := mgr.Resolve(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionManager_Resolve_RefreshFailureDeletesSession(t *testing.T) {
	store := newFakeStore()
	// Empty token response => x/oauth2 reports "server response missing
	// access_token", simulating invalid_grant/revoked/ended-session.
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	id := "10101010-1010-4101-8101-101010101010"
	_ = store.Create(context.Background(), &Session{
		ID: id,
		Tokens: TokenSet{
			AccessToken:          "stale-access",
			RefreshToken:         "revoked-refresh",
			AccessTokenExpiresAt: now.Add(-time.Minute),
		},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
	})

	if _, err := mgr.Resolve(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if _, ok := store.data[id]; ok {
		t.Fatal("session was not deleted after a failed refresh")
	}
}

// TestSessionManager_Resolve_ConcurrentCallersRefreshOnlyOnce is the direct
// regression test for "concurrent calls must not trigger a duplicate
// refresh": ten goroutines resolve the same expired-access-token session at
// once; the token endpoint (which a real Keycloak would reject the second
// call to, since it rotates and invalidates the previous refresh token on
// every use) must be hit exactly once.
func TestSessionManager_Resolve_ConcurrentCallersRefreshOnlyOnce(t *testing.T) {
	store := newFakeStore()
	flow, calls := newRefreshIssuer(t, map[string]any{
		"access_token":  "new-access",
		"refresh_token": "new-refresh",
		"expires_in":    300,
	})
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	id := "20202020-2020-4202-8202-202020202020"
	_ = store.Create(context.Background(), &Session{
		ID: id,
		Tokens: TokenSet{
			AccessToken:          "stale-access",
			RefreshToken:         "old-refresh",
			AccessTokenExpiresAt: now.Add(-time.Minute),
		},
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
	})

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := mgr.Resolve(context.Background(), id); err != nil {
				t.Errorf("Resolve: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("token endpoint called %d times, want exactly 1", got)
	}
}

func TestSessionManager_ComputeExpiresAt_NeverExceedsAbsoluteTimeout(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow, AbsoluteTimeout: time.Hour})

	created := time.Now()
	s := &Session{
		CreatedAt: created,
		Tokens: TokenSet{
			RefreshToken:          "rt",
			RefreshTokenExpiresAt: created.Add(30 * 24 * time.Hour), // issuer would allow 30 days
		},
	}

	got := mgr.computeExpiresAt(s)
	want := created.Add(time.Hour)
	if !got.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v (absolute timeout must win over the longer IdP-granted ceiling)", got, want)
	}
}

func TestSessionManager_ComputeExpiresAt_NoRefreshTokenUsesAccessTokenExpiry(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	now := time.Now()
	s := &Session{
		CreatedAt: now,
		Tokens:    TokenSet{AccessTokenExpiresAt: now.Add(5 * time.Minute)},
	}

	got := mgr.computeExpiresAt(s)
	if !got.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("ExpiresAt = %v, want access token expiry %v", got, now.Add(5*time.Minute))
	}
}

func TestSessionManager_Delete_IsIdempotent(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow})

	if err := mgr.Delete(context.Background(), "never-existed"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestSessionManager_HasRole(t *testing.T) {
	store := newFakeStore()
	flow, _ := newRefreshIssuer(t, nil)
	mgr := NewSessionManager(SessionManagerConfig{Store: store, Flow: flow, ClientID: "my-client"})

	claims := Claims{ResourceAccess: map[string]map[string][]string{
		"my-client": {"roles": {"admin", "operator"}},
	}}

	if !mgr.HasRole(claims, "admin") {
		t.Error("HasRole(admin) = false, want true")
	}
	if mgr.HasRole(claims, "superuser") {
		t.Error("HasRole(superuser) = true, want false")
	}
}
