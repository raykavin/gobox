package httpclient

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/raykavin/gobox/retry"
)

// ErrTransientHTTPStatus marks an HTTP response worth retrying (429 or
// 5xx). WithRetry returns the original response (body, status, nil error)
// once retries are exhausted on this kind of failure, exactly as if no
// retry wrapper were present, so callers keep inspecting the status code
// themselves; it is exported only so a caller can tell, via errors.Is on
// the pre-exhaustion error, that a failure was transient rather than
// checking the returned status is enough for the common case.
var ErrTransientHTTPStatus = errors.New("transient http status")

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

const (
	RetryMaxAttempts = 4
	RetryWaitMin     = 200 * time.Millisecond
	RetryWaitMax     = 5 * time.Second
	retryAfterCap    = 30 * time.Second
)

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
var defaultClient = &http.Client{Timeout: 30 * time.Second}

// NewRequestWithContext builds and executes an HTTP request, returning the
// raw response body, the status code, and any error.
// Decompression is not applied automatically use DecompressResponse if needed.
func NewRequestWithContext(
	ctx context.Context,
	method string,
	urlStr string,
	queryParams map[string]string,
	headers map[string]string,
	payload []byte,
	client ...*http.Client,
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

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqPayload)
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

// WithRetry runs do once per attempt (up to RetryMaxAttempts, with
// exponential backoff via gobox/retry) and retries only transient
// failures: a transport-level error (network I/O, auth token acquisition)
// or a response with an IsTransientHTTPStatus code. A non-transient status
// (any other 4xx) is returned as-is after the very first attempt, exactly
// as if no retry wrapper were present validation errors and definitive
// authentication failures are never retried. Context cancellation is
// never retried either gobox/retry's own wait already respects ctx, and
// shouldRetry here bails out immediately rather than spending an attempt.
//
// When a transient response carries a Retry-After header, WithRetry waits
// at least that long (capped at 30s) before the next attempt, in addition
// to gobox/retry's own backoff a deliberate "wait at least this long"
// floor, not a replacement for the schedule gobox/retry already owns.
func WithRetry(ctx context.Context, do func() (
	body []byte,
	status int,
	retryAfter time.Duration,
	err error,
)) ([]byte, int, error) {
	var (
		body   []byte
		status int
	)

	err := retry.Do(
		ctx,
		RetryMaxAttempts,
		RetryWaitMin,
		RetryWaitMax,
		func(_ int, err error) bool {
			return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
		},
		func() error {
			var (
				retryAfter time.Duration
				innerErr   error
			)

			body, status, retryAfter, innerErr = do()
			if innerErr != nil {
				return innerErr
			}
			if !IsTransientHTTPStatus(status) {
				return nil
			}

			if retryAfter > 0 {
				sleepCtx(ctx, min(retryAfter, retryAfterCap))
			}
			return fmt.Errorf("%w: status %d", ErrTransientHTTPStatus, status)
		},
	)

	if err != nil && !errors.Is(err, ErrTransientHTTPStatus) {
		return nil, 0, err
	}
	return body, status, nil
}

// ParseRetryAfter parses an HTTP Retry-After header value (either an
// integer number of seconds or an HTTP-date), returning 0 if empty or
// unparseable.
func ParseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// IsTransientHTTPStatus reports whether status is worth retrying: 429 (rate
// limited) or any 5xx (server-side failure). Any other 4xx is a definitive
// client error (bad request, unauthorized, not found, ...) and must never
// be retried.
func IsTransientHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status < 600)
}

// DecompressResponse wraps the response body in the appropriate decompression
// reader based on the Content-Encoding header.
// The caller is responsible for closing the returned reader.
func DecompressResponse(r *http.Response) (io.ReadCloser, error) {
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

func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
