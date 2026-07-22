// Package searchcmd implements Spotify resource search commands.
package searchcmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
)

const (
	defaultMax = 10
	maxResults = 10
	maxOffset  = 1000
)

// Session is the authenticated capability required by track search.
type Session interface {
	Close() error
	SearchTracks(context.Context, string, int, int) (client.TrackPage, error)
}

// SessionOpener opens the authenticated capability required by track search.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the authenticated effect used by search commands.
type Dependencies struct {
	OpenSession SessionOpener
	Backend     *string
}

// New constructs the search command group.
func New(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "search", Short: "Search Spotify", Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newTrack(deps))
	return command
}

type trackOptions struct {
	max           int
	nextPageToken string
	id            bool
	fields        string
	extended      bool
	artwork       bool
}

func newTrack(deps Dependencies) *cobra.Command {
	options := trackOptions{}
	command := &cobra.Command{
		Use:   "track <query>",
		Short: "Search tracks (at most 10 results per page)",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return exitcode.New(exitcode.Usage, errors.New("search track requires exactly one query"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			return runTrack(command, deps, args[0], options)
		},
	}
	flags := command.Flags()
	flags.IntVarP(&options.max, "max", "m", defaultMax, "Maximum results (1-10; Spotify development-mode cap)")
	flags.StringVar(&options.nextPageToken, "next-page-token", "", "Opaque continuation token")
	flags.BoolVar(&options.id, "id", false, "Emit only track IDs")
	flags.StringVar(&options.fields, "fields", "", "Comma-separated output columns")
	flags.BoolVar(&options.extended, "extended", false, "Add less-frequent track columns")
	flags.BoolVar(&options.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
}

func runTrack(command *cobra.Command, deps Dependencies, query string, options trackOptions) error {
	if strings.TrimSpace(query) == "" {
		return exitcode.New(exitcode.Usage, errors.New("search query must not be blank"))
	}
	if options.max < 1 || options.max > maxResults {
		return exitcode.New(exitcode.Usage, fmt.Errorf("--max must be between 1 and %d", maxResults))
	}
	offset, err := decodePageToken(options.nextPageToken)
	if err != nil {
		return exitcode.New(exitcode.Usage, err)
	}
	var fields []output.TrackField
	if !options.id {
		fields, err = output.SelectTrackFields(options.fields, options.extended, options.artwork)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
	}
	backendFlag := command.Flags().Lookup(credstore.BackendFlagName)
	backendSet := backendFlag != nil && backendFlag.Changed
	backend := pointerValue(deps.Backend)
	if err := credentials.ValidateExplicitBackend(backend, backendSet); err != nil {
		return exitcode.New(exitcode.Usage, err)
	}
	if deps.OpenSession == nil {
		return exitcode.New(exitcode.Generic, errors.New("authenticated session is unavailable"))
	}
	authenticated, err := deps.OpenSession(command.Context(), backend, backendSet)
	if err != nil {
		return exitcode.New(exitcode.Config, err)
	}
	defer func() { _ = authenticated.Close() }()
	page, err := authenticated.SearchTracks(command.Context(), query, options.max, offset)
	if err != nil {
		return classify(err)
	}
	rendered := output.RenderTracks(page.Items, fields)
	if options.id {
		rendered = output.RenderTrackIDs(page.Items)
	}
	if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing track output failed"))
	}
	nextOffset := page.Offset + page.Limit
	if page.HasNext && nextOffset <= maxOffset {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", encodePageToken(nextOffset)); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
		}
	}
	return nil
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("v1:track:" + strconv.Itoa(offset)))
}

func decodePageToken(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	if len(value) > 64 {
		return 0, errors.New("invalid --next-page-token")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, errors.New("invalid --next-page-token")
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != "track" {
		return 0, errors.New("invalid --next-page-token")
	}
	offset, err := strconv.Atoi(parts[2])
	if err != nil || offset < 0 || offset > maxOffset {
		return 0, errors.New("invalid --next-page-token")
	}
	return offset, nil
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

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) != 0 {
		return exitcode.New(exitcode.Usage, errors.New("search takes no arguments"))
	}
	return nil
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
