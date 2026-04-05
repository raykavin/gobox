# prest

The `prest` package provides a lightweight generic client for authenticated pREST-style APIs. It is intended for service integrations that need OAuth2 client-credentials authentication, token reuse, and typed JSON decoding without introducing a large abstraction layer over HTTP.

## Import

```go
import "github.com/raykavin/gokit/prest"
```

## What it provides

- a generic `Client[T]` for authenticated GET requests returning typed JSON responses
- OAuth2 client-credentials authentication against a configurable token endpoint
- cached access tokens with expiry checks and a small safety buffer
- optional use of a custom `*http.Client`
- `GetPaginated()` convenience for `limit` and `offset` based endpoints
- sentinel errors for missing credentials or endpoints during client creation

## Main types

- `Client[T]`: stores authentication settings, caches tokens, and executes authenticated requests

## Example

```go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/raykavin/gokit/prest"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := prest.NewClient[[]User](
		os.Getenv("PREST_CLIENT_ID"),
		os.Getenv("PREST_CLIENT_SECRET"),
		"client_credentials",
		"prest.read",
		"https://auth.example.com/oauth/token",
	)
	if err != nil {
		log.Fatal(err)
	}

	users, err := client.GetPaginated(ctx, "https://api.example.com/users", 50, 0)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("users fetched: %d", len(users))
}
```

## Notes

- `NewClient()` validates the required credentials, scope, grant type, and authentication endpoint before returning a client
- `Get()` authenticates lazily, reuses the cached token while it is still valid, and decodes the JSON response into `T`
- `GetPaginated()` is a small convenience wrapper over `Get()` that sends `limit` and `offset` query params
- `Reset()` clears the cached token and forces a new authentication request on the next call
- requests currently expect an HTTP `200 OK` response and a JSON body
- `ErrInvalidCredentials` and `ErrInvalidEndpoints` can be checked with `errors.Is`
