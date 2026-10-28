package fcm

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

const (
	serviceAccountType = "service_account"
	messagingScope     = "https://www.googleapis.com/auth/firebase.messaging"
)

// serviceAccount is the subset of a Google service account key file the
// client reads.
type serviceAccount struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

func parseServiceAccount(raw []byte) (serviceAccount, error) {
	var account serviceAccount
	if err := json.Unmarshal(raw, &account); err != nil {
		return serviceAccount{}, fmt.Errorf("%w: %w", ErrInvalidCredentials, err)
	}
	if account.Type != serviceAccountType || account.ClientEmail == "" || account.PrivateKey == "" {
		return serviceAccount{}, fmt.Errorf("%w: expected a service account key with client_email and private_key", ErrInvalidCredentials)
	}
	if err := validateKey([]byte(account.PrivateKey)); err != nil {
		return serviceAccount{}, fmt.Errorf("%w: %w", ErrInvalidCredentials, err)
	}
	return account, nil
}

// validateKey checks that key is an RSA private key, PEM-encoded or not, in
// PKCS #8 or PKCS #1: the form the JWT signer needs on the first send.
func validateKey(key []byte) error {
	if block, _ := pem.Decode(key); block != nil {
		key = block.Bytes
	}

	parsed, err := x509.ParsePKCS8PrivateKey(key)
	if err != nil {
		if _, pkcs1Err := x509.ParsePKCS1PrivateKey(key); pkcs1Err != nil {
			return fmt.Errorf("private_key is neither PKCS #8 nor PKCS #1: %w", err)
		}
		return nil
	}
	if _, ok := parsed.(*rsa.PrivateKey); !ok {
		return errors.New("private_key is not an RSA key")
	}
	return nil
}

func resolveProjectID(configured, account string) (string, error) {
	projectID := strings.TrimSpace(configured)
	if projectID == "" {
		projectID = account
	}
	if projectID == "" {
		return "", ErrMissingProject
	}
	if account != "" && projectID != account {
		return "", fmt.Errorf("%w: %q is not %q", ErrProjectMismatch, projectID, account)
	}
	return projectID, nil
}
