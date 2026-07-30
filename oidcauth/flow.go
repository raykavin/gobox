package oidcauth

import (
	"context"
	"fmt"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Flow performs the Authorization Code + PKCE exchange on behalf of the
// browser: it builds the authorize URL, trades the returned code for tokens,
// refreshes them, and builds the issuer's end-session (logout) URL.
type Flow struct {
	oauth2Config  oauth2.Config
	endSessionURL string
}

// FlowConfig holds the confidential-client credentials and the redirect URI
// registered with the issuer. Scopes may be left empty to use defaultScopes.
type FlowConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
}

// defaultScopes is used when FlowConfig.Scopes is empty.
var defaultScopes = []string{"openid", "profile", "email"}

// NewFlow discovers the issuer's endpoints and returns a ready-to-use Flow.
// end_session_endpoint is read straight from the raw discovery document
// because go-oidc's typed Provider does not expose that field.
func NewFlow(ctx context.Context, cfg FlowConfig) (*Flow, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc flow: discover provider: %w", err)
	}

	var discovery struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&discovery); err != nil {
		return nil, fmt.Errorf("oidc flow: read discovery claims: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = defaultScopes
	}

	return &Flow{
		oauth2Config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURI,
			Scopes:       scopes,
			Endpoint:     provider.Endpoint(),
		},
		endSessionURL: discovery.EndSessionEndpoint,
	}, nil
}

// AuthCodeURL returns the authorize URL to redirect the browser to, for the
// given state and PKCE verifier (see oauth2.GenerateVerifier). The caller must
// be able to recover verifier when Exchange runs; encoding it into state is
// more reliable than a cookie surviving the issuer's cross-site redirect.
func (f *Flow) AuthCodeURL(state, verifier string) string {
	return f.oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// Exchange trades an authorization code for a token set, presenting the PKCE
// verifier that was used to build the matching authorize URL.
func (f *Flow) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	token, err := f.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("oidc flow: exchange code: %w", err)
	}
	return token, nil
}

// Refresh trades a refresh token for a new token set. Issuers that rotate
// refresh tokens (Keycloak, for one) return a fresh value, so callers should
// reuse the previous one only when Token.RefreshToken comes back empty.
func (f *Flow) Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	src := f.oauth2Config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	token, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("oidc flow: refresh token: %w", err)
	}
	return token, nil
}

// EndSessionURL builds the issuer's RP-Initiated Logout URL, passing idToken
// as id_token_hint when it is non-empty. Returns "" if the issuer does not
// advertise an end_session_endpoint.
func (f *Flow) EndSessionURL(idToken, postLogoutRedirectURI string) string {
	if f.endSessionURL == "" {
		return ""
	}
	params := url.Values{
		"client_id":                {f.oauth2Config.ClientID},
		"post_logout_redirect_uri": {postLogoutRedirectURI},
	}
	if idToken != "" {
		params.Set("id_token_hint", idToken)
	}
	return f.endSessionURL + "?" + params.Encode()
}
