// Package config owns sptfy's non-secret configuration file.
package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"gopkg.in/yaml.v3"
)

// Service and related constants identify this CLI's state.
const (
	Service              = "spotify-cli"
	Tool                 = "sptfy"
	FileName             = "config.yml"
	DefaultRedirectURI   = "http://127.0.0.1/callback"
	DefaultCredentialRef = "spotify-cli/default" // #nosec G101 -- non-secret credential reference.
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Config is the complete non-secret configuration document.
type Config struct {
	ClientID      string        `yaml:"client_id,omitempty" json:"client_id"`
	RedirectURI   string        `yaml:"redirect_uri" json:"redirect_uri"`
	CredentialRef string        `yaml:"credential_ref" json:"credential_ref"`
	Keyring       KeyringConfig `yaml:"keyring,omitempty" json:"keyring"`
}

// KeyringConfig selects and configures a credential backend.
type KeyringConfig struct {
	Backend     string            `yaml:"backend,omitempty" json:"backend,omitempty"`
	OnePassword OnePasswordConfig `yaml:"onepassword,omitempty" json:"onepassword,omitzero"`
}

// OnePasswordConfig contains non-secret 1Password backend settings.
type OnePasswordConfig struct {
	Timeout          Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	VaultID          string   `yaml:"vault_id,omitempty" json:"vault_id,omitempty"`
	ItemTitlePrefix  string   `yaml:"item_title_prefix,omitempty" json:"item_title_prefix,omitempty"`
	ItemTag          string   `yaml:"item_tag,omitempty" json:"item_tag,omitempty"`
	ItemFieldTitle   string   `yaml:"item_field_title,omitempty" json:"item_field_title,omitempty"`
	ConnectHost      string   `yaml:"connect_host,omitempty" json:"connect_host,omitempty"`
	ConnectTokenEnv  string   `yaml:"connect_token_env,omitempty" json:"connect_token_env,omitempty"`
	ServiceTokenEnv  string   `yaml:"service_token_env,omitempty" json:"service_token_env,omitempty"`
	DesktopAccountID string   `yaml:"desktop_account_id,omitempty" json:"desktop_account_id,omitempty"`
}

// IsZero reports whether no 1Password settings are configured.
func (c OnePasswordConfig) IsZero() bool {
	return c == (OnePasswordConfig{})
}

// Duration is a human-readable YAML/JSON duration.
type Duration struct{ time.Duration }

// UnmarshalYAML parses a positive duration.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Value == "" {
		d.Duration = 0
		return nil
	}
	value, err := time.ParseDuration(node.Value)
	if err != nil || value <= 0 {
		return errors.New("keyring.onepassword.timeout must be a positive duration")
	}
	d.Duration = value
	return nil
}

// MarshalYAML emits the duration as text.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

// MarshalJSON emits the duration as text.
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", d.String())), nil
}

// IsZero reports whether the duration is unset.
func (d Duration) IsZero() bool { return d.Duration == 0 }

// Default returns configuration defaults.
func Default() Config {
	return Config{RedirectURI: DefaultRedirectURI, CredentialRef: DefaultCredentialRef}
}

// Path returns the configuration file path.
func Path(scope statedir.Scope) (string, error) {
	dir, err := scope.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Exists reports whether the configuration file exists.
func Exists(scope statedir.Scope) (bool, error) {
	path, err := Path(scope)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("checking config file: %w", err)
	}
}

// Load reads and validates configuration, or returns defaults when absent.
func Load(scope statedir.Scope) (Config, error) {
	path, err := Path(scope)
	if err != nil {
		return Config{}, err
	}
	file, err := os.Open(path) // #nosec G304 -- path comes from statedir, not user input.
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("opening config: %w", err)
	}
	defer func() { _ = file.Close() }()

	cfg := Default()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decoding config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("decoding config: multiple YAML documents are not allowed")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save validates and atomically replaces the configuration file.
func Save(scope statedir.Scope, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	dir, err := scope.ConfigDirEnsured()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting config permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing config: %w", err)
	}
	path := filepath.Join(dir, FileName)
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}

// Validate checks configuration values.
func (cfg Config) Validate() error {
	if err := ValidateRedirectURI(cfg.RedirectURI); err != nil {
		return err
	}
	service, _, err := credstore.ParseRef(cfg.CredentialRef)
	if err != nil {
		return fmt.Errorf("invalid credential_ref: %w", err)
	}
	if service != Service {
		return fmt.Errorf("credential_ref service must be %q", Service)
	}
	if cfg.Keyring.Backend != "" {
		backend, err := credstore.ParseBackend(cfg.Keyring.Backend)
		if err != nil {
			return fmt.Errorf("invalid keyring.backend: %w", err)
		}
		if backend == credstore.BackendMemory {
			return errors.New("keyring.backend memory is test-only")
		}
	}
	op := cfg.Keyring.OnePassword
	if op.ConnectHost != "" {
		u, err := url.ParseRequestURI(op.ConnectHost)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return errors.New("keyring.onepassword.connect_host must be an absolute URI")
		}
	}
	for name, value := range map[string]string{
		"connect_token_env": op.ConnectTokenEnv,
		"service_token_env": op.ServiceTokenEnv,
	} {
		if value != "" && !ValidEnvName(value) {
			return fmt.Errorf("keyring.onepassword.%s must be an environment variable name", name)
		}
	}
	return nil
}

// ValidateRedirectURI checks Spotify's redirect URI requirements.
func ValidateRedirectURI(value string) error {
	u, err := url.ParseRequestURI(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("redirect_uri must be an absolute URI")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return errors.New("redirect_uri must not use localhost; use 127.0.0.1")
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		if host != "127.0.0.1" {
			return errors.New("HTTP redirect_uri must use 127.0.0.1")
		}
	case "https":
	default:
		return errors.New("redirect_uri scheme must be http or https")
	}
	return nil
}

// ValidEnvName reports whether value is a portable environment variable name.
func ValidEnvName(value string) bool { return envNamePattern.MatchString(value) }
