package middlewares

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/httpserver/respond"
)

const (
	// CSRFCookie is the double-submit cookie handler.Auth writes on
	// login/callback/refresh. Unlike AccessTokenCookie it is NOT HttpOnly:
	// the frontend must be able to read it and echo it back in CSRFHeader.
	CSRFCookie = "csrf_token"

	// CSRFHeader is the request header the frontend echoes CSRFCookie's
	// value into on every mutating request.
	CSRFHeader = "X-CSRF-Token"
)

// safeMethods lists the HTTP methods CSRF exempts: they must not mutate
// state, so a cross-site request forging one is not a CSRF concern.
var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// CSRF implements the double-submit cookie pattern: a mutating request
// must echo the CSRFCookie value in the CSRFHeader header. Since cookies
// are sent automatically by the browser but a cross-site page cannot read
// another origin's cookies (no-CORS requests can't see the response, and
// a real cross-site fetch would be blocked by CORS before this middleware
// ever runs), only script running on an allowed origin can ever produce a
// matching pair. Stateless by design, matching this API's existing
// stateless (no server-side session) architecture rather than requiring a
// synchronizer-token store.
func CSRF() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if safeMethods[ctx.Request.Method] {
			ctx.Next()
			return
		}

		cookieValue, err := ctx.Cookie(CSRFCookie)
		if err != nil || cookieValue == "" {
			respond.Forbidden(ctx, respond.NewError(
				"ERR_CSRF_TOKEN_MISSING",
				"Missing CSRF cookie",
			))
			return
		}

		headerValue := ctx.GetHeader(CSRFHeader)
		if headerValue == "" || subtle.ConstantTimeCompare([]byte(cookieValue), []byte(headerValue)) != 1 {
			respond.Forbidden(ctx, respond.NewError(
				"ERR_CSRF_TOKEN_MISMATCH",
				"CSRF token missing or does not match",
			))
			return
		}

		ctx.Next()
	}
}
