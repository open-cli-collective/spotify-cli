// Package root defines the top-level sptfy command.
package root

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/catalogcmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/cmdutil"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/configcmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/initcmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/librarycmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/mecmd"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/playlistcmd"
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
	OpenStore      credentials.StoreOpener
	Now            func() time.Time
	Interactive    bool
	OpenBrowser    func(string) error
	HTTPClient     *http.Client
	OAuthEndpoints auth.Endpoints
	APIBaseURL     string
}

// New constructs the top-level command from its runtime effects.
func New(deps Dependencies) *cobra.Command {
	var backend string
	cmd := &cobra.Command{
		Use:           "sptfy",
		Short:         "Use Spotify from the command line",
		Args:          cmdutil.ExactArgs(0, "unknown command"),
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
		Scope: deps.Scope, Cache: deps.Cache, Data: deps.Data,
		OpenStore: func(request credentials.OpenRequest) (configcmd.CredentialStore, error) {
			return deps.OpenStore(request)
		},
	}))
	cmd.AddCommand(setcredential.New(setcredential.Dependencies{
		Scope: deps.Scope, Now: deps.Now,
		OpenStore: func(request credentials.OpenRequest) (setcredential.CredentialStore, error) {
			return deps.OpenStore(request)
		},
	}))
	authorizer := auth.Authorizer{
		HTTPClient: deps.HTTPClient, Endpoints: deps.OAuthEndpoints, OpenBrowser: deps.OpenBrowser,
	}
	cmd.AddCommand(initcmd.New(initcmd.Dependencies{
		Scope: deps.Scope, Interactive: deps.Interactive,
		Initializer: initcmd.Initializer{
			OpenStore: func(request credentials.OpenRequest) (initcmd.CredentialStore, error) {
				return deps.OpenStore(request)
			},
			Now: deps.Now, Authorize: authorizer.Authorize,
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
			SaveConfig: func(value config.Config) error { return config.Save(deps.Scope, value) },
		},
	}))
	sessionOpener := session.Opener{
		Scope: deps.Scope,
		OpenStore: func(request credentials.OpenRequest) (session.CredentialStore, error) {
			return deps.OpenStore(request)
		},
		Now: deps.Now, HTTPClient: deps.HTTPClient,
		TokenURL: deps.OAuthEndpoints.TokenURL, APIBaseURL: deps.APIBaseURL,
	}
	cmd.AddCommand(mecmd.New(mecmd.Dependencies{
		OpenSession: func(ctx context.Context, backend string, backendSet bool) (mecmd.Session, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
	}))
	cmd.AddCommand(searchcmd.New(searchcmd.Dependencies{
		OpenSession: func(ctx context.Context, backend string, backendSet bool) (searchcmd.Session, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
	}))
	cmd.AddCommand(catalogcmd.New(catalogcmd.Dependencies{
		OpenSession: func(ctx context.Context, backend string, backendSet bool) (catalogcmd.Session, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
	})...)
	cmd.AddCommand(librarycmd.New(librarycmd.Dependencies{
		OpenSession: func(ctx context.Context, backend string, backendSet bool) (librarycmd.Session, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
	}))
	cmd.AddCommand(playlistcmd.New(playlistcmd.Dependencies{
		OpenReadSession: func(ctx context.Context, backend string, backendSet bool) (playlistcmd.ReadSession, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
		OpenAddSession: func(ctx context.Context, backend string, backendSet bool) (playlistcmd.AddSession, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
		OpenRemoveSession: func(ctx context.Context, backend string, backendSet bool) (playlistcmd.RemoveSession, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
		OpenUpdateSession: func(ctx context.Context, backend string, backendSet bool) (playlistcmd.UpdateSession, error) {
			return sessionOpener.Open(ctx, backend, backendSet)
		},
	}))
	return cmd
}
