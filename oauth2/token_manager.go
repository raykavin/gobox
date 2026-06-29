package oauth2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/raykavin/gobox/httpclient"
)

// TokenAccess is a access token returned from the OAuth2 integration
type TokenAccess struct {
	AccessToken        string     `json:"access_token"`
	TokenType          string     `json:"token_type"`
	ExpiresIn          int        `json:"expires_in"`
	RefreshToken       string     `json:"refresh_token"`
	Scope              string     `json:"scope"`
	LastAuthentication *time.Time `json:"-"`
}

// TokenManager manages the life cycle and token's cache
type TokenManager struct {
	authUrl      string
	clientID     string
	clientSecret string
	grantType    string
	scope        string
	client       *http.Client
	cache        map[string]*TokenAccess
	authParams   map[string]string
	sendAsPost   bool
}

// NewTokenManager creates a new instance of token manager
func NewTokenManager(
	client *http.Client,
	clientID,
	clientSecret,
	grantType string,
) *TokenManager {
	tm := &TokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
		grantType:    grantType,
		scope:        "",
		authUrl:      "",
		client:       &http.Client{Timeout: 20 * time.Second},
		cache:        map[string]*TokenAccess{},
		authParams:   map[string]string{},
	}

	if client != nil {
		tm.client = client
	}

	return tm
}

// SetAuthorizationHeader injects in request the authorization header
// Format:
//   - "Authorization <token_type> <token_access>"
//
// Parameters
//   - ctx: Context for cancel operations
//   - r: A pointer of request to inject the header
//   - scope: The token scope for recover from cache or make new authentication if cache is empty
func (tm *TokenManager) SetAuthorizationHeader(ctx context.Context, r *http.Request, scope string) error {
	tokenAccess, err := tm.getAccessToken(ctx, scope)
	if err != nil {
		return err
	}

	r.Header.Set("Authorization", tokenAccess.TokenType+" "+tokenAccess.AccessToken)

	return nil
}

// GetAccessToken returns a string of access token data
func (tm *TokenManager) GetAccessToken(ctx context.Context, scope string) (string, error) {
	tokenAccess, err := tm.getAccessToken(ctx, scope)
	if err != nil {
		return "", err
	}

	return tokenAccess.AccessToken, nil
}

func (tm *TokenManager) GetTokenType(ctx context.Context, scope string) (string, error) {
	tokenAccess, err := tm.getAccessToken(ctx, scope)
	if err != nil {
		return "", err
	}

	return tokenAccess.TokenType, nil
}

// SendAsGet implements TokenManagerProvider.
func (tm *TokenManager) SendAsGet() {
	tm.sendAsPost = false
}

// SendAsPost implements TokenManagerProvider.
func (tm *TokenManager) SendAsPost() {
	tm.sendAsPost = true
}

// WithAuthenticationURL implements TokenManagerProvider.
func (tm *TokenManager) WithAuthenticationURL(url string) {
	tm.authUrl = url
}

// WithOptionalParams implements TokenManagerProvider.
func (tm *TokenManager) WithOptionalParams(params map[string]string) {
	tm.authParams = params
}

// authenticate requests a new OAuth2 token from the scope
func (tm *TokenManager) authenticate(ctx context.Context, scope string) error {
	u, err := url.Parse(tm.authUrl)
	if err != nil {
		return fmt.Errorf("invalid authentication URL: %v", err)
	}

	// Set url queries
	q := u.Query()
	q.Set("client_id", tm.clientID)
	q.Set("client_secret", tm.clientSecret)
	q.Set("grant_type", tm.grantType)
	q.Set("scope", scope)

	// Set optional queries if provided
	for k, v := range tm.authParams {
		q.Set(k, v)
	}

	// Defines the request send method
	sendMethod := http.MethodGet
	var requestBody io.Reader = nil

	// Create request
	if tm.sendAsPost {
		sendMethod = http.MethodPost
		requestBody = bytes.NewBufferString(q.Encode())
		u.RawQuery = ""
	} else {
		u.RawQuery = q.Encode()
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, sendMethod, u.String(), requestBody)
	if err != nil {
		return fmt.Errorf("failed to create authentication request: %v", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json; charset=utf-8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")

	// Set content type if POST method
	if tm.sendAsPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	// Send request
	resp, err := tm.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send authentication request: %v", err)
	}

	defer resp.Body.Close()

	reader, err := httpclient.DecompressResponse(resp)
	if err != nil {
		return fmt.Errorf("failed to decompress response: %v", err)
	}

	// Read response body
	body, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	// Check status code error
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication request returned error status code: %d", resp.StatusCode)
	}

	// Decode body data
	var tokenAccess TokenAccess
	if err := json.Unmarshal(body, &tokenAccess); err != nil {
		return fmt.Errorf("failed to decode response body: %v", err)
	}

	now := time.Now().Add(-5 * time.Second)
	tokenAccess.LastAuthentication = &now

	// Set token in cache for the scope
	tm.cache[scope] = &tokenAccess

	return nil
}

// getAccessToken returns a valid token from cache or request a new
func (tm *TokenManager) getAccessToken(ctx context.Context, scope string) (*TokenAccess, error) {
	if scope == "" {
		return nil, fmt.Errorf("invalid access scope")
	}

	tokenScope := tm.getTokenFromScope(scope)
	if tokenScope == nil {
		if err := tm.authenticate(ctx, scope); err != nil {
			return nil, err
		}

		return tm.getTokenFromScope(scope), nil
	}

	now := time.Now()
	expiration := tokenScope.LastAuthentication.
		Add(time.Duration(tokenScope.ExpiresIn) * time.Second)

	// Checks if  the token is still valid
	if expiration.After(now) {
		return tokenScope, nil
	}

	// Token is expired, re-authenticate
	if err := tm.authenticate(ctx, scope); err != nil {
		return nil, err
	}

	return tm.getTokenFromScope(scope), nil
}

// getTokenFromScope fetch a token from cache from the scope
func (tm *TokenManager) getTokenFromScope(scope string) *TokenAccess {
	if t, ok := tm.cache[scope]; ok {
		return t
	}

	return nil
}
