// Package mecmd implements the authenticated identity health check.
package mecmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
	"github.com/open-cli-collective/spotify-cli/internal/token"
)

// Dependencies contains the runtime effects used by me.
type Dependencies struct {
	Scope      statedir.Scope
	OpenStore  credentials.Opener
	Backend    *string
	Now        func() time.Time
	HTTPClient *http.Client
	TokenURL   string
	APIBaseURL string
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
	configValue, err := config.Load(dependencies.Scope)
	if err != nil {
		return exitcode.New(exitcode.Config, err)
	}
	if strings.TrimSpace(configValue.ClientID) == "" {
		return exitcode.New(exitcode.Config, errors.New("spotify client ID is not configured; run sptfy init"))
	}
	profile, err := credentials.ParseProfile(configValue.CredentialRef)
	if err != nil {
		return exitcode.New(exitcode.Config, err)
	}
	backendFlag := command.Flags().Lookup(credstore.BackendFlagName)
	backendSet := backendFlag != nil && backendFlag.Changed
	if err := credentials.ValidateExplicitBackend(pointerValue(dependencies.Backend), backendSet); err != nil {
		return exitcode.New(exitcode.Usage, err)
	}
	store, err := dependencies.OpenStore(credentials.OpenRequest{
		Config: configValue, Backend: pointerValue(dependencies.Backend), BackendSet: backendSet,
	})
	if err != nil {
		return exitcode.New(exitcode.Config, errors.New("opening credential store failed"))
	}
	defer func() { _ = store.Close() }()
	raw, err := store.Get(profile, credentials.OAuthTokenKey)
	if errors.Is(err, credstore.ErrNotFound) {
		return exitcode.New(exitcode.Config, errors.New("spotify authorization is not configured; run sptfy init"))
	}
	if err != nil {
		return exitcode.New(exitcode.Config, errors.New("reading Spotify authorization failed"))
	}
	now := time.Now()
	if dependencies.Now != nil {
		now = dependencies.Now()
	}
	envelope, err := token.Decode([]byte(raw), now)
	if err != nil {
		return exitcode.New(exitcode.Config, errors.New("stored Spotify authorization is invalid; run sptfy init"))
	}
	if !slices.Contains(envelope.Scopes, auth.ScopeUserReadPrivate) {
		return exitcode.New(exitcode.Config, errors.New("spotify authorization lacks user-read-private; run sptfy init"))
	}
	persist := func(value token.Envelope) error {
		encoded, err := token.Encode(value, now)
		if err != nil {
			return err
		}
		if err := store.Set(profile, credentials.OAuthTokenKey, string(encoded), credstore.WithOverwrite()); err != nil {
			return err
		}
		envelope = value
		return nil
	}
	tokenSource := auth.NewTokenSource(command.Context(), dependencies.HTTPClient, configValue.ClientID, dependencies.TokenURL, envelope, persist)
	oauthContext := command.Context()
	if dependencies.HTTPClient != nil {
		oauthContext = context.WithValue(oauthContext, oauth2.HTTPClient, dependencies.HTTPClient)
	}
	spotifyClient := client.Client{
		HTTPClient: oauth2.NewClient(oauthContext, tokenSource),
		BaseURL:    dependencies.APIBaseURL,
	}
	user, err := spotifyClient.Me(command.Context())
	if err != nil {
		return classify(err)
	}
	if jsonOutput {
		if err := json.NewEncoder(command.OutOrStdout()).Encode(output.NewMeResult(user, envelope.Scopes)); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing JSON output failed"))
		}
		return nil
	}
	if _, err := io.WriteString(command.OutOrStdout(), output.RenderMeText(user, envelope.Scopes)); err != nil {
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
