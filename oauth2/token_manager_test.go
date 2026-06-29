package oauth2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// tokenResponse is the JSON body returned by the mock token endpoint.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// newTokenServer starts an httptest.Server that serves a token endpoint at
// POST /token and calls handler for all other paths.
func newTokenServer(t *testing.T, tok tokenResponse, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tok)
			return
		}
		if handler != nil {
			handler(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func validToken() tokenResponse {
	return tokenResponse{AccessToken: "abc123", TokenType: "Bearer", ExpiresIn: 3600}
}

func TestNewTokenManager_Defaults(t *testing.T) {
	tm := NewTokenManager(nil, "id", "secret", "client_credentials")
	if tm == nil {
		t.Fatal("expected non-nil token manager")
	}
}

func TestTokenManager_SendAsPost_Get(t *testing.T) {
	tm := NewTokenManager(nil, "id", "secret", "client_credentials")
	tm.SendAsPost()
	if !tm.sendAsPost {
		t.Error("expected sendAsPost=true after SendAsPost()")
	}
	tm.SendAsGet()
	if tm.sendAsPost {
		t.Error("expected sendAsPost=false after SendAsGet()")
	}
}

func TestTokenManager_WithAuthenticationURL(t *testing.T) {
	tm := NewTokenManager(nil, "id", "secret", "client_credentials")
	tm.WithAuthenticationURL("https://auth.example.com/token")
	if tm.authUrl != "https://auth.example.com/token" {
		t.Errorf("expected auth URL to be set, got %q", tm.authUrl)
	}
}

func TestTokenManager_WithOptionalParams(t *testing.T) {
	tm := NewTokenManager(nil, "id", "secret", "client_credentials")
	tm.WithOptionalParams(map[string]string{"syndata": "xyz"})
	if tm.authParams["syndata"] != "xyz" {
		t.Errorf("expected syndata='xyz', got %q", tm.authParams["syndata"])
	}
}

func TestTokenManager_GetAccessToken_PostMethod(t *testing.T) {
	srv := newTokenServer(t, validToken(), nil)
	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.SendAsPost()
	tm.WithAuthenticationURL(srv.URL + "/token")

	token, err := tm.GetAccessToken(context.Background(), "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "abc123" {
		t.Errorf("expected token 'abc123', got %q", token)
	}
}

func TestTokenManager_GetAccessToken_GetMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validToken())
	}))
	t.Cleanup(srv.Close)

	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.SendAsGet()
	tm.WithAuthenticationURL(srv.URL)

	token, err := tm.GetAccessToken(context.Background(), "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "abc123" {
		t.Errorf("expected token 'abc123', got %q", token)
	}
}

func TestTokenManager_GetAccessToken_CachesToken(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validToken())
	}))
	t.Cleanup(srv.Close)

	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.WithAuthenticationURL(srv.URL)

	ctx := context.Background()
	_, _ = tm.GetAccessToken(ctx, "scope1")
	_, _ = tm.GetAccessToken(ctx, "scope1")

	if callCount != 1 {
		t.Errorf("expected exactly 1 auth call for cached token, got %d", callCount)
	}
}

func TestTokenManager_GetAccessToken_DifferentScopesAuthenticateSeparately(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validToken())
	}))
	t.Cleanup(srv.Close)

	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.WithAuthenticationURL(srv.URL)

	ctx := context.Background()
	_, _ = tm.GetAccessToken(ctx, "scope-a")
	_, _ = tm.GetAccessToken(ctx, "scope-b")

	if callCount != 2 {
		t.Errorf("expected 2 auth calls for 2 different scopes, got %d", callCount)
	}
}

func TestTokenManager_GetAccessToken_ExpiredTokenReauthenticates(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validToken())
	}))
	t.Cleanup(srv.Close)

	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.WithAuthenticationURL(srv.URL)

	ctx := context.Background()
	_, _ = tm.GetAccessToken(ctx, "scope1")

	// Manually expire the cached token.
	past := time.Now().Add(-2 * time.Hour)
	tm.cache["scope1"].LastAuthentication = &past
	tm.cache["scope1"].ExpiresIn = 1

	_, _ = tm.GetAccessToken(ctx, "scope1")

	if callCount != 2 {
		t.Errorf("expected 2 auth calls after expiry, got %d", callCount)
	}
}

func TestTokenManager_GetAccessToken_EmptyScope(t *testing.T) {
	tm := NewTokenManager(nil, "id", "secret", "client_credentials")
	_, err := tm.GetAccessToken(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty scope")
	}
}

func TestTokenManager_GetAccessToken_AuthServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	t.Cleanup(srv.Close)

	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.WithAuthenticationURL(srv.URL)

	_, err := tm.GetAccessToken(context.Background(), "scope")
	if err == nil {
		t.Error("expected error when auth server returns 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to mention status 401, got %v", err)
	}
}

func TestTokenManager_GetAccessToken_InvalidAuthURL(t *testing.T) {
	tm := NewTokenManager(nil, "id", "secret", "client_credentials")
	tm.WithAuthenticationURL("://bad-url")

	_, err := tm.GetAccessToken(context.Background(), "scope")
	if err == nil {
		t.Error("expected error for invalid auth URL")
	}
}

func TestTokenManager_GetAccessToken_InvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json{{{"))
	}))
	t.Cleanup(srv.Close)

	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.WithAuthenticationURL(srv.URL)

	_, err := tm.GetAccessToken(context.Background(), "scope")
	if err == nil {
		t.Error("expected error for invalid JSON token response")
	}
}

func TestTokenManager_GetTokenType(t *testing.T) {
	srv := newTokenServer(t, validToken(), nil)
	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.WithAuthenticationURL(srv.URL + "/token")

	tt, err := tm.GetTokenType(context.Background(), "scope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tt != "Bearer" {
		t.Errorf("expected token type 'Bearer', got %q", tt)
	}
}

func TestTokenManager_SetAuthorizationHeader(t *testing.T) {
	srv := newTokenServer(t, validToken(), nil)
	tm := NewTokenManager(srv.Client(), "id", "secret", "client_credentials")
	tm.WithAuthenticationURL(srv.URL + "/token")

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := tm.SetAuthorizationHeader(context.Background(), req, "scope"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("expected Authorization to start with 'Bearer ', got %q", auth)
	}
	if !strings.Contains(auth, "abc123") {
		t.Errorf("expected token 'abc123' in Authorization header, got %q", auth)
	}
}

func TestTokenManager_SetAuthorizationHeader_EmptyScope(t *testing.T) {
	tm := NewTokenManager(nil, "id", "secret", "client_credentials")
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := tm.SetAuthorizationHeader(context.Background(), req, ""); err == nil {
		t.Error("expected error for empty scope")
	}
}
