# totp

The `totp` package implements RFC 6238 time-based one-time passwords on top of RFC 4226 HOTP. It is intended for services that need to enroll users in an authenticator app and verify six-digit codes, without pulling in a larger MFA framework.

## Import

```go
import "github.com/raykavin/gobox/totp"
```

## What it provides

- `GenerateSecret` for cryptographically secure random shared secrets
- `GenerateCode` for computing the code at an explicit time step
- `Validate` for verifying a submitted code within a tolerance window, in constant time
- `EncodeSecretBase32` / `DecodeSecretBase32` for the manual-entry key format authenticator apps display
- `BuildOTPAuthURI` for the `otpauth://` enrollment URI that becomes the QR code
- `TimeStepAt` for converting a Unix timestamp into the HOTP counter

The package is pure standard library: HMAC-SHA1, base32, and `crypto/rand`.

## Main types

- `Config`: the parameters of a TOTP calculation, namely `Digits` (6 to 8) and `Period` (the time step in seconds)
- `DefaultConfig()`: the broadly-compatible defaults, i.e. SHA1, 6 digits, and a 30-second period

The algorithm is fixed to SHA1. That is not an oversight: it is what guarantees compatibility across authenticator apps, and the security of TOTP does not rest on the hash's collision resistance.

## Example

### Enrollment

```go
secret, err := totp.GenerateSecret(20) // 160 bits, the RFC 4226 recommendation
if err != nil {
    log.Fatal(err)
}

cfg := totp.DefaultConfig()

// Render this as a QR code for the authenticator app...
uri := totp.BuildOTPAuthURI("Acme", "alice@example.com", secret, cfg)

// ...and show this as the manual-entry fallback.
manualKey := totp.EncodeSecretBase32(secret)

// Store `secret` encrypted at rest, then require one valid code
// before marking the enrollment confirmed.
```

### Verification

```go
cfg := totp.DefaultConfig()

step, ok := totp.Validate(secret, submittedCode, time.Now().Unix(), cfg, 1)
if !ok {
    return errors.New("invalid code")
}

// Reject a code whose step was already used, otherwise it stays
// replayable for the rest of the period.
if step <= lastUsedStep {
    return errors.New("code already used")
}
lastUsedStep = step
```

## Reference

### Functions

| Function                                                       | Description                                                                                                       |
| -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `GenerateSecret(numBytes int) ([]byte, error)`                  | Random secret of `numBytes` length from `crypto/rand`; errors when `numBytes <= 0`                                 |
| `TimeStepAt(unixTime, period int64) int64`                      | The HOTP counter for a Unix timestamp: `unixTime / period`                                                         |
| `GenerateCode(secret []byte, timeStep int64, cfg Config)`       | The code for an explicit time step; validates `secret`, `Digits`, and `Period`                                     |
| `Validate(secret []byte, code string, now int64, cfg Config, window int) (matchedStep int64, ok bool)` | Checks every step in `[-window, +window]` around `now`, returning the step that matched |
| `EncodeSecretBase32(secret []byte) string`                      | Unpadded uppercase base32, as used in the URI and the manual-entry key                                             |
| `DecodeSecretBase32(encoded string) ([]byte, error)`            | Decodes base32 with or without padding, tolerating spaces and lowercase                                            |
| `BuildOTPAuthURI(issuer, account string, secret []byte, cfg Config) string` | The `otpauth://totp/...` enrollment URI                                                                |

### Config

| Field    | Default | Description                                                     |
| -------- | ------- | --------------------------------------------------------------- |
| `Digits` | `6`     | Number of digits in the generated code; must be 6, 7, or 8      |
| `Period` | `30`    | Time step in seconds; must be positive                          |

### Errors

| Error               | Returned when                                                    |
| ------------------- | ----------------------------------------------------------------- |
| `ErrInvalidSecret`  | `GenerateCode` receives an empty secret                           |
| `ErrInvalidDigits`  | `Config.Digits` is outside 6 to 8                                 |
| `ErrInvalidPeriod`  | `Config.Period` is not positive                                   |

`GenerateSecret` returns its own error for a non-positive `numBytes`, and wraps any failure from `crypto/rand`.

## Notes

- `Validate` returns `matchedStep` so the caller can prevent replay. Persist the last accepted step per user and reject any code at or below it: within a single period the same code is otherwise valid on every submission.
- `Validate` reports `false` rather than an error for malformed input: an empty secret or code, a non-positive `Period`, or a negative `window`.
- Candidate codes are compared with `hmac.Equal`, so a partial match does not leak through timing.
- A `window` of `1` accepts the previous, current, and next step, which tolerates roughly ±30 seconds of clock skew at the default period. Widening it beyond that lengthens the window in which a phished code stays usable.
- Secrets are the full authentication factor. Encrypt them at rest (see [`secure`](../secure/README.md)) and never log them or the `otpauth://` URI, which embeds the secret in plaintext.
- `BuildOTPAuthURI` escapes `issuer` and `account` in the label, but `issuer` is also written to the query string as provided.
- `totp_test.go` pins the implementation against the RFC 6238 Appendix B test vectors.
