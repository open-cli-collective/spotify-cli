package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/open-cli-collective/cli-common/statedirtest"
)

func TestLoadMissingReturnsDefaultsWithoutCreating(t *testing.T) {
	statedirtest.Hermetic(t)
	scope := statedir.Scope{Name: Service}

	cfg, err := Load(scope)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedirectURI != DefaultRedirectURI || cfg.CredentialRef != DefaultCredentialRef {
		t.Fatalf("defaults = %+v", cfg)
	}
	dir, err := scope.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("config dir was created by Load: %v", err)
	}
}

func TestSaveAtomicModeAndRoundTrip(t *testing.T) {
	statedirtest.Hermetic(t)
	scope := statedir.Scope{Name: Service}
	cfg := Default()
	cfg.ClientID = "client-id"
	cfg.Keyring.Backend = "file"
	cfg.Keyring.OnePassword.VaultID = "vault-id"

	if err := Save(scope, cfg); err != nil {
		t.Fatal(err)
	}
	path, err := Path(scope)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	loaded, err := Load(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientID != cfg.ClientID || loaded.Keyring.Backend != "file" || loaded.Keyring.OnePassword.VaultID != "vault-id" {
		t.Fatalf("Load() = %+v, want %+v", loaded, cfg)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	statedirtest.Hermetic(t)
	scope := statedir.Scope{Name: Service}
	dir, err := scope.ConfigDirEnsured()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		yaml string
	}{
		{name: "unknown field", yaml: "unknown: value\n"},
		{name: "other service ref", yaml: "credential_ref: other/default\n"},
		{name: "memory backend", yaml: "keyring:\n  backend: memory\n"},
		{name: "localhost redirect", yaml: "redirect_uri: http://localhost/callback\n"},
		{name: "non-loopback http", yaml: "redirect_uri: http://example.com/callback\n"},
		{name: "bad timeout", yaml: "keyring:\n  onepassword:\n    timeout: tomorrow\n"},
		{name: "bad token env", yaml: "keyring:\n  onepassword:\n    service_token_env: bad-name\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, FileName)
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(scope); err == nil {
				t.Fatalf("Load() accepted %q", tt.yaml)
			}
		})
	}
}

func TestFailedSaveRetainsPriorConfiguration(t *testing.T) {
	statedirtest.Hermetic(t)
	scope := statedir.Scope{Name: Service}
	prior := Default()
	prior.ClientID = "prior-client"
	if err := Save(scope, prior); err != nil {
		t.Fatal(err)
	}
	invalid := prior
	invalid.Keyring.Backend = "memory"
	if err := Save(scope, invalid); err == nil {
		t.Fatal("Save accepted invalid replacement")
	}
	loaded, err := Load(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientID != prior.ClientID || loaded.Keyring.Backend != "" {
		t.Fatalf("failed Save changed config: %+v", loaded)
	}
}
