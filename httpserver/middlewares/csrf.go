package middlewares

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/httpserver/respond"
)

const (
	// CSRFCookie is the default double-submit cookie handler.Auth writes on
	// login/callback/refresh. Unlike the session cookie it is NOT HttpOnly:
	// the frontend must be able to read it and echo it back in CSRFHeader.
	CSRFCookie = "csrf_token"

	// CSRFHeader is the default request header the frontend echoes
	// CSRFCookie's value into on every mutating request.
	CSRFHeader = "X-CSRF-Token"
)

// safeMethods lists the HTTP methods CSRF exempts: they must not mutate
// state, so a cross-site request forging one is not a CSRF concern.
var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// CSRFOptions names the cookie/header pair the double-submit check uses.
//
// Both default to CSRFCookie/CSRFHeader when empty. CookieName must match the
// name whatever issues the cookie writes for the bundled login flow that is
// AuthCookieOptions.CSRFName, which shares the same default.
type CSRFOptions struct {
	CookieName string
	HeaderName string
}

func (o CSRFOptions) withDefaults() CSRFOptions {
	if o.CookieName == "" {
		o.CookieName = CSRFCookie
	}
	if o.HeaderName == "" {
		o.HeaderName = CSRFHeader
	}
	return o
}

// CSRF implements the double-submit cookie pattern with the default names.
// It is the backwards-compatible shorthand for CSRFWithOptions.
func CSRF() gin.HandlerFunc {
	return CSRFWithOptions(CSRFOptions{})
}

// CSRFWithOptions implements the double-submit cookie pattern: a mutating
// request must echo the cookie's value in the header. Cookies are sent
// automatically by the browser, but a cross-site page cannot read another
// origin's cookies (no-CORS requests can't see the response, and a real
// cross-site fetch is blocked by CORS before this middleware runs), so only
// script running on an allowed origin can produce a matching pair.
//
// Stateless by design, matching the token-in-cookie architecture rather than
// requiring a synchronizer-token store.
func CSRFWithOptions(opts CSRFOptions) gin.HandlerFunc {
	opts = opts.withDefaults()

	return func(ctx *gin.Context) {
		if safeMethods[ctx.Request.Method] {
			ctx.Next()
			return
		}

		cookieValue, err := ctx.Cookie(opts.CookieName)
		if err != nil || cookieValue == "" {
			respond.Forbidden(ctx, respond.NewError(
				"ERR_CSRF_TOKEN_MISSING",
				"Missing CSRF cookie",
			))
			return
		}

		headerValue := ctx.GetHeader(opts.HeaderName)
		if headerValue == "" ||
			subtle.ConstantTimeCompare([]byte(cookieValue), []byte(headerValue)) != 1 {
			respond.Forbidden(ctx, respond.NewError(
				"ERR_CSRF_TOKEN_MISMATCH",
				"CSRF token missing or does not match",
			))
			return
		}

		ctx.Next()
	}
}
