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

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

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
func DefaultJSONHeaders() map[string]string {
	return map[string]string{
		HeaderContentType: MIMEApplicationJSON,
		HeaderAccept:      MIMEApplicationJSON,
	}
}

// DefaultFormHeaders returns a new map with standard form-encoded request headers.
func DefaultFormHeaders() map[string]string {
	return map[string]string{
		HeaderContentType: MIMEApplicationFormURLEncoded,
	}
}

// DefaultCompressedHeaders returns a new map that advertises support for all
// compressed encodings. Use alongside DecompressResponse.
func DefaultCompressedHeaders() map[string]string {
	return map[string]string{
		HeaderAcceptEncoding: AcceptEncodingAll,
	}
}

// defaultClient is the package-level HTTP client.
// Unlike stdhttp.DefaultClient, it has no global timeout — callers are
// expected to pass a context with an appropriate deadline.
var defaultClient = &stdhttp.Client{}

// NewRequestWithContext builds and executes an HTTP request, returning the
// raw response body, the status code, and any error.
// Decompression is not applied automatically — use DecompressResponse if needed.
func NewRequestWithContext(
	ctx context.Context,
	method string,
	urlStr string,
	queryParams map[string]string,
	headers map[string]string,
	body []byte,
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

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	req, err := stdhttp.NewRequestWithContext(ctx, method, u.String(), reqBody)
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
