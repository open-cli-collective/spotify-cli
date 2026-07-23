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

	"github.com/open-cli-collective/spotify-cli/internal/spotifyref"
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

// Artist is the typed subset of Spotify artist data rendered by the CLI.
type Artist struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	URI          string       `json:"uri"`
	ExternalURLs ExternalURLs `json:"external_urls"`
	Images       []Image      `json:"images"`
}

// Image is one Spotify-hosted catalog image.
type Image struct {
	URL    string `json:"url"`
	Height *int   `json:"height"`
	Width  *int   `json:"width"`
}

// Album is the typed subset of Spotify album data rendered by the CLI.
type Album struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Artists              []Artist     `json:"artists"`
	Images               []Image      `json:"images"`
	ReleaseDate          string       `json:"release_date"`
	ReleaseDatePrecision string       `json:"release_date_precision"`
	TotalTracks          int          `json:"total_tracks"`
	AlbumType            string       `json:"album_type"`
	URI                  string       `json:"uri"`
	ExternalURLs         ExternalURLs `json:"external_urls"`
	Restrictions         Restriction  `json:"restrictions"`
}

// ExternalURLs contains public Spotify web URLs.
type ExternalURLs struct {
	Spotify string `json:"spotify"`
}

// Restriction explains why a catalog resource is unavailable.
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

// SavedTrack is one track and its library timestamp.
type SavedTrack struct {
	AddedAt string `json:"added_at"`
	Track   Track  `json:"track"`
}

// SavedTrackPage is one validated saved-track page.
type SavedTrackPage struct {
	Items   []SavedTrack
	Offset  int
	Limit   int
	Total   int
	HasNext bool
}

// AlbumPage is one validated Spotify album-search page.
type AlbumPage struct {
	Items   []Album
	Offset  int
	Limit   int
	Total   int
	HasNext bool
}

// ArtistPage is one validated Spotify artist-search page.
type ArtistPage struct {
	Items   []Artist
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

// GetTrack returns one track by ID.
func (client Client) GetTrack(ctx context.Context, id string) (Track, error) {
	var track Track
	if !spotifyref.ValidID(id) {
		return Track{}, ErrInvalidResponse
	}
	if err := client.getJSON(ctx, "/tracks/"+id, &track); err != nil {
		return Track{}, err
	}
	if strings.TrimSpace(track.ID) == "" {
		return Track{}, ErrInvalidResponse
	}
	return track, nil
}

// GetAlbum returns one album by ID.
func (client Client) GetAlbum(ctx context.Context, id string) (Album, error) {
	var album Album
	if !spotifyref.ValidID(id) {
		return Album{}, ErrInvalidResponse
	}
	if err := client.getJSON(ctx, "/albums/"+id, &album); err != nil {
		return Album{}, err
	}
	if strings.TrimSpace(album.ID) == "" {
		return Album{}, ErrInvalidResponse
	}
	return album, nil
}

// GetArtist returns one artist by ID.
func (client Client) GetArtist(ctx context.Context, id string) (Artist, error) {
	var artist Artist
	if !spotifyref.ValidID(id) {
		return Artist{}, ErrInvalidResponse
	}
	if err := client.getJSON(ctx, "/artists/"+id, &artist); err != nil {
		return Artist{}, err
	}
	if strings.TrimSpace(artist.ID) == "" {
		return Artist{}, ErrInvalidResponse
	}
	return artist, nil
}

type trackPageResponse struct {
	Items  *[]Track `json:"items"`
	Limit  int      `json:"limit"`
	Next   *string  `json:"next"`
	Offset int      `json:"offset"`
	Total  int      `json:"total"`
}

// ListAlbumTracks returns one album-track page without following provider pagination URLs.
func (client Client) ListAlbumTracks(ctx context.Context, id string, limit, offset int) (TrackPage, error) {
	if !spotifyref.ValidID(id) || limit < 1 || limit > 50 || offset < 0 {
		return TrackPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response trackPageResponse
	if err := client.getJSON(ctx, "/albums/"+id+"/tracks?"+values.Encode(), &response); err != nil {
		return TrackPage{}, err
	}
	if response.Offset != offset || response.Limit != limit || response.Items == nil ||
		response.Total < 0 || len(*response.Items) > limit {
		return TrackPage{}, ErrInvalidResponse
	}
	for _, track := range *response.Items {
		if strings.TrimSpace(track.ID) == "" {
			return TrackPage{}, ErrInvalidResponse
		}
	}
	return TrackPage{
		Items: *response.Items, Offset: response.Offset, Limit: response.Limit,
		Total: response.Total, HasNext: response.Next != nil && *response.Next != "",
	}, nil
}

type albumPageResponse struct {
	Items  *[]Album `json:"items"`
	Limit  int      `json:"limit"`
	Next   *string  `json:"next"`
	Offset int      `json:"offset"`
	Total  int      `json:"total"`
}

// ListArtistAlbums returns one artist-album page without following provider pagination URLs.
func (client Client) ListArtistAlbums(ctx context.Context, id string, limit, offset int) (AlbumPage, error) {
	if !spotifyref.ValidID(id) || limit < 1 || limit > 10 || offset < 0 {
		return AlbumPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response albumPageResponse
	if err := client.getJSON(ctx, "/artists/"+id+"/albums?"+values.Encode(), &response); err != nil {
		return AlbumPage{}, err
	}
	if response.Offset != offset || response.Limit != limit || response.Items == nil ||
		response.Total < 0 || len(*response.Items) > limit {
		return AlbumPage{}, ErrInvalidResponse
	}
	for _, album := range *response.Items {
		if strings.TrimSpace(album.ID) == "" {
			return AlbumPage{}, ErrInvalidResponse
		}
	}
	return AlbumPage{
		Items: *response.Items, Offset: response.Offset, Limit: response.Limit,
		Total: response.Total, HasNext: response.Next != nil && *response.Next != "",
	}, nil
}

type savedTrackPageResponse struct {
	Items  *[]SavedTrack `json:"items"`
	Limit  int           `json:"limit"`
	Next   *string       `json:"next"`
	Offset int           `json:"offset"`
	Total  int           `json:"total"`
}

// ListSavedTracks returns one saved-track page without following provider pagination URLs.
func (client Client) ListSavedTracks(ctx context.Context, limit, offset int) (SavedTrackPage, error) {
	if limit < 1 || limit > 50 || offset < 0 {
		return SavedTrackPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response savedTrackPageResponse
	if err := client.requestJSON(ctx, http.MethodGet, "/me/tracks?"+values.Encode(), &response); err != nil {
		return SavedTrackPage{}, err
	}
	if response.Offset != offset || response.Limit != limit || response.Items == nil ||
		response.Total < 0 || len(*response.Items) > limit {
		return SavedTrackPage{}, ErrInvalidResponse
	}
	for _, item := range *response.Items {
		if !spotifyref.ValidID(item.Track.ID) {
			return SavedTrackPage{}, ErrInvalidResponse
		}
		if _, err := time.Parse(time.RFC3339, item.AddedAt); err != nil {
			return SavedTrackPage{}, ErrInvalidResponse
		}
	}
	return SavedTrackPage{
		Items: *response.Items, Offset: response.Offset, Limit: response.Limit,
		Total: response.Total, HasNext: response.Next != nil && *response.Next != "",
	}, nil
}

// CheckSavedTracks reports saved membership in input order.
func (client Client) CheckSavedTracks(ctx context.Context, uris []string) ([]bool, error) {
	if !validTrackURIs(uris) {
		return nil, ErrInvalidResponse
	}
	result := make([]bool, 0, len(uris))
	for start := 0; start < len(uris); start += 40 {
		end := min(start+40, len(uris))
		values := url.Values{"uris": {strings.Join(uris[start:end], ",")}}
		var chunk []bool
		if err := client.requestJSON(ctx, http.MethodGet, "/me/library/contains?"+values.Encode(), &chunk); err != nil {
			return nil, err
		}
		if len(chunk) != end-start {
			return nil, ErrInvalidResponse
		}
		result = append(result, chunk...)
	}
	return result, nil
}

// SaveSavedTracks adds tracks to the current user's library.
func (client Client) SaveSavedTracks(ctx context.Context, uris []string) error {
	return client.mutateSavedTracks(ctx, http.MethodPut, uris)
}

// RemoveSavedTracks removes tracks from the current user's library.
func (client Client) RemoveSavedTracks(ctx context.Context, uris []string) error {
	return client.mutateSavedTracks(ctx, http.MethodDelete, uris)
}

func (client Client) mutateSavedTracks(ctx context.Context, method string, uris []string) error {
	if !validTrackURIs(uris) {
		return ErrInvalidResponse
	}
	for start := 0; start < len(uris); start += 40 {
		end := min(start+40, len(uris))
		values := url.Values{"uris": {strings.Join(uris[start:end], ",")}}
		if err := client.requestJSON(ctx, method, "/me/library?"+values.Encode(), nil); err != nil {
			return err
		}
	}
	return nil
}

func validTrackURIs(uris []string) bool {
	if len(uris) == 0 {
		return false
	}
	for _, uri := range uris {
		if !strings.HasPrefix(uri, "spotify:track:") || !spotifyref.ValidID(strings.TrimPrefix(uri, "spotify:track:")) {
			return false
		}
	}
	return true
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

type albumSearchResponse struct {
	Albums *struct {
		Items  *[]Album `json:"items"`
		Limit  int      `json:"limit"`
		Next   *string  `json:"next"`
		Offset int      `json:"offset"`
		Total  int      `json:"total"`
	} `json:"albums"`
}

// SearchAlbums returns one album-search page without following provider pagination URLs.
func (client Client) SearchAlbums(ctx context.Context, query string, limit, offset int) (AlbumPage, error) {
	values := url.Values{"q": {query}, "type": {"album"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response albumSearchResponse
	if err := client.getJSON(ctx, "/search?"+values.Encode(), &response); err != nil {
		return AlbumPage{}, err
	}
	if response.Albums == nil || response.Albums.Offset != offset || response.Albums.Limit != limit ||
		response.Albums.Items == nil || response.Albums.Total < 0 || len(*response.Albums.Items) > limit {
		return AlbumPage{}, ErrInvalidResponse
	}
	for _, album := range *response.Albums.Items {
		if strings.TrimSpace(album.ID) == "" {
			return AlbumPage{}, ErrInvalidResponse
		}
	}
	return AlbumPage{
		Items: *response.Albums.Items, Offset: response.Albums.Offset, Limit: response.Albums.Limit,
		Total: response.Albums.Total, HasNext: response.Albums.Next != nil && *response.Albums.Next != "",
	}, nil
}

type artistSearchResponse struct {
	Artists *struct {
		Items  *[]Artist `json:"items"`
		Limit  int       `json:"limit"`
		Next   *string   `json:"next"`
		Offset int       `json:"offset"`
		Total  int       `json:"total"`
	} `json:"artists"`
}

// SearchArtists returns one artist-search page without following provider pagination URLs.
func (client Client) SearchArtists(ctx context.Context, query string, limit, offset int) (ArtistPage, error) {
	values := url.Values{"q": {query}, "type": {"artist"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response artistSearchResponse
	if err := client.getJSON(ctx, "/search?"+values.Encode(), &response); err != nil {
		return ArtistPage{}, err
	}
	if response.Artists == nil || response.Artists.Offset != offset || response.Artists.Limit != limit ||
		response.Artists.Items == nil || response.Artists.Total < 0 || len(*response.Artists.Items) > limit {
		return ArtistPage{}, ErrInvalidResponse
	}
	for _, artist := range *response.Artists.Items {
		if strings.TrimSpace(artist.ID) == "" {
			return ArtistPage{}, ErrInvalidResponse
		}
	}
	return ArtistPage{
		Items: *response.Artists.Items, Offset: response.Artists.Offset, Limit: response.Artists.Limit,
		Total: response.Artists.Total, HasNext: response.Artists.Next != nil && *response.Artists.Next != "",
	}, nil
}

func (client Client) getJSON(ctx context.Context, path string, target any) error {
	return client.requestJSON(ctx, http.MethodGet, path, target)
}

func (client Client) requestJSON(ctx context.Context, method, path string, target any) error {
	baseURL := strings.TrimRight(client.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
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
		if err != nil || len(body) > maxResponseBytes {
			return ErrInvalidResponse
		}
		if target != nil && json.Unmarshal(body, target) != nil {
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
