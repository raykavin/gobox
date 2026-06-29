package oidcauth

import (
	"testing"
	"time"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{
			name:    "empty RealmURL",
			config:  Config{ClientID: "app"},
			wantErr: ErrInvalidRealmURL,
		},
		{
			name:    "invalid URL",
			config:  Config{RealmURL: "not a url", ClientID: "app"},
			wantErr: ErrInvalidRealmURL,
		},
		{
			name:    "non-HTTP scheme",
			config:  Config{RealmURL: "ftp://example.com/realm", ClientID: "app"},
			wantErr: ErrInvalidRealmURL,
		},
		{
			name:    "missing host",
			config:  Config{RealmURL: "https:///realm", ClientID: "app"},
			wantErr: ErrInvalidRealmURL,
		},
		{
			name:    "empty ClientID",
			config:  Config{RealmURL: "https://kc.example.com/realms/main"},
			wantErr: ErrEmptyClientID,
		},
		{
			name:   "valid HTTPS config",
			config: Config{RealmURL: "https://kc.example.com/realms/main", ClientID: "app"},
		},
		{
			name:   "valid HTTP config",
			config: Config{RealmURL: "http://localhost:8080/realms/main", ClientID: "app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.config)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateConfig_AppliesDefaultTimeout(t *testing.T) {
	cfg := Config{RealmURL: "https://kc.example.com/realms/main", ClientID: "app"}
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RequestTimeout != defaultRequestTimeout {
		t.Errorf("expected default timeout %v, got %v", defaultRequestTimeout, cfg.RequestTimeout)
	}
}

func TestValidateConfig_PreservesCustomTimeout(t *testing.T) {
	cfg := Config{
		RealmURL:       "https://kc.example.com/realms/main",
		ClientID:       "app",
		RequestTimeout: 10 * time.Second,
	}
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RequestTimeout != 10*time.Second {
		t.Errorf("expected 10s, got %v", cfg.RequestTimeout)
	}
}

func TestHashToken_Stability(t *testing.T) {
	h1 := hashToken("some-jwt-token")
	h2 := hashToken("some-jwt-token")
	if h1 != h2 {
		t.Errorf("same input produced different hashes: %q vs %q", h1, h2)
	}
}

func TestHashToken_Uniqueness(t *testing.T) {
	h1 := hashToken("token-a")
	h2 := hashToken("token-b")
	if h1 == h2 {
		t.Error("different inputs produced the same hash")
	}
}

func TestHashToken_EmptyInput(t *testing.T) {
	h1 := hashToken("")
	h2 := hashToken("")
	if h1 != h2 {
		t.Errorf("empty input produced inconsistent hashes: %q vs %q", h1, h2)
	}
}

func TestHashToken_Length(t *testing.T) {
	h := hashToken("test")
	if len(h) != 64 {
		t.Errorf("expected 64 hex chars (SHA-256), got %d", len(h))
	}
}
