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

// OpenFunc opens an authenticated session for a command invocation.
type OpenFunc func(context.Context, string, bool) (*Session, error)

// Opener contains the effects needed to restore an authenticated session.
type Opener struct {
	Scope      statedir.Scope
	OpenStore  credentials.Opener
	Now        func() time.Time
	HTTPClient *http.Client
	TokenURL   string
	APIBaseURL string
}

// Session exposes only the authenticated client, granted scopes, and lifecycle.
type Session struct {
	Client client.Client
	mu     sync.RWMutex
	scopes []string
	close  func() error
}

// New creates a session around an authenticated client.
func New(spotifyClient client.Client, scopes []string, closeSession func() error) *Session {
	return &Session{Client: spotifyClient, scopes: append([]string(nil), scopes...), close: closeSession}
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
	authenticated.Client = client.Client{
		HTTPClient: oauth2.NewClient(oauthContext, tokenSource),
		BaseURL:    opener.APIBaseURL,
	}
	return authenticated, nil
}
