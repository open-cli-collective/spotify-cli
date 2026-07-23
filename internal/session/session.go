// Package session opens one authenticated Spotify API session from persisted state.
package session

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"golang.org/x/oauth2"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

// CredentialStore is the credential capability required by a session.
type CredentialStore interface {
	Close() error
	Get(profile, key string) (string, error)
	Set(profile, key, value string, opts ...credstore.SetOpt) error
}

// StoreOpener opens the credential capability required by a session.
type StoreOpener func(credentials.OpenRequest) (CredentialStore, error)

// Opener contains the effects needed to restore an authenticated session.
type Opener struct {
	Scope      statedir.Scope
	OpenStore  StoreOpener
	Now        func() time.Time
	HTTPClient *http.Client
	TokenURL   string
	APIBaseURL string
}

// Session exposes only the authenticated client, granted scopes, and lifecycle.
type Session struct {
	client client.Client
	mu     sync.RWMutex
	scopes []string
	close  func() error
}

// New creates a session around an authenticated client.
func New(spotifyClient client.Client, scopes []string, closeSession func() error) *Session {
	return &Session{client: spotifyClient, scopes: append([]string(nil), scopes...), close: closeSession}
}

// Me returns the authenticated Spotify identity.
func (session *Session) Me(ctx context.Context) (client.User, error) {
	return session.client.Me(ctx)
}

// GetTrack returns one track with the authenticated Spotify client.
func (session *Session) GetTrack(ctx context.Context, id string) (client.Track, error) {
	return session.client.GetTrack(ctx, id)
}

// GetAlbum returns one album with the authenticated Spotify client.
func (session *Session) GetAlbum(ctx context.Context, id string) (client.Album, error) {
	return session.client.GetAlbum(ctx, id)
}

// GetArtist returns one artist with the authenticated Spotify client.
func (session *Session) GetArtist(ctx context.Context, id string) (client.Artist, error) {
	return session.client.GetArtist(ctx, id)
}

// ListAlbumTracks lists tracks with the authenticated Spotify client.
func (session *Session) ListAlbumTracks(ctx context.Context, id string, limit, offset int) (client.TrackPage, error) {
	return session.client.ListAlbumTracks(ctx, id, limit, offset)
}

// ListArtistAlbums lists albums with the authenticated Spotify client.
func (session *Session) ListArtistAlbums(ctx context.Context, id string, limit, offset int) (client.AlbumPage, error) {
	return session.client.ListArtistAlbums(ctx, id, limit, offset)
}

// SearchTracks searches tracks with the authenticated Spotify client.
func (session *Session) SearchTracks(ctx context.Context, query string, limit, offset int) (client.TrackPage, error) {
	return session.client.SearchTracks(ctx, query, limit, offset)
}

// SearchAlbums searches albums with the authenticated Spotify client.
func (session *Session) SearchAlbums(ctx context.Context, query string, limit, offset int) (client.AlbumPage, error) {
	return session.client.SearchAlbums(ctx, query, limit, offset)
}

// SearchArtists searches artists with the authenticated Spotify client.
func (session *Session) SearchArtists(ctx context.Context, query string, limit, offset int) (client.ArtistPage, error) {
	return session.client.SearchArtists(ctx, query, limit, offset)
}

// Scopes returns the scopes currently attached to the persisted OAuth token.
func (session *Session) Scopes() []string {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return append([]string(nil), session.scopes...)
}

// Close releases the credential store retained for refresh persistence.
func (session *Session) Close() error {
	if session == nil || session.close == nil {
		return nil
	}
	return session.close()
}

// Open restores the persisted credential and creates an OAuth-backed API client.
func (opener Opener) Open(ctx context.Context, backend string, backendSet bool) (*Session, error) {
	configValue, err := config.Load(opener.Scope)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(configValue.ClientID) == "" {
		return nil, errors.New("spotify client ID is not configured; run sptfy init")
	}
	profile, err := credentials.ParseProfile(configValue.CredentialRef)
	if err != nil {
		return nil, err
	}
	store, err := opener.OpenStore(credentials.OpenRequest{
		Config: configValue, Backend: backend, BackendSet: backendSet,
	})
	if err != nil {
		return nil, errors.New("opening credential store failed")
	}
	raw, err := store.Get(profile, credentials.OAuthTokenKey)
	if errors.Is(err, credstore.ErrNotFound) {
		_ = store.Close()
		return nil, errors.New("spotify authorization is not configured; run sptfy init")
	}
	if err != nil {
		_ = store.Close()
		return nil, errors.New("reading Spotify authorization failed")
	}
	now := time.Now
	if opener.Now != nil {
		now = opener.Now
	}
	envelope, err := token.Decode([]byte(raw), now())
	if err != nil {
		_ = store.Close()
		return nil, errors.New("stored Spotify authorization is invalid; run sptfy init")
	}
	authenticated := New(client.Client{}, envelope.Scopes, store.Close)
	persist := func(value token.Envelope) error {
		encoded, err := token.Encode(value, now())
		if err != nil {
			return err
		}
		if err := store.Set(profile, credentials.OAuthTokenKey, string(encoded), credstore.WithOverwrite()); err != nil {
			return err
		}
		authenticated.mu.Lock()
		authenticated.scopes = append(authenticated.scopes[:0], value.Scopes...)
		authenticated.mu.Unlock()
		return nil
	}
	tokenSource := auth.NewTokenSource(ctx, opener.HTTPClient, configValue.ClientID, opener.TokenURL, envelope, persist)
	oauthContext := ctx
	if opener.HTTPClient != nil {
		oauthContext = context.WithValue(ctx, oauth2.HTTPClient, opener.HTTPClient)
	}
	authenticated.client = client.Client{
		HTTPClient: oauth2.NewClient(oauthContext, tokenSource),
		BaseURL:    opener.APIBaseURL,
	}
	return authenticated, nil
}
