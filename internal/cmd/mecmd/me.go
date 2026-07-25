// Package mecmd implements the authenticated identity health check.
package mecmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/cmdutil"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
)

// Session is the authenticated capability required by me.
type Session interface {
	Close() error
	Scopes() []string
	Me(context.Context) (client.User, error)
}

// SessionOpener opens the authenticated capability required by me.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the runtime effects used by me.
type Dependencies struct {
	OpenSession SessionOpener
}

// New constructs the me command.
func New(dependencies Dependencies) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "me",
		Short: "Show the authenticated Spotify identity",
		Args:  cmdutil.NoArgs("me"),
		RunE: func(command *cobra.Command, _ []string) error {
			return run(command, dependencies, jsonOutput)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Emit JSON")
	return command
}

func run(command *cobra.Command, dependencies Dependencies, jsonOutput bool) error {
	authenticated, err := cmdutil.OpenSession(command, dependencies.OpenSession)
	if err != nil {
		return exitcode.New(exitcode.Config, err)
	}
	defer func() { _ = authenticated.Close() }()
	if !slices.Contains(authenticated.Scopes(), auth.ScopeUserReadPrivate) {
		return exitcode.New(exitcode.Config, errors.New("spotify authorization lacks user-read-private; run sptfy init"))
	}
	user, err := authenticated.Me(command.Context())
	if err != nil {
		return exitcode.New(cmdutil.Classify(err), err)
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
