package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Sentinel errors returned by the OIDC verifier. See also introspection.go
// for the introspection-specific sentinels (ErrAccessTokenInactive,
// ErrInvalidIntrospectionResponse, ErrInvalidOIDCConfiguration) and
// ErrTokenRevoked's deprecation note.
var (
	ErrInvalidRealmURL       = errors.New("invalid OIDC configuration URL")
	ErrEmptyClientID         = errors.New("client ID cannot be empty")
	ErrMissingClientSecret   = errors.New("client secret is required when introspection is enabled")
	ErrTokenValidationFailed = errors.New("token validation failed")
	ErrProviderInitFailed    = errors.New("failed to initialize OIDC provider")
	ErrIntrospectionFailed   = errors.New("token introspection failed")
)

const defaultRequestTimeout = 30 * time.Second

// OIDC verifies tokens issued by an OpenID Connect provider and exposes
// helpers for role- and scope-based authorization. A single OIDC value is
// safe for concurrent use.
type OIDC struct {
	config       Config
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	httpClient   *http.Client
	cache        Cache
	introspector *Introspector
}

// Option configures an OIDC verifier.
type Option func(*OIDC)

// WithCache attaches a Cache to the verifier. Verify returns cached claims on
// hit and stores validated claims on miss. This caches the full Verify()
// result (local JWT check plus introspection); it is independent of and
// composes with the more granular, introspection-only cache Verify's
// internal Introspector keeps when Config.IntrospectionCacheTTL is set.
// Cache lifecycle (e.g. Close) is managed by the caller.
func WithCache(c Cache) Option {
	return func(o *OIDC) {
		o.cache = c
	}
}

// New builds an OIDC verifier and discovers the provider's metadata. The
// supplied context bounds only the discovery call; use WithCache to control
// caching behaviour.
//
// Unless Config.DisableIntrospection is set, New also builds an internal
// Introspector for the RFC 7662 introspection Verify performs on every
// access token: it resolves the introspection endpoint from the provider's
// discovery document (falling back to Config.IntrospectionEndpoint see
// resolveIntrospectionEndpoint) and applies Config.IntrospectionCacheTTL /
// IntrospectionHTTPTimeout. Call Close when the OIDC verifier is no longer
// needed to stop the Introspector's background cache-eviction goroutine.
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

	if !config.DisableIntrospection {
		endpoint, err := resolveIntrospectionEndpoint(provider, config)
		if err != nil {
			return nil, err
		}
		introspector, err := NewIntrospector(IntrospectorConfig{
			Endpoint:     endpoint,
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			HTTPTimeout:  config.IntrospectionHTTPTimeout,
			CacheTTL:     config.IntrospectionCacheTTL,
		})
		if err != nil {
			return nil, err
		}
		o.introspector = introspector
	}

	for _, opt := range opts {
		opt(o)
	}

	return o, nil
}

// Close stops the internal Introspector's background cache-eviction
// goroutine, if introspection is enabled and Config.IntrospectionCacheTTL is
// set. Idempotent; safe to call even when introspection is disabled or
// caching was never enabled.
func (o *OIDC) Close() {
	if o.introspector != nil {
		o.introspector.Close()
	}
}

// Verify validates an access token and returns its claims.
//
// # Access token vs. ID token
//
// Verify validates an access token, not an ID token an important
// distinction Keycloak (and OIDC generally) does not erase just because
// both may be JWTs signed by the same provider:
//
//   - A JWT-shaped access token (three dot-separated segments Keycloak's
//     default) is checked locally using the same signature/issuer/expiry
//     machinery go-oidc exposes for ID tokens, since structurally that
//     machinery only verifies "this JWT was validly signed by this
//     provider's keys and its iss/exp/aud claims check out" it makes no
//     ID-token-specific assertion (no nonce, no at_hash). This local check
//     is a real signature verification, not a rubber stamp.
//   - An opaque access token (no JWT structure at all) cannot be checked
//     locally by definition, so this step is skipped entirely for it rather
//     than failing closed on a token that was never going to parse as a
//     JWT in the first place.
//
// Either way, the local check (when applicable) and introspection answer
// different questions: local verification proves the token's signature,
// issuer, and audience are genuine and it has not expired *by its own
// claims*; introspection proves the issuer still considers it active *right
// now* (catching server-side revocation a stale local check cannot see).
// Skipping the local check for an opaque token does not skip that second,
// authoritative question introspection alone answers it.
//
// # Flow
//
//  1. cache lookup, return immediately on hit (if a Cache was attached via
//     WithCache);
//  2. local verification, only if the token looks JWT-shaped;
//  3. remote introspection (RFC 7662, via the internal Introspector see
//     its doc comment for its own caching/concurrency behavior), unless
//     disabled via Config.DisableIntrospection;
//  4. cache the claims (if a Cache was attached).
//
// If introspection is disabled and the token is opaque, Verify cannot
// establish anything about it and returns ErrTokenValidationFailed: with no
// local structure to check and no remote authority consulted, there is
// nothing left that could have verified it.
//
// A local-verification failure, or an introspection failure other than an
// inactive token, returns ErrTokenValidationFailed wrapping the underlying
// cause (inspect it with errors.Is/errors.As e.g.
// errors.Is(err, ErrIntrospectionFailed) or
// errors.Is(err, ErrInvalidIntrospectionResponse)). An inactive token
// returns ErrAccessTokenInactive (also matched by the deprecated
// ErrTokenRevoked) directly, not wrapped under ErrTokenValidationFailed.
func (o *OIDC) Verify(ctx context.Context, token string) (Claims, error) {
	now := time.Now()

	var cacheKey string
	if o.cache != nil {
		cacheKey = hashToken(token)
		if claims, ok := o.cache.Get(cacheKey, now); ok {
			return claims, nil
		}
	}

	var claims Claims
	var knownExpiry time.Time
	var haveLocalClaims bool

	if looksLikeJWT(token) {
		idToken, err := o.verifier.Verify(ctx, token)
		if err != nil {
			return Claims{}, fmt.Errorf("%w: %w", ErrTokenValidationFailed, err)
		}
		if err := idToken.Claims(&claims); err != nil {
			return Claims{}, fmt.Errorf("%w: %w", ErrTokenValidationFailed, err)
		}
		haveLocalClaims = true
		knownExpiry = idToken.Expiry
	}

	if o.config.DisableIntrospection {
		if !haveLocalClaims {
			return Claims{}, fmt.Errorf(
				"%w: token is opaque and introspection is disabled, so it cannot be verified",
				ErrTokenValidationFailed,
			)
		}
	} else {
		introspected, err := o.introspector.Introspect(ctx, token, knownExpiry)
		if err != nil {
			if errors.Is(err, ErrAccessTokenInactive) {
				return Claims{}, err
			}
			return Claims{}, fmt.Errorf("%w: %w", ErrTokenValidationFailed, err)
		}
		if !haveLocalClaims {
			// Opaque token: introspection is the only source of claims.
			claims = introspected
		}
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
	return slices.Contains(strings.Fields(claims.Scope), scope)
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
