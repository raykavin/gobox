# oauth2

The `oauth2` package manages OAuth 2.0 access tokens for outbound calls, caching one token per scope and re-authenticating only once the cached token has expired. It is intended for service-to-service clients that need to attach a bearer token to every request without hand-rolling the token lifecycle.

For inbound token *verification*, see [`oidcauth`](../oidcauth/README.md); this package is the client side.

## Import

```go
import "github.com/raykavin/gobox/oauth2"
```

## What it provides

- `TokenManager` for fetching, caching, and refreshing tokens per scope
- `SetAuthorizationHeader` for stamping `Authorization` onto an outbound request
- `GetAccessToken` and `GetTokenType` for callers that need the parts separately
- a choice of sending credentials as query parameters or as a form-encoded POST body
- `TokenAccess`, the decoded token response

## Example

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/raykavin/gobox/oauth2"
)

func main() {
    tm := oauth2.NewTokenManager(nil, "my-client", "my-secret", "client_credentials")
    tm.WithAuthenticationURL("https://auth.example.com/oauth/token")
    tm.SendAsPost()

    req, _ := http.NewRequestWithContext(context.Background(),
        http.MethodGet, "https://api.example.com/orders", nil)

    if err := tm.SetAuthorizationHeader(context.Background(), req, "orders:read"); err != nil {
        log.Fatal(err)
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
}
```

Passing `nil` as the first argument to `NewTokenManager` installs a default `*http.Client` with a 20 second timeout.

## Reference

### Constructor

```go
func NewTokenManager(client *http.Client, clientID, clientSecret, grantType string) *TokenManager
```

The authentication URL is not a constructor argument; set it with `WithAuthenticationURL` before the first token request.

### Methods

| Method | Description |
|---|---|
| `WithAuthenticationURL(url string)` | Sets the token endpoint. Required |
| `WithOptionalParams(params map[string]string)` | Extra parameters added to every token request |
| `SendAsPost()` | Sends credentials as a form-encoded request body |
| `SendAsGet()` | Sends credentials as query-string parameters (the default) |
| `SetAuthorizationHeader(ctx, r *http.Request, scope string) error` | Resolves a token for `scope` and sets `Authorization: <type> <token>` |
| `GetAccessToken(ctx, scope string) (string, error)` | The access token alone |
| `GetTokenType(ctx, scope string) (string, error)` | The token type alone, typically `Bearer` |

### TokenAccess

| Field | JSON | Description |
|---|---|---|
| `AccessToken` | `access_token` | The token itself |
| `TokenType` | `token_type` | Usually `Bearer` |
| `ExpiresIn` | `expires_in` | Lifetime in seconds, used to compute expiry |
| `RefreshToken` | `refresh_token` | Present only if the issuer returns one |
| `Scope` | `scope` | The granted scope |
| `LastAuthentication` | not serialized | When this token was obtained |

## Notes

- **`TokenManager` is not safe for concurrent use.** The per-scope cache is a plain map with no locking, so sharing one manager across goroutines races on both read and write. Confine one to a single goroutine, or guard it with your own mutex
- tokens are cached per scope, and a scope is required: `GetAccessToken`, `GetTokenType`, and `SetAuthorizationHeader` all reject an empty one
- expiry is computed as `LastAuthentication + ExpiresIn`, and `LastAuthentication` is backdated by five seconds when the token is stored, so a token is treated as expired five seconds before the issuer would. That margin absorbs clock skew and request latency, but it is fixed: it is not proportional to `ExpiresIn`, and a very short-lived token gets no more slack than a long-lived one
- the refresh token is decoded and stored but never used: an expired token triggers a full re-authentication with the client credentials rather than a `refresh_token` grant
- `SendAsGet`, the default, puts `client_secret` in the query string, where it commonly lands in proxy and server access logs. Prefer `SendAsPost`
