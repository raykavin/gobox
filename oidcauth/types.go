package oidcauth

import (
	"context"
	"time"
)

// Claims represents the structure of the
// claims extracted from an authentication token.
type Claims struct {
	Aud               []string                       `json:"aud"`
	AllowedOrigins    []string                       `json:"allowed-origins"`
	Jti               string                         `json:"jti"`
	Iss               string                         `json:"iss"`
	Sub               string                         `json:"sub"`
	Typ               string                         `json:"typ"`
	Azp               string                         `json:"azp"`
	Sid               string                         `json:"sid"`
	Acr               string                         `json:"acr"`
	Scope             string                         `json:"scope"`
	Name              string                         `json:"name"`
	PreferredUsername string                         `json:"preferred_username"`
	GivenName         string                         `json:"given_name"`
	FamilyName        string                         `json:"family_name"`
	Email             string                         `json:"email"`
	Exp               float64                        `json:"exp"`
	Iat               float64                        `json:"iat"`
	AuthTime          int                            `json:"auth_time"`
	RealmAccess       map[string][]string            `json:"realm_access"`
	ResourceAccess    map[string]map[string][]string `json:"resource_access"`
	EmailVerified     bool                           `json:"email_verified"`
}

// Introspection represents the result of token introspection, including the
// claims and the active status of the token.
type Introspection struct {
	Claims
	Active bool `json:"active"`
}

// ClaimsVerifier turns a freshly exchanged token into Claims. *OIDC already
// satisfies this with no wrapper; it exists so middlewares.Auth can extract
// claims once at login/callback time without depending on OIDC's full
// surface (introspection, HasRole, HasScope, ...).
type ClaimsVerifier interface {
	Verify(ctx context.Context, token string) (Claims, error)
}

// Config controls how the OIDC verifier connects to the identity provider and
// validates tokens. RealmURL and ClientID are required; everything else has a
// sensible default.
//
// Introspection is enabled by default. When enabled, ClientSecret is required
// and Verify performs a remote RFC 7662 introspection call to detect
// revoked-but-not-yet-expired tokens. Set DisableIntrospection to rely solely
// on local JWT verification (no revocation detection, no provider round-trip).
//
// Verify's access token handling depends on the token's shape, not on a
// config flag: a JWT-shaped access token (three dot-separated segments
// Keycloak's default) is checked locally (signature, issuer, expiry) using
// the same machinery as ID token verification, then optionally introspected;
// an opaque access token skips local verification entirely (it has no JWT
// structure to check) and relies solely on introspection. See Verify's doc
// comment for the full rationale.
type Config struct {
	RealmURL                 string        // Issuer URL (e.g. https://kc.example.com/realms/main).
	ClientID                 string        // OAuth client ID used for audience checks and introspection auth.
	ClientSecret             string        // Confidential client secret used for introspection. Required unless DisableIntrospection is set.
	RequestTimeout           time.Duration // HTTP timeout for provider calls (discovery, JWKS). Defaults to 30s.
	SkipIssuerCheck          bool          // Disable iss claim validation (test-only).
	SkipClientIDCheck        bool          // Disable aud claim validation against ClientID (test-only).
	SkipExpiryCheck          bool          // Disable exp claim validation (test-only).
	DisableIntrospection     bool          // Skip remote RFC 7662 introspection in Verify; rely only on local JWT verification (opaque tokens then can never be verified).
	IntrospectionEndpoint    string        // Overrides the RFC 7662 introspection endpoint. Empty (the default) uses the provider's discovery "introspection_endpoint", falling back to {RealmURL}/protocol/openid-connect/token/introspect only for issuers/test fixtures that predate that field.
	IntrospectionCacheTTL    time.Duration // Maximum window a positive (active=true) introspection result may be reused without a new round trip, i.e. how long a server-side revocation can go unnoticed; see Introspector for the full TTL calculation, which never lets an entry outlive the token's own expiry. Zero (the default) disables caching; negative values are rejected by New with ErrInvalidOIDCConfiguration.
	IntrospectionHTTPTimeout time.Duration // HTTP timeout for each introspection request, independent of RequestTimeout (which only governs discovery/JWKS). Defaults to 10s when zero; negative values are rejected by New with ErrInvalidOIDCConfiguration.
}