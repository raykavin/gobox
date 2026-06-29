package oidcauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Sentinel errors returned by the OIDC verifier.
var (
	ErrInvalidRealmURL       = errors.New("invalid OIDC configuration URL")
	ErrEmptyClientID         = errors.New("client ID cannot be empty")
	ErrTokenValidationFailed = errors.New("token validation failed")
	ErrProviderInitFailed    = errors.New("failed to initialize OIDC provider")
	ErrIntrospectionFailed   = errors.New("token introspection failed")
	ErrTokenRevoked          = errors.New("access token has been revoked")
)

const (
	defaultRequestTimeout = 30 * time.Second
	introspectEndpoint    = "/protocol/openid-connect/token/introspect"
)

// OIDC verifies tokens issued by an OpenID Connect provider and exposes
// helpers for role-based authorization. A single OIDC value is safe for
// concurrent use.
type OIDC struct {
	config     Config
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	httpClient *http.Client
	cache      Cache
}

// Option configures an OIDC verifier.
type Option func(*OIDC)

// WithCache attaches a Cache to the verifier. Verify returns cached claims on
// hit and stores validated claims on miss. Cache lifecycle (e.g. Close) is
// managed by the caller.
func WithCache(c Cache) Option {
	return func(o *OIDC) {
		o.cache = c
	}
}

// New builds an OIDC verifier and discovers the provider's metadata. The
// supplied context bounds only the discovery call; use WithCache to control
// caching behaviour.
func New(ctx context.Context, config Config, opts ...Option) (*OIDC, error) {
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: config.RequestTimeout}

	// ClientContext attaches our HTTP client so go-oidc honors the timeout
	// during discovery and JWKS fetches.
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), config.RealmURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrProviderInitFailed, err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID:          config.ClientID,
		SkipClientIDCheck: config.SkipClientIDCheck,
		SkipExpiryCheck:   config.SkipExpiryCheck,
		SkipIssuerCheck:   config.SkipIssuerCheck,
	})

	o := &OIDC{
		config:     config,
		provider:   provider,
		verifier:   verifier,
		httpClient: httpClient,
	}

	for _, opt := range opts {
		opt(o)
	}

	return o, nil
}

// Verify validates a bearer token and returns its claims.
//
// The flow is:
//  1. cache lookup, return immediately on hit (if a Cache was attached);
//  2. local JWT verification (signature, iss, aud, exp);
//  3. remote introspection to catch revoked-but-not-yet-expired tokens;
//  4. cache the claims (if a Cache was attached).
//
// Any failure in steps 2 or 3 returns ErrTokenValidationFailed; an inactive
// token returns ErrTokenRevoked. The original error is wrapped so callers
// using errors.Unwrap can still inspect it.
func (o *OIDC) Verify(ctx context.Context, token string) (Claims, error) {
	now := time.Now()

	var cacheKey string
	if o.cache != nil {
		cacheKey = hashToken(token)
		if claims, ok := o.cache.Get(cacheKey, now); ok {
			return claims, nil
		}
	}

	idToken, err := o.verifier.Verify(ctx, token)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrTokenValidationFailed, err)
	}

	var claims Claims
	if err := idToken.Claims(&claims); err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrTokenValidationFailed, err)
	}

	result, err := o.introspect(ctx, token)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrTokenValidationFailed, err)
	}
	if !result.Active {
		return Claims{}, ErrTokenRevoked
	}

	if o.cache != nil {
		o.cache.Set(cacheKey, claims, now)
	}
	return claims, nil
}

// HasRole reports whether the given claims include the named role for this
// verifier's client (i.e. claims.ResourceAccess[ClientID].roles).
func (o *OIDC) HasRole(claims Claims, role string) bool {
	clientRoles, ok := claims.ResourceAccess[o.config.ClientID]
	if !ok {
		return false
	}
	return slices.Contains(clientRoles["roles"], role)
}

// HasScope reports whether the given claims include the named scope.
// Scopes in claims.Scope are space-separated per RFC 6749.
func (o *OIDC) HasScope(claims Claims, scope string) bool {
	for _, s := range strings.Fields(claims.Scope) {
		if s == scope {
			return true
		}
	}
	return false
}

// HasAllScopes reports whether the given claims include all of the named
// scopes. Returns true if scopes is empty (vacuous truth).
func (o *OIDC) HasAllScopes(claims Claims, scopes ...string) bool {
	for _, scope := range scopes {
		if !o.HasScope(claims, scope) {
			return false
		}
	}
	return true
}

// IsAuthorizedParty reports whether the claims' azp (authorized party) field
// matches the provided value.
func (o *OIDC) IsAuthorizedParty(claims Claims, azp string) bool {
	return claims.Azp == azp
}

// introspect calls the provider's RFC 7662 introspection endpoint and returns
// the full Introspection result. Callers should check result.Active to
// determine whether the token is still valid server-side.
func (o *OIDC) introspect(ctx context.Context, token string) (Introspection, error) {
	introspectURL, err := url.JoinPath(o.config.RealmURL, introspectEndpoint)
	if err != nil {
		return Introspection{}, fmt.Errorf("%w: building URL: %w", ErrIntrospectionFailed, err)
	}

	form := url.Values{}
	form.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, introspectURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Introspection{}, fmt.Errorf("%w: building request: %w", ErrIntrospectionFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(o.config.ClientID, o.config.ClientSecret)

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return Introspection{}, fmt.Errorf("%w: %w", ErrIntrospectionFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Introspection{}, fmt.Errorf("%w: unexpected status %d", ErrIntrospectionFailed, resp.StatusCode)
	}

	var out Introspection
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Introspection{}, fmt.Errorf("%w: decoding response: %w", ErrIntrospectionFailed, err)
	}

	return out, nil
}
