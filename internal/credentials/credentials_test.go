package credentials

import (
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedirtest"

	"github.com/open-cli-collective/spotify-cli/internal/config"
)

func TestBuildOptionsMapsConfiguration(t *testing.T) {
	cfg := config.Default()
	cfg.Keyring.Backend = "file"
	cfg.Keyring.OnePassword.Timeout = config.Duration{Duration: 5 * time.Second}
	cfg.Keyring.OnePassword.VaultID = "vault"
	cfg.Keyring.OnePassword.ItemTitlePrefix = "prefix"
	cfg.Keyring.OnePassword.ItemTag = "tag"
	cfg.Keyring.OnePassword.ItemFieldTitle = "field"
	cfg.Keyring.OnePassword.ConnectHost = "https://connect.example"
	cfg.Keyring.OnePassword.ConnectTokenEnv = "CONNECT_TOKEN"
	cfg.Keyring.OnePassword.ServiceTokenEnv = "SERVICE_TOKEN"
	cfg.Keyring.OnePassword.DesktopAccountID = "account"

	opts, err := buildOptions(OpenRequest{Config: cfg, Backend: "pass", BackendSet: true}, func() (string, error) { return "passphrase", nil })
	if err != nil {
		t.Fatal(err)
	}
	if opts.Backend != credstore.BackendPass || opts.ConfigBackend != credstore.BackendFile {
		t.Fatalf("backend binding = explicit %q config %q", opts.Backend, opts.ConfigBackend)
	}
	if opts.OnePassword.Timeout != 5*time.Second || opts.OnePassword.VaultID != "vault" || opts.OnePassword.DesktopAccountID != "account" {
		t.Fatalf("1Password options = %+v", opts.OnePassword)
	}
}

func TestBuildOptionsRejectsProductionMemory(t *testing.T) {
	_, err := buildOptions(OpenRequest{Config: config.Default(), Backend: "memory", BackendSet: true}, nil)
	if err == nil {
		t.Fatal("buildOptions accepted memory backend")
	}
}

func TestValidateExplicitBackend(t *testing.T) {
	for _, tt := range []struct {
		value string
		set   bool
		ok    bool
	}{
		{value: "definitely-invalid", set: false, ok: true},
		{value: "file", set: true, ok: true},
		{value: "definitely-invalid", set: true},
		{value: "memory", set: true},
	} {
		if err := ValidateExplicitBackend(tt.value, tt.set); (err == nil) != tt.ok {
			t.Fatalf("ValidateExplicitBackend(%q, %t) = %v", tt.value, tt.set, err)
		}
	}
}

func TestProductionOpenerRejectsMemoryFromEnvironment(t *testing.T) {
	t.Setenv("SPOTIFY_CLI_KEYRING_BACKEND", "memory")
	_, err := ProductionOpener(nil)(OpenRequest{Config: config.Default()})
	if err == nil || !strings.Contains(err.Error(), "test-only") {
		t.Fatalf("ProductionOpener() error = %v", err)
	}
}

func TestProductionBackendPrecedenceSources(t *testing.T) {
	statedirtest.Hermetic(t)
	t.Setenv("SPOTIFY_CLI_KEYRING_PASSPHRASE", "test-passphrase")

	t.Run("environment over config", func(t *testing.T) {
		t.Setenv("SPOTIFY_CLI_KEYRING_BACKEND", "file")
		cfg := config.Default()
		cfg.Keyring.Backend = "pass"
		store, err := ProductionOpener(nil)(OpenRequest{Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = store.Close() }()
		backend, source := store.Backend()
		if backend != credstore.BackendFile || source != credstore.SourceEnv {
			t.Fatalf("backend = %s (%s)", backend, source)
		}
	})

	t.Run("config over default", func(t *testing.T) {
		t.Setenv("SPOTIFY_CLI_KEYRING_BACKEND", "")
		cfg := config.Default()
		cfg.Keyring.Backend = "file"
		store, err := ProductionOpener(nil)(OpenRequest{Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = store.Close() }()
		backend, source := store.Backend()
		if backend != credstore.BackendFile || source != credstore.SourceConfig {
			t.Fatalf("backend = %s (%s)", backend, source)
		}
	})

}

func TestParseRefRequiresSpotifyService(t *testing.T) {
	if _, err := ParseProfile("other/default"); err == nil {
		t.Fatal("ParseProfile accepted another service")
	}
	profile, err := ParseProfile("spotify-cli/work")
	if err != nil || profile != "work" {
		t.Fatalf("ParseProfile = %q, %v", profile, err)
	}
}
