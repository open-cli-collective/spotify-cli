package pagetoken

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestRoundTripAndStableEncoding(t *testing.T) {
	const scope = "album-tracks:0123456789ABCDEFGHIJKL"
	token := Encode(scope, 50)
	if token != base64.RawURLEncoding.EncodeToString([]byte("v1:"+scope+":50")) {
		t.Fatalf("token=%q", token)
	}
	offset, err := Decode(scope, token, 1000)
	if err != nil || offset != 50 {
		t.Fatalf("offset=%d error=%v", offset, err)
	}
}

func TestDecodeRejectsInvalidTokens(t *testing.T) {
	encoded := func(value string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(value))
	}
	for _, token := range []string{
		"not-base64!",
		encoded("v2:track:1"),
		encoded("v1:album:1"),
		encoded("v1:track:-1"),
		encoded("v1:track:not-a-number"),
		encoded("v1:track:1001"),
		strings.Repeat("a", maxEncodedLength+1),
	} {
		if _, err := Decode("track", token, 1000); err == nil {
			t.Fatalf("token %q accepted", token)
		}
	}
}

func TestDecodeEmptyTokenStartsAtZero(t *testing.T) {
	offset, err := Decode("track", "", 1000)
	if err != nil || offset != 0 {
		t.Fatalf("offset=%d error=%v", offset, err)
	}
}
