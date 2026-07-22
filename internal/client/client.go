// Package client provides the small typed Spotify Web API surface used by commands.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL = "https://api.spotify.com/v1"
	maxResponseBytes  = 1 << 20
	maxAttempts       = 3
	maxRetryDelay     = 5 * time.Minute
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
	Wait       func(context.Context, time.Duration) error
}

// User is the stable identity returned by Spotify's current-user endpoint.
type User struct {
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
	ID          string `json:"id"`
	URI         string `json:"uri"`
}

// Artist is the breadcrumb identity attached to a track.
type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Image is one Spotify-hosted album image.
type Image struct {
	URL    string `json:"url"`
	Height *int   `json:"height"`
	Width  *int   `json:"width"`
}

// Album is the parent breadcrumb attached to a track.
type Album struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Images []Image `json:"images"`
}

// ExternalURLs contains public Spotify web URLs.
type ExternalURLs struct {
	Spotify string `json:"spotify"`
}

// Restriction explains why a track is unavailable.
type Restriction struct {
	Reason string `json:"reason"`
}

// Track is the typed subset of Spotify track data rendered by the CLI.
type Track struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Artists      []Artist     `json:"artists"`
	Album        Album        `json:"album"`
	DurationMS   int          `json:"duration_ms"`
	URI          string       `json:"uri"`
	ExternalURLs ExternalURLs `json:"external_urls"`
	DiscNumber   int          `json:"disc_number"`
	TrackNumber  int          `json:"track_number"`
	Explicit     bool         `json:"explicit"`
	Restrictions Restriction  `json:"restrictions"`
}

// TrackPage is one validated Spotify search page.
type TrackPage struct {
	Items   []Track
	Offset  int
	Limit   int
	Total   int
	HasNext bool
}

// Me returns the current Spotify user's stable identity.
func (client Client) Me(ctx context.Context) (User, error) {
	var user User
	if err := client.getJSON(ctx, "/me", &user); err != nil {
		return User{}, err
	}
	if strings.TrimSpace(user.AccountID) == "" {
		return User{}, ErrInvalidResponse
	}
	return user, nil
}

type trackSearchResponse struct {
	Tracks *struct {
		Items  *[]Track `json:"items"`
		Limit  int      `json:"limit"`
		Next   *string  `json:"next"`
		Offset int      `json:"offset"`
		Total  int      `json:"total"`
	} `json:"tracks"`
}

// SearchTracks returns one track-search page without following provider pagination URLs.
func (client Client) SearchTracks(ctx context.Context, query string, limit, offset int) (TrackPage, error) {
	values := url.Values{"q": {query}, "type": {"track"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response trackSearchResponse
	if err := client.getJSON(ctx, "/search?"+values.Encode(), &response); err != nil {
		return TrackPage{}, err
	}
	if response.Tracks == nil || response.Tracks.Offset != offset || response.Tracks.Limit != limit ||
		response.Tracks.Items == nil || response.Tracks.Total < 0 || len(*response.Tracks.Items) > limit {
		return TrackPage{}, ErrInvalidResponse
	}
	return TrackPage{
		Items: *response.Tracks.Items, Offset: response.Tracks.Offset, Limit: response.Tracks.Limit,
		Total: response.Tracks.Total, HasNext: response.Tracks.Next != nil && *response.Tracks.Next != "",
	}, nil
}

func (client Client) getJSON(ctx context.Context, path string, target any) error {
	baseURL := strings.TrimRight(client.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			return ErrUpstream
		}
		response, err := httpClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return transportError{cause: err}
		}
		if delay, retry, valid := retryDelay(response, attempt); retry {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
			_ = response.Body.Close()
			if !valid || attempt == maxAttempts-1 {
				return ErrUpstream
			}
			if err := client.wait(ctx, delay); err != nil {
				return err
			}
			continue
		}
		switch response.StatusCode {
		case http.StatusUnauthorized:
			_ = response.Body.Close()
			return ErrUnauthorized
		case http.StatusForbidden:
			_ = response.Body.Close()
			return ErrForbidden
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			_ = response.Body.Close()
			return ErrUpstream
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		_ = response.Body.Close()
		if err != nil || len(body) > maxResponseBytes || json.Unmarshal(body, target) != nil {
			return ErrInvalidResponse
		}
		return nil
	}
	return ErrUpstream
}

func retryDelay(response *http.Response, attempt int) (time.Duration, bool, bool) {
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		seconds, err := strconv.ParseInt(strings.TrimSpace(response.Header.Get("Retry-After")), 10, 64)
		if err != nil || seconds < 0 || seconds > int64(maxRetryDelay/time.Second) {
			return 0, true, false
		}
		return time.Duration(seconds) * time.Second, true, true
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return time.Duration(1<<attempt) * 250 * time.Millisecond, true, true
	default:
		return 0, false, true
	}
}

func (client Client) wait(ctx context.Context, delay time.Duration) error {
	if client.Wait != nil {
		return client.Wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type transportError struct{ cause error }

func (err transportError) Error() string { return ErrUpstream.Error() }
func (err transportError) Unwrap() error { return err.cause }
func (err transportError) Is(target error) bool {
	return target == ErrUpstream || errors.Is(err.cause, target)
}
