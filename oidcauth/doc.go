// Package oidcauth provides OpenID Connect token verification with optional
// in-memory caching and authorization helpers for roles, scopes, and
// authorized parties.
//
// # Basic usage
//
//	verifier, err := oidcauth.New(ctx, oidcauth.Config{
//	    RealmURL:     "https://keycloak.example.com/realms/main",
//	    ClientID:     "my-app",
//	    ClientSecret: "secret",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	claims, err := verifier.Verify(ctx, bearerToken)
//	if err != nil {
//	    // inspect with errors.Is(err, oidcauth.ErrTokenRevoked), etc.
//	}
//
// # Caching
//
// By default no cache is used and every call to Verify hits the provider.
// Attach a MemoryCache to avoid redundant network round-trips:
//
//	cache := oidcauth.NewMemoryCache(ctx, oidcauth.DefaultCacheDuration)
//	defer cache.Close()
//
//	verifier, err := oidcauth.New(ctx, config, oidcauth.WithCache(cache))
//
// Custom backends (Redis, Memcached, etc.) can be used by implementing the
// Cache interface:
//
//	type Cache interface {
//	    Get(key string, now time.Time) (Claims, bool)
//	    Set(key string, claims Claims, now time.Time)
//	}
//
// # Role-based authorization
//
// HasRole checks whether a token carries a specific Keycloak client role
// (resource_access[clientID].roles):
//
//	if !verifier.HasRole(claims, "admin") {
//	    http.Error(w, "forbidden", http.StatusForbidden)
//	    return
//	}
//
// # Scope-based authorization
//
// HasScope checks whether a token carries a specific OAuth 2.0 scope:
//
//	if !verifier.HasScope(claims, "read:data") {
//	    http.Error(w, "insufficient scope", http.StatusForbidden)
//	    return
//	}
//
// HasAllScopes requires every listed scope to be present:
//
//	if !verifier.HasAllScopes(claims, "read:data", "write:data") {
//	    http.Error(w, "insufficient scope", http.StatusForbidden)
//	    return
//	}
//
// IsAuthorizedParty compares the azp claim against an expected client ID,
// useful in multi-service architectures where a gateway forwards tokens:
//
//	if !verifier.IsAuthorizedParty(claims, "api-gateway") {
//	    http.Error(w, "unauthorized party", http.StatusForbidden)
//	    return
//	}
//
// # Machine-to-machine (client credentials)
//
// Access tokens obtained via the OAuth 2.0 client credentials grant often
// carry an aud value that does not match the resource server's ClientID,
// causing the default audience check to fail. Set SkipClientIDCheck: true
// to bypass that check and validate the caller identity manually instead:
//
//	verifier, err := oidcauth.New(ctx, oidcauth.Config{
//	    RealmURL:          "https://keycloak.example.com/realms/main",
//	    ClientID:          "resource-server",
//	    ClientSecret:      "secret",
//	    SkipClientIDCheck: true, // M2M tokens may not carry this client's ID in aud
//	})
//
//	claims, err := verifier.Verify(ctx, token)
//	if err != nil { /* ... */ }
//
//	if !verifier.IsAuthorizedParty(claims, "allowed-service") {
//	    http.Error(w, "unauthorized party", http.StatusForbidden)
//	    return
//	}
//	if !verifier.HasAllScopes(claims, "read:data") {
//	    http.Error(w, "insufficient scope", http.StatusForbidden)
//	    return
//	}
//
// # Introspection and its cache
//
// By default Verify performs a remote RFC 7662 introspection call (via an
// internal Introspector) to catch revoked-but-not-yet-expired tokens. The
// introspection endpoint is resolved from the provider's discovery document
// (the standard "introspection_endpoint" field) unless
// Config.IntrospectionEndpoint overrides it. When introspection is enabled,
// ClientSecret is required.
//
// Set Config.IntrospectionCacheTTL to cache positive (active=true) results,
// avoiding a network round trip on every Verify call for the same token.
// The cache is keyed by the SHA-256 digest of the token (never the token
// itself) and an entry never survives past the token's own expiry,
// regardless of the configured TTL see Introspector's doc comment for the
// full rule. Concurrent Introspect calls for the same token are coalesced
// into a single request.
//
//	verifier, err := oidcauth.New(ctx, oidcauth.Config{
//	    RealmURL:                 "https://keycloak.example.com/realms/main",
//	    ClientID:                 "my-app",
//	    ClientSecret:             "secret",
//	    IntrospectionCacheTTL:    5 * time.Minute, // 0 disables caching
//	    IntrospectionHTTPTimeout: 10 * time.Second,
//	})
//	defer verifier.Close() // stops the cache's background eviction goroutine
//
// Call Close when the verifier is no longer needed (e.g. at server
// shutdown) to stop the cache's background eviction goroutine, started only
// when IntrospectionCacheTTL > 0.
//
// # Distinguishing failure reasons
//
//	claims, err := verifier.Verify(ctx, accessToken)
//	switch {
//	case errors.Is(err, oidcauth.ErrAccessTokenInactive):
//	    // issuer reports the token inactive: revoked, expired, or unknown.
//	    // A BFF should invalidate its local session and require re-login.
//	case errors.Is(err, oidcauth.ErrInvalidIntrospectionResponse):
//	    // the issuer's response could not be parsed.
//	case errors.Is(err, oidcauth.ErrIntrospectionFailed):
//	    // network error, unexpected HTTP status, or timeout reaching the issuer.
//	case errors.Is(err, oidcauth.ErrTokenValidationFailed):
//	    // local JWT check failed (bad signature, issuer, audience, or expiry).
//	}
//
// # Disabling introspection
//
// To rely solely on local JWT verification and skip the introspection
// round-trip entirely (no Introspector is even built), set
// DisableIntrospection: true. An opaque access token can then never be
// verified, since nothing is left to check it: local verification has
// nothing to parse, and introspection never runs.
//
//	verifier, err := oidcauth.New(ctx, oidcauth.Config{
//	    RealmURL:             "https://keycloak.example.com/realms/main",
//	    ClientID:             "my-app",
//	    DisableIntrospection: true, // no ClientSecret needed; no revocation detection
//	})
//
// # Server-side sessions (BFF pattern)
//
// For a browser-facing backend-for-frontend, exchanged tokens should never
// reach the browser: SessionManager keeps them server-side (via a
// SessionStore the consuming application implements against its own
// database and repository conventions this package only defines the
// interface, deliberately staying agnostic of any specific ORM or SQL
// driver) and hands out only an opaque session ID, refreshing the access
// token transparently as needed:
//
//	sessions := oidcauth.NewSessionManager(oidcauth.SessionManagerConfig{
//	    Store:           myapp.NewOIDCSessionRepository(db, encryptor), // implements oidcauth.SessionStore
//	    Flow:            flow,
//	    ClientID:        "my-app",
//	    IdleTimeout:     30 * time.Minute, // 0 disables idle expiry
//	    AbsoluteTimeout: 12 * time.Hour,   // 0 = only the IdP's own ceiling applies
//	})
//
//	session, err := sessions.Create(ctx, token, claims) // at login callback
//	claims, err := sessions.Verify(ctx, sessionID)      // per request; refreshes if needed
//
// A SessionStore implementation is expected to encrypt Session.Tokens at
// rest (see the secure package for a ready-made AES-256-GCM Encryptor) and
// to implement WithLock with real locking (e.g. SELECT ... FOR UPDATE
// inside a transaction), so SessionManager's refresh-on-demand never races
// two concurrent requests into refreshing the same session twice.
//
// SessionManager satisfies the same Verify/HasRole shape OIDC does, so it
// can be passed directly to middlewares.Authorization / RequireRole in place
// of an *OIDC verifying raw JWTs see httpserver/middlewares.Auth, which
// implements the full login/callback/refresh/logout flow around it.
//
// Expired sessions are removed both reactively (on their next use, inside
// Resolve) and periodically; see RunSessionCleanup for a ready-made ticker
// loop suitable for running in every instance of a horizontally-scaled
// deployment.
//
// # Error handling
//
// All errors wrap one of the package-level sentinels and can be inspected
// with errors.Is:
//
//	switch {
//	case errors.Is(err, oidcauth.ErrTokenRevoked):
//	    // token was revoked server-side
//	case errors.Is(err, oidcauth.ErrTokenValidationFailed):
//	    // signature / expiry / audience check failed
//	case errors.Is(err, oidcauth.ErrIntrospectionFailed):
//	    // could not reach the introspection endpoint
//	case errors.Is(err, oidcauth.ErrMissingClientSecret):
//	    // ClientSecret not set and introspection is enabled
//	}
package oidcauth
