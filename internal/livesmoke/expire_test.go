//go:build spotify_live

package livesmoke

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"

	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

func TestExpireCredential(t *testing.T) {
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
	cfg, err := config.Load(statedir.Scope{Name: config.Service})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := credentials.ParseProfile(cfg.CredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := os.Getenv("SPOTIFY_CLI_KEYRING_PASSPHRASE")
	if passphrase == "" {
		t.Fatal("SPOTIFY_CLI_KEYRING_PASSPHRASE is required")
	}
	store, err := credentials.ProductionOpener(func() (string, error) { return passphrase, nil })(credentials.OpenRequest{
		Config: cfg, Backend: string(credstore.BackendFile), BackendSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	raw, err := store.Get(profile, credentials.OAuthTokenKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	envelope, err := token.Decode([]byte(raw), now)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.RefreshToken == "" {
		t.Fatal(errors.New("live credential has no refresh token"))
	}
	envelope.AccessToken = "expired-live-smoke-token"
	envelope.ExpiresAt = now.Add(-time.Minute)
	encoded, err := token.Encode(envelope, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(profile, credentials.OAuthTokenKey, string(encoded), credstore.WithOverwrite()); err != nil {
		t.Fatal(err)
	}
}
