// Package totp implements RFC 6238 TOTP on top of RFC 4226 HOTP.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // required by RFC 6238; not used for anything else.
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Config holds the parameters of a TOTP calculation.
type Config struct {
	// Digits is the number of digits in the generated code.
	Digits int
	// Period is the time step, in seconds (30 is the broadly-compatible default).
	Period int64
}

// DefaultConfig returns the standard, broadly-compatible parameters: SHA1,
// 6 digits, 30-second period. Do not change these without a proven need —
// they are what guarantees compatibility across authenticator apps.
func DefaultConfig() Config {
	return Config{Digits: 6, Period: 30}
}

var (
	// ErrInvalidSecret is returned when the secret is empty.
	ErrInvalidSecret = errors.New("totp: secret must not be empty")
	// ErrInvalidDigits is returned when Digits is out of the supported range.
	ErrInvalidDigits = errors.New("totp: digits must be between 6 and 8")
	// ErrInvalidPeriod is returned when Period is not positive.
	ErrInvalidPeriod = errors.New("totp: period must be positive")
)

// GenerateSecret returns a cryptographically secure random secret of
// numBytes length. 20 bytes (160 bits) is the RFC 4226-recommended size and
// what this package's callers should use in production.
func GenerateSecret(numBytes int) ([]byte, error) {
	if numBytes <= 0 {
		return nil, errors.New("totp: numBytes must be positive")
	}
	secret := make([]byte, numBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("totp: generate secret: %w", err)
	}
	return secret, nil
}

// TimeStepAt returns the time step (the HOTP "counter") for a given Unix
// timestamp and period.
func TimeStepAt(unixTime int64, period int64) int64 {
	return unixTime / period
}

// GenerateCode computes the TOTP code for secret at the given time step.
func GenerateCode(secret []byte, timeStep int64, cfg Config) (string, error) {
	if len(secret) == 0 {
		return "", ErrInvalidSecret
	}
	if cfg.Digits < 6 || cfg.Digits > 8 {
		return "", ErrInvalidDigits
	}
	if cfg.Period <= 0 {
		return "", ErrInvalidPeriod
	}
	return hotp(secret, uint64(timeStep), cfg.Digits), nil
}

// hotp implements RFC 4226 HOTP: HMAC-SHA1 over the big-endian counter,
// followed by dynamic truncation to Digits decimal digits.
func hotp(secret []byte, counter uint64, digits int) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, truncated%mod)
}

// Validate reports whether code matches within the configured time-step window.
// It returns the matched step so callers can prevent replay.
// Candidate codes are compared in constant time.
func Validate(secret []byte, code string, now int64, cfg Config, window int) (matchedStep int64, ok bool) {
	if len(secret) == 0 || strings.TrimSpace(code) == "" || cfg.Period <= 0 || window < 0 {
		return 0, false
	}

	currentStep := TimeStepAt(now, cfg.Period)
	for delta := -window; delta <= window; delta++ {
		step := currentStep + int64(delta)
		if step < 0 {
			continue
		}
		candidate, err := GenerateCode(secret, step, cfg)
		if err != nil {
			return 0, false
		}
		if constantTimeEqual(candidate, code) {
			return step, true
		}
	}
	return 0, false
}

// constantTimeEqual compares two strings without leaking timing information
// about a partial match, using hmac.Equal (byte-wise constant-time compare).
func constantTimeEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// EncodeSecretBase32 encodes a raw secret as the unpadded base32 string used
// both in the otpauth:// URI and as the manual entry key shown to the user.
func EncodeSecretBase32(secret []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// DecodeSecretBase32 decodes a base32 secret (with or without padding),
// accepting the manual-entry key format authenticator apps commonly display.
func DecodeSecretBase32(encoded string) ([]byte, error) {
	encoded = strings.ToUpper(strings.TrimSpace(encoded))
	encoded = strings.ReplaceAll(encoded, " ", "")
	if pad := len(encoded) % 8; pad != 0 {
		encoded += strings.Repeat("=", 8-pad)
	}
	return base32.StdEncoding.DecodeString(encoded)
}

// BuildOTPAuthURI builds an enrollment otpauth URI.
// issuer and account must be safe for use in a URI.
func BuildOTPAuthURI(issuer, account string, secret []byte, cfg Config) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", EncodeSecretBase32(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", cfg.Digits))
	q.Set("period", fmt.Sprintf("%d", cfg.Period))

	return fmt.Sprintf("otpauth://totp/%s?%s", label, q.Encode())
}
