package token

import (
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

func TestDecodeNormalizesEnvelope(t *testing.T) {
	raw := `{"version":1,"access_token":"access-canary","token_type":"bearer","refresh_token":"refresh-canary","expires_at":"2026-07-22T09:00:00-04:00","scopes":[" user-read-private ","user-read-private","playlist-read-private"]}`

	envelope, err := Decode([]byte(raw), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Encode(envelope, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"access_token":"access-canary","token_type":"Bearer","refresh_token":"refresh-canary","expires_at":"2026-07-22T13:00:00Z","scopes":["playlist-read-private","user-read-private"]}`
	if string(got) != want {
		t.Fatalf("Encode() = %s, want %s", got, want)
	}
}

func TestDecodeExpiryRules(t *testing.T) {
	tests := []struct {
		name    string
		refresh string
		expiry  string
		wantErr bool
	}{
		{name: "future access token", expiry: "2026-07-22T12:01:00Z"},
		{name: "expired refreshable token", refresh: `,"refresh_token":"refresh-canary"`, expiry: "2026-07-22T11:59:00Z"},
		{name: "expired access-only token", expiry: "2026-07-22T11:59:00Z", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"version":1,"access_token":"access-canary","token_type":"Bearer"` + tt.refresh + `,"expires_at":"` + tt.expiry + `","scopes":["user-read-private"]}`
			_, err := Decode([]byte(raw), fixedNow)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFractionalExpirySurvivesCanonicalRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 100, time.UTC)
	raw := `{"version":1,"access_token":"access","token_type":"Bearer","expires_at":"2026-07-22T12:00:00.2Z","scopes":["user-read-private"]}`
	envelope, err := Decode([]byte(raw), now)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := Encode(envelope, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(canonical, now); err != nil {
		t.Fatalf("canonical envelope no longer validates: %v; JSON = %s", err, canonical)
	}
}

func TestDecodeRejectsStrictlyWithoutLeaking(t *testing.T) {
	canary := "super-secret-canary"
	tests := []string{
		`{"version":2,"access_token":"` + canary + `","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`,
		`{"version":1,"access_token":"` + canary + `","token_type":"Basic","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]}`,
		`{"version":1,"access_token":"` + canary + `","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":[""]}`,
		`{"version":1,"access_token":"` + canary + `","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"],"extra":"` + canary + `"}`,
		`{"version":1,"access_token":"` + canary + `","token_type":"Bearer","expires_at":"2026-07-22T13:00:00Z","scopes":["user-read-private"]} trailing`,
	}
	for _, raw := range tests {
		_, err := Decode([]byte(raw), fixedNow)
		if err == nil {
			t.Fatalf("Decode(%q) unexpectedly succeeded", raw)
		}
		if strings.Contains(err.Error(), canary) {
			t.Fatalf("error leaked canary: %v", err)
		}
	}
}
