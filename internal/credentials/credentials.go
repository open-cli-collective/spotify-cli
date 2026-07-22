// Package credentials adapts cli-common's service-scoped credential store.
package credentials

import (
	"errors"
	"fmt"

	"github.com/open-cli-collective/cli-common/credstore"

	"github.com/open-cli-collective/spotify-cli/internal/config"
)

// OAuthTokenKey is the only credential key accepted by sptfy.
const OAuthTokenKey = "oauth_token" // #nosec G101 -- credential key name, not a credential.

// ErrMemoryBackend reports use of the test-only in-memory backend.
var ErrMemoryBackend = errors.New("credential backend memory is test-only")

// ValidateExplicitBackend validates only user-supplied backend selection.
// Runtime availability remains the opener's responsibility.
func ValidateExplicitBackend(value string, set bool) error {
	if !set {
		return nil
	}
	backend, err := credstore.ParseBackend(value)
	if err != nil {
		return err
	}
	if backend == credstore.BackendMemory {
		return ErrMemoryBackend
	}
	return nil
}

// Store is the effect boundary command tests replace.
type Store interface {
	Backend() (credstore.Backend, credstore.Source)
	Close() error
	Get(profile, key string) (string, error)
	Set(profile, key, value string, opts ...credstore.SetOpt) error
	Delete(profile, key string) error
	Exists(profile, key string) (bool, error)
}

// OpenRequest contains credential backend selection inputs.
type OpenRequest struct {
	Config     config.Config
	Backend    string
	BackendSet bool
}

// Opener opens a credential store for a command.
type Opener func(OpenRequest) (Store, error)

// ProductionOpener returns the concrete cli-common store opener.
func ProductionOpener(filePassphrase func() (string, error)) Opener {
	return func(request OpenRequest) (Store, error) {
		opts, err := buildOptions(request, filePassphrase)
		if err != nil {
			return nil, err
		}
		store, err := credstore.Open(config.Service, opts)
		if err != nil {
			return nil, err
		}
		backend, _ := store.Backend()
		if backend == credstore.BackendMemory {
			_ = store.Close()
			return nil, ErrMemoryBackend
		}
		return store, nil
	}
}

func buildOptions(request OpenRequest, filePassphrase func() (string, error)) (*credstore.Options, error) {
	op := request.Config.Keyring.OnePassword
	opts := &credstore.Options{
		AllowedKeys:    []string{OAuthTokenKey},
		FilePassphrase: filePassphrase,
		OnePassword: &credstore.OnePasswordOptions{
			Timeout:          op.Timeout.Duration,
			VaultID:          op.VaultID,
			ItemTitlePrefix:  op.ItemTitlePrefix,
			ItemTag:          op.ItemTag,
			ItemFieldTitle:   op.ItemFieldTitle,
			ConnectHost:      op.ConnectHost,
			ConnectTokenEnv:  op.ConnectTokenEnv,
			ServiceTokenEnv:  op.ServiceTokenEnv,
			DesktopAccountID: op.DesktopAccountID,
		},
	}
	if err := credstore.BindBackendFlag(opts, request.Backend, request.BackendSet, request.Config.Keyring.Backend); err != nil {
		return nil, err
	}
	if opts.Backend == credstore.BackendMemory || opts.ConfigBackend == credstore.BackendMemory {
		return nil, ErrMemoryBackend
	}
	return opts, nil
}

// ParseProfile validates a full ref and returns the profile used by Store methods.
func ParseProfile(ref string) (string, error) {
	service, profile, err := credstore.ParseRef(ref)
	if err != nil {
		return "", err
	}
	if service != config.Service {
		return "", fmt.Errorf("credential ref service must be %q", config.Service)
	}
	return profile, nil
}
