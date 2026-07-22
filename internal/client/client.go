// Package client provides the small typed Spotify Web API surface used by commands.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const (
	defaultAPIBaseURL = "https://api.spotify.com/v1"
	maxResponseBytes  = 1 << 20
)

var (
	// ErrUnauthorized reports an invalid Spotify access token.
	ErrUnauthorized = errors.New("spotify authorization is invalid; run sptfy init")
	// ErrForbidden reports an account or scope mismatch.
	ErrForbidden = errors.New("spotify authorization lacks the required access; run sptfy init")
	// ErrUpstream reports an unavailable or failed Spotify request.
	ErrUpstream = errors.New("spotify is unreachable or returned an error")
	// ErrInvalidResponse reports a malformed Spotify response.
	ErrInvalidResponse = errors.New("spotify returned an invalid response")
)

// Client calls the Spotify Web API with an already-authenticated HTTP client.
type Client struct {
	HTTPClient *http.Client
	BaseURL    string
}

// User is the stable identity returned by Spotify's current-user endpoint.
type User struct {
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
	ID          string `json:"id"`
	URI         string `json:"uri"`
}

// Me returns the current Spotify user's stable identity.
func (client Client) Me(ctx context.Context) (User, error) {
	baseURL := strings.TrimRight(client.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/me", nil)
	if err != nil {
		return User{}, ErrUpstream
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return User{}, ctx.Err()
		}
		return User{}, transportError{cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return User{}, ErrUnauthorized
	case http.StatusForbidden:
		return User{}, ErrForbidden
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return User{}, ErrUpstream
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return User{}, ErrInvalidResponse
	}
	var user User
	if err := json.Unmarshal(body, &user); err != nil || strings.TrimSpace(user.AccountID) == "" {
		return User{}, ErrInvalidResponse
	}
	return user, nil
}

type transportError struct{ cause error }

func (err transportError) Error() string { return ErrUpstream.Error() }
func (err transportError) Unwrap() error { return err.cause }
func (err transportError) Is(target error) bool {
	return target == ErrUpstream || errors.Is(err.cause, target)
}
