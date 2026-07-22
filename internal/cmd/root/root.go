// Package root defines the top-level sptfy command.
package root

import (
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/version"
)

// New constructs the top-level command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sptfy",
		Short:         "Use Spotify from the command line",
		Version:       version.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.SetVersionTemplate("sptfy " + version.Info() + "\n")
	return cmd
}
