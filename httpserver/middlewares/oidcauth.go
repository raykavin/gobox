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
	Path     string
	Secure   bool
	SameSite http.SameSite
}

// Auth handles the server-side OIDC Authorization Code + PKCE login flow: it
// talks to the issuer on the browser's behalf, keeps the resulting OIDC
// credentials exclusively server-side in a session (see
// oidcauth.SessionManager), and exposes the browser only an opaque session
// identifier as an HttpOnly cookie never an access, refresh, or ID token.
// The PKCE verifier and return path travel inside the signed state
// parameter, not a cookie (see loginState).
type Auth struct {
	flow                  *oidcauth.Flow
	sessions              *oidcauth.SessionManager
	verifier              oidcauth.ClaimsVerifier
	cookies               AuthCookieOptions
	clientID              string
	postLoginRedirectURI  string
	postLogoutRedirectURI string
	stateSigningKey       []byte
}

// NewAuth builds the handler. stateSigningKey authenticates the login state
// parameter; passing the OIDC client secret works, since it is already
// server-only secret material. Missing required arguments panic rather than
// degrade, so a misconfigured handler never starts serving traffic.
//
// verifier extracts identity claims from the token Callback just exchanged,
// once, at login time (an *oidcauth.OIDC satisfies this with no wrapper).
// sessions owns every session's lifecycle afterwards: creation, expiry,
// refresh, and deletion.
func NewAuth(
	flow *oidcauth.Flow,
	verifier oidcauth.ClaimsVerifier,
	sessions *oidcauth.SessionManager,
	clientID string,
	cookies AuthCookieOptions,
	postLoginRedirectURI string,
	postLogoutRedirectURI string,
	stateSigningKey string,
) *Auth {
	if flow == nil {
		panic("oidc flow cannot be nil")
	}
	if verifier == nil {
		panic("claims verifier cannot be nil")
	}
	if sessions == nil {
		panic("session manager cannot be nil")
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
		flow:                  flow,
		verifier:              verifier,
		sessions:              sessions,
		clientID:              clientID,
		cookies:               cookies,
		postLoginRedirectURI:  postLoginRedirectURI,
		postLogoutRedirectURI: postLogoutRedirectURI,
		stateSigningKey:       []byte(stateSigningKey),
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
// the code for tokens, extracts the claims the application needs, creates the
// server-side session, writes the session cookie, and redirects the browser
// to the path Login encoded into state. The exchanged tokens never leave
// this handler: only the opaque session ID reaches the browser.
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

	claims, err := h.verifier.Verify(ctx.Request.Context(), token.AccessToken)
	if err != nil {
		respond.Unauthorized(ctx, respond.NewError(
			"ERR_OIDC_CLAIMS",
			"Failed to read identity claims",
		))
		return
	}

	session, err := h.sessions.Create(ctx.Request.Context(), token, claims)
	if err != nil {
		respond.InternalServerError(ctx, respond.NewError(
			"ERR_SESSION_CREATE",
			"Failed to create session",
		))
		return
	}

	h.applySession(ctx, session)
	ctx.Redirect(http.StatusFound, h.resolveReturnTo(loginState.ReturnTo))
}

// Refresh re-resolves the caller's session transparently renewing its
// access token if it has expired and the session itself has not and
// rewrites the session cookie with the (possibly extended) expiry. A session
// that cannot be resolved (expired, revoked, or refresh rejected by the
// issuer) is cleared, so the browser stops sending credentials the issuer no
// longer honours.
func (h *Auth) Refresh(ctx *gin.Context) {
	noStore(ctx)
	sessionID, err := ctx.Cookie(SessionCookie)
	if err != nil || sessionID == "" {
		respond.Unauthorized(ctx, respond.NewError(
			"ERR_MISSING_SESSION",
			"No session cookie present",
		))
		return
	}

	session, err := h.sessions.Resolve(ctx.Request.Context(), sessionID)
	if err != nil {
		h.clearSessionCookie(ctx)
		respond.Unauthorized(ctx, respond.NewError(
			"ERR_SESSION_INVALID",
			"Session is invalid or expired",
		))
		return
	}

	h.applySession(ctx, session)
	respond.NoContent(ctx)
}

// Logout deletes the caller's session, clears every auth cookie, and returns
// the issuer's RP-Initiated Logout URL so the frontend can navigate there and
// end the SSO session too. Safe and idempotent to call with no session, an
// already expired session, or no cookie at all it always clears cookies and
// always succeeds.
func (h *Auth) Logout(ctx *gin.Context) {
	noStore(ctx)
	sessionID, _ := ctx.Cookie(SessionCookie)

	var idToken string
	if sessionID != "" {
		// Peek, not Resolve: logout must succeed even for a session that is
		// already expired or otherwise unresolvable, and must never trigger
		// a refresh just to read the ID token on the way out.
		if s, err := h.sessions.Peek(ctx.Request.Context(), sessionID); err == nil {
			idToken = s.Tokens.IDToken
		}
		_ = h.sessions.Delete(ctx.Request.Context(), sessionID)
	}

	logoutURL := h.flow.EndSessionURL(idToken, h.postLogoutRedirectURI)
	if logoutURL == "" {
		logoutURL = h.postLogoutRedirectURI
	}

	h.clearSessionCookie(ctx)

	respond.OK(ctx, gin.H{"logout_url": logoutURL})
}

// Me returns the caller's identity and client roles, read from the claims the
// authorization middleware already resolved for this request.
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

// applySession writes the session cookie (session.ID, expiring with the
// session) and a fresh CSRF double-submit cookie alongside it.
func (h *Auth) applySession(ctx *gin.Context, session *oidcauth.Session) {
	maxAge := time.Until(session.ExpiresAt)
	if maxAge < 0 {
		maxAge = 0
	}
	h.setCookie(ctx, SessionCookie, session.ID, maxAge, true)

	// Readable by JS on purpose: the frontend echoes it back in a header for
	// the CSRF middleware to compare against this cookie.
	h.setCookie(ctx, CSRFCookie, randomToken(24), maxAge, false)
}

func (h *Auth) clearSessionCookie(ctx *gin.Context) {
	h.clearCookie(ctx, SessionCookie)
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
