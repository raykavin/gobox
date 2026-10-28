package fcm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/oauth2"
)

const (
	testProject     = "test-project"
	testClientEmail = "fcm@test-project.iam.gserviceaccount.com"
	testAccessToken = "test-access-token"
	testDeviceToken = "device-registration-token"
	testMessageName = "projects/test-project/messages/0:1234"
)

var testRSAKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

func pkcs8PEM(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS #8 key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func pkcs1PEM(key *rsa.PrivateKey) string {
	der := x509.MarshalPKCS1PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

// serviceAccountJSON returns a valid service account key, changed by edit
// when it is not nil. An empty value removes the field.
func serviceAccountJSON(t *testing.T, edit func(map[string]string)) []byte {
	t.Helper()
	fields := map[string]string{
		"type":           serviceAccountType,
		"project_id":     testProject,
		"private_key_id": "key-id",
		"private_key":    pkcs8PEM(t, testRSAKey()),
		"client_email":   testClientEmail,
		"token_uri":      DefaultTokenURL,
	}
	if edit != nil {
		edit(fields)
	}
	for name, value := range fields {
		if value == "" {
			delete(fields, name)
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal service account: %v", err)
	}
	return raw
}

// fakeGoogle serves the OAuth 2.0 token endpoint at /token and the FCM send
// endpoint at /v1/projects/{project}/messages:send.
type fakeGoogle struct {
	*httptest.Server
	tokenStatus int
	send        http.HandlerFunc
	tokenCalls  atomic.Int32
	sendCalls   atomic.Int32
}

type fakeOption func(*fakeGoogle)

func withTokenStatus(status int) fakeOption {
	return func(f *fakeGoogle) { f.tokenStatus = status }
}

func withSendHandler(handler http.HandlerFunc) fakeOption {
	return func(f *fakeGoogle) { f.send = handler }
}

func newFakeGoogle(t *testing.T, opts ...fakeOption) *fakeGoogle {
	t.Helper()
	f := &fakeGoogle{tokenStatus: http.StatusOK}
	for _, opt := range opts {
		opt(f)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", f.serveToken)
	mux.HandleFunc("POST /v1/projects/{project}/messages:send", f.serveSend)

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGoogle) serveToken(w http.ResponseWriter, r *http.Request) {
	f.tokenCalls.Add(1)
	w.Header().Set("Content-Type", "application/json")

	if f.tokenStatus != http.StatusOK {
		w.WriteHeader(f.tokenStatus)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refused by the test"}`))
		return
	}

	if r.FormValue("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || !hasMessagingScope(r.FormValue("assertion")) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": testAccessToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

func (f *fakeGoogle) serveSend(w http.ResponseWriter, r *http.Request) {
	f.sendCalls.Add(1)
	if r.Header.Get("Authorization") != "Bearer "+testAccessToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.send != nil {
		f.send(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"name": testMessageName})
}

func hasMessagingScope(assertion string) bool {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
	}
	return json.Unmarshal(raw, &claims) == nil && claims.Iss == testClientEmail && claims.Scope == messagingScope
}

func newTestClient(t *testing.T, srv *fakeGoogle) *Client {
	t.Helper()
	client, err := New(Config{
		Credentials: serviceAccountJSON(t, func(f map[string]string) { f["token_uri"] = srv.URL + "/token" }),
		Endpoint:    srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestNew_InvalidCredentials(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}

	tests := []struct {
		name        string
		credentials []byte
	}{
		{"empty", nil},
		{"not JSON", []byte("{")},
		{"wrong type", serviceAccountJSON(t, func(f map[string]string) { f["type"] = "authorized_user" })},
		{"missing client_email", serviceAccountJSON(t, func(f map[string]string) { f["client_email"] = "" })},
		{"missing private_key", serviceAccountJSON(t, func(f map[string]string) { f["private_key"] = "" })},
		{"malformed private_key", serviceAccountJSON(t, func(f map[string]string) { f["private_key"] = "not a key" })},
		{"non-RSA private_key", serviceAccountJSON(t, func(f map[string]string) { f["private_key"] = pkcs8PEM(t, ecKey) })},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(Config{Credentials: tt.credentials})
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("err = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestNew_AcceptsPKCS1Key(t *testing.T) {
	credentials := serviceAccountJSON(t, func(f map[string]string) { f["private_key"] = pkcs1PEM(testRSAKey()) })
	if _, err := New(Config{Credentials: credentials}); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_ProjectID(t *testing.T) {
	tests := []struct {
		name           string
		configured     string
		accountProject string
		want           string
		wantErr        error
	}{
		{name: "from service account", accountProject: testProject, want: testProject},
		{name: "configured and matching", configured: "  " + testProject + " ", accountProject: testProject, want: testProject},
		{name: "configured without account project", configured: "other-project", want: "other-project"},
		{name: "mismatch", configured: "other-project", accountProject: testProject, wantErr: ErrProjectMismatch},
		{name: "missing", wantErr: ErrMissingProject},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credentials := serviceAccountJSON(t, func(f map[string]string) { f["project_id"] = tt.accountProject })

			client, err := New(Config{ProjectID: tt.configured, Credentials: credentials})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := client.ProjectID(); got != tt.want {
				t.Errorf("ProjectID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNew_SendURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"default", "", DefaultEndpoint + "/v1/projects/test-project/messages:send"},
		{"trailing slash", "https://fcm.example.test/", "https://fcm.example.test/v1/projects/test-project/messages:send"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(Config{Credentials: serviceAccountJSON(t, nil), Endpoint: tt.endpoint})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if client.sendURL != tt.want {
				t.Errorf("sendURL = %q, want %q", client.sendURL, tt.want)
			}
		})
	}
}

func TestNew_HTTPClient(t *testing.T) {
	base := &http.Client{Timeout: 3 * time.Second}

	client, err := New(Config{Credentials: serviceAccountJSON(t, nil), HTTPClient: base})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.http == base {
		t.Fatal("the caller's client must not be modified")
	}
	if client.http.Timeout != base.Timeout {
		t.Errorf("Timeout = %v, want %v", client.http.Timeout, base.Timeout)
	}
	transport, ok := client.http.Transport.(*oauth2.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *oauth2.Transport", client.http.Transport)
	}
	if transport.Base != http.DefaultTransport {
		t.Errorf("Base = %T, want http.DefaultTransport", transport.Base)
	}
	if base.Transport != nil {
		t.Error("the caller's Transport must stay untouched")
	}
}

func TestNewSendRequest(t *testing.T) {
	msg := NotificationMessage{
		Address: "  " + testDeviceToken + "  ",
		Title:   strings.Repeat("t", maxTitleBytes+10),
		Body:    "body",
		Link:    "/orders/42",
		Tag:     "order-42",
		Data:    map[string]string{"delivery_id": "d-1", "title": "overwritten"},
		TTL:     90*time.Second + 500*time.Millisecond,
	}

	req, err := newSendRequest(msg)
	if err != nil {
		t.Fatalf("newSendRequest: %v", err)
	}

	if req.Message.Token != testDeviceToken {
		t.Errorf("Token = %q, want %q", req.Message.Token, testDeviceToken)
	}

	wantData := map[string]string{
		"delivery_id": "d-1",
		"title":       strings.Repeat("t", maxTitleBytes),
		"body":        "body",
		"link":        "/orders/42",
		"tag":         "order-42",
	}
	assertStringMap(t, "Data", req.Message.Data, wantData)
	assertStringMap(t, "Headers", req.Message.WebPush.Headers, map[string]string{"Urgency": "high", "TTL": "90"})

	if msg.Data["title"] != "overwritten" {
		t.Error("the caller's Data map must not be modified")
	}
}

func TestNewSendRequest_WithoutTTL(t *testing.T) {
	req, err := newSendRequest(NotificationMessage{Address: testDeviceToken})
	if err != nil {
		t.Fatalf("newSendRequest: %v", err)
	}
	if _, ok := req.Message.WebPush.Headers["TTL"]; ok {
		t.Error("TTL header must be omitted when TTL is zero")
	}
}

func TestSend_Accepted(t *testing.T) {
	var received sendRequest
	srv := newFakeGoogle(t, withSendHandler(func(w http.ResponseWriter, r *http.Request) {
		if got := r.PathValue("project"); got != testProject {
			t.Errorf("project = %q, want %q", got, testProject)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"name": testMessageName})
	}))
	client := newTestClient(t, srv)

	resp, err := client.Send(context.Background(), NotificationMessage{
		Address: testDeviceToken,
		Title:   "Order shipped",
		Body:    "Your order is on its way.",
		Link:    "/orders/42",
		Tag:     "order-42",
		TTL:     time.Hour,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !resp.Accepted() || resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if resp.AcceptedName != testMessageName {
		t.Errorf("AcceptedName = %q, want %q", resp.AcceptedName, testMessageName)
	}
	if resp.ErrorCode != "" || resp.Retryable() || resp.InvalidToken() {
		t.Errorf("an accepted send must carry no error: %+v", resp)
	}
	if resp.Headers["TTL"] != "3600" {
		t.Errorf("Headers[TTL] = %q, want 3600", resp.Headers["TTL"])
	}
	if received.Message.Token != testDeviceToken || received.Message.Data["title"] != "Order shipped" {
		t.Errorf("unexpected request: %+v", received.Message)
	}
}

func TestSend_ReusesAccessToken(t *testing.T) {
	srv := newFakeGoogle(t)
	client := newTestClient(t, srv)

	for range 3 {
		if _, err := client.Send(context.Background(), NotificationMessage{Address: testDeviceToken}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	if got := srv.tokenCalls.Load(); got != 1 {
		t.Errorf("token requests = %d, want 1", got)
	}
	if got := srv.sendCalls.Load(); got != 3 {
		t.Errorf("send requests = %d, want 3", got)
	}
}

func TestSend_AcceptedWithoutName(t *testing.T) {
	srv := newFakeGoogle(t, withSendHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := newTestClient(t, srv)

	resp, err := client.Send(context.Background(), NotificationMessage{Address: testDeviceToken})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !resp.Accepted() {
		t.Errorf("a 200 without a name must still be accepted: %+v", resp)
	}
	if resp.AcceptedName != "" {
		t.Errorf("AcceptedName = %q, want empty", resp.AcceptedName)
	}
}

func TestSend_Rejected(t *testing.T) {
	tests := []struct {
		name             string
		status           int
		retryAfter       string
		body             string
		wantCode         ErrorCode
		wantRetryable    bool
		wantInvalidToken bool
		wantRetryAfter   time.Duration
	}{
		{
			name:             "unregistered",
			status:           http.StatusNotFound,
			body:             fcmError(http.StatusNotFound, "NOT_FOUND", "UNREGISTERED"),
			wantCode:         ErrorUnregistered,
			wantInvalidToken: true,
		},
		{
			name:             "sender id mismatch",
			status:           http.StatusForbidden,
			body:             fcmError(http.StatusForbidden, "PERMISSION_DENIED", "SENDER_ID_MISMATCH"),
			wantCode:         ErrorSenderIDMismatch,
			wantInvalidToken: true,
		},
		{
			name:     "invalid argument",
			status:   http.StatusBadRequest,
			body:     fcmError(http.StatusBadRequest, "INVALID_ARGUMENT", "INVALID_ARGUMENT"),
			wantCode: ErrorInvalidArgument,
		},
		{
			name:           "quota exceeded",
			status:         http.StatusTooManyRequests,
			retryAfter:     "30",
			body:           fcmError(http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", "QUOTA_EXCEEDED"),
			wantCode:       ErrorQuotaExceeded,
			wantRetryable:  true,
			wantRetryAfter: 30 * time.Second,
		},
		{
			name:          "status without details",
			status:        http.StatusServiceUnavailable,
			body:          `{"error":{"code":503,"status":"UNAVAILABLE"}}`,
			wantCode:      ErrorUnavailable,
			wantRetryable: true,
		},
		{
			name:          "gateway error without JSON",
			status:        http.StatusBadGateway,
			body:          "<html>bad gateway</html>",
			wantRetryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeGoogle(t, withSendHandler(func(w http.ResponseWriter, _ *http.Request) {
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			client := newTestClient(t, srv)

			resp, err := client.Send(context.Background(), NotificationMessage{Address: testDeviceToken})
			if err != nil {
				t.Fatalf("Send: %v", err)
			}

			if resp.Accepted() || resp.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.status)
			}
			if resp.AcceptedName != "" {
				t.Errorf("AcceptedName = %q, want empty", resp.AcceptedName)
			}
			if resp.ErrorCode != tt.wantCode {
				t.Errorf("ErrorCode = %q, want %q", resp.ErrorCode, tt.wantCode)
			}
			if resp.Retryable() != tt.wantRetryable {
				t.Errorf("Retryable() = %v, want %v", resp.Retryable(), tt.wantRetryable)
			}
			if resp.InvalidToken() != tt.wantInvalidToken {
				t.Errorf("InvalidToken() = %v, want %v", resp.InvalidToken(), tt.wantInvalidToken)
			}
			if resp.RetryAfter != tt.wantRetryAfter {
				t.Errorf("RetryAfter = %v, want %v", resp.RetryAfter, tt.wantRetryAfter)
			}
			if string(resp.Body) != tt.body {
				t.Errorf("Body = %q, want %q", resp.Body, tt.body)
			}
		})
	}
}

func TestSend_EmptyAddress(t *testing.T) {
	srv := newFakeGoogle(t)
	client := newTestClient(t, srv)

	_, err := client.Send(context.Background(), NotificationMessage{Address: "   "})
	if !errors.Is(err, ErrEmptyAddress) {
		t.Fatalf("err = %v, want ErrEmptyAddress", err)
	}
	if srv.tokenCalls.Load() != 0 || srv.sendCalls.Load() != 0 {
		t.Error("no request must be made for an empty address")
	}
}

func TestSend_TokenExchangeRefused(t *testing.T) {
	srv := newFakeGoogle(t, withTokenStatus(http.StatusBadRequest))
	client := newTestClient(t, srv)

	_, err := client.Send(context.Background(), NotificationMessage{Address: testDeviceToken})

	var retrieveErr *oauth2.RetrieveError
	if !errors.As(err, &retrieveErr) {
		t.Fatalf("err = %v, want *oauth2.RetrieveError", err)
	}
	if srv.sendCalls.Load() != 0 {
		t.Error("the message must not be sent without an access token")
	}
}

func TestSend_ContextCanceled(t *testing.T) {
	srv := newFakeGoogle(t)
	client := newTestClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Send(ctx, NotificationMessage{Address: testDeviceToken})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)

	tests := []struct {
		name  string
		value string
		min   time.Duration
		max   time.Duration
	}{
		{"absent", "", 0, 0},
		{"seconds", "120", 2 * time.Minute, 2 * time.Minute},
		{"negative seconds", "-5", 0, 0},
		{"future date", future, 58 * time.Minute, time.Hour},
		{"past date", past, 0, 0},
		{"garbage", "soon", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.value)
			if got < tt.min || got > tt.max {
				t.Errorf("parseRetryAfter(%q) = %v, want between %v and %v", tt.value, got, tt.min, tt.max)
			}
		})
	}
}

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"shorter than limit", "abc", 5, "abc"},
		{"exactly the limit", "abcde", 5, "abcde"},
		{"ASCII cut", "abcdef", 3, "abc"},
		{"inside a 2-byte rune", "aé", 2, "a"},
		{"after a 2-byte rune", "aéb", 3, "aé"},
		{"inside a 4-byte rune", "ab😀c", 4, "ab"},
		{"zero limit", "abc", 0, ""},
		{"invalid UTF-8 before the cut", "\xffabc", 2, "\xffa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateBytes(tt.in, tt.limit)
			if got != tt.want {
				t.Errorf("truncateBytes(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
			if len(got) > tt.limit && len(tt.in) > tt.limit {
				t.Errorf("result has %d bytes, limit is %d", len(got), tt.limit)
			}
			if utf8.ValidString(tt.in) && !utf8.ValidString(got) {
				t.Errorf("result %q splits a character", got)
			}
		})
	}
}

func fcmError(code int, status, errorCode string) string {
	raw, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": "rejected by the test",
			"status":  status,
			"details": []map[string]string{{
				"@type":     "type.googleapis.com/google.firebase.fcm.v1.FcmError",
				"errorCode": errorCode,
			}},
		},
	})
	return string(raw)
}

func assertStringMap(t *testing.T, name string, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
		return
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s[%q] = %q, want %q", name, key, got[key], value)
		}
	}
}
