package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	// DefaultAllowedHeaders is the header set CORS advertises when an option
	// set leaves AllowedHeaders empty.
	DefaultAllowedHeaders = []string{
		"Content-Type",
		"Content-Length",
		"Accept-Encoding",
		"X-CSRF-Token",
		"Authorization",
		"Accept",
		"Origin",
		"Cache-Control",
		"X-Requested-With",
	}

	// DefaultAllowedMethods is the method set CORS advertises when an option
	// set leaves AllowedMethods empty.
	DefaultAllowedMethods = []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	}
)

// CORSOptions describes the cross-origin policy the middleware advertises.
//
// Only AllowedOrigins is mandatory. AllowedMethods and AllowedHeaders fall
// back to the Default* sets above when empty, so a caller that only cares
// about origins keeps the previous behaviour without spelling the rest out.
type CORSOptions struct {
	// AllowedOrigins is matched against the request's Origin header by exact
	// string comparison: include the scheme, include a non-default port, and
	// never a trailing slash. "*" is not a wildcard here; list real origins.
	AllowedOrigins []string

	// AllowedMethods and AllowedHeaders are advertised verbatim. Empty means
	// DefaultAllowedMethods / DefaultAllowedHeaders.
	AllowedMethods []string
	AllowedHeaders []string

	// AllowCredentials decides whether Access-Control-Allow-Credentials is
	// sent at all. When false the header is omitted rather than set to
	// "false", which is what the fetch specification reads as "not allowed".
	AllowCredentials bool
}

// CORS returns a middleware restricted to allowedOrigins, advertising the
// default method and header sets with credentials enabled.
//
// It is the backwards-compatible shorthand for CORSWithOptions. The previous
// signature also took a customCors map that replaced every header
// unconditionally; that variant is gone, because writing a static header set
// bypassed the per-origin check and handed CORS headers to any caller. Build a
// CORSOptions instead.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	return CORSWithOptions(CORSOptions{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: true,
	})
}

// CORSWithOptions returns a middleware applying opts.
//
// An Origin outside the list gets no CORS headers at all, which is what makes
// the browser block the response. Credentials (a session cookie) travel with
// every request when AllowCredentials is set, so Access-Control-Allow-Origin
// can never reflect an arbitrary origin the way it could when only a
// self-attached Authorization header was at stake.
func CORSWithOptions(opts CORSOptions) gin.HandlerFunc {
	policy := NewCORSPolicy(opts)
	return CORSWithProvider(func() *CORSPolicy { return policy })
}

// CORSPolicy is CORSOptions with the per-request work already done: the origin
// set as a map and the advertised lists already joined.
//
// It exists so a policy can be swapped at runtime without paying for that work
// on every request: build a new one, publish the pointer, and CORSWithProvider
// picks it up on the next request.
type CORSPolicy struct {
	allowed     map[string]bool
	methods     string
	headers     string
	credentials bool
}

// NewCORSPolicy precomputes opts into an immutable policy.
func NewCORSPolicy(opts CORSOptions) *CORSPolicy {
	allowed := make(map[string]bool, len(opts.AllowedOrigins))
	for _, origin := range opts.AllowedOrigins {
		allowed[origin] = true
	}

	return &CORSPolicy{
		allowed:     allowed,
		methods:     joinHeaderValues(opts.AllowedMethods, DefaultAllowedMethods),
		headers:     joinHeaderValues(opts.AllowedHeaders, DefaultAllowedHeaders),
		credentials: opts.AllowCredentials,
	}
}

// CORSWithProvider is CORSWithOptions against a policy that may change.
//
// provider is called once per request and must be cheap: the intended source
// is an atomic pointer load. A nil policy disables CORS headers rather than
// panicking, so a half-configured reload cannot take the server down.
func CORSWithProvider(provider func() *CORSPolicy) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		policy := provider()
		origin := ctx.Request.Header.Get("Origin")

		if policy != nil && origin != "" && policy.allowed[origin] {
			header := ctx.Writer.Header()

			header.Set("Vary", "Origin")
			header.Set("Access-Control-Allow-Origin", origin)

			if policy.methods != "" {
				header.Set("Access-Control-Allow-Methods", policy.methods)
			}
			if policy.headers != "" {
				header.Set("Access-Control-Allow-Headers", policy.headers)
			}
			if policy.credentials {
				header.Set("Access-Control-Allow-Credentials", "true")
			}
		}

		if ctx.Request.Method == http.MethodOptions {
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}

		ctx.Next()
	}
}

// joinHeaderValues renders values as a comma-separated header value, dropping
// blank entries and falling back to fallback when nothing usable is left.
func joinHeaderValues(values, fallback []string) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		out = fallback
	}
	return strings.Join(out, ", ")
}
