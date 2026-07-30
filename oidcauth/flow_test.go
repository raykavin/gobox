package oidcauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// newMockIssuer serves a minimal OIDC discovery document plus a token
// endpoint that echoes back a canned response, so Flow can be exercised
// end-to-end without a real Keycloak.
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

func newTestFlow(t *testing.T, tokenResponse map[string]any) (*Flow, *httptest.Server) {
	t.Helper()
	srv := newMockIssuer(t, tokenResponse)

	flow, err := NewFlow(context.Background(), FlowConfig{
		IssuerURL:    srv.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURI:  "https://api.example.com/api/v1/auth/callback",
	})
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}
	return flow, srv
}

func TestFlow_AuthCodeURL(t *testing.T) {
	flow, _ := newTestFlow(t, nil)

	verifier := oauth2.GenerateVerifier()
	authURL := flow.AuthCodeURL("state-123", verifier)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authURL: %v", err)
	}
	q := parsed.Query()

	if got := q.Get("client_id"); got != "test-client" {
		t.Errorf("client_id = %q, want %q", got, "test-client")
	}
	if got := q.Get("redirect_uri"); got != "https://api.example.com/api/v1/auth/callback" {
		t.Errorf("redirect_uri = %q, want the configured callback", got)
	}
	if got := q.Get("state"); got != "state-123" {
		t.Errorf("state = %q, want %q", got, "state-123")
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want %q", got, "S256")
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge is empty")
	}
	if !strings.HasPrefix(authURL, "http") {
		t.Errorf("authURL = %q, want an absolute URL", authURL)
	}
}

func TestFlow_Exchange(t *testing.T) {
	flow, _ := newTestFlow(t, map[string]any{
		"access_token":       "access-abc",
		"refresh_token":      "refresh-abc",
		"id_token":           "id-abc",
		"token_type":         "Bearer",
		"expires_in":         300,
		"refresh_expires_in": 1800,
	})

	token, err := flow.Exchange(context.Background(), "auth-code", "verifier-value")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if token.AccessToken != "access-abc" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "access-abc")
	}
	if token.RefreshToken != "refresh-abc" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "refresh-abc")
	}
	if idToken, _ := token.Extra("id_token").(string); idToken != "id-abc" {
		t.Errorf("id_token extra = %q, want %q", idToken, "id-abc")
	}
	if secs, _ := token.Extra("refresh_expires_in").(float64); secs != 1800 {
		t.Errorf("refresh_expires_in extra = %v, want 1800", secs)
	}
	if token.Expiry.IsZero() {
		t.Error("Expiry is zero, want it computed from expires_in")
	}
}

func TestFlow_Refresh(t *testing.T) {
	flow, _ := newTestFlow(t, map[string]any{
		"access_token":  "new-access",
		"refresh_token": "rotated-refresh",
		"expires_in":    300,
	})

	token, err := flow.Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if token.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "new-access")
	}
	if token.RefreshToken != "rotated-refresh" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "rotated-refresh")
	}
}

func TestFlow_EndSessionURL(t *testing.T) {
	flow, srv := newTestFlow(t, nil)

	got := flow.EndSessionURL("id-token-abc", "https://app.example.com/")
	want := srv.URL + "/protocol/openid-connect/logout"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("EndSessionURL = %q, want prefix %q", got, want)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := parsed.Query()
	if q.Get("id_token_hint") != "id-token-abc" {
		t.Errorf("id_token_hint = %q, want %q", q.Get("id_token_hint"), "id-token-abc")
	}
	if q.Get("post_logout_redirect_uri") != "https://app.example.com/" {
		t.Errorf("post_logout_redirect_uri = %q, want %q", q.Get("post_logout_redirect_uri"), "https://app.example.com/")
	}
	if q.Get("client_id") != "test-client" {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), "test-client")
	}
}

func TestFlow_EndSessionURL_EmptyWhenNotAdvertised(t *testing.T) {
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
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	flow, err := NewFlow(context.Background(), FlowConfig{
		IssuerURL: srv.URL,
		ClientID:  "test-client",
	})
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}

	if got := flow.EndSessionURL("id-token", "https://app.example.com/"); got != "" {
		t.Errorf("EndSessionURL = %q, want empty when end_session_endpoint is not advertised", got)
	}
}
