# oidcauth

OpenID Connect token verification for Go, with optional in-memory caching and role-based authorization helpers. Designed for Keycloak but compatible with any OIDC-compliant provider.

## Features

- JWT verification (signature, issuer, audience, expiry) via [`go-oidc`](https://github.com/coreos/go-oidc)
- RFC 7662 token introspection to detect server-side revocation
- Pluggable cache via the `Cache` interface ship with `MemoryCache` or bring your own (Redis, Memcached, etc.)
- Keycloak client-role helpers via `HasRole`
- Safe for concurrent use

## Installation

```sh
go get github.com/raykavin/gobox/oidcauth
```

## Usage

### Without cache

Every call to `Verify` performs a full JWT check plus a remote introspection request.

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

### With MemoryCache

Cache verified claims to avoid a network round-trip on every request.

```go
cache := oidcauth.NewMemoryCache(ctx, oidcauth.DefaultCacheDuration) // 5m TTL
defer cache.Close()

verifier, err := oidcauth.New(ctx, config, oidcauth.WithCache(cache))
```

The entry TTL is `min(token.exp, now + duration)` the cache never serves a
token past its own expiry.

### With a custom cache backend

Implement the `Cache` interface to use any external store.

```go
type Cache interface {
    Get(key string, now time.Time) (Claims, bool)
    Set(key string, claims Claims, now time.Time)
}
```

```go
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
| `ErrProviderInitFailed` | OIDC discovery request failed |
| `ErrTokenValidationFailed` | JWT signature, issuer, audience, or expiry check failed |
| `ErrIntrospectionFailed` | Introspection endpoint unreachable or returned unexpected status |
| `ErrTokenRevoked` | Token is valid but marked inactive by the provider |

## Configuration

```go
oidcauth.Config{
    RealmURL:     "https://keycloak.example.com/realms/main", // required
    ClientID:     "my-app",                                   // required
    ClientSecret: "secret",                                   // required for introspection
    RequestTimeout: 10 * time.Second,                         // default: 30s
    // test-only flags do not use in production
    SkipIssuerCheck:   false,
    SkipClientIDCheck: false,
    SkipExpiryCheck:   false,
}
```

## Security notes

- Cache keys are SHA-256 hashes of the raw bearer token raw tokens are never stored in memory as map keys.
- `SkipIssuerCheck`, `SkipClientIDCheck`, and `SkipExpiryCheck` are intended for testing only. Enabling them in production disables core JWT security checks.
- Revoked tokens remain valid for up to `CacheDuration` when caching is enabled. Choose a TTL that matches your revocation latency requirements.
