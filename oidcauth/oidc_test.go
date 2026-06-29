package oidcauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type mockProvider struct {
	mu               sync.Mutex
	server           *httptest.Server
	privateKey       *rsa.PrivateKey
	keyID            string
	active           bool
	introspectStatus int
	introspectCalls  int
}

func newMockProvider(t *testing.T) *mockProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	m := &mockProvider{
		privateKey:       key,
		keyID:            "test-kid",
		active:           true,
		introspectStatus: http.StatusOK,
	}
	mux := http.NewServeMux()
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	mux.HandleFunc("/.well-known/openid-configuration", m.serveDiscovery)
	mux.HandleFunc("/protocol/openid-connect/certs", m.serveJWKS)
	mux.HandleFunc("/protocol/openid-connect/token/introspect", m.serveIntrospect)
	return m
}

func (m *mockProvider) issuer() string { return m.server.URL }

func (m *mockProvider) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	base := m.server.URL
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"issuer":                                base,
		"jwks_uri":                              base + "/protocol/openid-connect/certs",
		"authorization_endpoint":                base + "/protocol/openid-connect/auth",
		"token_endpoint":                        base + "/protocol/openid-connect/token",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (m *mockProvider) serveJWKS(w http.ResponseWriter, r *http.Request) {
	pub := &m.privateKey.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys": []map[string]interface{}{
			{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": m.keyID, "n": n, "e": e},
		},
	})
}

func (m *mockProvider) serveIntrospect(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	active := m.active
	status := m.introspectStatus
	m.introspectCalls++
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusOK {
		json.NewEncoder(w).Encode(map[string]bool{"active": active})
	}
}

func (m *mockProvider) setActive(active bool) {
	m.mu.Lock()
	m.active = active
	m.mu.Unlock()
}

func (m *mockProvider) setIntrospectStatus(code int) {
	m.mu.Lock()
	m.introspectStatus = code
	m.mu.Unlock()
}

func (m *mockProvider) introspectCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.introspectCalls
}

// makeToken creates a signed RS256 JWT using the given claims map.
func (m *mockProvider) makeToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	return signJWT(t, m.keyID, m.privateKey, claims)
}

// signJWT signs a JWT with the given key, allowing tests to use a wrong key.
func signJWT(t *testing.T, kid string, key *rsa.PrivateKey, claims map[string]interface{}) string {
	t.Helper()
	hdrBytes, err := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal JWT header: %v", err)
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal JWT claims: %v", err)
	}
	hdr := base64.RawURLEncoding.EncodeToString(hdrBytes)
	pld := base64.RawURLEncoding.EncodeToString(claimsBytes)
	sigInput := hdr + "." + pld
	digest := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// newTestVerifier builds an OIDC verifier pointed at the mock provider.
// SkipExpiryCheck is always true so tests do not need to manage exp values.
func newTestVerifier(t *testing.T, mp *mockProvider, opts ...Option) *OIDC {
	t.Helper()
	v, err := New(context.Background(), Config{
		RealmURL:        mp.issuer(),
		ClientID:        "test-client",
		ClientSecret:    "secret",
		SkipExpiryCheck: true,
	}, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

// validClaims returns a minimal claim set that passes all verifier checks.
func validClaims(mp *mockProvider) map[string]interface{} {
	return map[string]interface{}{
		"iss": mp.issuer(),
		"sub": "user-123",
		"aud": []string{"test-client"},
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"azp": "test-client",
	}
}

func TestNew_InvalidRealmURL(t *testing.T) {
	_, err := New(context.Background(), Config{ClientID: "app"})
	if !errors.Is(err, ErrInvalidRealmURL) {
		t.Errorf("expected ErrInvalidRealmURL, got %v", err)
	}
}

func TestNew_EmptyClientID(t *testing.T) {
	_, err := New(context.Background(), Config{RealmURL: "https://kc.example.com/realms/main"})
	if !errors.Is(err, ErrEmptyClientID) {
		t.Errorf("expected ErrEmptyClientID, got %v", err)
	}
}

func TestNew_ProviderInitFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := New(context.Background(), Config{
		RealmURL:     srv.URL,
		ClientID:     "app",
		ClientSecret: "secret",
	})
	if !errors.Is(err, ErrProviderInitFailed) {
		t.Errorf("expected ErrProviderInitFailed, got %v", err)
	}
}

func TestNew_Success(t *testing.T) {
	mp := newMockProvider(t)
	v := newTestVerifier(t, mp)
	if v == nil {
		t.Fatal("expected non-nil verifier")
	}
}

// ---- Verify ----

func TestVerify_ValidToken(t *testing.T) {
	mp := newMockProvider(t)
	v := newTestVerifier(t, mp)

	token := mp.makeToken(t, validClaims(mp))
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Sub != "user-123" {
		t.Errorf("Sub = %q, want user-123", claims.Sub)
	}
}

func TestVerify_RevokedToken(t *testing.T) {
	mp := newMockProvider(t)
	mp.setActive(false)
	v := newTestVerifier(t, mp)

	token := mp.makeToken(t, validClaims(mp))
	_, err := v.Verify(context.Background(), token)
	if !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("expected ErrTokenRevoked, got %v", err)
	}
}

func TestVerify_InvalidSignature(t *testing.T) {
	mp := newMockProvider(t)
	v := newTestVerifier(t, mp)

	wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	token := signJWT(t, mp.keyID, wrongKey, validClaims(mp))
	_, err := v.Verify(context.Background(), token)
	if !errors.Is(err, ErrTokenValidationFailed) {
		t.Errorf("expected ErrTokenValidationFailed, got %v", err)
	}
}

func TestVerify_MalformedToken(t *testing.T) {
	mp := newMockProvider(t)
	v := newTestVerifier(t, mp)

	_, err := v.Verify(context.Background(), "not.a.jwt")
	if !errors.Is(err, ErrTokenValidationFailed) {
		t.Errorf("expected ErrTokenValidationFailed, got %v", err)
	}
}

func TestVerify_IntrospectionFailure(t *testing.T) {
	mp := newMockProvider(t)
	v := newTestVerifier(t, mp)
	mp.setIntrospectStatus(http.StatusInternalServerError)

	token := mp.makeToken(t, validClaims(mp))
	_, err := v.Verify(context.Background(), token)
	if !errors.Is(err, ErrTokenValidationFailed) {
		t.Errorf("expected ErrTokenValidationFailed, got %v", err)
	}
	if !errors.Is(err, ErrIntrospectionFailed) {
		t.Errorf("expected wrapped ErrIntrospectionFailed, got %v", err)
	}
}

func TestVerify_CacheHitSkipsIntrospection(t *testing.T) {
	mp := newMockProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewMemoryCache(ctx, 5*time.Minute)
	defer cache.Close()
	v := newTestVerifier(t, mp, WithCache(cache))

	token := mp.makeToken(t, validClaims(mp))

	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("first Verify: %v", err)
	}
	callsAfterFirst := mp.introspectCount()

	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("second Verify: %v", err)
	}
	if mp.introspectCount() != callsAfterFirst {
		t.Error("expected no introspection call on cache hit")
	}
}

func TestVerify_CacheMissCallsIntrospection(t *testing.T) {
	mp := newMockProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewMemoryCache(ctx, 5*time.Minute)
	defer cache.Close()
	v := newTestVerifier(t, mp, WithCache(cache))

	token1 := mp.makeToken(t, validClaims(mp))
	token2 := mp.makeToken(t, func() map[string]interface{} {
		c := validClaims(mp)
		c["sub"] = "other-user"
		c["jti"] = "different-jti"
		return c
	}())

	if _, err := v.Verify(context.Background(), token1); err != nil {
		t.Fatalf("Verify token1: %v", err)
	}
	if _, err := v.Verify(context.Background(), token2); err != nil {
		t.Fatalf("Verify token2: %v", err)
	}
	if mp.introspectCount() != 2 {
		t.Errorf("expected 2 introspect calls for 2 distinct tokens, got %d", mp.introspectCount())
	}
}

func TestIntrospect_Active(t *testing.T) {
	mp := newMockProvider(t)
	v := newTestVerifier(t, mp)

	result, err := v.introspect(context.Background(), "any-token")
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if !result.Active {
		t.Error("expected active=true")
	}
}

func TestIntrospect_Inactive(t *testing.T) {
	mp := newMockProvider(t)
	mp.setActive(false)
	v := newTestVerifier(t, mp)

	result, err := v.introspect(context.Background(), "any-token")
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if result.Active {
		t.Error("expected active=false")
	}
}

func TestIntrospect_HTTPError(t *testing.T) {
	mp := newMockProvider(t)
	mp.setIntrospectStatus(http.StatusUnauthorized)
	v := newTestVerifier(t, mp)

	_, err := v.introspect(context.Background(), "any-token")
	if !errors.Is(err, ErrIntrospectionFailed) {
		t.Errorf("expected ErrIntrospectionFailed, got %v", err)
	}
}

func TestIntrospect_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-json"))
	}))
	t.Cleanup(srv.Close)

	v := &OIDC{
		config:     Config{RealmURL: srv.URL, ClientID: "app", ClientSecret: "s"},
		httpClient: &http.Client{},
	}
	_, err := v.introspect(context.Background(), "token")
	if !errors.Is(err, ErrIntrospectionFailed) {
		t.Errorf("expected ErrIntrospectionFailed, got %v", err)
	}
}

func TestHasRole_Present(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "my-app"}}
	claims := Claims{
		ResourceAccess: map[string]map[string][]string{
			"my-app": {"roles": {"admin", "editor"}},
		},
	}
	if !v.HasRole(claims, "admin") {
		t.Error("expected HasRole(admin) = true")
	}
	if !v.HasRole(claims, "editor") {
		t.Error("expected HasRole(editor) = true")
	}
}

func TestHasRole_Absent(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "my-app"}}
	claims := Claims{
		ResourceAccess: map[string]map[string][]string{
			"my-app": {"roles": {"viewer"}},
		},
	}
	if v.HasRole(claims, "admin") {
		t.Error("expected HasRole(admin) = false")
	}
}

func TestHasRole_ClientNotInResourceAccess(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "my-app"}}
	claims := Claims{
		ResourceAccess: map[string]map[string][]string{
			"other-app": {"roles": {"admin"}},
		},
	}
	if v.HasRole(claims, "admin") {
		t.Error("expected false when client is not in ResourceAccess")
	}
}

func TestHasRole_EmptyResourceAccess(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "my-app"}}
	if v.HasRole(Claims{}, "admin") {
		t.Error("expected false for empty ResourceAccess")
	}
}

func TestHasScope_Present(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	claims := Claims{Scope: "openid profile email read:data"}

	for _, scope := range []string{"openid", "profile", "email", "read:data"} {
		if !v.HasScope(claims, scope) {
			t.Errorf("expected HasScope(%q) = true", scope)
		}
	}
}

func TestHasScope_Absent(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	claims := Claims{Scope: "openid profile"}

	if v.HasScope(claims, "write:data") {
		t.Error("expected HasScope(write:data) = false")
	}
}

func TestHasScope_NoPartialMatch(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	claims := Claims{Scope: "openid"}

	if v.HasScope(claims, "open") {
		t.Error("expected no partial match: 'open' should not match 'openid'")
	}
}

func TestHasScope_EmptyClaimScope(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	if v.HasScope(Claims{}, "openid") {
		t.Error("expected false for empty Scope field")
	}
}

func TestHasScope_EmptyScopeArg(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	claims := Claims{Scope: "openid profile"}
	if v.HasScope(claims, "") {
		t.Error("expected false for empty scope argument")
	}
}

func TestHasAllScopes_AllPresent(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	claims := Claims{Scope: "openid profile read:data"}

	if !v.HasAllScopes(claims, "openid", "profile", "read:data") {
		t.Error("expected true when all scopes are present")
	}
}

func TestHasAllScopes_OneMissing(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	claims := Claims{Scope: "openid profile"}

	if v.HasAllScopes(claims, "openid", "write:data") {
		t.Error("expected false when one scope is missing")
	}
}

func TestHasAllScopes_EmptyVariadic(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	if !v.HasAllScopes(Claims{} /* no scopes required */) {
		t.Error("expected true (vacuous) when no scopes are required")
	}
}

func TestHasAllScopes_SingleScope(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	claims := Claims{Scope: "read:data"}

	if !v.HasAllScopes(claims, "read:data") {
		t.Error("expected true for single matching scope")
	}
	if v.HasAllScopes(claims, "write:data") {
		t.Error("expected false for single missing scope")
	}
}

func TestIsAuthorizedParty_Match(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	if !v.IsAuthorizedParty(Claims{Azp: "gateway"}, "gateway") {
		t.Error("expected true for matching azp")
	}
}

func TestIsAuthorizedParty_Mismatch(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	if v.IsAuthorizedParty(Claims{Azp: "gateway"}, "other") {
		t.Error("expected false for mismatched azp")
	}
}

func TestIsAuthorizedParty_EmptyClaimsAzp(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	if v.IsAuthorizedParty(Claims{Azp: ""}, "gateway") {
		t.Error("expected false when claims.Azp is empty")
	}
}

func TestIsAuthorizedParty_EmptyArgument(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	if v.IsAuthorizedParty(Claims{Azp: "gateway"}, "") {
		t.Error("expected false when azp argument is empty")
	}
}

func TestIsAuthorizedParty_BothEmpty(t *testing.T) {
	v := &OIDC{config: Config{ClientID: "app"}}
	if !v.IsAuthorizedParty(Claims{Azp: ""}, "") {
		t.Error("expected true when both azp values are empty strings")
	}
}

func TestNew_MissingClientSecretWhenIntrospectionEnabled(t *testing.T) {
	mp := newMockProvider(t)
	_, err := New(context.Background(), Config{
		RealmURL:        mp.issuer(),
		ClientID:        "test-client",
		SkipExpiryCheck: true,
		// ClientSecret intentionally empty, introspection enabled (default).
	})
	if !errors.Is(err, ErrMissingClientSecret) {
		t.Errorf("expected ErrMissingClientSecret, got %v", err)
	}
}

func TestNew_NoClientSecretRequiredWhenIntrospectionDisabled(t *testing.T) {
	mp := newMockProvider(t)
	_, err := New(context.Background(), Config{
		RealmURL:             mp.issuer(),
		ClientID:             "test-client",
		SkipExpiryCheck:      true,
		DisableIntrospection: true,
		// ClientSecret empty is allowed here.
	})
	if err != nil {
		t.Errorf("expected no error when introspection is disabled, got %v", err)
	}
}

func TestVerify_DisableIntrospectionSkipsCall(t *testing.T) {
	mp := newMockProvider(t)
	v, err := New(context.Background(), Config{
		RealmURL:             mp.issuer(),
		ClientID:             "test-client",
		SkipExpiryCheck:      true,
		DisableIntrospection: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token := mp.makeToken(t, validClaims(mp))
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if mp.introspectCount() != 0 {
		t.Errorf("expected 0 introspect calls when disabled, got %d", mp.introspectCount())
	}
}

func TestVerify_DisableIntrospectionIgnoresRevocation(t *testing.T) {
	mp := newMockProvider(t)
	mp.setActive(false) // provider would report revoked, but we never ask.
	v, err := New(context.Background(), Config{
		RealmURL:             mp.issuer(),
		ClientID:             "test-client",
		SkipExpiryCheck:      true,
		DisableIntrospection: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token := mp.makeToken(t, validClaims(mp))
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("expected valid token when introspection disabled, got %v", err)
	}
	if claims.Sub != "user-123" {
		t.Errorf("Sub = %q, want user-123", claims.Sub)
	}
}
