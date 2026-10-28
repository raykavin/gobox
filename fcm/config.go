package fcm

import (
	"net/http"
	"time"
)

const (
	// DefaultEndpoint is the FCM API root.
	DefaultEndpoint = "https://fcm.googleapis.com"
	// DefaultTokenURL is Google's OAuth 2.0 token endpoint, used when the
	// service account key does not name one.
	DefaultTokenURL = "https://oauth2.googleapis.com/token"
	// DefaultTimeout bounds each token exchange and send when
	// Config.HTTPClient is nil.
	DefaultTimeout = 15 * time.Second
)

// Config configures a Client.
type Config struct {
	// ProjectID is the Firebase project messages are sent through. Empty
	// means the service account's project.
	ProjectID string
	// Credentials is the service account key file (JSON). It never leaves
	// the backend.
	Credentials []byte
	// Endpoint overrides DefaultEndpoint.
	Endpoint string
	// TokenURL overrides the token endpoint the service account names.
	TokenURL string
	// HTTPClient is used for both the token exchange and the sends. Nil
	// means a client with DefaultTimeout.
	HTTPClient *http.Client
}
