package oidcauth

import "time"

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

// Config controls how the OIDC verifier connects to the identity provider and
// validates tokens. RealmURL and ClientID are required; everything else has a
// sensible default.
//
// Introspection is enabled by default. When enabled, ClientSecret is required
// and Verify performs a remote RFC 7662 introspection call to detect
// revoked-but-not-yet-expired tokens. Set DisableIntrospection to rely solely
// on local JWT verification (no revocation detection, no provider round-trip).
type Config struct {
	RealmURL             string        // Issuer URL (e.g. https://kc.example.com/realms/main).
	ClientID             string        // OAuth client ID used for audience checks and introspection auth.
	ClientSecret         string        // Confidential client secret used for introspection. Required unless DisableIntrospection is set.
	RequestTimeout       time.Duration // HTTP timeout for provider calls. Defaults to 30s.
	SkipIssuerCheck      bool          // Disable iss claim validation (test-only).
	SkipClientIDCheck    bool          // Disable aud claim validation against ClientID (test-only).
	SkipExpiryCheck      bool          // Disable exp claim validation (test-only).
	DisableIntrospection bool          // Skip remote RFC 7662 introspection in Verify; rely only on local JWT verification.
}
