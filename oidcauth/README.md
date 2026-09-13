# oidcauth

OpenID Connect token verification for Go, with optional in-memory caching and authorization helpers. Designed for Keycloak but compatible with any OIDC-compliant provider.

## Features

- JWT verification (signature, issuer, audience, expiry) via [`go-oidc`](https://github.com/coreos/go-oidc)
- RFC 7662 token introspection to detect server-side revocation, with request coalescing and an optional positive-result cache
- Pluggable claims cache via the `Cache` interface: ships with `MemoryCache`, or bring your own (Redis, Memcached, etc.)
- Authorization helpers: `HasRole`, `HasScope`, `HasAllScopes`, `IsAuthorizedParty`
- Machine-to-machine (client credentials) support via `SkipClientIDCheck`
- `Flow` for the Authorization Code + PKCE exchange, refresh, and end-session URL
- `SessionManager` for server-side sessions that keep OIDC tokens out of the browser
- Safe for concurrent use

The package has three layers that can be used independently:

| Layer | Type | Use it when |
|---|---|---|
| Token verification | `OIDC` | You receive a bearer token and need to validate it and read claims |
| Login flow | `Flow` | Your server drives the browser through Authorization Code + PKCE |
| Sessions | `SessionManager` | You want an opaque session cookie instead of tokens in the browser |

The `httpserver/middlewares` package wires all three together; see its [README](../httpserver/middlewares/README.md) for a ready-made set of handlers.

## Installation

```sh
go get github.com/raykavin/gobox/oidcauth
```

## Usage

### Basic

Every call to `Verify` performs a full JWT check plus a remote introspection request.
`ClientSecret` is required when introspection is enabled (the default).

```go
verifier, err := oidcauth.New(ctx, oidcauth.Config{
    RealmURL:     "https://keycloak.example.com/realms/main",
    ClientID:     "my-app",
    ClientSecret: "secret",
})
if err != nil {
    log.Fatal(err)
}

claims, err := verifier.Verify(ctx, bearerToken)
if err != nil {
    log.Fatal(err)
}

fmt.Println(claims.PreferredUsername)
```

### Without introspection

Set `DisableIntrospection: true` to skip the remote RFC 7662 call and rely solely on local JWT verification. In this mode `ClientSecret` is not required, but revoked tokens will not be detected until they expire.

```go
verifier, err := oidcauth.New(ctx, oidcauth.Config{
    RealmURL:             "https://keycloak.example.com/realms/main",
    ClientID:             "my-app",
    DisableIntrospection: true,
})
```

### With MemoryCache

Cache verified claims to avoid a network round-trip on every request.

```go
cache := oidcauth.NewMemoryCache(ctx, oidcauth.DefaultCacheDuration) // 5m TTL
defer cache.Close()

verifier, err := oidcauth.New(ctx, config, oidcauth.WithCache(cache))
```

The entry TTL is `min(token.exp, now + duration)` the cache never serves a token past its own expiry.

### With a custom cache backend

Implement the `Cache` interface to use any external store.

```go
type Cache interface {
    Get(key string, now time.Time) (Claims, bool)
    Set(key string, claims Claims, now time.Time)
}

verifier, err := oidcauth.New(ctx, config, oidcauth.WithCache(myRedisCache))
```

### Role-based authorization

`HasRole` checks for a Keycloak client role inside `resource_access[clientID].roles`.

```go
if !verifier.HasRole(claims, "admin") {
    http.Error(w, "forbidden", http.StatusForbidden)
    return
}
```

### Scope-based authorization

`HasScope` checks for a single OAuth 2.0 scope in `claims.Scope` (space-separated, per RFC 6749).

```go
if !verifier.HasScope(claims, "read:data") {
    http.Error(w, "insufficient scope", http.StatusForbidden)
    return
}
```

`HasAllScopes` requires every listed scope to be present.

```go
if !verifier.HasAllScopes(claims, "read:data", "write:data") {
    http.Error(w, "insufficient scope", http.StatusForbidden)
    return
}
```

### Authorized party

`IsAuthorizedParty` compares `claims.Azp` against an expected client ID. Useful when a gateway or another service forwards tokens downstream.

```go
if !verifier.IsAuthorizedParty(claims, "api-gateway") {
    http.Error(w, "unauthorized party", http.StatusForbidden)
    return
}
```

### Machine-to-machine (client credentials)

Access tokens obtained via the OAuth 2.0 client credentials grant often carry an `aud` value that does not match the resource server's `ClientID`, causing the default audience check to fail. Set `SkipClientIDCheck: true` to bypass it and validate the caller identity manually with the authorization helpers.

```go
verifier, err := oidcauth.New(ctx, oidcauth.Config{
    RealmURL:          "https://keycloak.example.com/realms/main",
    ClientID:          "resource-server",
    ClientSecret:      "secret",
    SkipClientIDCheck: true, // M2M tokens may not carry this client's ID in aud
})

claims, err := verifier.Verify(ctx, token)
if err != nil {
    // handle error
}

if !verifier.IsAuthorizedParty(claims, "allowed-service") {
    http.Error(w, "unauthorized party", http.StatusForbidden)
    return
}
if !verifier.HasAllScopes(claims, "read:data") {
    http.Error(w, "insufficient scope", http.StatusForbidden)
    return
}
```

### Error handling

All errors wrap a package-level sentinel and can be inspected with `errors.Is`.

```go
claims, err := verifier.Verify(ctx, token)
switch {
case errors.Is(err, oidcauth.ErrTokenRevoked):
    // token was revoked server-side
case errors.Is(err, oidcauth.ErrTokenValidationFailed):
    // signature / expiry / audience check failed
case errors.Is(err, oidcauth.ErrIntrospectionFailed):
    // could not reach the introspection endpoint
case err != nil:
    // unexpected error
}
```

| Sentinel | Cause |
|---|---|
| `ErrInvalidRealmURL` | `RealmURL` is empty or not a valid HTTP(S) URL |
| `ErrEmptyClientID` | `ClientID` is empty |
| `ErrMissingClientSecret` | `ClientSecret` not set and introspection is enabled |
| `ErrProviderInitFailed` | OIDC discovery request failed |
| `ErrTokenValidationFailed` | JWT signature, issuer, audience, or expiry check failed |
| `ErrIntrospectionFailed` | Introspection endpoint unreachable or returned an unexpected status |
| `ErrAccessTokenInactive` | The issuer reports the token as inactive: revoked, expired, never existed, or wrong client. RFC 7662 collapses all of these into one `"active": false`, so this error does not distinguish them either |
| `ErrTokenRevoked` | Deprecated alias of `ErrAccessTokenInactive`, the same error value under both names |
| `ErrInvalidIntrospectionResponse` | The introspection response could not be parsed; distinct from `ErrIntrospectionFailed`, which is transport level |
| `ErrInvalidOIDCConfiguration` | A configuration value cannot be honored, such as a negative TTL or timeout, or a missing introspection endpoint |
| `ErrSessionNotFound` | A session could not be resolved by `SessionStore` or `SessionManager` |

## Configuration

```go
oidcauth.Config{
    RealmURL:     "https://keycloak.example.com/realms/main", // required
    ClientID:     "my-app",                                   // required
    ClientSecret: "secret",                                   // required unless DisableIntrospection is set
    RequestTimeout: 10 * time.Second,                         // default: 30s

    // DisableIntrospection skips the remote RFC 7662 call in Verify.
    // Revoked tokens will not be detected until their exp claim elapses.
    // ClientSecret is not required when this is true.
    DisableIntrospection: false,

    // SkipClientIDCheck disables audience validation against ClientID.
    // Use for M2M / client-credentials flows where aud does not match.
    SkipClientIDCheck: false,

    // Test-only: do not enable in production.
    SkipIssuerCheck: false,
    SkipExpiryCheck: false,

    // Overrides the introspection endpoint. Empty uses the provider's
    // discovery "introspection_endpoint", falling back to
    // {RealmURL}/protocol/openid-connect/token/introspect only for
    // issuers that predate that field.
    IntrospectionEndpoint: "",

    // How long a positive introspection result may be reused, i.e. the
    // window in which a revocation can go unnoticed. Zero disables the
    // cache; negative is rejected with ErrInvalidOIDCConfiguration.
    IntrospectionCacheTTL: 0,

    // Per-introspection-request timeout, independent of RequestTimeout
    // (which governs only discovery and JWKS). Zero defaults to 10s.
    IntrospectionHTTPTimeout: 0,
}
```

`Verify`'s handling of an access token depends on the token's shape, not on a config flag. A JWT-shaped access token (three dot-separated segments, Keycloak's default) is verified locally for signature, issuer, and expiry, then optionally introspected. An opaque access token has no structure to check, so it skips local verification and relies solely on introspection: with `DisableIntrospection` set, an opaque token can never be verified at all.

## Authorization helpers

| Method | Checks |
|---|---|
| `HasRole(claims, role)` | `resource_access[clientID].roles` contains `role` |
| `HasScope(claims, scope)` | `claims.Scope` contains `scope` (exact word match) |
| `HasAllScopes(claims, scopes...)` | every scope in the list is present |
| `IsAuthorizedParty(claims, azp)` | `claims.Azp == azp` |

## Login flow

`Flow` performs the Authorization Code + PKCE exchange on the browser's behalf.

```go
flow, err := oidcauth.NewFlow(ctx, oidcauth.FlowConfig{
    IssuerURL:    "https://keycloak.example.com/realms/main",
    ClientID:     "my-app",
    ClientSecret: "secret",
    RedirectURI:  "https://api.example.com/auth/callback",
    Scopes:       []string{"openid", "profile", "email"}, // empty uses the package defaults
})
```

| Method | Purpose |
|---|---|
| `AuthCodeURL(state, verifier string) string` | The issuer URL to redirect the browser to |
| `Exchange(ctx, code, verifier string) (*oauth2.Token, error)` | Trades the returned code for tokens |
| `Refresh(ctx, refreshToken string) (*oauth2.Token, error)` | Runs the refresh_token grant |
| `EndSessionURL(idToken, postLogoutRedirectURI string) string` | The issuer's federated logout URL |

## Server-side sessions

`SessionManager` keeps OIDC credentials on the server and hands the browser nothing but an opaque session ID. It is the single place that decides what counts as expired and when to refresh, so the login handler, the authorization middleware, an explicit refresh endpoint, logout, and the cleanup job all apply the same rule.

```go
sessions := oidcauth.NewSessionManager(oidcauth.SessionManagerConfig{
    Store:           myStore,    // your SessionStore implementation
    Flow:            flow,       // required, performs the refresh grant
    Verifier:        verifier,   // *OIDC; strongly recommended in production
    ClientID:        "my-app",
    IdleTimeout:     30 * time.Minute,
    AbsoluteTimeout: 12 * time.Hour,
})
```

| Method | Purpose |
|---|---|
| `Create(ctx, token, claims) (*Session, error)` | Persists a new session after a successful login |
| `Resolve(ctx, id) (*Session, error)` | Loads a session, refreshing its tokens on demand |
| `Verify(ctx, sessionID) (Claims, error)` | `Resolve` reduced to its claims |
| `Peek(ctx, id) (*Session, error)` | Reads a session without enforcing expiry, refreshing, or touching `LastSeenAt` |
| `Delete(ctx, id) error` | Ends a session; idempotent |
| `CleanupExpired(ctx, now) (int64, error)` | Removes expired sessions and reports how many |
| `HasRole(claims, role) bool` | Evaluates a role against `resource_access[ClientID].roles` |

Without a `Verifier`, a user disabled or revoked in the identity provider keeps a live session until the access token naturally expires and a refresh is attempted. Setting it revalidates against the issuer on every `Resolve`.

`Verify` and `HasRole` exist so that a `*SessionManager` satisfies the same `TokenVerifier` interface an `*OIDC` does. It can therefore be handed straight to `middlewares.Authorization` and `middlewares.RequireRole` in place of a raw-JWT verifier, with the session ID taking the place of the token.

`Peek` deliberately skips expiry enforcement and refresh. It exists for logout, which needs the stored ID token to build the federated end-session URL even when the session is already expired, and must not fail loudly just because it is stale.

### Implementing SessionStore

`SessionStore` is deliberately left to the application, so this package stays agnostic of any ORM or SQL driver. It has six methods:

| Method | Contract |
|---|---|
| `Create(ctx, s *Session) error` | Persists a new session. `s.ID` is already a random UUID v4 generated by `SessionManager` and can be assumed unique |
| `Get(ctx, id) (*Session, error)` | Read-only lookup, no row lock. Must return an error wrapping `ErrSessionNotFound` when `id` does not exist |
| `Touch(ctx, id, lastSeenAt) error` | Updates `LastSeenAt` alone, without a lock, so recording activity on the common path costs one single-column update |
| `WithLock(ctx, id, fn) error` | Loads the session inside an atomic unit of work, calls `fn`, and persists every field of the session `fn` returns before releasing the lock. `fn` returning `(nil, nil)` leaves the session untouched; `fn` returning an error persists nothing and propagates that error as-is |
| `Delete(ctx, id) error` | Ends a session. Deleting one that does not exist is **not** an error: `Delete` must be idempotent |
| `DeleteExpired(ctx, now) (int64, error)` | Removes every session whose `ExpiresAt` is at or before `now`, reporting the count. Safe to run concurrently from several instances |

`WithLock` is the contract that matters most: the atomic unit of work (a `SELECT ... FOR UPDATE` in a transaction, say) is what guarantees at most one concurrent refresh per session. A second request arriving mid-refresh blocks until the first commits, then sees the refreshed state. Implementing it without real locking silently reintroduces the double-refresh race.

`Session.Tokens` must be encrypted at rest, and implementations must never log its contents. The [`secure`](../secure/README.md) package provides a ready-made AES-256-GCM `Encryptor` for exactly this.

### Session and TokenSet

These are the fields a `SessionStore` has to round-trip intact.

| `Session` field | Meaning |
|---|---|
| `ID` | Random UUID v4, generated by `Create`. Carries no structure of its own; this is the only part the browser ever sees |
| `Tokens` | The `TokenSet` below. Encrypted at rest |
| `Claims` | Only the identity claims the application reads, not a verbatim copy of the issuer's token |
| `ExpiresAt` | The session's absolute, non-extendable ceiling |
| `CreatedAt` / `UpdatedAt` | Record timestamps |
| `LastSeenAt` | Last time the session authenticated a request; zero means never used since creation. Drives the idle timeout |

| `TokenSet` field | Meaning |
|---|---|
| `AccessToken`, `RefreshToken`, `IDToken` | The credentials themselves |
| `TokenType` | Usually `Bearer` |
| `Scopes` | Scopes granted with the token set |
| `AccessTokenExpiresAt` | When the access token must be refreshed |
| `RefreshTokenExpiresAt` | When the refresh token, and with it the issuer-side SSO session, stops working. Zero means the issuer reported no lifetime, and a built-in 30-day default applies instead |

`ExpiresAt` is never later than the refresh token's own lifetime (or that 30-day default), and never later than `CreatedAt + AbsoluteTimeout` when one is configured.

### ClaimsVerifier

`SessionManagerConfig.Verifier` and `middlewares.NewAuth` both take a `ClaimsVerifier`, a deliberately narrow interface:

```go
type ClaimsVerifier interface {
    Verify(ctx context.Context, token string) (Claims, error)
}
```

`*OIDC` satisfies it with no wrapper. It is separate from the full `OIDC` surface so a caller that only needs to turn a freshly exchanged token into `Claims` does not depend on introspection, `HasRole`, or `HasScope` — and so tests can supply a stub.

### Background cleanup

```go
go oidcauth.RunSessionCleanup(ctx, sessions, time.Hour, 30*time.Second, func(r oidcauth.CleanupResult) {
    if r.Err != nil {
        log.WithError(r.Err).Error("session cleanup failed")
        return
    }
    log.Infof("removed %d expired sessions in %s", r.Removed, r.Duration)
})
```

It runs until `ctx` is cancelled and never logs anything itself, so no session data passes through it. An `interval` or `timeout` of zero or less falls back to one hour and 30s respectively. Running it redundantly from several instances is safe: the delete is idempotent.

## Introspection tuning

`Introspector` is used internally by `Verify`, and is also usable directly. A zero `CacheTTL` means every call is a fresh round trip. Otherwise a positive result is cached under the SHA-256 digest of the token, for the shortest of `now + CacheTTL`, the caller-supplied known expiry, and the `exp` the issuer reports. Negative and failed results are never cached, so a revoked token is never remembered as briefly-still-good and a transient outage is retried on the next call.

Concurrent calls for the same token are coalesced into one HTTP request; each caller's own context cancellation is still honored independently.

```go
introspector, err := oidcauth.NewIntrospector(oidcauth.IntrospectorConfig{
    Endpoint:     endpoint,
    ClientID:     "my-app",
    ClientSecret: "secret",
    CacheTTL:     30 * time.Second,
    OnEvent: func(ev oidcauth.IntrospectionEvent) {
        metrics.Count("oidc.introspection", ev.Kind)
    },
})
if err != nil {
    log.Fatal(err)
}
defer introspector.Close()
```

`OnEvent` reports `EventCacheHit`, `EventCacheMiss`, `EventIntrospectionSucceeded`, `EventTokenInactive`, `EventIntrospectionFailed`, and `EventCacheEvicted`. An `IntrospectionEvent` never carries the token, its hash, or any claim value, so it is always safe to log or export verbatim. It is called synchronously, so keep it fast: a counter increment, not a network call.

`ClearCache` drops every cached entry immediately, which is the escape hatch for a bulk revocation that must take effect before the TTL elapses. `Close` stops the background eviction goroutine and is idempotent.

## Security notes

- Cache keys are SHA-256 hashes of the raw bearer token raw tokens are never stored in memory as map keys.
- `SkipIssuerCheck` and `SkipExpiryCheck` are intended for testing only. Enabling them in production disables core JWT security checks.
- `SkipClientIDCheck` is legitimate for M2M flows; compensate by validating `azp` and scopes explicitly.
- `DisableIntrospection` removes server-side revocation detection. Use only when the provider does not expose an introspection endpoint or when latency constraints prevent the extra round-trip, and accept the trade-off.
- Revoked tokens remain valid for up to `CacheDuration` when caching is enabled. Choose a TTL that matches your revocation latency requirements.
