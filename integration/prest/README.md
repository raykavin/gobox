# integration/prest

The `prest` package provides a generic HTTP client for pREST APIs. It is intended for services that consume a pREST backend and need automatic OAuth2 client credentials authentication, transparent token refresh, and typed JSON response unmarshalling without writing that plumbing in every call site.

## Import

```go
import "github.com/raykavin/gobox/integration/prest"
```

## What it provides

- `Client[T]` for sending authenticated GET requests to a pREST API
- automatic OAuth2 client credentials token acquisition and caching
- transparent token refresh when the cached token is about to expire
- `GetPaginated()` for adding `limit` and `offset` query parameters
- `Reset()` for forcing re-authentication on the next request

## Main types

- `Client[T]`: generic client that returns `T` from JSON responses

## Example

```go
package main

import (
    "context"
    "log"

    "github.com/raykavin/gobox/integration/prest"
)

type Order struct {
    ID     int     `json:"id"`
    Total  float64 `json:"total"`
    Status string  `json:"status"`
}

func main() {
    client, err := prest.NewClient[[]Order](
        "my-client-id",
        "my-client-secret",
        "client_credentials",
        "orders:read",
        "https://auth.example.com/oauth/token",
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // first page
    orders, err := client.GetPaginated(ctx, "https://api.example.com/public.orders", 50, 0)
    if err != nil {
        log.Fatal(err)
    }

    for _, o := range orders {
        log.Printf("order %d: %s (%.2f)", o.ID, o.Status, o.Total)
    }
}
```

## Notes

- `NewClient` validates that `clientID`, `clientSecret`, `grantType`, `scope`, and `authEndpoint` are all non-empty; it does not make any network calls at construction time
- the token is cached in memory and reused across requests; it is considered expired `10s` before the `expires_in` value from the token response
- pass a custom `*http.Client` as the last argument to `NewClient` to control timeouts and transport; the default is `http.DefaultClient`
- `Reset()` clears the cached token without making a network call; the next request will re-authenticate
- `Get` and `GetPaginated` return an error for any non-200 HTTP status code
