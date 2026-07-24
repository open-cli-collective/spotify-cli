//go:build spotify_live

package livesmoke

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"

	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

func TestExpireCredential(t *testing.T) {
	requireLiveOptIn(t)
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
