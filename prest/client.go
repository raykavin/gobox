package prest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/raykavin/gokit/http"
)

// tokenExpiryBuffer is subtracted from the token's expiry time to avoid using
// a token that expires mid-request.
const tokenExpiryBuffer = 10 * time.Second

type authentication struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidEndpoints   = errors.New("invalid endpoints")
)

// Client is a generic pREST API client that authenticates via OAuth2
// client credentials and unmarshals responses into T.
type Client[T any] struct {
	authEndpoint string
	clientID     string
	clientSecret string
	grantType    string
	scope        string
	auth         *authentication
	lastAuth     *time.Time
	client       *stdhttp.Client
}

// NewClient creates a new pREST Client.
func NewClient[T any](
	clientID string,
	clientSecret string,
	grantType string,
	scope string,
	authEndpoint string,
	client ...*stdhttp.Client,
) (*Client[T], error) {
	if clientID == "" || clientSecret == "" || grantType == "" || scope == "" {
		return nil, ErrInvalidCredentials
	}
	if authEndpoint == "" {
		return nil, ErrInvalidEndpoints
	}

	var c *stdhttp.Client
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}

	return &Client[T]{
		authEndpoint: authEndpoint,
		clientID:     clientID,
		clientSecret: clientSecret,
		grantType:    grantType,
		scope:        scope,
		client:       c,
	}, nil
}

// Reset clears the cached authentication token,
// forcing re-authentication on the next request.
func (c *Client[T]) Reset() {
	c.auth = nil
	c.lastAuth = nil
}

// Get sends a GET request to endpoint, appending the given query params, and
// unmarshals the JSON response body into T.
func (c *Client[T]) Get(ctx context.Context, endpoint string, params map[string]string) (T, error) {
	var zero T

	if err := c.authenticate(ctx); err != nil {
		return zero, fmt.Errorf("authenticating: %w", err)
	}

	headers := http.DefaultJSONHeaders()
	headers.Set(http.HeaderAuthorization, fmt.Sprintf("%s %s", c.auth.TokenType, c.auth.AccessToken))

	return do[T](ctx, endpoint, stdhttp.MethodGet, http.MapParams(params), headers, nil, c.client)
}

// GetPaginated sends a GET request with limit/offset pagination query params.
func (c *Client[T]) GetPaginated(ctx context.Context, endpoint string, limit, offset int) (T, error) {
	return c.Get(ctx, endpoint, map[string]string{
		"limit":  strconv.Itoa(limit),
		"offset": strconv.Itoa(offset),
	})
}

// authenticate fetches and caches an access token if the current one is
// missing or expired.
func (c *Client[T]) authenticate(ctx context.Context) error {
	if c.isAuthenticated() {
		return nil
	}

	q := url.Values{}
	q.Set("client_id", c.clientID)
	q.Set("client_secret", c.clientSecret)
	q.Set("grant_type", c.grantType)
	q.Set("scope", c.scope)

	headers := http.DefaultFormHeaders()
	headers.Set(http.HeaderAccept, http.MIMEApplicationJSON)

	auth, err := do[authentication](
		ctx,
		c.authEndpoint,
		stdhttp.MethodPost,
		nil,
		headers,
		[]byte(q.Encode()),
		c.client,
	)
	if err != nil {
		return fmt.Errorf("executing auth request: %w", err)
	}

	now := time.Now().Add(-1 * time.Minute) // safety margin
	c.auth = &auth
	c.lastAuth = &now

	return nil
}

// isAuthenticated reports whether the cached token exists and is still valid,
// accounting for a small expiry buffer to avoid mid-request expiry.
func (c *Client[T]) isAuthenticated() bool {
	if c.auth == nil || c.lastAuth == nil {
		return false
	}
	expiry := c.lastAuth.Add(time.Duration(c.auth.ExpiresIn)*time.Second - tokenExpiryBuffer)
	return time.Now().Before(expiry)
}

// do executes the request and unmarshals the JSON response into T.
func do[T any](
	ctx context.Context,
	endpoint, method string,
	queries, headers map[string]string,
	payload []byte,
	client *stdhttp.Client,
) (T, error) {
	var zero T

	body, statusCode, err := http.NewRequestWithContext(
		ctx,
		method,
		endpoint,
		queries,
		headers,
		payload,
		client,
	)
	if err != nil {
		return zero, fmt.Errorf("executing request: %w", err)
	}

	if statusCode != stdhttp.StatusOK {
		return zero, fmt.Errorf("unexpected status %d", statusCode)
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("decoding response: %w", err)
	}

	return result, nil
}
