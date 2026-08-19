package middlewares

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/oidcauth"
)

// memStore is a minimal in-memory oidcauth.SessionStore for testing Auth
// without a real database.
type memStore struct {
	mu   sync.Mutex
	data map[string]*oidcauth.Session
}

func newMemStore() *memStore { return &memStore{data: make(map[string]*oidcauth.Session)} }

func (s *memStore) Create(_ context.Context, sess *oidcauth.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *sess
	s.data[sess.ID] = &cp
	return nil
}

func (s *memStore) Get(_ context.Context, id string) (*oidcauth.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data[id]
	if !ok {
		return nil, oidcauth.ErrSessionNotFound
	}
	cp := *sess
	return &cp, nil
}

func (s *memStore) Touch(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.data[id]; ok {
		sess.LastSeenAt = at
	}
	return nil
}

func (s *memStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func (s *memStore) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
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

func (s *memStore) WithLock(ctx context.Context, id string, fn func(context.Context, *oidcauth.Session) (*oidcauth.Session, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data[id]
	if !ok {
		return oidcauth.ErrSessionNotFound
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

// fakeClaimsVerifier lets tests control the claims Callback extracts from a
// freshly exchanged token, without a real OIDC provider.
type fakeClaimsVerifier struct {
	claims oidcauth.Claims
	err    error
}

func (f *fakeClaimsVerifier) Verify(context.Context, string) (oidcauth.Claims, error) {
	return f.claims, f.err
}

// newMockIssuer serves a minimal OIDC discovery document plus a token
// endpoint that echoes back a canned response, mirroring oidcauth's own
// flow_test.go helper (unexported there, so duplicated here for this
// package's tests).
func newMockIssuer(t *testing.T, tokenResponse map[string]any) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/protocol/openid-connect/auth",
			"token_endpoint":         srv.URL + "/protocol/openid-connect/token",
			"end_session_endpoint":   srv.URL + "/protocol/openid-connect/logout",
			"jwks_uri":               srv.URL + "/protocol/openid-connect/certs",
		})
	})

	mux.HandleFunc("/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse)
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newTestAuth builds an Auth wired to a fake issuer, a fake claims verifier,
// and an in-memory session store, plus the store itself for assertions.
func newTestAuth(t *testing.T, claims oidcauth.Claims, tokenResponse map[string]any) (*Auth, *memStore) {
	t.Helper()

	srv := newMockIssuer(t, tokenResponse)
	flow, err := oidcauth.NewFlow(context.Background(), oidcauth.FlowConfig{
		IssuerURL:    srv.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURI:  "https://api.example.com/api/v1/auth/callback",
	})
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}

	store := newMemStore()
	sessions := oidcauth.NewSessionManager(oidcauth.SessionManagerConfig{
		Store:    store,
		Flow:     flow,
		ClientID: "test-client",
	})

	auth := NewAuth(
		flow,
		&fakeClaimsVerifier{claims: claims},
		sessions,
		"test-client",
		AuthCookieOptions{Path: "/", SameSite: http.SameSiteLaxMode},
		"https://app.example.com/",
		"https://app.example.com/",
		"state-signing-key",
	)
	return auth, store
}

func newGinContext(method, target string, cookies []*http.Cookie) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		ctx.Request.AddCookie(c)
	}
	return ctx, rec
}

func TestCallback_CreatesSessionAndSetsOpaqueCookieOnly(t *testing.T) {
	auth, store := newTestAuth(t, oidcauth.Claims{Sub: "user-1", PreferredUsername: "alice"}, map[string]any{
		"access_token":       "super-secret-access-token",
		"refresh_token":      "super-secret-refresh-token",
		"id_token":           "super-secret-id-token",
		"token_type":         "Bearer",
		"expires_in":         300,
		"refresh_expires_in": 1800,
	})

	ctx, rec := newGinContext(http.MethodGet, "/auth/callback", nil)
	state := auth.encodeState("verifier-value", "/dashboard")
	ctx.Request.URL.RawQuery = "state=" + state + "&code=auth-code"

	auth.Callback(ctx)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body: %s)", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, secret := range []string{"super-secret-access-token", "super-secret-refresh-token", "super-secret-id-token"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response body leaks a raw token: %q found in body", secret)
		}
	}

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			sessionCookie = c
		}
		if strings.Contains(c.Value, "super-secret") {
			t.Fatalf("cookie %q carries a raw token value: %q", c.Name, c.Value)
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session_id cookie was set")
	}
	if !sessionCookie.HttpOnly {
		t.Error("session_id cookie is not HttpOnly")
	}
	if sessionCookie.Value == "super-secret-access-token" {
		t.Fatal("session_id cookie value is the raw access token")
	}

	if len(store.data) != 1 {
		t.Fatalf("sessions stored = %d, want 1", len(store.data))
	}
	for _, sess := range store.data {
		if sess.Tokens.AccessToken != "super-secret-access-token" {
			t.Errorf("stored session access token = %q, want the exchanged token", sess.Tokens.AccessToken)
		}
		if sess.Claims.Sub != "user-1" {
			t.Errorf("stored session claims.Sub = %q, want %q", sess.Claims.Sub, "user-1")
		}
	}
}

func TestLogout_DeletesSessionAndClearsCookies(t *testing.T) {
	auth, store := newTestAuth(t, oidcauth.Claims{Sub: "user-1"}, nil)

	// Seed a session directly, as if a prior Callback had created it.
	_ = store.Create(context.Background(), &oidcauth.Session{
		ID:        "11111111-1111-4111-8111-111111111111",
		Tokens:    oidcauth.TokenSet{AccessToken: "a", IDToken: "id-token-abc", AccessTokenExpiresAt: time.Now().Add(time.Hour)},
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	})

	ctx, rec := newGinContext(http.MethodPost, "/auth/logout", []*http.Cookie{
		{Name: SessionCookie, Value: "11111111-1111-4111-8111-111111111111"},
	})

	auth.Logout(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if _, ok := store.data["11111111-1111-4111-8111-111111111111"]; ok {
		t.Fatal("session was not deleted from the store")
	}

	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	for _, name := range []string{SessionCookie, CSRFCookie} {
		if !cleared[name] {
			t.Errorf("cookie %q was not cleared on logout", name)
		}
	}
}

func TestLogout_IsIdempotentWithNoSessionCookie(t *testing.T) {
	auth, _ := newTestAuth(t, oidcauth.Claims{}, nil)

	ctx, rec := newGinContext(http.MethodPost, "/auth/logout", nil)
	auth.Logout(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestRefresh_ExpiredAccessTokenIsRenewedWithValidRefreshToken(t *testing.T) {
	auth, store := newTestAuth(t, oidcauth.Claims{}, map[string]any{
		"access_token":  "renewed-access-token",
		"refresh_token": "rotated-refresh-token",
		"expires_in":    300,
	})

	past := time.Now().Add(-time.Minute)
	_ = store.Create(context.Background(), &oidcauth.Session{
		ID: "22222222-2222-4222-8222-222222222222",
		Tokens: oidcauth.TokenSet{
			AccessToken:          "stale-access-token",
			RefreshToken:         "still-valid-refresh-token",
			AccessTokenExpiresAt: past,
		},
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	})

	ctx, rec := newGinContext(http.MethodPost, "/auth/refresh", []*http.Cookie{
		{Name: SessionCookie, Value: "22222222-2222-4222-8222-222222222222"},
	})

	auth.Refresh(ctx)
	ctx.Writer.WriteHeaderNow() // NoContent writes no body, so flush the status explicitly for the recorder

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}
	got := store.data["22222222-2222-4222-8222-222222222222"]
	if got == nil {
		t.Fatal("session disappeared after refresh")
	}
	if got.Tokens.AccessToken != "renewed-access-token" {
		t.Errorf("access token = %q, want the renewed one", got.Tokens.AccessToken)
	}
}

func TestRefresh_FailureDeletesSessionAndClearsCookie(t *testing.T) {
	auth, store := newTestAuth(t, oidcauth.Claims{}, nil) // token endpoint returns an empty body: no access_token, Refresh's TokenSource treats it as failure

	past := time.Now().Add(-time.Minute)
	_ = store.Create(context.Background(), &oidcauth.Session{
		ID: "33333333-3333-4333-8333-333333333333",
		Tokens: oidcauth.TokenSet{
			AccessToken:          "stale",
			RefreshToken:         "revoked-refresh-token",
			AccessTokenExpiresAt: past,
		},
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	})

	ctx, rec := newGinContext(http.MethodPost, "/auth/refresh", []*http.Cookie{
		{Name: SessionCookie, Value: "33333333-3333-4333-8333-333333333333"},
	})

	auth.Refresh(ctx)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
	if _, ok := store.data["33333333-3333-4333-8333-333333333333"]; ok {
		t.Fatal("session was not deleted after a failed refresh")
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie && c.MaxAge >= 0 {
			t.Errorf("session_id cookie was not cleared: %+v", c)
		}
	}
}

func TestMe_ReturnsIdentityFromContextClaims(t *testing.T) {
	auth, _ := newTestAuth(t, oidcauth.Claims{}, nil)

	ctx, rec := newGinContext(http.MethodGet, "/me", nil)
	ctx.Set(claimsKey, oidcauth.Claims{
		Sub:               "user-1",
		Email:             "user@example.com",
		Name:              "Test User",
		PreferredUsername: "testuser",
		ResourceAccess: map[string]map[string][]string{
			"test-client": {"roles": {"admin"}},
		},
	})

	auth.Me(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "testuser") {
		t.Errorf("body = %s, want preferred_username", rec.Body.String())
	}
}
