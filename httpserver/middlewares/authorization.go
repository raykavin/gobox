package middlewares

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/httpserver/respond"
	"github.com/raykavin/gobox/oidcauth"
)

const (
	claimsKey = "oidc_claims"

	// SessionCookie is the cookie name handler.Auth writes on login/callback
	// and this middleware reads on every request. Its value is an opaque
	// session identifier (a UUID), never an OIDC token: exported so both
	// sides share a single name instead of duplicating the literal.
	SessionCookie = "session_id"
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

// extractToken resolves the caller's token, preferring the session_id
// cookie (browsers/WebSocket can't set Authorization) over the Bearer
// header. Supplying both is rejected (ErrForbidden) to prevent token
// confusion: one channel must not silently win over the other. The
// returned value is passed to TokenVerifier.Verify as-is; it does not
// validate its shape (a session ID is a UUID, but a verifier backed by
// something else may expect a different format), leaving that to the
// verifier itself.
func extractToken(ctx *gin.Context) (string, error) {
	cookie, hasCookie := readSessionCookie(ctx)
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

// readSessionCookie resolves the session_id cookie.
func readSessionCookie(ctx *gin.Context) (string, bool) {
	value, err := ctx.Cookie(SessionCookie)
	if err != nil || value == "" {
		return "", false
	}
	return value, true
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
