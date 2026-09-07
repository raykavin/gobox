# httpclient

The `httpclient` package provides a thin HTTP client wrapper. It is intended for service code that needs to send JSON, form, or raw HTTP requests without importing a larger HTTP client framework, with built-in support for query parameters, common header presets, and response decompression.

## Import

```go
import "github.com/raykavin/gobox/httpclient"
```

## What it provides

- `NewRequestWithContext()` for building and executing an HTTP request in a single call
- `DecompressResponse()` for transparent body decompression based on `Content-Encoding`
- `MapParams` type with `Set` and `Del` helpers for headers and query parameters
- header name constants (`HeaderContentType`, `HeaderAuthorization`, etc.)
- MIME type constants (`MIMEApplicationJSON`, `MIMEApplicationXML`, etc.)
- header preset constructors: `DefaultJSONHeaders`, `DefaultFormHeaders`, `DefaultCompressedHeaders`

## Main functions

- `NewRequestWithContext`: builds the request, applies query params and headers, executes it, and returns the raw body bytes, HTTP status code, and any error. The variadic final argument optionally replaces the default client
- `DecompressResponse`: wraps `http.Response.Body` in a decompressing reader for gzip, deflate, Brotli, or zstd

## Example

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "net/http"

    "github.com/raykavin/gobox/httpclient"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    ctx := context.Background()

    body, status, err := httpclient.NewRequestWithContext(
        ctx,
        http.MethodGet,
        "https://jsonplaceholder.typicode.com/users",
        map[string]string{"_limit": "5"},
        httpclient.DefaultJSONHeaders(),
        nil,
    )
    if err != nil {
        log.Fatal(err)
    }
    if status != http.StatusOK {
        log.Fatalf("unexpected status: %d", status)
    }

    var users []User
    if err := json.Unmarshal(body, &users); err != nil {
        log.Fatal(err)
    }

    for _, u := range users {
        log.Printf("%d: %s", u.ID, u.Name)
    }
}
```

## Decompression example

```go
resp, err := http.Get("https://api.example.com/data")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

reader, err := httpclient.DecompressResponse(resp)
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

data, _ := io.ReadAll(reader)
```

## Notes

- the package-level default client has a 30s timeout; pass a custom `*http.Client` as the last argument to `NewRequestWithContext` to override it
- `DecompressResponse` supports `gzip`, `deflate`, `br` (Brotli), `zstd`, and `identity`; an unknown encoding returns an error
- `NewRequestWithContext` returns the body exactly as it came off the wire and never decompresses it. `DecompressResponse` takes an `*http.Response`, so the two cannot be combined: if you need `Accept-Encoding` control and decompression together, build the request with `net/http` directly and pass the response to `DecompressResponse`
- by default Go's transport adds its own `Accept-Encoding: gzip` and decompresses the response for you. Setting the header explicitly, which is what `DefaultCompressedHeaders` does, turns that off and hands you the compressed bytes
- `MapParams` is a plain `map[string]string`, so it can be passed anywhere the `queryParams` or `headers` arguments are expected, and a plain map literal works just as well
