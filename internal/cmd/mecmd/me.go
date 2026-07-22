// Package mecmd implements the authenticated identity health check.
package mecmd

import (
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
	"github.com/open-cli-collective/spotify-cli/internal/session"
)

// Dependencies contains the runtime effects used by me.
type Dependencies struct {
	OpenSession session.OpenFunc
	Backend     *string
}

// New constructs the me command.
func New(dependencies Dependencies) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "me",
		Short: "Show the authenticated Spotify identity",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return exitcode.New(exitcode.Usage, errors.New("me takes no arguments"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return run(command, dependencies, jsonOutput)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Emit JSON")
	return command
}

func run(command *cobra.Command, dependencies Dependencies, jsonOutput bool) error {
	backendFlag := command.Flags().Lookup(credstore.BackendFlagName)
	backendSet := backendFlag != nil && backendFlag.Changed
	if err := credentials.ValidateExplicitBackend(pointerValue(dependencies.Backend), backendSet); err != nil {
		return exitcode.New(exitcode.Usage, err)
	}
	if dependencies.OpenSession == nil {
		return exitcode.New(exitcode.Generic, errors.New("authenticated session is unavailable"))
	}
	authenticated, err := dependencies.OpenSession(command.Context(), pointerValue(dependencies.Backend), backendSet)
	if err != nil {
		return exitcode.New(exitcode.Config, err)
	}
	defer func() { _ = authenticated.Close() }()
	if !slices.Contains(authenticated.Scopes(), auth.ScopeUserReadPrivate) {
		return exitcode.New(exitcode.Config, errors.New("spotify authorization lacks user-read-private; run sptfy init"))
	}
	user, err := authenticated.Client.Me(command.Context())
	if err != nil {
		return classify(err)
	}
	if jsonOutput {
		if err := json.NewEncoder(command.OutOrStdout()).Encode(output.NewMeResult(user, authenticated.Scopes())); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing JSON output failed"))
		}
		return nil
	}
	if _, err := io.WriteString(command.OutOrStdout(), output.RenderMeText(user, authenticated.Scopes())); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing text output failed"))
	}
	return nil
}

func classify(err error) error {
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
