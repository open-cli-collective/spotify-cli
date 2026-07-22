package initcmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

// Initializer owns the secure authorization and persistence transaction.
type Initializer struct {
	OpenStore  StoreOpener
	Now        func() time.Time
	Authorize  func(context.Context, auth.Request) (token.Envelope, error)
	Verify     func(context.Context, config.Config, token.Envelope) (client.User, error)
	SaveConfig func(config.Config) error
}

// CredentialStore is the credential capability required by initialization.
type CredentialStore interface {
	Close() error
	Get(profile, key string) (string, error)
	Set(profile, key, value string, opts ...credstore.SetOpt) error
	Delete(profile, key string) error
	Exists(profile, key string) (bool, error)
}

// StoreOpener opens the credential capability required by initialization.
type StoreOpener func(credentials.OpenRequest) (CredentialStore, error)

// InitializeOptions contains validated setup and invocation choices.
type InitializeOptions struct {
	Config        config.Config
	Profile       string
	Backend       string
	BackendSet    bool
	Overwrite     bool
	Verify        bool
	Authorization auth.Request
}

// InitializeResult contains safe values the command may render.
type InitializeResult struct {
	User     client.User
	Verified bool
}

type failureKind uint8

const (
	failureGeneric failureKind = iota
	failureConfig
	failureAuthorization
	failureVerification
)

type initializationFailure struct {
	kind failureKind
	err  error
}

func (failure *initializationFailure) Error() string { return failure.err.Error() }
func (failure *initializationFailure) Unwrap() error { return failure.err }

func failInitialization(kind failureKind, err error) error {
	return &initializationFailure{kind: kind, err: err}
}

// Initialize authorizes, verifies, and atomically persists one Spotify setup.
func (initializer Initializer) Initialize(ctx context.Context, options InitializeOptions) (InitializeResult, error) {
	if initializer.OpenStore == nil {
		return InitializeResult{}, failInitialization(failureGeneric, errors.New("credential store opener is unavailable"))
	}
	if initializer.SaveConfig == nil {
		return InitializeResult{}, failInitialization(failureConfig, errors.New("configuration saver is unavailable"))
	}
	store, err := initializer.OpenStore(credentials.OpenRequest{
		Config: options.Config, Backend: options.Backend, BackendSet: options.BackendSet,
	})
	if err != nil {
		return InitializeResult{}, failInitialization(failureConfig, errors.New("opening credential store failed"))
	}
	defer func() { _ = store.Close() }()

	present, err := store.Exists(options.Profile, credentials.OAuthTokenKey)
	if err != nil {
		return InitializeResult{}, failInitialization(failureConfig, errors.New("checking existing Spotify authorization failed"))
	}
	var previous string
	if present {
		if !options.Overwrite {
			return InitializeResult{}, failInitialization(failureGeneric, fmt.Errorf("%w at %s; use --overwrite or sptfy config clear", credstore.ErrExists, options.Config.CredentialRef))
		}
		previous, err = store.Get(options.Profile, credentials.OAuthTokenKey)
		if err != nil {
			return InitializeResult{}, failInitialization(failureConfig, errors.New("reading existing Spotify authorization for rollback failed"))
		}
	}

	if initializer.Authorize == nil {
		return InitializeResult{}, failInitialization(failureGeneric, errors.New("spotify authorizer is unavailable"))
	}
	envelope, err := initializer.Authorize(ctx, options.Authorization)
	if err != nil {
		return InitializeResult{}, failInitialization(failureAuthorization, err)
	}

	result := InitializeResult{Verified: options.Verify}
	if options.Verify {
		if initializer.Verify == nil {
			return InitializeResult{}, failInitialization(failureGeneric, errors.New("spotify verifier is unavailable"))
		}
		result.User, err = initializer.Verify(ctx, options.Config, envelope)
		if err != nil {
			return InitializeResult{}, failInitialization(failureVerification, err)
		}
	}

	now := time.Now()
	if initializer.Now != nil {
		now = initializer.Now()
	}
	encoded, err := token.Encode(envelope, now)
	if err != nil {
		return InitializeResult{}, failInitialization(failureConfig, err)
	}
	setOptions := []credstore.SetOpt(nil)
	if options.Overwrite {
		setOptions = append(setOptions, credstore.WithOverwrite())
	}
	if err := store.Set(options.Profile, credentials.OAuthTokenKey, string(encoded), setOptions...); err != nil {
		return InitializeResult{}, failInitialization(failureConfig, redactStoreError(err, previous, string(encoded), envelope))
	}
	if err := initializer.SaveConfig(options.Config); err != nil {
		rollbackErr := rollbackCredential(store, options.Profile, previous, present)
		if rollbackErr != nil {
			rollbackErr = redactStoreError(rollbackErr, previous, string(encoded), envelope)
		}
		return InitializeResult{}, failInitialization(failureConfig, errors.Join(errors.New("saving configuration failed"), rollbackErr))
	}
	return result, nil
}

func rollbackCredential(store CredentialStore, profile, previous string, existed bool) error {
	if existed {
		return store.Set(profile, credentials.OAuthTokenKey, previous, credstore.WithOverwrite())
	}
	if err := store.Delete(profile, credentials.OAuthTokenKey); err != nil && !errors.Is(err, credstore.ErrNotFound) {
		return err
	}
	return nil
}

func redactStoreError(err error, previous, encoded string, envelope token.Envelope) error {
	redactor := credstore.NewRedactor(previous, encoded, envelope.AccessToken, envelope.RefreshToken)
	message := redactor.Redact(err.Error())
	if message == "" {
		message = "credential store operation failed"
	}
	return errors.New(message)
}
