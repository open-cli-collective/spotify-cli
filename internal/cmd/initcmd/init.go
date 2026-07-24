// Package initcmd implements first-time Spotify authorization and configuration.
package initcmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
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
	Backend     *string
	Interactive bool
	Prompt      func(*Setup) error
	Initializer Initializer
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
	configuredBackend := strings.TrimSpace(configValue.Keyring.Backend)
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
	interactive := dependencies.Interactive && !nonInteractive
	if backendSet {
		setup.Backend = runtimeBackend
	}

	if interactive {
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

	result, err := dependencies.Initializer.Initialize(command.Context(), InitializeOptions{
		Config: configValue, Profile: profile, Backend: runtimeBackend, BackendSet: runtimeBackendSet,
		Overwrite: overwrite || (interactive && strings.TrimSpace(setup.Backend) != configuredBackend), Verify: !noVerify,
		Authorization: auth.Request{
			ClientID: configValue.ClientID, RedirectURI: configValue.RedirectURI,
			NoBrowser: noBrowser || authCodeStdin, AuthCodeStdin: authCodeStdin,
			In: command.InOrStdin(), ErrOut: command.ErrOrStderr(),
		},
	})
	if err != nil {
		return classifyInitialization(err)
	}
	if result.Verified {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "Authenticated as %s.\n", result.User.AccountID); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing setup confirmation failed"))
		}
	}
	if _, err := fmt.Fprintln(command.ErrOrStderr(), "Setup complete."); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing setup confirmation failed"))
	}
	return nil
}

func classifyInitialization(err error) error {
	var failure *initializationFailure
	if !errors.As(err, &failure) {
		return exitcode.New(exitcode.Generic, err)
	}
	switch failure.kind {
	case failureGeneric:
		return exitcode.New(exitcode.Generic, failure.err)
	case failureConfig:
		return exitcode.New(exitcode.Config, failure.err)
	case failureAuthorization:
		return classifyAuthorization(failure.err)
	case failureVerification:
		return classifyVerification(failure.err)
	}
	return exitcode.New(exitcode.Generic, failure.err)
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
