// Package prest provides a generic HTTP client for pREST APIs that
// authenticates automatically via OAuth2 client credentials.
//
// Client[T] fetches and caches an access token, refreshing it before expiry,
// and unmarshals JSON responses directly into T.
//
// # Basic usage
//
//	type Product struct {
//	    ID    int    `json:"id"`
//	    Name  string `json:"name"`
//	}
//
//	client, err := prest.NewClient[[]Product](
//	    "client-id",
//	    "client-secret",
//	    "client_credentials",
//	    "api:read",
//	    "https://auth.example.com/oauth/token",
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	products, err := client.Get(ctx, "https://api.example.com/products", nil)
//
// # Pagination
//
//	page, err := client.GetPaginated(ctx, "https://api.example.com/products", 50, 0)
package prest
