// Package root defines the top-level sptfy command.
package root

import (
	"errors"
	"io"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/cmd/configcmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/setcredential"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/version"
)

// Dependencies contains the runtime effects used by the command tree.
type Dependencies struct {
	In        io.Reader
	Out       io.Writer
	ErrOut    io.Writer
	Scope     statedir.Scope
	Cache     statedir.Cache
	Data      statedir.Data
	OpenStore credentials.Opener
	Now       func() time.Time
}

// New constructs the top-level command from its runtime effects.
func New(deps Dependencies) *cobra.Command {
	var backend string
	cmd := &cobra.Command{
		Use:   "sptfy",
		Short: "Use Spotify from the command line",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return exitcode.New(exitcode.Usage, errors.New("unknown command"))
			}
			return nil
		},
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Version:       version.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.SetVersionTemplate("sptfy " + version.Info() + "\n")
	cmd.SetIn(deps.In)
	cmd.SetOut(deps.Out)
	cmd.SetErr(deps.ErrOut)
	cmd.PersistentFlags().StringVar(&backend, credstore.BackendFlagName, "", credstore.BackendFlagUsage())
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		flag := cmd.PersistentFlags().Lookup(credstore.BackendFlagName)
		if err := credentials.ValidateExplicitBackend(backend, flag != nil && flag.Changed); err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		return nil
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcode.New(exitcode.Usage, err)
	})
	cmd.AddCommand(configcmd.New(configcmd.Dependencies{
		Scope: deps.Scope, Cache: deps.Cache, Data: deps.Data, OpenStore: deps.OpenStore, Backend: &backend,
	}))
	cmd.AddCommand(setcredential.New(setcredential.Dependencies{
		Scope: deps.Scope, OpenStore: deps.OpenStore, Backend: &backend, Now: deps.Now,
	}))
	return cmd
}
