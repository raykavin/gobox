# oidcauth

OpenID Connect token verification for Go, with optional in-memory caching and authorization helpers. Designed for Keycloak but compatible with any OIDC-compliant provider.

## Features

- JWT verification (signature, issuer, audience, expiry) via [`go-oidc`](https://github.com/coreos/go-oidc)
- RFC 7662 token introspection to detect server-side revocation (opt-out via `DisableIntrospection`)
- Pluggable cache via the `Cache` interface ships with `MemoryCache` or bring your own (Redis, Memcached, etc.)
- Authorization helpers: `HasRole`, `HasScope`, `HasAllScopes`, `IsAuthorizedParty`
- Machine-to-machine (client credentials) support via `SkipClientIDCheck`
- Safe for concurrent use

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
| `ErrIntrospectionFailed` | Introspection endpoint unreachable or returned unexpected status |
| `ErrTokenRevoked` | Token is valid but marked inactive by the provider |

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

    // Test-only do not enable in production.
    SkipIssuerCheck: false,
    SkipExpiryCheck: false,
}
```

## Authorization helpers

| Method | Checks |
|---|---|
| `HasRole(claims, role)` | `resource_access[clientID].roles` contains `role` |
| `HasScope(claims, scope)` | `claims.Scope` contains `scope` (exact word match) |
| `HasAllScopes(claims, scopes...)` | every scope in the list is present |
| `IsAuthorizedParty(claims, azp)` | `claims.Azp == azp` |

## Security notes

- Cache keys are SHA-256 hashes of the raw bearer token raw tokens are never stored in memory as map keys.
- `SkipIssuerCheck` and `SkipExpiryCheck` are intended for testing only. Enabling them in production disables core JWT security checks.
- `SkipClientIDCheck` is legitimate for M2M flows; compensate by validating `azp` and scopes explicitly.
- `DisableIntrospection` removes server-side revocation detection. Use only when the provider does not expose an introspection endpoint or when latency constraints prevent the extra round-trip, and accept the trade-off.
- Revoked tokens remain valid for up to `CacheDuration` when caching is enabled. Choose a TTL that matches your revocation latency requirements.
