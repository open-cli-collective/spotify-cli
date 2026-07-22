// Package initcmd implements first-time Spotify authorization and configuration.
package initcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

// Setup contains every value edited by the interactive form.
type Setup struct {
	ClientID      string
	RedirectURI   string
	CredentialRef string
	Backend       string
}

// Dependencies contains the runtime effects used by init.
type Dependencies struct {
	Scope       statedir.Scope
	OpenStore   credentials.Opener
	Backend     *string
	Now         func() time.Time
	Interactive bool
	Prompt      func(*Setup) error
	Authorize   func(context.Context, auth.Request) (token.Envelope, error)
	Verify      func(context.Context, config.Config, token.Envelope) (client.User, error)
	SaveConfig  func(config.Config) error
}

// New constructs the init command.
func New(dependencies Dependencies) *cobra.Command {
	var clientID, redirectURI, credentialRef string
	var nonInteractive, noBrowser, authCodeStdin, noVerify, overwrite bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Authorize Spotify and save configuration",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return exitcode.New(exitcode.Usage, errors.New("init takes no arguments"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return run(command, dependencies, Setup{
				ClientID: clientID, RedirectURI: redirectURI, CredentialRef: credentialRef,
			}, nonInteractive, noBrowser, authCodeStdin, noVerify, overwrite)
		},
	}
	flags := command.Flags()
	flags.StringVar(&clientID, "client-id", "", "Spotify application client ID")
	flags.StringVar(&redirectURI, "redirect-uri", "", "Allowlisted Spotify redirect URI")
	flags.StringVar(&credentialRef, "credential-ref", "", "Credential reference (service/profile)")
	flags.BoolVar(&nonInteractive, "non-interactive", false, "Disable setup prompts")
	flags.BoolVar(&noBrowser, "no-browser", false, "Do not open the authorization URL")
	flags.BoolVar(&authCodeStdin, "auth-code-stdin", false, "Read the complete redirected URL from stdin (implies --no-browser)")
	flags.BoolVar(&noVerify, "no-verify", false, "Skip the Spotify identity check")
	flags.BoolVar(&overwrite, "overwrite", false, "Replace an existing OAuth credential")
	return command
}

func run(command *cobra.Command, dependencies Dependencies, flags Setup, nonInteractive, noBrowser, authCodeStdin, noVerify, overwrite bool) error {
	configValue, err := config.Load(dependencies.Scope)
	if err != nil {
		return exitcode.New(exitcode.Config, err)
	}
	setup := Setup{
		ClientID: configValue.ClientID, RedirectURI: configValue.RedirectURI,
		CredentialRef: configValue.CredentialRef, Backend: configValue.Keyring.Backend,
	}
	if command.Flags().Changed("client-id") {
		setup.ClientID = flags.ClientID
	}
	if command.Flags().Changed("redirect-uri") {
		setup.RedirectURI = flags.RedirectURI
	}
	if command.Flags().Changed("credential-ref") {
		setup.CredentialRef = flags.CredentialRef
	}
	backendFlag := command.Flags().Lookup(credstore.BackendFlagName)
	backendSet := backendFlag != nil && backendFlag.Changed
	runtimeBackend := pointerValue(dependencies.Backend)
	runtimeBackendSet := backendSet
	if backendSet {
		setup.Backend = runtimeBackend
	}

	if dependencies.Interactive && !nonInteractive {
		prompt := dependencies.Prompt
		if prompt == nil {
			prompt = func(value *Setup) error { return RunPrompt(command.InOrStdin(), command.ErrOrStderr(), value) }
		}
		if err := prompt(&setup); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("setup prompt failed"))
		}
		runtimeBackend = strings.TrimSpace(setup.Backend)
		runtimeBackendSet = runtimeBackend != ""
	} else if strings.TrimSpace(setup.ClientID) == "" {
		return exitcode.New(exitcode.Usage, errors.New("--client-id is required in non-interactive mode"))
	}

	configValue.ClientID = strings.TrimSpace(setup.ClientID)
	configValue.RedirectURI = strings.TrimSpace(setup.RedirectURI)
	configValue.CredentialRef = strings.TrimSpace(setup.CredentialRef)
	configValue.Keyring.Backend = strings.TrimSpace(setup.Backend)
	if configValue.ClientID == "" {
		return exitcode.New(exitcode.Usage, errors.New("spotify client ID is required"))
	}
	if err := credentials.ValidateExplicitBackend(runtimeBackend, runtimeBackendSet); err != nil {
		return exitcode.New(exitcode.Usage, err)
	}
	if err := configValue.Validate(); err != nil {
		return exitcode.New(exitcode.Usage, err)
	}
	profile, err := credentials.ParseProfile(configValue.CredentialRef)
	if err != nil {
		return exitcode.New(exitcode.Usage, err)
	}

	store, err := dependencies.OpenStore(credentials.OpenRequest{
		Config: configValue, Backend: runtimeBackend, BackendSet: runtimeBackendSet,
	})
	if err != nil {
		return exitcode.New(exitcode.Config, errors.New("opening credential store failed"))
	}
	defer func() { _ = store.Close() }()
	present, err := store.Exists(profile, credentials.OAuthTokenKey)
	if err != nil {
		return exitcode.New(exitcode.Config, errors.New("checking existing Spotify authorization failed"))
	}
	var previous string
	if present {
		if !overwrite {
			return exitcode.New(exitcode.Generic, fmt.Errorf("%w at %s; use --overwrite or sptfy config clear", credstore.ErrExists, configValue.CredentialRef))
		}
		previous, err = store.Get(profile, credentials.OAuthTokenKey)
		if err != nil {
			return exitcode.New(exitcode.Config, errors.New("reading existing Spotify authorization for rollback failed"))
		}
	}
	if dependencies.Authorize == nil {
		return exitcode.New(exitcode.Generic, errors.New("spotify authorizer is unavailable"))
	}
	envelope, err := dependencies.Authorize(command.Context(), auth.Request{
		ClientID: configValue.ClientID, RedirectURI: configValue.RedirectURI,
		NoBrowser: noBrowser || authCodeStdin, AuthCodeStdin: authCodeStdin,
		In: command.InOrStdin(), ErrOut: command.ErrOrStderr(),
	})
	if err != nil {
		return classifyAuthorization(err)
	}
	var user client.User
	if !noVerify {
		if dependencies.Verify == nil {
			return exitcode.New(exitcode.Generic, errors.New("spotify verifier is unavailable"))
		}
		user, err = dependencies.Verify(command.Context(), configValue, envelope)
		if err != nil {
			return classifyVerification(err)
		}
	}

	now := time.Now()
	if dependencies.Now != nil {
		now = dependencies.Now()
	}
	encoded, err := token.Encode(envelope, now)
	if err != nil {
		return exitcode.New(exitcode.Config, err)
	}
	options := []credstore.SetOpt(nil)
	if overwrite {
		options = append(options, credstore.WithOverwrite())
	}
	if err := store.Set(profile, credentials.OAuthTokenKey, string(encoded), options...); err != nil {
		return exitcode.New(exitcode.Config, redactStoreError(err, previous, string(encoded), envelope))
	}
	saveConfig := dependencies.SaveConfig
	if saveConfig == nil {
		saveConfig = func(value config.Config) error { return config.Save(dependencies.Scope, value) }
	}
	if err := saveConfig(configValue); err != nil {
		rollbackErr := rollbackCredential(store, profile, previous, present)
		if rollbackErr != nil {
			rollbackErr = redactStoreError(rollbackErr, previous, string(encoded), envelope)
		}
		return exitcode.New(exitcode.Config, errors.Join(errors.New("saving configuration failed"), rollbackErr))
	}
	if !noVerify {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "Authenticated as %s.\n", user.AccountID); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing setup confirmation failed"))
		}
	}
	if _, err := fmt.Fprintln(command.ErrOrStderr(), "Setup complete."); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing setup confirmation failed"))
	}
	return nil
}

func classifyAuthorization(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidCallback), errors.Is(err, auth.ErrStateMismatch):
		return exitcode.New(exitcode.Usage, err)
	case errors.Is(err, auth.ErrAccessDenied):
		return exitcode.New(exitcode.Config, err)
	case errors.Is(err, auth.ErrAuthorizationTimeout), errors.Is(err, auth.ErrExchange):
		return exitcode.New(exitcode.Upstream, err)
	default:
		return exitcode.New(exitcode.Generic, err)
	}
}

// RunPrompt runs the small setup form on the supplied streams.
func RunPrompt(input io.Reader, output io.Writer, setup *Setup) error {
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Spotify client ID").Value(&setup.ClientID),
		huh.NewInput().Title("Redirect URI").Value(&setup.RedirectURI),
		huh.NewInput().Title("Credential reference").Value(&setup.CredentialRef),
		huh.NewSelect[string]().Title("Credential backend").Options(
			huh.NewOption("Automatic (OS default)", ""),
			huh.NewOption("macOS Keychain", "keychain"),
			huh.NewOption("Windows Credential Manager", "wincred"),
			huh.NewOption("Linux Secret Service", "secret-service"),
			huh.NewOption("Encrypted file", "file"),
			huh.NewOption("pass", "pass"),
			huh.NewOption("1Password", "op"),
			huh.NewOption("1Password Connect", "op-connect"),
			huh.NewOption("1Password Desktop", "op-desktop"),
		).Value(&setup.Backend),
	)).WithInput(input).WithOutput(output).Run()
}

func rollbackCredential(store credentials.Store, profile, previous string, existed bool) error {
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

func classifyVerification(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidGrant), errors.Is(err, auth.ErrPersistRefresh),
		errors.Is(err, client.ErrUnauthorized), errors.Is(err, client.ErrForbidden):
		return exitcode.New(exitcode.Config, err)
	default:
		return exitcode.New(exitcode.Upstream, err)
	}
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
