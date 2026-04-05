# http

The `http` package provides lightweight helpers built on top of `net/http`. It is intended for shared service integrations where applications need common request headers, reusable content-type constants, simple request execution, and optional response decompression without hiding the standard library.

## Import

```go
import gkhttp "github.com/raykavin/gokit/http"
```

## What it provides

- common header name constants such as `Content-Type`, `Accept`, and `Authorization`
- MIME type and `Cache-Control` constants for common request scenarios
- default header builders for JSON, form-encoded, and compressed requests
- `NewRequestWithContext()` for context-aware requests with query params and an optional custom client
- `DecompressResponse()` for `gzip`, `deflate`, `br`, and `zstd` encoded responses
- `AcceptEncodingAll` to advertise support for every encoding handled by `DecompressResponse()`

## Main functions and constants

- `NewRequestWithContext()`: builds, executes, and reads an HTTP request in one call
- `DecompressResponse()`: wraps a response body with the appropriate decompression reader
- `DefaultJSONHeaders()`, `DefaultFormHeaders()`, and `DefaultCompressedHeaders()`: return ready-to-use header maps for common cases
- `Header*`, `MIME*`, and `CacheControl*` constants: avoid repeating common header and content values
- `AcceptEncodingAll`: `Accept-Encoding` value covering all supported decompression formats

## Request helper example

```go
package main

import (
	"context"
	"fmt"
	"log"
	stdhttp "net/http"
	"time"

	gkhttp "github.com/raykavin/gokit/http"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := gkhttp.DefaultJSONHeaders()
	headers[gkhttp.HeaderAuthorization] = "Bearer <token>"

	body, status, err := gkhttp.NewRequestWithContext(
		ctx,
		stdhttp.MethodGet,
		"https://api.example.com/users",
		map[string]string{
			"page":  "1",
			"limit": "20",
		},
		headers,
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("status=%d body=%s\n", status, body)
}
```

## Decompression example

Use `DecompressResponse()` when you are working directly with `*http.Response` and need to handle compressed payloads yourself.

```go
package main

import (
	"context"
	"io"
	"log"
	stdhttp "net/http"
	"time"

	gkhttp "github.com/raykavin/gokit/http"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, "https://example.com/data", nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set(gkhttp.HeaderAcceptEncoding, gkhttp.AcceptEncodingAll)

	resp, err := (&stdhttp.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	reader, err := gkhttp.DecompressResponse(resp)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	payload, err := io.ReadAll(reader)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("status=%d bytes=%d", resp.StatusCode, len(payload))
}
```

## Notes

- importing the package with an alias such as `gkhttp` helps avoid confusion with the standard library `net/http`
- `NewRequestWithContext()` reads the full response body and returns it together with the HTTP status code
- the package-level default client does not define a timeout, so callers should pass a context deadline or a custom client when needed
- `DefaultCompressedHeaders()` returns `Accept-Encoding: gzip, deflate, br, zstd`
- `DecompressResponse()` does not execute requests by itself; it only wraps an existing `*http.Response`
