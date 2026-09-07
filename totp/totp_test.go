package totp

import (
	"strings"
	"testing"
)

// rfc6238SecretSHA1 is the ASCII secret "12345678901234567890" used by the
// official RFC 6238 Appendix B test vectors for SHA1.
var rfc6238SecretSHA1 = []byte("12345678901234567890")

// TestGenerateCode_RFC6238Vectors pins this implementation against the
// official RFC 6238 Appendix B test vectors (8-digit codes, 30s period,
// SHA1) the strongest possible guarantee of interoperability with any
// compliant authenticator app.
func TestGenerateCode_RFC6238Vectors(t *testing.T) {
	cfg := Config{Digits: 8, Period: 30}
	tests := []struct {
		unixTime int64
		want     string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}

	for _, tt := range tests {
		step := TimeStepAt(tt.unixTime, cfg.Period)
		got, err := GenerateCode(rfc6238SecretSHA1, step, cfg)
		if err != nil {
			t.Fatalf("unexpected error for time %d: %v", tt.unixTime, err)
		}
		if got != tt.want {
			t.Errorf("time=%d: got %s, want %s", tt.unixTime, got, tt.want)
		}
	}
}

func TestGenerateCode_DefaultConfig_ProducesSixDigits(t *testing.T) {
	secret, err := GenerateSecret(20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	code, err := GenerateCode(secret, 1, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("expected a 6-digit code, got %q (len=%d)", code, len(code))
	}
}

func TestGenerateCode_Validation(t *testing.T) {
	if _, err := GenerateCode(nil, 1, DefaultConfig()); err != ErrInvalidSecret {
		t.Errorf("expected ErrInvalidSecret, got %v", err)
	}
	if _, err := GenerateCode([]byte("secret"), 1, Config{Digits: 5, Period: 30}); err != ErrInvalidDigits {
		t.Errorf("expected ErrInvalidDigits, got %v", err)
	}
	if _, err := GenerateCode([]byte("secret"), 1, Config{Digits: 6, Period: 0}); err != ErrInvalidPeriod {
		t.Errorf("expected ErrInvalidPeriod, got %v", err)
	}
}

func TestValidate_AcceptsWithinToleranceWindow(t *testing.T) {
	cfg := DefaultConfig()
	secret := rfc6238SecretSHA1
	now := int64(1111111111)
	currentStep := TimeStepAt(now, cfg.Period)

	// The code for the PREVIOUS step (now - period) must be accepted with window=1.
	prevCode, err := GenerateCode(secret, currentStep-1, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	step, ok := Validate(secret, prevCode, now, cfg, 1)
	if !ok {
		t.Fatal("expected the previous step's code to be accepted within the tolerance window")
	}
	if step != currentStep-1 {
		t.Errorf("expected matched step %d, got %d", currentStep-1, step)
	}
}

func TestValidate_RejectsOutsideToleranceWindow(t *testing.T) {
	cfg := DefaultConfig()
	secret := rfc6238SecretSHA1
	now := int64(1111111111)
	currentStep := TimeStepAt(now, cfg.Period)

	farCode, err := GenerateCode(secret, currentStep-5, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := Validate(secret, farCode, now, cfg, 1); ok {
		t.Fatal("expected a code 5 steps away to be rejected with window=1")
	}
}

func TestValidate_RejectsWrongSecret(t *testing.T) {
	cfg := DefaultConfig()
	now := int64(1111111111)
	currentStep := TimeStepAt(now, cfg.Period)
	code, err := GenerateCode(rfc6238SecretSHA1, currentStep, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := Validate([]byte("wrong-secret"), code, now, cfg, 1); ok {
		t.Fatal("expected validation to fail against the wrong secret")
	}
}

func TestValidate_EmptyInputsRejected(t *testing.T) {
	cfg := DefaultConfig()
	if _, ok := Validate(nil, "123456", 0, cfg, 1); ok {
		t.Error("expected empty secret to be rejected")
	}
	if _, ok := Validate([]byte("secret"), "", 0, cfg, 1); ok {
		t.Error("expected empty code to be rejected")
	}
}

func TestBase32_RoundTrip(t *testing.T) {
	secret, err := GenerateSecret(20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	encoded := EncodeSecretBase32(secret)
	decoded, err := DecodeSecretBase32(encoded)
	if err != nil {
		t.Fatalf("unexpected error decoding: %v", err)
	}
	if string(decoded) != string(secret) {
		t.Error("expected round-trip to preserve the original secret")
	}
}

func TestDecodeSecretBase32_AcceptsLowercaseAndSpaces(t *testing.T) {
	secret, err := GenerateSecret(20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	encoded := EncodeSecretBase32(secret)
	var spaced strings.Builder
	for i, r := range strings.ToLower(encoded) {
		if i > 0 && i%4 == 0 {
			spaced.WriteByte(' ')
		}
		spaced.WriteRune(r)
	}
	decoded, err := DecodeSecretBase32(spaced.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(decoded) != string(secret) {
		t.Error("expected decoding to tolerate lowercase and spaces (as typed manually by a user)")
	}
}

func TestBuildOTPAuthURI_ContainsExpectedParams(t *testing.T) {
	secret := []byte("12345678901234567890")
	uri := BuildOTPAuthURI("Zaurak", "fulano", secret, DefaultConfig())

	wantSubstrings := []string{
		"otpauth://totp/Zaurak:fulano?",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
		"issuer=Zaurak",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(uri, want) {
			t.Errorf("expected URI to contain %q, got %s", want, uri)
		}
	}
}
