package oidcauth

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestAudienceUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    Audience
		wantErr bool
	}{
		{name: "array", payload: `{"aud":["zaurak","account"]}`, want: Audience{"zaurak", "account"}},
		{name: "single string", payload: `{"aud":"zaurak"}`, want: Audience{"zaurak"}},
		{name: "empty array", payload: `{"aud":[]}`, want: Audience{}},
		{name: "null", payload: `{"aud":null}`, want: nil},
		{name: "absent", payload: `{}`, want: nil},
		{name: "number", payload: `{"aud":42}`, wantErr: true},
		{name: "object", payload: `{"aud":{"a":1}}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var claims Claims
			err := json.Unmarshal([]byte(tt.payload), &claims)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got aud = %#v", claims.Aud)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !slices.Equal(claims.Aud, tt.want) {
				t.Errorf("aud = %#v, want %#v", claims.Aud, tt.want)
			}
		})
	}
}

// A string aud must not stop the rest of the claim set from decoding: the
// regression this guards against surfaced as a whole-token verification
// failure, not just an empty Aud.
func TestAudienceUnmarshalJSON_SiblingClaimsSurvive(t *testing.T) {
	var claims Claims
	if err := json.Unmarshal([]byte(`{"aud":"zaurak","sub":"user-123","azp":"zaurak"}`), &claims); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if claims.Sub != "user-123" || claims.Azp != "zaurak" {
		t.Errorf("sibling claims lost: sub=%q azp=%q", claims.Sub, claims.Azp)
	}
}
