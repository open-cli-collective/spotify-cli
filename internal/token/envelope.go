// Package token defines the credential-store wire format for Spotify OAuth tokens.
package token

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// Version is the current credential envelope format.
const Version = 1

// Envelope is the normalized in-memory OAuth credential.
type Envelope struct {
	AccessToken  string
	TokenType    string
	RefreshToken string
	ExpiresAt    time.Time
	Scopes       []string
}

type inputEnvelope struct {
	Version      *int      `json:"version"`
	AccessToken  *string   `json:"access_token"`
	TokenType    *string   `json:"token_type"`
	RefreshToken *string   `json:"refresh_token"`
	ExpiresAt    *string   `json:"expires_at"`
	Scopes       *[]string `json:"scopes"`
}

type outputEnvelope struct {
	Version      int      `json:"version"`
	AccessToken  string   `json:"access_token"`
	TokenType    string   `json:"token_type"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	ExpiresAt    string   `json:"expires_at"`
	Scopes       []string `json:"scopes"`
}

// Decode strictly parses and normalizes an OAuth credential without echoing input.
func Decode(data []byte, now time.Time) (Envelope, error) {
	var wire inputEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Envelope{}, errors.New("invalid oauth token envelope JSON")
	}
	if err := requireEOF(decoder); err != nil {
		return Envelope{}, err
	}

	if wire.Version == nil || *wire.Version != Version {
		return Envelope{}, fmt.Errorf("oauth token envelope version must be %d", Version)
	}
	if wire.AccessToken == nil || strings.TrimSpace(*wire.AccessToken) == "" {
		return Envelope{}, errors.New("oauth token envelope access_token is required")
	}
	if wire.TokenType == nil || !strings.EqualFold(strings.TrimSpace(*wire.TokenType), "Bearer") {
		return Envelope{}, errors.New("oauth token envelope token_type must be Bearer")
	}
	if wire.RefreshToken != nil && strings.TrimSpace(*wire.RefreshToken) == "" {
		return Envelope{}, errors.New("oauth token envelope refresh_token must be non-empty when present")
	}
	if wire.ExpiresAt == nil {
		return Envelope{}, errors.New("oauth token envelope expires_at is required")
	}
	expiresAt, err := time.Parse(time.RFC3339, *wire.ExpiresAt)
	if err != nil {
		return Envelope{}, errors.New("oauth token envelope expires_at must be RFC3339")
	}
	if wire.Scopes == nil || len(*wire.Scopes) == 0 {
		return Envelope{}, errors.New("oauth token envelope scopes must not be empty")
	}
	scopes, err := normalizeScopes(*wire.Scopes)
	if err != nil {
		return Envelope{}, err
	}

	envelope := Envelope{
		AccessToken: *wire.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt.UTC(),
		Scopes:      scopes,
	}
	if wire.RefreshToken != nil {
		envelope.RefreshToken = *wire.RefreshToken
	}
	if !envelope.ExpiresAt.After(now) && envelope.RefreshToken == "" {
		return Envelope{}, errors.New("oauth token envelope is expired and has no refresh_token")
	}
	return envelope, nil
}

// Encode validates and emits the canonical version-1 JSON representation.
func Encode(envelope Envelope, now time.Time) ([]byte, error) {
	scopes, err := normalizeScopes(envelope.Scopes)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(envelope.AccessToken) == "" {
		return nil, errors.New("oauth token envelope access_token is required")
	}
	if !strings.EqualFold(strings.TrimSpace(envelope.TokenType), "Bearer") {
		return nil, errors.New("oauth token envelope token_type must be Bearer")
	}
	if envelope.ExpiresAt.IsZero() {
		return nil, errors.New("oauth token envelope expires_at is required")
	}
	if envelope.RefreshToken != "" && strings.TrimSpace(envelope.RefreshToken) == "" {
		return nil, errors.New("oauth token envelope refresh_token must be non-empty when present")
	}
	if !envelope.ExpiresAt.After(now) && strings.TrimSpace(envelope.RefreshToken) == "" {
		return nil, errors.New("oauth token envelope is expired and has no refresh_token")
	}

	wire := outputEnvelope{
		Version:      Version,
		AccessToken:  envelope.AccessToken,
		TokenType:    "Bearer",
		RefreshToken: envelope.RefreshToken,
		ExpiresAt:    envelope.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Scopes:       scopes,
	}
	return json.Marshal(wire) // #nosec G117 -- this function intentionally serializes the credential-store payload.
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid oauth token envelope JSON")
	}
	return nil
}

func normalizeScopes(input []string) ([]string, error) {
	seen := make(map[string]struct{}, len(input))
	for _, value := range input {
		scope := strings.TrimSpace(value)
		if scope == "" {
			return nil, errors.New("oauth token envelope scopes must contain only non-empty strings")
		}
		seen[scope] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, errors.New("oauth token envelope scopes must not be empty")
	}
	result := make([]string, 0, len(seen))
	for scope := range seen {
		result = append(result, scope)
	}
	slices.Sort(result)
	return result, nil
}
