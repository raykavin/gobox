package middlewares

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/httpserver/respond"
	"github.com/raykavin/gobox/oidcauth"
	"golang.org/x/oauth2"
)

// Cookie names Auth writes but nothing else reads. AccessTokenCookie and
// CSRFCookie are exported instead, since the authorization and CSRF
// middlewares in this package read them on every request.
const (
	refreshTokenCookie = "refresh_token"
	idTokenCookie      = "id_token"
)

// loginStateTTL bounds how long a login attempt has to complete the redirect
// round trip to the issuer and back before its signed state expires.
const loginStateTTL = 10 * time.Minute

var errInvalidLoginState = errors.New("middlewares: invalid or expired login state")

// loginState is the payload Login encodes into the OAuth2 state parameter and
// Callback reads back out of it: the PKCE verifier, the post-login path, and
// an expiry. Keeping it in state (echoed back verbatim by the issuer) means
// Callback needs no cookie to survive the cross-site redirect back.
type loginState struct {
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
	Exp      int64  `json:"e"`
}

// MeResponse is the identity payload Me returns: enough for a frontend to
// render the signed-in user and drive UX-only permission gates without ever
// decoding a JWT itself. Real authorization stays server-side.
type MeResponse struct {
	Sub               string   `json:"sub"`
	Email             string   `json:"email"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	ClientRoles       []string `json:"client_roles"`
}

// AuthCookieOptions carries the attributes applied to every cookie Auth writes.
type AuthCookieOptions struct {
	Domain   string
	Secure   bool
	SameSite http.SameSite
	// Path applies to every cookie Auth writes. One shared path, defaulting
	// to "/", stays correct behind any reverse-proxy prefix in front of the
	// API; narrower per-cookie paths would have to account for that prefix,
	// which Auth cannot see.
	Path string
}

// Auth handles the server-side OIDC Authorization Code + PKCE login flow: it
// talks to the issuer on the browser's behalf and exposes the result only as
// HttpOnly cookies, never in a response body. The PKCE verifier and return
// path travel inside the signed state parameter, not a cookie (see loginState).
type Auth struct {
	flow                  *oidcauth.Flow
	clientID              string
	cookies               AuthCookieOptions
	postLoginRedirectURI  string
	postLogoutRedirectURI string
	stateSigningKey       []byte

	// Fallback TTLs, used only when the token response carries no explicit
	// lifetime for that token (defensive; Keycloak always sends one).
	accessCookieDefaultTTL  time.Duration
	refreshCookieDefaultTTL time.Duration
}

// NewAuth builds the handler. stateSigningKey authenticates the login state
// parameter; passing the OIDC client secret works, since it is already
// server-only secret material. Missing required arguments panic rather than
// degrade, so a misconfigured handler never starts serving traffic.
func NewAuth(
	flow *oidcauth.Flow,
	clientID string,
	cookies AuthCookieOptions,
	postLoginRedirectURI string,
	postLogoutRedirectURI string,
	stateSigningKey string,
) *Auth {
	if flow == nil {
		panic("oidc flow cannot be nil")
	}
	if clientID == "" {
		panic("client id cannot be empty")
	}
	if stateSigningKey == "" {
		panic("state signing key cannot be empty")
	}
	if cookies.Path == "" {
		cookies.Path = "/"
	}
	if cookies.SameSite == 0 {
		cookies.SameSite = http.SameSiteLaxMode
	}
	return &Auth{
		flow:                    flow,
		clientID:                clientID,
		cookies:                 cookies,
		postLoginRedirectURI:    postLoginRedirectURI,
		postLogoutRedirectURI:   postLogoutRedirectURI,
		stateSigningKey:         []byte(stateSigningKey),
		accessCookieDefaultTTL:  5 * time.Minute,
		refreshCookieDefaultTTL: 30 * 24 * time.Hour,
	}
}

// Login redirects the browser to the issuer's hosted login page, carrying the
// PKCE verifier and the post-login path inside the signed state parameter. The
// optional redirect_uri query parameter is honoured only as a relative path
// (see sanitizeReturnTo).
func (h *Auth) Login(ctx *gin.Context) {
	noStore(ctx)
	returnTo := sanitizeReturnTo(ctx.Query("redirect_uri"))
	verifier := oauth2.GenerateVerifier()

	state := h.encodeState(verifier, returnTo)
	authURL := h.flow.AuthCodeURL(state, verifier)

	ctx.Redirect(http.StatusFound, authURL)
}

// Callback completes the login flow: it validates the signed state, exchanges
// the code for tokens, writes the session cookies, and redirects the browser
// to the path Login encoded into state.
func (h *Auth) Callback(ctx *gin.Context) {
	noStore(ctx)

	if idpErr := ctx.Query("error"); idpErr != "" {
		respond.BadRequest(ctx, respond.NewError(
			"ERR_OIDC_CALLBACK",
			ctx.Query("error_description"),
		))
		return
	}

	loginState, err := h.decodeState(ctx.Query("state"))
	if err != nil {
		respond.BadRequest(ctx, respond.NewError(
			"ERR_OIDC_STATE",
			"Invalid or expired login state",
		))
		return
	}

	code := ctx.Query("code")
	if code == "" {
		respond.BadRequest(ctx, respond.NewError(
			"ERR_OIDC_CALLBACK",
			"Missing authorization code",
		))
		return
	}

	token, err := h.flow.Exchange(ctx.Request.Context(), code, loginState.Verifier)
	if err != nil {
		respond.Unauthorized(ctx, respond.NewError(
			"ERR_OIDC_EXCHANGE",
			"Failed to exchange authorization code",
		))
		return
	}

	h.applyToken(ctx, token)
	ctx.Redirect(http.StatusFound, h.resolveReturnTo(loginState.ReturnTo))
}

// Refresh trades the refresh_token cookie for a new token set and rewrites the
// session cookies. A rejected refresh clears them, so the browser stops
// retrying with credentials the issuer no longer honours.
func (h *Auth) Refresh(ctx *gin.Context) {
	noStore(ctx)
	refreshToken, err := ctx.Cookie(refreshTokenCookie)
	if err != nil || refreshToken == "" {
		respond.Unauthorized(ctx, respond.NewError(
			"ERR_MISSING_REFRESH_TOKEN",
			"No refresh token cookie present",
		))
		return
	}

	token, err := h.flow.Refresh(ctx.Request.Context(), refreshToken)
	if err != nil {
		h.clearAuthCookies(ctx)
		respond.Unauthorized(ctx, respond.NewError(
			"ERR_REFRESH_FAILED",
			"Refresh token is invalid or expired",
		))
		return
	}

	h.applyToken(ctx, token)
	respond.NoContent(ctx)
}

// Logout clears every auth cookie and returns the issuer's RP-Initiated Logout
// URL, so the frontend can navigate there and end the SSO session too. Falls
// back to the post-logout URI when the issuer advertises no end-session
// endpoint; the local cookies are cleared either way.
func (h *Auth) Logout(ctx *gin.Context) {
	noStore(ctx)
	idToken, _ := ctx.Cookie(idTokenCookie)

	logoutURL := h.flow.EndSessionURL(idToken, h.postLogoutRedirectURI)
	if logoutURL == "" {
		logoutURL = h.postLogoutRedirectURI
	}

	h.clearAuthCookies(ctx)

	respond.OK(ctx, gin.H{"logout_url": logoutURL})
}

// Me returns the caller's identity and client roles, read from the claims the
// authorization middleware already validated for this request.
func (h *Auth) Me(ctx *gin.Context) {
	noStore(ctx)
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		respond.Unauthorized(ctx, respond.NewError(
			"ERR_INVALID_TOKEN",
			"Authorization token is invalid or expired",
		))
		return
	}

	respond.OK(ctx, MeResponse{
		Sub:               claims.Sub,
		Email:             claims.Email,
		Name:              claims.Name,
		PreferredUsername: claims.PreferredUsername,
		ClientRoles:       claims.ResourceAccess[h.clientID]["roles"],
	})
}

// applyToken writes the session cookies from a freshly issued or refreshed
// token set. A response that omits refresh_token (Keycloak always rotates it,
// other issuers may not) leaves the existing refresh_token cookie in place
// rather than clearing a session that is still valid.
func (h *Auth) applyToken(ctx *gin.Context, token *oauth2.Token) {
	accessTTL := time.Until(token.Expiry)
	if accessTTL <= 0 {
		accessTTL = h.accessCookieDefaultTTL
	}
	h.setCookie(ctx, AccessTokenCookie, token.AccessToken, accessTTL, true)

	refreshTTL := h.refreshCookieDefaultTTL
	if secs, ok := token.Extra("refresh_expires_in").(float64); ok && secs > 0 {
		refreshTTL = time.Duration(secs) * time.Second
	}
	if token.RefreshToken != "" {
		h.setCookie(ctx, refreshTokenCookie, token.RefreshToken, refreshTTL, true)
	}

	if idToken, ok := token.Extra("id_token").(string); ok && idToken != "" {
		h.setCookie(ctx, idTokenCookie, idToken, refreshTTL, true)
	}

	// Readable by JS on purpose: the frontend echoes it back in a header for
	// the CSRF middleware to compare against this cookie.
	h.setCookie(ctx, CSRFCookie, randomToken(24), refreshTTL, false)
}

// clearAuthCookies removes every cookie a signed-in session relies on.
func (h *Auth) clearAuthCookies(ctx *gin.Context) {
	h.clearCookie(ctx, AccessTokenCookie)
	h.clearCookie(ctx, refreshTokenCookie)
	h.clearCookie(ctx, idTokenCookie)
	h.clearCookie(ctx, CSRFCookie)
}

// noStore forbids caching of a response that carries or depends on session
// cookies. Without it, a cached login redirect or callback result replayed by
// an intermediary could resurrect an already-consumed state/code pair.
func noStore(ctx *gin.Context) {
	ctx.Header("Cache-Control", "no-store")
}

func (h *Auth) setCookie(ctx *gin.Context, name, value string, maxAge time.Duration, httpOnly bool) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     h.cookies.Path,
		Domain:   h.cookies.Domain,
		MaxAge:   int(maxAge.Seconds()),
		Secure:   h.cookies.Secure,
		HttpOnly: httpOnly,
		SameSite: h.cookies.SameSite,
	})
}

func (h *Auth) clearCookie(ctx *gin.Context, name string) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     h.cookies.Path,
		Domain:   h.cookies.Domain,
		MaxAge:   -1,
		Secure:   h.cookies.Secure,
		SameSite: h.cookies.SameSite,
	})
}

// randomToken returns nBytes of entropy, base64url-encoded. It panics on
// failure, matching oauth2.GenerateVerifier's convention: a crypto/rand read
// only fails when the OS entropy source is broken, which is not something a
// request handler can recover from.
func randomToken(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		panic("middlewares: failed to generate random token: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// sanitizeReturnTo restricts the post-login target to a same-origin relative
// path, closing an open redirect through a crafted redirect_uri such as an
// absolute URL or a protocol-relative "//evil.example.com".
func sanitizeReturnTo(raw string) string {
	if raw == "" || raw[0] != '/' || strings.HasPrefix(raw, "//") || strings.Contains(raw, "://") {
		return "/"
	}
	return raw
}

// resolveReturnTo makes the path sanitizeReturnTo produced absolute against
// the frontend's own origin. Redirecting with the bare relative path would
// resolve it against the API's origin instead, landing the browser back on the
// API rather than the frontend.
func (h *Auth) resolveReturnTo(returnTo string) string {
	base, err := url.Parse(h.postLoginRedirectURI)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return h.postLoginRedirectURI
	}
	rel, err := url.Parse(returnTo)
	if err != nil {
		return h.postLoginRedirectURI
	}
	return base.ResolveReference(rel).String()
}

// encodeState serializes and HMAC-signs a loginState into an opaque, URL-safe
// string suitable for the OAuth2 state parameter.
func (h *Auth) encodeState(verifier, returnTo string) string {
	payload, err := json.Marshal(loginState{
		Verifier: verifier,
		ReturnTo: returnTo,
		Exp:      time.Now().Add(loginStateTTL).Unix(),
	})
	if err != nil {
		// loginState is a static, marshalable struct; this cannot fail.
		panic("middlewares: marshal login state: " + err.Error())
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + h.signState(encoded)
}

// decodeState verifies raw's signature and expiry and returns the loginState
// it carries, erroring out if raw is malformed, tampered with, or stale. The
// signature is compared in constant time so a forged state can't be tuned
// byte by byte against timing feedback.
func (h *Auth) decodeState(raw string) (loginState, error) {
	encoded, sig, ok := strings.Cut(raw, ".")
	if !ok || encoded == "" || sig == "" {
		return loginState{}, errInvalidLoginState
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(h.signState(encoded))) != 1 {
		return loginState{}, errInvalidLoginState
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return loginState{}, errInvalidLoginState
	}
	var ls loginState
	if err := json.Unmarshal(payload, &ls); err != nil {
		return loginState{}, errInvalidLoginState
	}
	if ls.Verifier == "" || time.Now().Unix() > ls.Exp {
		return loginState{}, errInvalidLoginState
	}
	return ls, nil
}

func (h *Auth) signState(encoded string) string {
	mac := hmac.New(sha256.New, h.stateSigningKey)
	_, _ = mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
