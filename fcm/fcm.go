package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

const maxResponseBody = 64 << 10

// Client sends messages through FCM. It is safe for concurrent use; access
// tokens are cached and renewed shortly before they expire.
type Client struct {
	projectID string
	sendURL   string
	http      *http.Client
}

// New validates the configuration and builds a Client. It makes no network
// call: a credential the token endpoint refuses surfaces as an error on the
// first Send, so a Google outage at startup does not stop the service.
func New(cfg Config) (*Client, error) {
	account, err := parseServiceAccount(cfg.Credentials)
	if err != nil {
		return nil, err
	}

	projectID, err := resolveProjectID(cfg.ProjectID, account.ProjectID)
	if err != nil {
		return nil, err
	}

	base := cfg.HTTPClient
	if base == nil {
		base = &http.Client{Timeout: DefaultTimeout}
	}

	jwtConfig := &jwt.Config{
		Email:        account.ClientEmail,
		PrivateKey:   []byte(account.PrivateKey),
		PrivateKeyID: account.PrivateKeyID,
		Scopes:       []string{messagingScope},
		TokenURL:     firstNonEmpty(cfg.TokenURL, account.TokenURI, DefaultTokenURL),
	}

	// The token source outlives any single send, so it is bound to a
	// background context carrying base: the token exchange then uses the
	// same transport and timeout as the sends.
	tokenCtx := context.WithValue(context.Background(), oauth2.HTTPClient, base)

	httpClient := *base
	httpClient.Transport = &oauth2.Transport{
		Source: jwtConfig.TokenSource(tokenCtx),
		Base:   transportOf(base),
	}

	endpoint := strings.TrimRight(firstNonEmpty(cfg.Endpoint, DefaultEndpoint), "/")

	return &Client{
		projectID: projectID,
		sendURL:   endpoint + "/v1/projects/" + url.PathEscape(projectID) + "/messages:send",
		http:      &httpClient,
	}, nil
}

// ProjectID returns the Firebase project messages are sent through.
func (c *Client) ProjectID() string {
	return c.projectID
}

// Send delivers msg to one device. Whenever FCM answers, accepted or not, the
// answer is returned as a SendResponse with a nil error; an error means FCM
// was not reached: an empty address, a transport failure or a refused token
// exchange.
func (c *Client) Send(ctx context.Context, msg NotificationMessage) (SendResponse, error) {
	request, err := newSendRequest(msg)
	if err != nil {
		return SendResponse{}, err
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return SendResponse{}, fmt.Errorf("fcm: encode message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sendURL, bytes.NewReader(payload))
	if err != nil {
		return SendResponse{}, fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.http.Do(req)
	if err != nil {
		return SendResponse{}, fmt.Errorf("fcm: send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))

	return newSendResponse(resp, body, request.Message.WebPush.Headers), nil
}

func transportOf(client *http.Client) http.RoundTripper {
	if client.Transport != nil {
		return client.Transport
	}
	return http.DefaultTransport
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}