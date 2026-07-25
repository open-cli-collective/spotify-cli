//go:build spotify_live

package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/session"
	"github.com/open-cli-collective/spotify-cli/internal/spotifyref"
)

func TestPlaylistDuplicateURIRemovalContract(t *testing.T) {
	requireLiveOptIn(t)
	playlistID, err := spotifyref.Parse(os.Getenv("SPOTIFY_CLI_LIVE_PLAYLIST_ID"), spotifyref.Playlist)
	if err != nil {
		t.Fatal("SPOTIFY_CLI_LIVE_PLAYLIST_ID is invalid")
	}
	mutationID, err := spotifyref.Parse(os.Getenv("SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID"), spotifyref.Track)
	if err != nil {
		t.Fatal("SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID is invalid")
	}
	prefix := strings.Split(os.Getenv("SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS"), ",")
	if len(prefix) != 3 {
		t.Fatal("SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS must contain three IDs")
	}
	for _, id := range prefix {
		if !spotifyref.ValidID(id) || id == mutationID {
			t.Fatal("playlist fixture IDs are invalid or not distinct")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	authenticated := openLiveSession(t, ctx)
	t.Cleanup(func() { _ = authenticated.Close() })
	current, _, err := livePlaylistState(ctx, authenticated, playlistID)
	if err != nil || len(current) < len(prefix) || !slices.Equal(current[:len(prefix)], prefix) || slices.Contains(current, mutationID) {
		t.Fatal("playlist does not match the provider-contract prefix or mutation-track precondition")
	}
	baseline := append([]string(nil), current...)

	mutationStarted := false
	var mutationStartedAt time.Time
	t.Cleanup(func() {
		if !mutationStarted {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if cleanupErr := restoreLivePlaylist(cleanupContext, authenticated, playlistID, baseline, mutationID, mutationStartedAt.Add(75*time.Second)); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})

	position := 1
	uri := "spotify:track:" + mutationID
	mutationStarted = true
	mutationStartedAt = time.Now()
	if _, err := authenticated.AddPlaylistItems(ctx, playlistID, []string{uri, uri}, &position); err != nil {
		t.Fatal("provider-contract duplicate insertion failed")
	}
	wantInserted := make([]string, 0, len(baseline)+2)
	wantInserted = append(wantInserted, baseline[0], mutationID, mutationID)
	wantInserted = append(wantInserted, baseline[1:]...)
	snapshotID, err := waitForLivePlaylistState(ctx, authenticated, playlistID, wantInserted, 30*time.Second)
	if err != nil {
		t.Fatal("provider-contract duplicate insertion produced an unexpected order")
	}
	if !waitUntil(ctx, mutationStartedAt.Add(75*time.Second)) {
		t.Fatal("provider-contract timed out waiting for provider write settling")
	}
	inserted, snapshotID, err := livePlaylistState(ctx, authenticated, playlistID)
	if err != nil || !slices.Equal(inserted, wantInserted) {
		t.Fatal("provider-contract insertion changed during provider write settling")
	}
	removedSnapshot, err := authenticated.RemovePlaylistItemsByURI(ctx, playlistID, uri, snapshotID)
	if err != nil || removedSnapshot == snapshotID {
		t.Fatal("provider-contract URI removal did not advance the playlist snapshot")
	}
	if _, err := waitForLivePlaylistState(ctx, authenticated, playlistID, baseline, 30*time.Second); err != nil {
		t.Fatal("one URI removal did not remove both duplicate occurrences")
	}
	mutationStarted = false
}

func TestRestoreLivePlaylistWaitsForDelayedInsertion(t *testing.T) {
	const (
		playlistID = "0123456789ABCDEFGHIJKL"
		mutationID = "abcdefghijklmnopqrstuv"
		artistID   = "ZYXWVUTSRQPONMLKJIHGFE"
		albumID    = "1111111111111111111111"
	)
	baseline := []string{"2222222222222222222222", "3333333333333333333333", "4444444444444444444444"}
	inserted := []string{baseline[0], mutationID, baseline[1], baseline[2]}
	visibleAt := time.Now().Add(100 * time.Millisecond)
	removed := false
	removeCalls := 0
	respond := func(status int, value any) *http.Response {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
	}
	httpClient := &http.Client{Transport: liveRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		ids := baseline
		snapshotID := "baseline"
		if !removed && !time.Now().Before(visibleAt) {
			ids = inserted
			snapshotID = "inserted"
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/playlists/"+playlistID:
			return respond(http.StatusOK, map[string]any{"id": playlistID, "items": map[string]int{"total": len(ids)}, "snapshot_id": snapshotID}), nil
		case request.Method == http.MethodGet && request.URL.Path == "/v1/playlists/"+playlistID+"/items":
			items := make([]any, len(ids))
			for index, id := range ids {
				items[index] = map[string]any{"item": map[string]any{
					"type": "track", "id": id, "uri": "spotify:track:" + id,
					"artists": []any{map[string]string{"id": artistID}},
					"album":   map[string]any{"id": albumID, "images": []any{}},
				}}
			}
			return respond(http.StatusOK, map[string]any{"items": items, "limit": 50, "offset": 0, "total": len(ids), "next": nil}), nil
		case request.Method == http.MethodDelete && request.URL.Path == "/v1/playlists/"+playlistID+"/items":
			removeCalls++
			removed = true
			return respond(http.StatusOK, map[string]string{"snapshot_id": "restored"}), nil
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.RequestURI())
			return nil, nil
		}
	})}
	authenticated := session.New(client.Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	before, _, err := livePlaylistState(ctx, authenticated, playlistID)
	if err != nil || !slices.Equal(before, baseline) {
		t.Fatalf("pre-settle state=%v error=%v", before, err)
	}
	if err := restoreLivePlaylist(ctx, authenticated, playlistID, baseline, mutationID, visibleAt); err != nil {
		t.Fatal(err)
	}
	after, _, err := livePlaylistState(ctx, authenticated, playlistID)
	if err != nil || !slices.Equal(after, baseline) || removeCalls != 1 {
		t.Fatalf("restored state=%v remove calls=%d error=%v", after, removeCalls, err)
	}
}

type liveRoundTripFunc func(*http.Request) (*http.Response, error)

func (function liveRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func restoreLivePlaylist(ctx context.Context, authenticated *session.Session, playlistID string, baseline []string, mutationID string, settledAt time.Time) error {
	if !waitUntil(ctx, settledAt) {
		return errors.New("provider-contract cleanup timed out waiting for provider write settling")
	}
	current, snapshotID, err := livePlaylistState(ctx, authenticated, playlistID)
	if err != nil {
		return errors.New("provider-contract cleanup could not read settled playlist state")
	}
	if slices.Equal(current, baseline) {
		return nil
	}
	filtered := make([]string, 0, len(current))
	for _, id := range current {
		if id != mutationID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == len(current) || !slices.Equal(filtered, baseline) {
		return errors.New("provider-contract cleanup refused an unexpected playlist shape")
	}
	if _, err := authenticated.RemovePlaylistItemsByURI(ctx, playlistID, "spotify:track:"+mutationID, snapshotID); err != nil {
		return errors.New("provider-contract cleanup removal failed")
	}
	if _, err := waitForLivePlaylistState(ctx, authenticated, playlistID, baseline, 30*time.Second); err != nil {
		return errors.New("provider-contract cleanup did not restore the exact baseline")
	}
	return nil
}

func waitUntil(ctx context.Context, deadline time.Time) bool {
	delay := time.Until(deadline)
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func waitForLivePlaylistState(ctx context.Context, authenticated *session.Session, playlistID string, want []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		ids, snapshotID, err := livePlaylistState(ctx, authenticated, playlistID)
		if err == nil && slices.Equal(ids, want) {
			return snapshotID, nil
		}
		if time.Now().Add(5*time.Second).After(deadline) || !waitUntil(ctx, time.Now().Add(5*time.Second)) {
			return "", errors.New("playlist state did not settle")
		}
	}
}

func openLiveSession(t *testing.T, ctx context.Context) *session.Session {
	t.Helper()
	passphrase := os.Getenv("SPOTIFY_CLI_KEYRING_PASSPHRASE")
	if passphrase == "" {
		t.Fatal("SPOTIFY_CLI_KEYRING_PASSPHRASE is required")
	}
	openStore := credentials.ProductionOpener(func() (string, error) { return passphrase, nil })
	opener := session.Opener{
		Scope: statedir.Scope{Name: config.Service},
		OpenStore: func(request credentials.OpenRequest) (session.CredentialStore, error) {
			return openStore(request)
		},
		HTTPClient: &http.Client{
			Transport: http.DefaultTransport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	authenticated, err := opener.Open(ctx, string(credstore.BackendFile), true)
	if err != nil {
		t.Fatal(err)
	}
	return authenticated
}

func livePlaylistState(ctx context.Context, authenticated *session.Session, playlistID string) ([]string, string, error) {
	playlist, err := authenticated.GetPlaylist(ctx, playlistID)
	if err != nil || playlist.ItemCount == nil || strings.TrimSpace(playlist.SnapshotID) == "" {
		return nil, "", errors.New("reading playlist detail failed")
	}
	total := playlist.ItemCount.Total
	ids := make([]string, 0, total)
	for offset := 0; offset < total; {
		page, err := authenticated.ListPlaylistItems(ctx, playlistID, 50, offset)
		if err != nil || page.Total != total || page.Offset != offset || len(page.Items) == 0 {
			return nil, "", errors.New("reading playlist items failed")
		}
		for _, item := range page.Items {
			if item.Type != "track" || !spotifyref.ValidID(item.ID) {
				return nil, "", errors.New("playlist contains a non-track fixture item")
			}
			ids = append(ids, item.ID)
		}
		offset += len(page.Items)
	}
	if len(ids) != total {
		return nil, "", errors.New("playlist item total changed")
	}
	return ids, playlist.SnapshotID, nil
}

func requireLiveOptIn(t *testing.T) {
	t.Helper()
	if os.Getenv("SPOTIFY_CLI_LIVE") != "1" || os.Getenv("SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT") != "1" {
		t.Skip("live smoke opt-in is not enabled")
	}
	root := os.Getenv("SPOTIFY_CLI_LIVE_ROOT")
	if root == "" {
		t.Fatal("SPOTIFY_CLI_LIVE_ROOT is required")
	}
	for _, name := range []string{"HOME", "USERPROFILE", "AppData", "LocalAppData", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		value := os.Getenv(name)
		relative, err := filepath.Rel(root, value)
		if err != nil || value == "" || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("%s is not isolated under the live root", name)
		}
	}
}
