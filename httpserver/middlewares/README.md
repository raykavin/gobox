# httpserver/middlewares

The `middlewares` package provides Gin middleware for the concerns almost every authenticated API needs: bearer or session token verification, role checks, CORS, CSRF, and per-IP rate limiting. It also bundles `Auth`, a complete server-side OIDC Authorization Code + PKCE login flow.

## Import

```go
import "github.com/raykavin/gobox/httpserver/middlewares"
```

## What it provides

| Middleware | Purpose |
|---|---|
| `Authorization` / `AuthorizationWithOptions` | Resolves and verifies the caller's token, storing claims in the request context |
| `RequireRole` | Aborts unless the caller holds a role, optionally injecting extra context values |
| `CORS` / `CORSWithOptions` / `CORSWithProvider` | Per-origin cross-origin policy |
| `CSRF` / `CSRFWithOptions` | Double-submit cookie check on mutating requests |
| `RateLimitByIP` / `RateLimitByIPProvider` | Per-client-IP token bucket answering 429 |
| `Auth` | Handlers for the full OIDC login, callback, refresh, logout, and identity flow |

Every middleware has a plain form for the common case and an options form for everything else. The three `*Provider` variants read their policy through a callback on each request, so it can be swapped at runtime.

## Authorization

`Authorization` accepts a token from either the `Authorization: Bearer` header or the session cookie, but not both:

```go
r.Use(middlewares.Authorization(oidcClient)) // *oidcauth.OIDC satisfies TokenVerifier

r.GET("/me", func(c *gin.Context) {
    claims, ok := middlewares.ClaimsFromContext(c)
    if !ok {
        return
    }
    c.JSON(http.StatusOK, claims)
})
```

To read a differently named session cookie:

```go
r.Use(middlewares.AuthorizationWithOptions(oidcClient, middlewares.AuthorizationOptions{
    SessionCookieName: "my_session",
}))
```

The cookie name must match whatever issues the cookie. For the bundled login flow that is `AuthCookieOptions.Name`: issuing one name and reading another rejects every authenticated request.

### Role checks

`RequireRole` must run after `Authorization`:

```go
admin := r.Group("/admin", middlewares.RequireRole(oidcClient, "admin"))
```

It aborts 401 when claims are absent, 403 when the role is missing, and 500 on a `RoleContext` key conflict. Optional `RoleContext` values are injected into the context when the caller holds the named role:

```go
middlewares.RequireRole(oidcClient, "auditor",
    middlewares.RoleContext{
        RoleName: "auditor",
        Values:   map[string]any{"read_only": true},
    },
)
```

## CORS

`AllowedOrigins` is matched by exact string comparison. Include the scheme, include a non-default port, and never a trailing slash. `"*"` is not treated as a wildcard: list real origins.

```go
r.Use(middlewares.CORS([]string{"https://app.example.com"}))
```

```go
r.Use(middlewares.CORSWithOptions(middlewares.CORSOptions{
    AllowedOrigins:   []string{"https://app.example.com"},
    AllowedMethods:   []string{http.MethodGet, http.MethodPost},
    AllowCredentials: true,
}))
```

Leaving `AllowedMethods` or `AllowedHeaders` empty falls back to `DefaultAllowedMethods` and `DefaultAllowedHeaders`. When `AllowCredentials` is false the header is omitted entirely rather than sent as `"false"`, which is what the fetch specification reads as "not allowed".

## CSRF

Double-submit: the middleware compares the `csrf_token` cookie against the `X-CSRF-Token` header. `GET`, `HEAD`, and `OPTIONS` are exempt.

```go
r.Use(middlewares.CSRF())
```

```go
r.Use(middlewares.CSRFWithOptions(middlewares.CSRFOptions{
    CookieName: "my_csrf",
    HeaderName: "X-My-CSRF",
}))
```

Unlike the session cookie, the CSRF cookie is deliberately not `HttpOnly`: the frontend has to read it to echo it back.

## Rate limiting

```go
r.Use(middlewares.RateLimitByIP(middlewares.RateLimitOptions{
    RequestsPerMinute: 120,
    Burst:             20,
}))
```

A non-positive field is treated as a programming error rather than "unlimited". Use `WithDefaults` to fill gaps explicitly:

```go
opts := loaded.WithDefaults(60, 10)
```

The client is identified by `gin.Context.ClientIP()`, which honors `X-Forwarded-For` and `X-Real-IP` only for peers in the engine's trusted proxies. **Deploying behind a proxy without configuring `GinConfig.TrustedProxies` collapses every client into the proxy's single bucket.**

## Runtime-reloadable policies

`CORSWithProvider` and `RateLimitByIPProvider` call their provider once per request, so it must be cheap: an atomic pointer load is the intended source.

```go
var policy atomic.Pointer[middlewares.CORSPolicy]
policy.Store(middlewares.NewCORSPolicy(opts))

r.Use(middlewares.CORSWithProvider(policy.Load))
```

A nil `*CORSPolicy` disables CORS headers rather than panicking, so a half-applied reload cannot take the server down. When rate-limit options change, existing buckets are retuned in place rather than discarded: raising a limit does not hand every client a fresh full bucket, and lowering one takes effect without waiting for idle eviction.

## OIDC login flow

`Auth` runs the Authorization Code + PKCE flow server-side. OIDC tokens stay in the session; the browser only ever receives an opaque session identifier as an `HttpOnly` cookie.

```go
auth := middlewares.NewAuth(
    flow,        // *oidcauth.Flow
    oidcClient,  // oidcauth.ClaimsVerifier
    sessions,    // *oidcauth.SessionManager
    clientID,
    middlewares.AuthCookieOptions{
        Path:     "/",
        Secure:   true,
        SameSite: http.SameSiteLaxMode,
    },
    "https://app.example.com/",      // post-login redirect
    "https://app.example.com/bye",   // post-logout redirect
    clientSecret,                    // state signing key
)

r.GET("/auth/login", auth.Login)
r.GET("/auth/callback", auth.Callback)
r.POST("/auth/refresh", auth.Refresh)
r.POST("/auth/logout", auth.Logout)
r.GET("/auth/me", auth.Me)
```

`NewAuth` panics on missing required arguments rather than degrading, so a misconfigured handler never starts serving traffic. The PKCE verifier and return path travel inside the signed state parameter, not a cookie.

`Me` returns a `MeResponse` with `sub`, `email`, `name`, `preferred_username`, and `client_roles`: enough for a frontend to render the signed-in user and drive UX-only permission gates without decoding a JWT. Real authorization stays server-side.

## Reference

### Constants

| Constant | Value | Description |
|---|---|---|
| `SessionCookie` | `"session_id"` | Default session cookie name; an opaque identifier, never a token |
| `CSRFCookie` | `"csrf_token"` | Default double-submit cookie name |
| `CSRFHeader` | `"X-CSRF-Token"` | Default header the frontend echoes the CSRF cookie into |

### Types

| Type | Description |
|---|---|
| `TokenVerifier` | `Verify` plus `HasRole`; `*oidcauth.OIDC` satisfies it with no wrapper |
| `AuthorizationOptions` | Names the session cookie to read |
| `CORSOptions` | Origins, methods, headers, and the credentials flag |
| `CORSPolicy` | A compiled policy, built by `NewCORSPolicy`, for use with `CORSWithProvider` |
| `CSRFOptions` | Names the cookie and header pair |
| `RateLimitOptions` | `RequestsPerMinute` and `Burst`, plus `WithDefaults` |
| `RoleContext` | Extra context values injected when the caller holds `RoleName` |
| `AuthCookieOptions` | Attributes applied to every cookie `Auth` writes |
| `MeResponse` | The identity payload returned by `Auth.Me` |

### Errors

Every middleware here is a `gin.HandlerFunc`, so none of them returns an error: a rejection is written to the response as an `APIError` and the chain is aborted. The sentinels below are exported for tests and for code that wraps this package, but **no exported function ever returns one to a caller**; match on the response code instead.

| Sentinel | Meaning | Where it is produced |
|---|---|---|
| `ErrMissingToken` | No token in either the header or the cookie | Internal to token extraction; surfaces as 401 `ERR_MISSING_TOKEN` |
| `ErrForbidden` | Token supplied via both header and cookie | Internal to token extraction; surfaces as 403 `ERR_TOKEN_CONFLICT` |
| `ErrMissingAuthorizationHeader` | No `Authorization` header | Internal to bearer parsing; folded into `ERR_MISSING_TOKEN` |
| `ErrInvalidAuthorizationFormat` | Header is not `Bearer <token>` | Internal to bearer parsing; folded into `ERR_MISSING_TOKEN` |
| `ErrEmptyToken` | Header is well formed but the token is empty | Internal to bearer parsing; folded into `ERR_MISSING_TOKEN` |
| `ErrRoleContextConflict` | Two `RoleContext` entries write the same key | Internal to `RequireRole`; surfaces as 500 `ERR_ROLE_CONTEXT_CONFLICT` |
| `ErrInvalidToken` | Declared but unused | No code path produces it; a rejected token surfaces as 401 `ERR_INVALID_TOKEN` |
| `ErrPermissionDenied` | Declared but unused | No code path produces it; a missing role surfaces as 403 `ERR_PERMISSION_DENIED` |

The response `code` values in the last column are the stable contract for clients: `ERR_MISSING_TOKEN`, `ERR_TOKEN_CONFLICT`, `ERR_INVALID_TOKEN`, `ERR_PERMISSION_DENIED`, `ERR_ROLE_CONTEXT_CONFLICT`, `ERR_CSRF_TOKEN_MISSING`, and `ERR_CSRF_TOKEN_MISMATCH`.

## Notes

- three cookie names must agree across the stack: `AuthCookieOptions.Name` with `AuthorizationOptions.SessionCookieName`, and `AuthCookieOptions.CSRFName` with `CSRFOptions.CookieName`. They share defaults, so leaving all of them empty is consistent
- `Authorization` rejects a request that carries a token in both the header and the cookie with 403, rather than silently preferring one
- `RequireRole` depends on the claims `Authorization` stored, so ordering matters
- `ClaimsFromContext` reports `false` when `Authorization` did not run or did not succeed; always check the boolean
- the earlier `CORS(origins, customCors map[string]string)` signature is gone: writing a static header set bypassed the per-origin check and handed CORS headers to any caller. Build a `CORSOptions` instead
