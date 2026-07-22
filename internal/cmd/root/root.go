// Package root defines the top-level sptfy command.
package root

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/configcmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/initcmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/mecmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/searchcmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/setcredential"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/session"
	"github.com/open-cli-collective/spotify-cli/internal/token"
	"github.com/open-cli-collective/spotify-cli/internal/version"
)

// Dependencies contains the runtime effects used by the command tree.
type Dependencies struct {
	In             io.Reader
	Out            io.Writer
	ErrOut         io.Writer
	Scope          statedir.Scope
	Cache          statedir.Cache
	Data           statedir.Data
	OpenStore      credentials.Opener
	Now            func() time.Time
	Interactive    bool
	Prompt         func(*initcmd.Setup) error
	OpenBrowser    func(string) error
	HTTPClient     *http.Client
	OAuthEndpoints auth.Endpoints
	APIBaseURL     string
	SaveConfig     func(config.Config) error
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
	authorizer := auth.Authorizer{
		HTTPClient: deps.HTTPClient, Endpoints: deps.OAuthEndpoints, OpenBrowser: deps.OpenBrowser,
	}
	cmd.AddCommand(initcmd.New(initcmd.Dependencies{
		Scope: deps.Scope, OpenStore: deps.OpenStore, Backend: &backend, Now: deps.Now,
		Interactive: deps.Interactive, Prompt: deps.Prompt, Authorize: authorizer.Authorize,
		Verify: func(ctx context.Context, _ config.Config, envelope token.Envelope) (client.User, error) {
			oauthContext := ctx
			if deps.HTTPClient != nil {
				oauthContext = context.WithValue(ctx, oauth2.HTTPClient, deps.HTTPClient)
			}
			httpClient := oauth2.NewClient(oauthContext, oauth2.StaticTokenSource(&oauth2.Token{
				AccessToken: envelope.AccessToken, TokenType: envelope.TokenType,
				RefreshToken: envelope.RefreshToken, Expiry: envelope.ExpiresAt,
			}))
			return (client.Client{HTTPClient: httpClient, BaseURL: deps.APIBaseURL}).Me(ctx)
		},
		SaveConfig: deps.SaveConfig,
	}))
	sessionOpener := session.Opener{
		Scope: deps.Scope, OpenStore: deps.OpenStore, Now: deps.Now, HTTPClient: deps.HTTPClient,
		TokenURL: deps.OAuthEndpoints.TokenURL, APIBaseURL: deps.APIBaseURL,
	}
	cmd.AddCommand(mecmd.New(mecmd.Dependencies{OpenSession: sessionOpener.Open, Backend: &backend}))
	cmd.AddCommand(searchcmd.New(searchcmd.Dependencies{OpenSession: sessionOpener.Open, Backend: &backend}))
	return cmd
}
