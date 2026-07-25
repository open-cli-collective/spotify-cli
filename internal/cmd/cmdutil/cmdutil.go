// Package cmdutil contains shared command plumbing.
package cmdutil

import (
	"context"
	"errors"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

// Classify maps authenticated client failures to CLI exit codes.
func Classify(err error) int {
	switch {
	case errors.Is(err, auth.ErrInvalidGrant), errors.Is(err, auth.ErrPersistRefresh),
		errors.Is(err, client.ErrUnauthorized), errors.Is(err, client.ErrForbidden):
		return exitcode.Config
	default:
		return exitcode.Upstream
	}
}

// OpenSession validates the backend flag and opens an authenticated session.
func OpenSession[S any](command *cobra.Command, opener func(context.Context, string, bool) (S, error)) (S, error) {
	var zero S
	flag := command.Flags().Lookup(credstore.BackendFlagName)
	backend := ""
	if flag != nil {
		backend = flag.Value.String()
	}
	backendSet := flag != nil && flag.Changed
	if err := credentials.ValidateExplicitBackend(backend, backendSet); err != nil {
		return zero, exitcode.New(exitcode.Usage, err)
	}
	if opener == nil {
		return zero, exitcode.New(exitcode.Generic, errors.New("authenticated session is unavailable"))
	}
	authenticated, err := opener(command.Context(), backend, backendSet)
	if err != nil {
		return zero, exitcode.New(exitcode.Config, err)
	}
	return authenticated, nil
}

// NoArgs rejects positional arguments with the command's existing message.
func NoArgs(use string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return exitcode.New(exitcode.Usage, errors.New(use+" takes no arguments"))
		}
		return nil
	}
}

// ExactArgs rejects any positional argument count other than n.
func ExactArgs(n int, message string) func(*cobra.Command, []string) error {
	return func(command *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		if message != "" {
			return exitcode.New(exitcode.Usage, errors.New(message))
		}
		return exitcode.New(exitcode.Usage, cobra.ExactArgs(n)(command, args))
	}
}

// MinimumArgs rejects positional argument counts below n.
func MinimumArgs(n int) func(*cobra.Command, []string) error {
	return func(command *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(n)(command, args); err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		return nil
	}
}
