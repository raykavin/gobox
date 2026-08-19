package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// Fallback lifetimes, used only when the token response or configuration
// carries no explicit value (defensive; Keycloak always sends one for
// access tokens, and usually for refresh tokens too via refresh_expires_in).
const (
	defaultAccessTokenTTL  = 5 * time.Minute
	defaultRefreshTokenTTL = 30 * 24 * time.Hour
)

// SessionManager centralizes session creation, resolution (including
// refresh-on-demand), and invalidation, so the login/callback handler, the
// authorization middleware, the explicit /auth/refresh endpoint, logout, and
// the periodic cleanup job all apply the exact same rule for what counts as
// expired and when to refresh. No caller outside this type should decide
// that on its own.
type SessionManager struct {
	store SessionStore
	flow  *Flow

	// verifier, when set, revalidates the stored access token on every
	// Resolve including the fast path where the token has not locally
	// expired yet so a token Keycloak now considers inactive (the user
	// was disabled/locked, the token was revoked, the SSO session ended)
	// is caught well before the access token's own exp claim would
	// otherwise force a refresh. Nil disables this revalidation: a session
	// is then only ever rejected once its access token has locally expired
	// and refresh fails, which can leave a revoked-but-not-yet-expired
	// session usable for however long the access token's own lifetime is.
	// Pass an *OIDC (with introspection enabled) here in production; its
	// own caching (WithCache and/or Config.IntrospectionCacheTTL) bounds
	// how often this actually reaches the issuer.
	verifier ClaimsVerifier

	// clientID is used to evaluate HasRole against resource_access, mirroring
	// OIDC.HasRole so SessionManager can stand in for a TokenVerifier.
	clientID string

	// idleTimeout, if non-zero, expires a session that has not authenticated
	// a request in this long, even though ExpiresAt has not been reached yet.
	idleTimeout time.Duration

	// absoluteTimeout, if non-zero, caps a session's lifetime at CreatedAt +
	// absoluteTimeout regardless of how long the refresh token itself would
	// otherwise remain valid.
	absoluteTimeout time.Duration

	// now is time.Now, overridable in tests.
	now func() time.Time
}

// SessionManagerConfig configures a SessionManager.
type SessionManagerConfig struct {
	// Store persists sessions. Required.
	Store SessionStore

	// Flow performs the refresh_token grant against the issuer. Required.
	Flow *Flow

	// Verifier, when set, revalidates the stored access token against the
	// issuer on every Resolve call, including the fast (not-yet-locally-
	// expired) path see SessionManager.verifier's doc comment for why
	// this matters. An *OIDC value (with introspection enabled) satisfies
	// this directly. Strongly recommended in production: without it, a
	// user disabled or revoked in the identity provider keeps a live
	// session until its access token naturally expires and a refresh is
	// attempted.
	Verifier ClaimsVerifier

	// ClientID is the OAuth client ID used to evaluate HasRole against a
	// session's claims (resource_access[ClientID].roles).
	ClientID string

	// IdleTimeout expires a session after this long without an authenticated
	// request, even if it would otherwise still be valid. Zero disables
	// idle expiry.
	IdleTimeout time.Duration

	// AbsoluteTimeout caps every session at CreatedAt + AbsoluteTimeout,
	// regardless of the refresh token's own lifetime. Zero means the only
	// ceiling is the one the identity provider itself grants (the refresh
	// token's lifetime, or DefaultRefreshTokenTTL when the issuer does not
	// report one).
	AbsoluteTimeout time.Duration
}

// NewSessionManager builds a SessionManager. Panics if Store or Flow is nil,
// since a manager that cannot persist or refresh sessions can never do
// anything useful.
func NewSessionManager(cfg SessionManagerConfig) *SessionManager {
	if cfg.Store == nil {
		panic("oidcauth: session store cannot be nil")
	}
	if cfg.Flow == nil {
		panic("oidcauth: flow cannot be nil")
	}
	return &SessionManager{
		store:           cfg.Store,
		flow:            cfg.Flow,
		verifier:        cfg.Verifier,
		clientID:        cfg.ClientID,
		idleTimeout:     cfg.IdleTimeout,
		absoluteTimeout: cfg.AbsoluteTimeout,
		now:             time.Now,
	}
}

// Create persists a new session for a freshly obtained token set and its
// already-extracted claims, and returns it. The session ID is a
// cryptographically random UUID v4 (github.com/google/uuid, backed by
// crypto/rand), never sequential or derived from anything predictable.
func (m *SessionManager) Create(ctx context.Context, token *oauth2.Token, claims Claims) (*Session, error) {
	now := m.now()

	s := &Session{
		ID:        uuid.NewString(),
		Tokens:    tokenSetFrom(token, TokenSet{}),
		Claims:    claims,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.ExpiresAt = m.computeExpiresAt(s)

	if err := m.store.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("oidcauth: create session: %w", err)
	}
	return s, nil
}

// Resolve loads the session identified by id, transparently refreshing its
// access token if it has expired but the session itself has not, and
// enforces expiry (absolute and, if configured, idle) on every call. It is
// the single place that decides whether a session is still usable the
// authorization middleware, /auth/refresh, and anything else resolving a
// session ID must call this rather than reimplementing the rule.
//
// If a Verifier was configured, Resolve also revalidates the access token
// against the issuer on every call including when the token has not
// locally expired yet so a token the issuer no longer considers active
// (the user was disabled/locked in the identity provider, the token was
// revoked, the underlying SSO session ended) is rejected immediately
// instead of remaining usable until the access token's own exp claim
// forces a refresh. Without a Verifier, Resolve only ever checks the
// locally stored expiry, which can leave a revoked-but-not-yet-expired
// session usable for however long the access token's own lifetime is.
//
// Any failure (not found, expired, idle-timed-out, refresh rejected,
// revalidation rejected) deletes the session (best-effort the periodic
// cleanup job is the backstop if that delete itself fails) and returns an
// error wrapping ErrSessionNotFound.
func (m *SessionManager) Resolve(ctx context.Context, id string) (*Session, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("%w: malformed session id", ErrSessionNotFound)
	}

	now := m.now()

	// Fast path: a plain read, no row lock. This keeps the common
	// (non-refreshing) authenticated request down to a single indexed
	// lookup instead of a transaction.
	s, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, m.notFound(err)
	}

	if expired := m.isExpired(s, now); expired {
		_ = m.store.Delete(ctx, id)
		return nil, fmt.Errorf("%w: expired", ErrSessionNotFound)
	}

	if now.Before(s.Tokens.AccessTokenExpiresAt) {
		if m.verifier != nil {
			if _, err := m.verifier.Verify(ctx, s.Tokens.AccessToken); err != nil {
				_ = m.store.Delete(ctx, id)
				return nil, fmt.Errorf("%w: revalidation rejected: %w", ErrSessionNotFound, err)
			}
		}
		_ = m.store.Touch(ctx, id, now) // best-effort; a missed touch only makes idle expiry slightly stricter next time
		s.LastSeenAt = now
		return s, nil
	}

	return m.refresh(ctx, id, now)
}

// refresh renews the access token for id under a per-session lock, so
// concurrent requests for the same session cannot race the issuer with
// duplicate refresh_token grants (Keycloak rotates and invalidates the
// previous refresh token on every use). A refresh rejected by the issuer
// (invalid_grant, revoked token, ended SSO session, or any other error)
// deletes the session and reports it as not found, same as any other
// unrecoverable condition.
func (m *SessionManager) refresh(ctx context.Context, id string, now time.Time) (*Session, error) {
	var resolved *Session

	err := m.store.WithLock(ctx, id, func(ctx context.Context, locked *Session) (*Session, error) {
		if now.Before(locked.Tokens.AccessTokenExpiresAt) {
			// Another request already refreshed it while this one waited
			// for the lock; nothing left to do.
			resolved = locked
			return nil, nil
		}
		if expired := m.isExpired(locked, now); expired {
			return nil, fmt.Errorf("%w: expired", ErrSessionNotFound)
		}
		if locked.Tokens.RefreshToken == "" {
			return nil, fmt.Errorf("%w: access token expired and session has no refresh token", ErrSessionNotFound)
		}

		token, err := m.flow.Refresh(ctx, locked.Tokens.RefreshToken)
		if err != nil {
			// Any refresh failure (invalid_grant, revoked, ended SSO
			// session, or a transient issuer error) is treated the same
			// way: the session can no longer be trusted, so it is dropped
			// rather than left around to fail the same way on every
			// subsequent request.
			return nil, fmt.Errorf("%w: refresh rejected", ErrSessionNotFound)
		}

		locked.Tokens = tokenSetFrom(token, locked.Tokens)
		locked.ExpiresAt = m.computeExpiresAt(locked)
		locked.UpdatedAt = now
		locked.LastSeenAt = now
		resolved = locked
		return locked, nil
	})

	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			_ = m.store.Delete(ctx, id)
		}
		return nil, m.notFound(err)
	}
	return resolved, nil
}

// Peek reads a session without enforcing expiry, refreshing it, or updating
// LastSeenAt. It exists solely for logout, which needs the stored ID token
// for federated (RP-initiated) logout even when the session is otherwise
// already expired, and must not fail loudly (or trigger a refresh) just
// because the session is gone or stale.
func (m *SessionManager) Peek(ctx context.Context, id string) (*Session, error) {
	return m.store.Get(ctx, id)
}

// Delete removes a session. Idempotent: deleting an already-gone session is
// not an error.
func (m *SessionManager) Delete(ctx context.Context, id string) error {
	return m.store.Delete(ctx, id)
}

// CleanupExpired removes every session whose ExpiresAt is at or before now
// and reports how many were removed. See RunSessionCleanup for a ready-made
// periodic runner.
func (m *SessionManager) CleanupExpired(ctx context.Context, now time.Time) (int64, error) {
	return m.store.DeleteExpired(ctx, now)
}

// Verify makes SessionManager satisfy the same interface OIDC does
// (Verify(ctx, token) (Claims, error)), so it can be passed directly to
// middlewares.Authorization / middlewares.RequireRole in place of an
// *oidcauth.OIDC verifying raw JWTs. token here is a session ID, not a JWT.
func (m *SessionManager) Verify(ctx context.Context, sessionID string) (Claims, error) {
	s, err := m.Resolve(ctx, sessionID)
	if err != nil {
		return Claims{}, err
	}
	return s.Claims, nil
}

// HasRole mirrors OIDC.HasRole so SessionManager can stand in for a
// TokenVerifier wherever role checks are needed.
func (m *SessionManager) HasRole(claims Claims, role string) bool {
	clientRoles, ok := claims.ResourceAccess[m.clientID]
	if !ok {
		return false
	}
	return slices.Contains(clientRoles["roles"], role)
}

// isExpired reports whether s is unusable for reasons Resolve/refresh must
// treat identically: past its absolute ceiling, or (if configured) idle for
// longer than idleTimeout.
func (m *SessionManager) isExpired(s *Session, now time.Time) bool {
	if !now.Before(s.ExpiresAt) {
		return true
	}
	if m.idleTimeout > 0 && !s.LastSeenAt.IsZero() && now.Sub(s.LastSeenAt) > m.idleTimeout {
		return true
	}
	return false
}

// computeExpiresAt is the single formula for a session's absolute,
// non-extendable ceiling. It is never later than what the identity provider
// itself authorized (the refresh token's own lifetime, or
// defaultRefreshTokenTTL from creation when the issuer does not report one),
// and never later than CreatedAt + AbsoluteTimeout when AbsoluteTimeout is
// configured. Called both when a session is created and every time it is
// refreshed, so a session's ceiling can only ever stay the same or shrink,
// never move later than what was authorized at creation.
func (m *SessionManager) computeExpiresAt(s *Session) time.Time {
	ceiling := s.Tokens.AccessTokenExpiresAt
	switch {
	case s.Tokens.RefreshToken == "":
		// No refresh possible: the access token's own expiry is the
		// session's ceiling too.
	case !s.Tokens.RefreshTokenExpiresAt.IsZero():
		ceiling = s.Tokens.RefreshTokenExpiresAt
	default:
		ceiling = s.CreatedAt.Add(defaultRefreshTokenTTL)
	}

	if m.absoluteTimeout > 0 {
		if cap := s.CreatedAt.Add(m.absoluteTimeout); cap.Before(ceiling) {
			ceiling = cap
		}
	}
	return ceiling
}

// notFound normalizes any resolution failure to wrap ErrSessionNotFound, so
// callers can rely on errors.Is(err, ErrSessionNotFound) regardless of the
// underlying cause.
func (m *SessionManager) notFound(err error) error {
	if errors.Is(err, ErrSessionNotFound) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrSessionNotFound, err)
}

// tokenSetFrom builds a TokenSet from a freshly issued or refreshed token,
// falling back to the previous TokenSet's value for anything the issuer's
// response omits (Keycloak always rotates the refresh token and re-sends
// id_token/scope, but this stays defensive for issuers that don't).
func tokenSetFrom(token *oauth2.Token, previous TokenSet) TokenSet {
	ts := TokenSet{
		AccessToken:           token.AccessToken,
		TokenType:             token.TokenType,
		AccessTokenExpiresAt:  token.Expiry,
		RefreshToken:          previous.RefreshToken,
		IDToken:               previous.IDToken,
		RefreshTokenExpiresAt: previous.RefreshTokenExpiresAt,
		Scopes:                previous.Scopes,
	}

	if ts.AccessTokenExpiresAt.IsZero() {
		ts.AccessTokenExpiresAt = time.Now().Add(defaultAccessTokenTTL)
	}
	if token.RefreshToken != "" {
		ts.RefreshToken = token.RefreshToken
	}
	if idToken, ok := token.Extra("id_token").(string); ok && idToken != "" {
		ts.IDToken = idToken
	}
	if secs, ok := token.Extra("refresh_expires_in").(float64); ok && secs > 0 {
		ts.RefreshTokenExpiresAt = time.Now().Add(time.Duration(secs) * time.Second)
	}
	if scope, ok := token.Extra("scope").(string); ok && scope != "" {
		ts.Scopes = strings.Fields(scope)
	}

	return ts
}
