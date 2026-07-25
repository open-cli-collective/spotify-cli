// Package client provides the small typed Spotify Web API surface used by commands.
package client

import (
	"bytes"
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

// MutationOutcomeUncertainError reports a request that may have taken effect.
type MutationOutcomeUncertainError struct {
	Cause  error
	Method string
	Path   string
}

func (err *MutationOutcomeUncertainError) Error() string {
	return "spotify mutation outcome is uncertain; inspect current state before retrying"
}

// Unwrap returns the post-send failure.
func (err *MutationOutcomeUncertainError) Unwrap() error { return err.Cause }

// MutationRejectedError reports a failure known not to have applied the mutation.
type MutationRejectedError struct {
	Cause      error
	Method     string
	Path       string
	StatusCode int
}

func (err *MutationRejectedError) Error() string { return err.Cause.Error() }

// Unwrap returns the sanitized provider failure.
func (err *MutationRejectedError) Unwrap() error { return err.Cause }

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

// PlaylistOwner is the stable owner identity included with a playlist.
type PlaylistOwner struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// PlaylistItemCount is the item-count subset returned with a playlist.
type PlaylistItemCount struct {
	Total int `json:"total"`
}

// Playlist is the typed subset of Spotify playlist data rendered by the CLI.
type Playlist struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Owner         PlaylistOwner      `json:"owner"`
	ItemCount     *PlaylistItemCount `json:"items"`
	Public        *bool              `json:"public"`
	Collaborative bool               `json:"collaborative"`
	Description   string             `json:"description"`
	URI           string             `json:"uri"`
	ExternalURLs  ExternalURLs       `json:"external_urls"`
	Images        []Image            `json:"images"`
	SnapshotID    string             `json:"snapshot_id"`
}

// PlaylistItem is one flattened mixed-media playlist entry.
type PlaylistItem struct {
	Type        string
	ID          string
	Name        string
	Artists     []Artist
	AlbumID     string
	AlbumName   string
	DurationMS  *int
	URI         string
	URL         string
	AddedAt     string
	AddedByID   string
	DiscNumber  int
	TrackNumber int
	Explicit    *bool
	Restriction string
	Images      []Image
}

// Page is one validated Spotify API result page.
type Page[T any] struct {
	Items   []T
	Offset  int
	Limit   int
	HasNext bool
}

// PlaylistPage is one validated current-user playlist page.
type PlaylistPage = Page[Playlist]

// PlaylistItemPage is one validated playlist-item page.
type PlaylistItemPage = Page[PlaylistItem]

// TrackPage is one validated Spotify search page.
type TrackPage = Page[Track]

// SavedTrack is one track and its library timestamp.
type SavedTrack struct {
	AddedAt string `json:"added_at"`
	Track   Track  `json:"track"`
}

// SavedTrackPage is one validated saved-track page.
type SavedTrackPage = Page[SavedTrack]

// SavedAlbum is one album and its library timestamp.
type SavedAlbum struct {
	AddedAt string `json:"added_at"`
	Album   Album  `json:"album"`
}

// SavedAlbumPage is one validated saved-album page.
type SavedAlbumPage = Page[SavedAlbum]

// AlbumPage is one validated Spotify album-search page.
type AlbumPage = Page[Album]

// ArtistPage is one validated Spotify artist-search page.
type ArtistPage = Page[Artist]

type pageResponse[T any] struct {
	Items  []T     `json:"items"`
	Limit  int     `json:"limit"`
	Next   *string `json:"next"`
	Offset int     `json:"offset"`
	Total  int     `json:"total"`
}

func (response pageResponse[T]) page(offset, limit int, validItem func(T) error) (Page[T], error) {
	if response.Offset != offset || response.Limit != limit || response.Items == nil ||
		response.Total < 0 || len(response.Items) > limit {
		return Page[T]{}, ErrInvalidResponse
	}
	if validItem != nil {
		for _, item := range response.Items {
			if err := validItem(item); err != nil {
				return Page[T]{}, err
			}
		}
	}
	return Page[T]{
		Items: response.Items, Offset: response.Offset, Limit: response.Limit,
		HasNext: response.Next != nil && *response.Next != "",
	}, nil
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

// GetPlaylist returns one playlist by ID.
func (client Client) GetPlaylist(ctx context.Context, id string) (Playlist, error) {
	var playlist Playlist
	if !spotifyref.ValidID(id) {
		return Playlist{}, ErrInvalidResponse
	}
	if err := client.getJSON(ctx, "/playlists/"+id, &playlist); err != nil {
		return Playlist{}, err
	}
	if playlist.ID != id || !validPlaylist(playlist) {
		return Playlist{}, ErrInvalidResponse
	}
	return playlist, nil
}

// ListCurrentUserPlaylists returns one current-user playlist page without following provider pagination URLs.
func (client Client) ListCurrentUserPlaylists(ctx context.Context, limit, offset int) (PlaylistPage, error) {
	if limit < 1 || limit > 50 || offset < 0 {
		return PlaylistPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response pageResponse[Playlist]
	if err := client.getJSON(ctx, "/me/playlists?"+values.Encode(), &response); err != nil {
		return PlaylistPage{}, err
	}
	return response.page(offset, limit, func(playlist Playlist) error {
		if !validPlaylist(playlist) {
			return ErrInvalidResponse
		}
		return nil
	})
}

func validPlaylist(playlist Playlist) bool {
	return spotifyref.ValidID(playlist.ID) && playlist.ItemCount != nil && playlist.ItemCount.Total >= 0
}

type playlistItemWrapper struct {
	AddedAt string `json:"added_at"`
	AddedBy struct {
		ID string `json:"id"`
	} `json:"added_by"`
	IsLocal bool            `json:"is_local"`
	Item    json.RawMessage `json:"item"`
}

// ListPlaylistItems returns one playlist-item page without following provider pagination URLs.
func (client Client) ListPlaylistItems(ctx context.Context, id string, limit, offset int) (PlaylistItemPage, error) {
	if !spotifyref.ValidID(id) || limit < 1 || limit > 50 || offset < 0 {
		return PlaylistItemPage{}, ErrInvalidResponse
	}
	values := url.Values{
		"additional_types": {"episode"},
		"limit":            {strconv.Itoa(limit)},
		"offset":           {strconv.Itoa(offset)},
	}
	var response pageResponse[playlistItemWrapper]
	if err := client.getJSON(ctx, "/playlists/"+id+"/items?"+values.Encode(), &response); err != nil {
		return PlaylistItemPage{}, err
	}
	page, err := response.page(offset, limit, nil)
	if err != nil {
		return PlaylistItemPage{}, err
	}
	itemCount := len(page.Items)
	if itemCount > 0 && (itemCount > response.Total || response.Offset > response.Total-itemCount) ||
		page.HasNext && itemCount != response.Limit ||
		page.HasNext != (response.Offset < response.Total && itemCount < response.Total-response.Offset) {
		return PlaylistItemPage{}, ErrInvalidResponse
	}
	items := make([]PlaylistItem, len(page.Items))
	for index, wrapper := range page.Items {
		item, err := decodePlaylistItem(wrapper)
		if err != nil {
			return PlaylistItemPage{}, err
		}
		items[index] = item
	}
	return PlaylistItemPage{Items: items, Offset: page.Offset, Limit: page.Limit, HasNext: page.HasNext}, nil
}

type playlistSnapshotResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

type playlistItemReference struct {
	URI       string `json:"uri"`
	Positions []int  `json:"positions,omitempty"`
}

// AddPlaylistItems adds one ordered batch of tracks and returns the new snapshot.
func (client Client) AddPlaylistItems(ctx context.Context, id string, uris []string, position *int) (string, error) {
	if !spotifyref.ValidID(id) || len(uris) > 100 || !validLibraryURIs(spotifyref.Track, uris) ||
		position != nil && *position < 0 {
		return "", ErrInvalidResponse
	}
	body := struct {
		URIs     []string `json:"uris"`
		Position *int     `json:"position,omitempty"`
	}{URIs: uris, Position: position}
	path := "/playlists/" + id + "/items"
	var response playlistSnapshotResponse
	if err := client.mutateJSON(ctx, http.MethodPost, path, body, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.SnapshotID) == "" {
		return "", mutationOutcomeUncertain(http.MethodPost, path, ErrInvalidResponse)
	}
	return response.SnapshotID, nil
}

// RemovePlaylistItemsByURI removes every occurrence of one track URI against a known snapshot.
func (client Client) RemovePlaylistItemsByURI(ctx context.Context, id, uri, snapshotID string) (string, error) {
	if !spotifyref.ValidID(id) || !validLibraryURIs(spotifyref.Track, []string{uri}) || strings.TrimSpace(snapshotID) == "" {
		return "", ErrInvalidResponse
	}
	return client.removePlaylistItems(ctx, id, []playlistItemReference{{URI: uri}}, snapshotID)
}

// RemovePlaylistItemAtPosition removes one specific track occurrence against a known snapshot.
func (client Client) RemovePlaylistItemAtPosition(ctx context.Context, id, uri string, position int, snapshotID string) (string, error) {
	if !spotifyref.ValidID(id) || !validLibraryURIs(spotifyref.Track, []string{uri}) || position < 0 || strings.TrimSpace(snapshotID) == "" {
		return "", ErrInvalidResponse
	}
	return client.removePlaylistItems(ctx, id, []playlistItemReference{{URI: uri, Positions: []int{position}}}, snapshotID)
}

func (client Client) removePlaylistItems(ctx context.Context, id string, items []playlistItemReference, snapshotID string) (string, error) {
	body := struct {
		Items      []playlistItemReference `json:"items"`
		SnapshotID string                  `json:"snapshot_id"`
	}{Items: items, SnapshotID: snapshotID}
	path := "/playlists/" + id + "/items"
	var response playlistSnapshotResponse
	if err := client.mutateJSON(ctx, http.MethodDelete, path, body, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.SnapshotID) == "" {
		return "", mutationOutcomeUncertain(http.MethodDelete, path, ErrInvalidResponse)
	}
	return response.SnapshotID, nil
}

func decodePlaylistItem(wrapper playlistItemWrapper) (PlaylistItem, error) {
	item := PlaylistItem{AddedAt: wrapper.AddedAt, AddedByID: wrapper.AddedBy.ID}
	raw := bytes.TrimSpace(wrapper.Item)
	if len(raw) == 0 {
		return PlaylistItem{}, ErrInvalidResponse
	}
	if bytes.Equal(raw, []byte("null")) {
		item.Type = "unavailable"
		return item, nil
	}
	var base struct {
		Type         *string      `json:"type"`
		ID           *string      `json:"id"`
		Name         string       `json:"name"`
		DurationMS   *int         `json:"duration_ms"`
		URI          string       `json:"uri"`
		ExternalURLs ExternalURLs `json:"external_urls"`
		Explicit     *bool        `json:"explicit"`
		Restrictions Restriction  `json:"restrictions"`
		Images       *[]Image     `json:"images"`
	}
	if json.Unmarshal(raw, &base) != nil || base.DurationMS != nil && *base.DurationMS < 0 ||
		base.ID != nil && !spotifyref.ValidID(*base.ID) {
		return PlaylistItem{}, ErrInvalidResponse
	}
	item.Type = "unknown"
	if base.Type != nil && strings.TrimSpace(*base.Type) != "" {
		item.Type = *base.Type
	}
	if wrapper.IsLocal {
		item.Type = "local"
	}
	if (item.Type == "track" || item.Type == "episode") && base.ID == nil {
		return PlaylistItem{}, ErrInvalidResponse
	}
	if base.ID != nil {
		item.ID = *base.ID
	}
	item.Name = base.Name
	item.DurationMS = base.DurationMS
	item.URI = base.URI
	item.URL = base.ExternalURLs.Spotify
	item.Explicit = base.Explicit
	item.Restriction = base.Restrictions.Reason
	if item.Type == "episode" {
		if base.Images == nil {
			return PlaylistItem{}, ErrInvalidResponse
		}
		item.Images = *base.Images
	}
	if item.Type == "track" {
		var track struct {
			Artists     *[]Artist `json:"artists"`
			DiscNumber  int       `json:"disc_number"`
			TrackNumber int       `json:"track_number"`
			Album       *struct {
				ID     string   `json:"id"`
				Name   string   `json:"name"`
				Images *[]Image `json:"images"`
			} `json:"album"`
		}
		if json.Unmarshal(raw, &track) != nil || track.Artists == nil || len(*track.Artists) == 0 ||
			track.Album == nil || !spotifyref.ValidID(track.Album.ID) || track.Album.Images == nil {
			return PlaylistItem{}, ErrInvalidResponse
		}
		for _, artist := range *track.Artists {
			if !spotifyref.ValidID(artist.ID) {
				return PlaylistItem{}, ErrInvalidResponse
			}
		}
		item.Artists = *track.Artists
		item.AlbumID = track.Album.ID
		item.AlbumName = track.Album.Name
		item.DiscNumber = track.DiscNumber
		item.TrackNumber = track.TrackNumber
		item.Images = *track.Album.Images
	}
	if item.Type != "track" && item.Type != "episode" {
		item.Explicit = nil
		item.Restriction = ""
		item.Images = nil
	}
	return item, nil
}

// ListAlbumTracks returns one album-track page without following provider pagination URLs.
func (client Client) ListAlbumTracks(ctx context.Context, id string, limit, offset int) (TrackPage, error) {
	if !spotifyref.ValidID(id) || limit < 1 || limit > 50 || offset < 0 {
		return TrackPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response pageResponse[Track]
	if err := client.getJSON(ctx, "/albums/"+id+"/tracks?"+values.Encode(), &response); err != nil {
		return TrackPage{}, err
	}
	return response.page(offset, limit, func(track Track) error {
		if strings.TrimSpace(track.ID) == "" {
			return ErrInvalidResponse
		}
		return nil
	})
}

// ListArtistAlbums returns one artist-album page without following provider pagination URLs.
func (client Client) ListArtistAlbums(ctx context.Context, id string, limit, offset int) (AlbumPage, error) {
	if !spotifyref.ValidID(id) || limit < 1 || limit > 10 || offset < 0 {
		return AlbumPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response pageResponse[Album]
	if err := client.getJSON(ctx, "/artists/"+id+"/albums?"+values.Encode(), &response); err != nil {
		return AlbumPage{}, err
	}
	return response.page(offset, limit, func(album Album) error {
		if strings.TrimSpace(album.ID) == "" {
			return ErrInvalidResponse
		}
		return nil
	})
}

// ListSavedTracks returns one saved-track page without following provider pagination URLs.
func (client Client) ListSavedTracks(ctx context.Context, limit, offset int) (SavedTrackPage, error) {
	if limit < 1 || limit > 50 || offset < 0 {
		return SavedTrackPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response pageResponse[SavedTrack]
	if err := client.getJSON(ctx, "/me/tracks?"+values.Encode(), &response); err != nil {
		return SavedTrackPage{}, err
	}
	return response.page(offset, limit, func(item SavedTrack) error {
		if !spotifyref.ValidID(item.Track.ID) {
			return ErrInvalidResponse
		}
		if _, err := time.Parse(time.RFC3339, item.AddedAt); err != nil {
			return ErrInvalidResponse
		}
		return nil
	})
}

// ListSavedAlbums returns one saved-album page without following provider pagination URLs.
func (client Client) ListSavedAlbums(ctx context.Context, limit, offset int) (SavedAlbumPage, error) {
	if limit < 1 || limit > 50 || offset < 0 {
		return SavedAlbumPage{}, ErrInvalidResponse
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var response pageResponse[SavedAlbum]
	if err := client.getJSON(ctx, "/me/albums?"+values.Encode(), &response); err != nil {
		return SavedAlbumPage{}, err
	}
	return response.page(offset, limit, func(item SavedAlbum) error {
		if !spotifyref.ValidID(item.Album.ID) || len(item.Album.Artists) == 0 {
			return ErrInvalidResponse
		}
		for _, artist := range item.Album.Artists {
			if !spotifyref.ValidID(artist.ID) {
				return ErrInvalidResponse
			}
		}
		if _, err := time.Parse(time.RFC3339, item.AddedAt); err != nil {
			return ErrInvalidResponse
		}
		return nil
	})
}

// CheckSavedTracks reports saved membership in input order.
func (client Client) CheckSavedTracks(ctx context.Context, uris []string) ([]bool, error) {
	return client.checkSavedItems(ctx, spotifyref.Track, uris)
}

// CheckSavedAlbums reports saved membership in input order.
func (client Client) CheckSavedAlbums(ctx context.Context, uris []string) ([]bool, error) {
	return client.checkSavedItems(ctx, spotifyref.Album, uris)
}

func (client Client) checkSavedItems(ctx context.Context, kind spotifyref.Kind, uris []string) ([]bool, error) {
	if !validLibraryURIs(kind, uris) {
		return nil, ErrInvalidResponse
	}
	result := make([]bool, 0, len(uris))
	for start := 0; start < len(uris); start += 40 {
		end := min(start+40, len(uris))
		values := url.Values{"uris": {strings.Join(uris[start:end], ",")}}
		var chunk []bool
		if err := client.getJSON(ctx, "/me/library/contains?"+values.Encode(), &chunk); err != nil {
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
	return client.mutateSavedItems(ctx, http.MethodPut, spotifyref.Track, uris)
}

// RemoveSavedTracks removes tracks from the current user's library.
func (client Client) RemoveSavedTracks(ctx context.Context, uris []string) error {
	return client.mutateSavedItems(ctx, http.MethodDelete, spotifyref.Track, uris)
}

// SaveSavedAlbums adds albums to the current user's library.
func (client Client) SaveSavedAlbums(ctx context.Context, uris []string) error {
	return client.mutateSavedItems(ctx, http.MethodPut, spotifyref.Album, uris)
}

// RemoveSavedAlbums removes albums from the current user's library.
func (client Client) RemoveSavedAlbums(ctx context.Context, uris []string) error {
	return client.mutateSavedItems(ctx, http.MethodDelete, spotifyref.Album, uris)
}

func (client Client) mutateSavedItems(ctx context.Context, method string, kind spotifyref.Kind, uris []string) error {
	if !validLibraryURIs(kind, uris) {
		return ErrInvalidResponse
	}
	for start := 0; start < len(uris); start += 40 {
		end := min(start+40, len(uris))
		values := url.Values{"uris": {strings.Join(uris[start:end], ",")}}
		if err := client.mutateRequest(ctx, method, "/me/library?"+values.Encode()); err != nil {
			return err
		}
	}
	return nil
}

func validLibraryURIs(kind spotifyref.Kind, uris []string) bool {
	if len(uris) == 0 {
		return false
	}
	prefix := "spotify:" + string(kind) + ":"
	for _, uri := range uris {
		if !strings.HasPrefix(uri, prefix) || !spotifyref.ValidID(strings.TrimPrefix(uri, prefix)) {
			return false
		}
	}
	return true
}

type trackSearchResponse struct {
	Tracks pageResponse[Track] `json:"tracks"`
}

// SearchTracks returns one track-search page without following provider pagination URLs.
func (client Client) SearchTracks(ctx context.Context, query string, limit, offset int) (TrackPage, error) {
	var response trackSearchResponse
	if err := client.getJSON(ctx, searchPath("track", query, limit, offset), &response); err != nil {
		return TrackPage{}, err
	}
	return response.Tracks.page(offset, limit, nil)
}

type albumSearchResponse struct {
	Albums pageResponse[Album] `json:"albums"`
}

// SearchAlbums returns one album-search page without following provider pagination URLs.
func (client Client) SearchAlbums(ctx context.Context, query string, limit, offset int) (AlbumPage, error) {
	var response albumSearchResponse
	if err := client.getJSON(ctx, searchPath("album", query, limit, offset), &response); err != nil {
		return AlbumPage{}, err
	}
	return response.Albums.page(offset, limit, func(album Album) error {
		if strings.TrimSpace(album.ID) == "" {
			return ErrInvalidResponse
		}
		return nil
	})
}

type artistSearchResponse struct {
	Artists pageResponse[Artist] `json:"artists"`
}

// SearchArtists returns one artist-search page without following provider pagination URLs.
func (client Client) SearchArtists(ctx context.Context, query string, limit, offset int) (ArtistPage, error) {
	var response artistSearchResponse
	if err := client.getJSON(ctx, searchPath("artist", query, limit, offset), &response); err != nil {
		return ArtistPage{}, err
	}
	return response.Artists.page(offset, limit, func(artist Artist) error {
		if strings.TrimSpace(artist.ID) == "" {
			return ErrInvalidResponse
		}
		return nil
	})
}

func searchPath(kind, query string, limit, offset int) string {
	values := url.Values{"q": {query}, "type": {kind}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	return "/search?" + values.Encode()
}

func (client Client) getJSON(ctx context.Context, path string, target any) error {
	return client.doJSON(ctx, http.MethodGet, path, nil, target, false)
}

func (client Client) mutateRequest(ctx context.Context, method, path string) error {
	return client.doJSON(ctx, method, path, nil, nil, true)
}

func (client Client) mutateJSON(ctx context.Context, method, path string, body, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return mutationRejected(method, path, 0, ErrInvalidResponse)
	}
	return client.doJSON(ctx, method, path, encoded, target, true)
}

func (client Client) doJSON(ctx context.Context, method, path string, body []byte, target any, mutation bool) error {
	baseURL := strings.TrimRight(client.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	attempts := maxAttempts
	if mutation {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(body))
		if err != nil {
			if mutation {
				return mutationRejected(method, path, 0, ErrUpstream)
			}
			return ErrUpstream
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := httpClient.Do(request)
		if err != nil {
			cause := error(transportError{cause: err})
			if ctx.Err() != nil {
				cause = ctx.Err()
			}
			if mutation {
				return mutationOutcomeUncertain(method, path, cause)
			}
			return cause
		}
		if mutation && (response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices) {
			_ = response.Body.Close()
			cause := ErrUpstream
			switch response.StatusCode {
			case http.StatusUnauthorized:
				cause = ErrUnauthorized
			case http.StatusForbidden:
				cause = ErrForbidden
			}
			if response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError {
				return mutationRejected(method, path, response.StatusCode, cause)
			}
			return mutationOutcomeUncertain(method, path, cause)
		}
		if delay, retry, valid := retryDelay(response, attempt); retry {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
			_ = response.Body.Close()
			if !valid || attempt == attempts-1 {
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
		responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		_ = response.Body.Close()
		if err != nil {
			if mutation {
				return mutationOutcomeUncertain(method, path, errors.Join(ErrInvalidResponse, err))
			}
			return ErrInvalidResponse
		}
		if len(responseBody) > maxResponseBytes {
			if mutation {
				return mutationOutcomeUncertain(method, path, ErrInvalidResponse)
			}
			return ErrInvalidResponse
		}
		if target != nil {
			if err := json.Unmarshal(responseBody, target); err != nil {
				if mutation {
					return mutationOutcomeUncertain(method, path, errors.Join(ErrInvalidResponse, err))
				}
				return ErrInvalidResponse
			}
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

func mutationOutcomeUncertain(method, path string, cause error) error {
	return &MutationOutcomeUncertainError{Cause: cause, Method: method, Path: path}
}

func mutationRejected(method, path string, statusCode int, cause error) error {
	return &MutationRejectedError{Cause: cause, Method: method, Path: path, StatusCode: statusCode}
}
