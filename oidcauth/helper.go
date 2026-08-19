package oidcauth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
)

// hashToken returns a stable, fixed-size key for cache storage. We avoid
// keeping raw bearer tokens in map keys both for memory reasons (JWTs can be
// large) and to reduce the blast radius of a memory dump.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// validateConfig checks required fields and applies defaults in place.
func validateConfig(config *Config) error {
	if config.RealmURL == "" {
		return ErrInvalidRealmURL
	}
	u, err := url.ParseRequestURI(config.RealmURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidRealmURL
	}
	if config.ClientID == "" {
		return ErrEmptyClientID
	}
	if !config.DisableIntrospection && config.ClientSecret == "" {
		return ErrMissingClientSecret
	}
	if config.IntrospectionCacheTTL < 0 {
		return fmt.Errorf("%w: introspection cache TTL cannot be negative", ErrInvalidOIDCConfiguration)
	}
	if config.IntrospectionHTTPTimeout < 0 {
		return fmt.Errorf("%w: introspection HTTP timeout cannot be negative", ErrInvalidOIDCConfiguration)
	}

	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	if config.IntrospectionHTTPTimeout == 0 {
		config.IntrospectionHTTPTimeout = defaultIntrospectionHTTPTimeout
	}
	return nil
}
