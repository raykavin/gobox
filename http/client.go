package http

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// MapParams is a map type used by this package for request headers
// and query parameters.
type MapParams map[string]string

// Set assigns v to k in the map.
// The receiver must be initialized before calling Set.
func (m MapParams) Set(k, v string) {
	m[k] = v
}

// Del removes k from the map if it exists.
func (m MapParams) Del(k string) {
	delete(m, k)
}

// Header name constants.
const (
	HeaderContentType     = "Content-Type"
	HeaderAccept          = "Accept"
	HeaderAuthorization   = "Authorization"
	HeaderUserAgent       = "User-Agent"
	HeaderAcceptEncoding  = "Accept-Encoding"
	HeaderContentEncoding = "Content-Encoding"
	HeaderCacheControl    = "Cache-Control"
	HeaderXRequestID      = "X-Request-Id"
)

// MIME type constants.
const (
	MIMEApplicationJSON           = "application/json"
	MIMEApplicationXML            = "application/xml"
	MIMEApplicationFormURLEncoded = "application/x-www-form-urlencoded"
	MIMEMultipartFormData         = "multipart/form-data"
	MIMETextPlain                 = "text/plain; charset=utf-8"
	MIMEOctetStream               = "application/octet-stream"
)

// Cache-Control directive constants.
const (
	CacheControlNoCache = "no-cache"
	CacheControlNoStore = "no-store"
	CacheControlMaxAge0 = "max-age=0"
)

// AcceptEncodingAll declares support for all encodings implemented in
// DecompressResponse. Use alongside DecompressResponse.
const AcceptEncodingAll = "gzip, deflate, br, zstd"

// DefaultJSONHeaders returns a new map with standard JSON request headers.
func DefaultJSONHeaders() MapParams {
	return map[string]string{
		HeaderContentType: MIMEApplicationJSON,
		HeaderAccept:      MIMEApplicationJSON,
	}
}

// DefaultFormHeaders returns a new map with standard form-encoded request headers.
func DefaultFormHeaders() MapParams {
	return map[string]string{
		HeaderContentType: MIMEApplicationFormURLEncoded,
	}
}

// DefaultCompressedHeaders returns a new map that advertises support for all
// compressed encodings. Use alongside DecompressResponse.
func DefaultCompressedHeaders() MapParams {
	return map[string]string{
		HeaderAcceptEncoding: AcceptEncodingAll,
	}
}

// defaultClient is the package-level HTTP client.
var defaultClient = &stdhttp.Client{Timeout: 30 * time.Second}

// NewRequestWithContext builds and executes an HTTP request, returning the
// raw response body, the status code, and any error.
// Decompression is not applied automatically — use DecompressResponse if needed.
func NewRequestWithContext(
	ctx context.Context,
	method string,
	urlStr string,
	queryParams map[string]string,
	headers map[string]string,
	payload []byte,
	client ...*stdhttp.Client,
) ([]byte, int, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid URL: %w", err)
	}

	c := defaultClient
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}

	if len(queryParams) > 0 {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	var reqPayload io.Reader
	if len(payload) > 0 {
		reqPayload = bytes.NewReader(payload)
	}

	req, err := stdhttp.NewRequestWithContext(ctx, method, u.String(), reqPayload)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// DecompressResponse wraps the response body in the appropriate decompression
// reader based on the Content-Encoding header.
// The caller is responsible for closing the returned reader.
func DecompressResponse(r *stdhttp.Response) (io.ReadCloser, error) {
	switch enc := r.Header.Get("Content-Encoding"); enc {
	case "gzip":
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, fmt.Errorf("creating gzip reader: %w", err)
		}
		return gz, nil

	case "deflate":
		// Some servers send deflate as zlib-wrapped (RFC 1950); others send
		// raw deflate (RFC 1951). Peek at the first two bytes to detect the
		// zlib magic (0x78 + 0x9C/0xDA/0x01/0x5E) and fall back to raw.
		peek, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("buffering deflate body: %w", err)
		}
		if z, err := zlib.NewReader(bytes.NewReader(peek)); err == nil {
			return z, nil
		}
		return flate.NewReader(bytes.NewReader(peek)), nil

	case "br":
		return io.NopCloser(brotli.NewReader(r.Body)), nil

	case "zstd":
		dec, err := zstd.NewReader(r.Body)
		if err != nil {
			return nil, fmt.Errorf("creating zstd reader: %w", err)
		}
		return dec.IOReadCloser(), nil

	case "", "identity":
		return r.Body, nil

	default:
		return nil, fmt.Errorf("unsupported content encoding %q", enc)
	}
}
