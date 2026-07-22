package session

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/open-cli-collective/cli-common/statedirtest"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

func TestOpenPassesBackendAndClosesPrivateStore(t *testing.T) {
	now := time.Now().UTC()
	scope, store := configuredStore(t, now, token.Envelope{
		AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh",
		ExpiresAt: now.Add(time.Hour), Scopes: []string{"user-read-private"},
	})
	var request credentials.OpenRequest
	opener := Opener{Scope: scope, Now: func() time.Time { return now }, OpenStore: func(value credentials.OpenRequest) (CredentialStore, error) {
		request = value
		return store, nil
	}}
	authenticated, err := opener.Open(context.Background(), "file", true)
	if err != nil {
		t.Fatal(err)
	}
	if request.Backend != "file" || !request.BackendSet || len(authenticated.Scopes()) != 1 {
		t.Fatalf("request=%+v scopes=%v", request, authenticated.Scopes())
	}
	if err := authenticated.Close(); err != nil || !store.closed {
		t.Fatalf("close error=%v closed=%t", err, store.closed)
	}
}

func TestOpenSessionRefreshUsesCommandContext(t *testing.T) {
	now := time.Now().UTC()
	scope, store := configuredStore(t, now, token.Envelope{
		AccessToken: "expired", TokenType: "Bearer", RefreshToken: "refresh",
		ExpiresAt: now.Add(-time.Minute), Scopes: []string{"user-read-private"},
	})
	started := make(chan struct{})
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/token") {
			close(started)
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return nil, errors.New("API request occurred before refresh")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	opener := Opener{
		Scope: scope, OpenStore: func(credentials.OpenRequest) (CredentialStore, error) { return store, nil },
		Now: func() time.Time { return now }, HTTPClient: httpClient,
		TokenURL: "https://accounts.spotify.invalid/token", APIBaseURL: "https://api.spotify.invalid/v1",
	}
	authenticated, err := opener.Open(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := authenticated.Me(ctx)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenErrorsDoNotEchoStoredCredential(t *testing.T) {
	now := time.Now().UTC()
	statedirtest.Hermetic(t)
	scope := statedir.Scope{Name: config.Service}
	cfg := config.Default()
	cfg.ClientID = "client-id"
	if err := config.Save(scope, cfg); err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{values: map[string]string{"default/oauth_token": "secret-canary-invalid"}}
	opener := Opener{Scope: scope, OpenStore: func(credentials.OpenRequest) (CredentialStore, error) { return store, nil }, Now: func() time.Time { return now }}
	_, err := opener.Open(context.Background(), "", false)
	if err == nil || strings.Contains(err.Error(), "secret-canary") || !store.closed {
		t.Fatalf("error=%v closed=%t", err, store.closed)
	}
}

func TestSessionDelegatesCatalogSearch(t *testing.T) {
	var types []string
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		searchType := request.URL.Query().Get("type")
		types = append(types, searchType)
		body := `{"albums":{"items":[],"limit":1,"offset":0,"total":0,"next":null}}`
		if searchType == "artist" {
			body = `{"artists":{"items":[],"limit":1,"offset":0,"total":0,"next":null}}`
		}
		return response(http.StatusOK, body), nil
	})}
	authenticated := New(client.Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}, nil, nil)
	if _, err := authenticated.SearchAlbums(context.Background(), "album", 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticated.SearchArtists(context.Background(), "artist", 1, 0); err != nil {
		t.Fatal(err)
	}
	if strings.Join(types, ",") != "album,artist" {
		t.Fatalf("types = %v", types)
	}
}

func configuredStore(t *testing.T, now time.Time, envelope token.Envelope) (statedir.Scope, *memoryStore) {
	t.Helper()
	statedirtest.Hermetic(t)
	scope := statedir.Scope{Name: config.Service}
	cfg := config.Default()
	cfg.ClientID = "client-id"
	if err := config.Save(scope, cfg); err != nil {
		t.Fatal(err)
	}
	encoded, err := token.Encode(envelope, now)
	if err != nil {
		t.Fatal(err)
	}
	return scope, &memoryStore{values: map[string]string{"default/oauth_token": string(encoded)}}
}

type memoryStore struct {
	values map[string]string
	closed bool
}

func (store *memoryStore) Close() error { store.closed = true; return nil }
func (store *memoryStore) Get(profile, key string) (string, error) {
	value, ok := store.values[profile+"/"+key]
	if !ok {
		return "", credstore.ErrNotFound
	}
	return value, nil
}
func (store *memoryStore) Set(profile, key, value string, _ ...credstore.SetOpt) error {
	store.values[profile+"/"+key] = value
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
