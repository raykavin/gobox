package middlewares

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/httpserver/respond"
	"github.com/raykavin/gobox/oidcauth"
)

const (
	claimsKey = "oidc_claims"

	// AccessTokenCookie is the cookie name handler.Auth writes on login/
	// refresh and this middleware reads on every request. Exported so both
	// sides share a single name instead of duplicating the literal.
	AccessTokenCookie = "access_token"

	accessTokenChunksCountSuffix = ".__chunks"
	accessTokenChunkSeparator    = ".__chunk."

	// maxCookieValueBytes keeps a single cookie's value comfortably under
	// the ~4096-byte-per-cookie limit browsers enforce. Exceeding it doesn't
	// error: the browser silently drops the whole Set-Cookie, which is why
	// handler.Auth chunks anything larger instead of risking that.
	maxCookieValueBytes = 3500
)

var (
	ErrMissingToken               = errors.New("authorization: missing token")
	ErrForbidden                  = errors.New("authorization: token supplied via both header and cookie")
	ErrMissingAuthorizationHeader = errors.New("authorization: missing Authorization header")
	ErrInvalidAuthorizationFormat = errors.New("authorization: invalid Authorization header format")
	ErrEmptyToken                 = errors.New("authorization: empty bearer token")
	ErrInvalidToken               = errors.New("authorization: invalid token")
	ErrRoleContextConflict        = errors.New("authorization: role context key conflict")
	ErrPermissionDenied           = errors.New("authorization: permission denied")
)

// RoleContext injects extra context values when the caller holds RoleName.
type RoleContext struct {
	RoleName string
	Values   map[string]any
}

// TokenVerifier validates a bearer token and returns its claims. Declared
// here rather than in internal/port: *oidcauth.OIDC already satisfies it
// with no wrapper, and tests can supply a fake.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (oidcauth.Claims, error)
	HasRole(claims oidcauth.Claims, role string) bool
}

// Authorization resolves and verifies the caller's token, then stores its
// claims in the context (see ClaimsFromContext). Aborts 401 if the token
// is missing/malformed/invalid, 403 if supplied via both header and cookie.
func Authorization(verifier TokenVerifier) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, err := extractToken(ctx)
		if err != nil {
			if errors.Is(err, ErrForbidden) {
				respond.Forbidden(ctx, respond.NewError(
					"ERR_TOKEN_CONFLICT",
					"Authorization token supplied via both header and cookie",
				))
				return
			}
			respond.Unauthorized(ctx, respond.NewError(
				"ERR_MISSING_TOKEN",
				"Authorization token is missing or malformed",
			))
			return
		}

		claims, err := verifier.Verify(ctx.Request.Context(), token)
		if err != nil {
			respond.Unauthorized(ctx, respond.NewError(
				"ERR_INVALID_TOKEN",
				"Authorization token is invalid or expired",
			))
			return
		}

		ctx.Set(claimsKey, claims)
		ctx.Next()
	}
}

// RequireRole aborts unless the caller holds role, then injects the values
// of any applicable extras. Must run after Authorization. Aborts 401 if
// claims are absent, 403 if role is missing, 500 on a RoleContext conflict.
func RequireRole(verifier TokenVerifier, role string, extras ...RoleContext) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		claims, ok := ClaimsFromContext(ctx)
		if !ok {
			respond.Unauthorized(ctx, respond.NewError("ERR_INVALID_TOKEN",
				"Authorization token is invalid or expired"))
			return
		}

		if !verifier.HasRole(claims, role) {
			respond.Forbidden(ctx, respond.NewError("ERR_PERMISSION_DENIED",
				"You do not have permission to access this resource"))
			return
		}

		if err := applyRoleContexts(ctx, verifier, claims, extras); err != nil {
			respond.InternalServerError(ctx, respond.NewError(
				"ERR_ROLE_CONTEXT_CONFLICT",
				"Conflicting role context configuration",
			))
			return
		}

		ctx.Next()
	}
}

// applyRoleContexts stages the values of every applicable entry, failing
// with ErrRoleContextConflict if two disagree on a key, then commits them
// to the context so the outcome never depends on slice order.
func applyRoleContexts(ctx *gin.Context, verifier TokenVerifier, claims oidcauth.Claims, extras []RoleContext) error {
	staged := make(map[string]any)
	for _, e := range extras {
		if !verifier.HasRole(claims, e.RoleName) {
			continue
		}
		for k, v := range e.Values {
			if existing, seen := staged[k]; seen && existing != v {
				return fmt.Errorf("%w: key %q", ErrRoleContextConflict, k)
			}
			staged[k] = v
		}
	}
	for k, v := range staged {
		ctx.Set(k, v)
	}
	return nil
}

// ClaimsFromContext retrieves the claims Authorization stored. ok is false
// if Authorization did not run or did not succeed for this request.
func ClaimsFromContext(ctx *gin.Context) (oidcauth.Claims, bool) {
	v, ok := ctx.Get(claimsKey)
	if !ok {
		return oidcauth.Claims{}, false
	}
	claims, ok := v.(oidcauth.Claims)
	return claims, ok
}

// extractToken resolves the bearer token, preferring the access_token
// cookie (browsers/WebSocket can't set Authorization) over the Bearer
// header. Supplying both is rejected (ErrForbidden) to prevent token
// confusion: one channel must not silently win over the other.
func extractToken(ctx *gin.Context) (string, error) {
	cookie, hasCookie := readAccessTokenCookie(ctx)
	bearer, bearerErr := extractBearerToken(ctx.GetHeader("Authorization"))
	hasBearer := bearerErr == nil && bearer != ""

	switch {
	case hasCookie && hasBearer:
		return "", ErrForbidden
	case hasCookie:
		return cookie, nil
	case hasBearer:
		return bearer, nil
	default:
		return "", ErrMissingToken
	}
}

// readAccessTokenCookie resolves access_token, reassembling the chunk
// cookies (access_token.__chunks + .__chunk.0, .1, ...) if a value ever
// needs to be split to stay under the ~4KB per-cookie limit. handler.Auth
// currently always writes a single, unchunked cookie; this reassembly path
// exists for a future token large enough to require it.
func readAccessTokenCookie(ctx *gin.Context) (string, bool) {
	if value, err := ctx.Cookie(AccessTokenCookie); err == nil && value != "" {
		return decodeCookieValue(value), true
	}

	rawCount, err := ctx.Cookie(AccessTokenCookie + accessTokenChunksCountSuffix)
	if err != nil || rawCount == "" {
		return "", false
	}

	chunkCount, err := strconv.Atoi(rawCount)
	if err != nil || chunkCount <= 0 {
		return "", false
	}

	var sb strings.Builder
	for i := range chunkCount {
		chunkName := fmt.Sprintf("%s%s%d",
			AccessTokenCookie,
			accessTokenChunkSeparator,
			i,
		)
		chunk, err := ctx.Cookie(chunkName)
		if err != nil {
			// Missing chunk: treat as absent rather than validate a truncated token.
			return "", false
		}
		_, _ = sb.WriteString(chunk)
	}

	return decodeCookieValue(sb.String()), true
}

// decodeCookieValue undoes any percent-encoding a cookie value may carry.
// A no-op for the plain base64url JWTs handler.Auth writes today, but kept
// for compatibility with any percent-encoded value a client might send.
func decodeCookieValue(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func extractBearerToken(authHeader string) (string, error) {
	if authHeader == "" {
		return "", ErrMissingAuthorizationHeader
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", ErrInvalidAuthorizationFormat
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", ErrEmptyToken
	}
	return token, nil
}
