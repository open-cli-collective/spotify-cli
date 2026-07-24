// Package playlistcmd implements current-user playlist reads.
package playlistcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
	"github.com/open-cli-collective/spotify-cli/internal/pagetoken"
	"github.com/open-cli-collective/spotify-cli/internal/spotifyref"
)

const playlistPageScope = "playlists"

// Session is the authenticated capability required by playlist commands.
type Session interface {
	Close() error
	Scopes() []string
	ListCurrentUserPlaylists(context.Context, int, int) (client.PlaylistPage, error)
	GetPlaylist(context.Context, string) (client.Playlist, error)
	ListPlaylistItems(context.Context, string, int, int) (client.PlaylistItemPage, error)
}

// SessionOpener opens the authenticated capability required by playlist commands.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the authenticated effect used by playlist commands.
type Dependencies struct {
	OpenSession SessionOpener
	Backend     *string
}

type options struct {
	id       bool
	fields   string
	extended bool
	artwork  bool
}

type listOptions struct {
	options
	max           int
	nextPageToken string
}

// New constructs the playlists command tree.
func New(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "playlists", Aliases: []string{"playlist"}, Short: "Read Spotify playlists",
		Args: noArgs("playlists"), RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newList(deps), newGet(deps), newItems(deps))
	return command
}

func newItems(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "items", Short: "Read ordered Spotify playlist items", Args: noArgs("items"),
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newItemsList(deps))
	return command
}

func newItemsList(deps Dependencies) *cobra.Command {
	opts := listOptions{max: 10}
	command := &cobra.Command{
		Use: "list <spotify-id-uri-or-url>", Short: "List ordered Spotify playlist items",
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			id, err := spotifyref.Parse(args[0], spotifyref.Playlist)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			if opts.max < 1 || opts.max > 50 {
				return exitcode.New(exitcode.Usage, errors.New("--max must be between 1 and 50"))
			}
			var fields []output.PlaylistItemField
			if !opts.id {
				fields, err = output.SelectPlaylistItemFields(opts.fields, opts.extended, opts.artwork)
				if err != nil {
					return exitcode.New(exitcode.Usage, err)
				}
			}
			scope := playlistItemPageScope(id)
			offset, err := pagetoken.Decode(scope, opts.nextPageToken, math.MaxInt-50)
			if err != nil {
				return exitcode.New(exitcode.Usage, errors.New("invalid --next-page-token"))
			}
			authenticated, err := openSession(command, deps)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			page, err := authenticated.ListPlaylistItems(command.Context(), id, opts.max, offset)
			if err != nil {
				return classify(err)
			}
			rendered := output.RenderPlaylistItemIDs(page.Items)
			if !opts.id {
				rendered = "Playlist ID: " + id + "\n" + output.RenderPlaylistItems(page.Items, page.Offset, fields)
			}
			if err := writeOutput(command, rendered); err != nil {
				return err
			}
			if page.HasNext {
				if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", pagetoken.Encode(scope, page.Offset+page.Limit)); err != nil {
					return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
				}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.IntVarP(&opts.max, "max", "m", 10, "Maximum results (1-50)")
	flags.StringVar(&opts.nextPageToken, "next-page-token", "", "Opaque continuation token")
	addOutputFlags(command, &opts.options, "item IDs")
	return command
}

func playlistItemPageScope(id string) string {
	return "playlist-items:" + id
}

func newList(deps Dependencies) *cobra.Command {
	opts := listOptions{max: 10}
	command := &cobra.Command{
		Use: "list", Short: "List current user's Spotify playlists", Args: noArgs("list"),
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.max < 1 || opts.max > 50 {
				return exitcode.New(exitcode.Usage, errors.New("--max must be between 1 and 50"))
			}
			var fields []output.PlaylistField
			var err error
			if !opts.id {
				fields, err = output.SelectPlaylistFields(opts.fields, opts.extended, opts.artwork)
				if err != nil {
					return exitcode.New(exitcode.Usage, err)
				}
			}
			offset, err := pagetoken.Decode(playlistPageScope, opts.nextPageToken, math.MaxInt-50)
			if err != nil {
				return exitcode.New(exitcode.Usage, errors.New("invalid --next-page-token"))
			}
			authenticated, err := openSession(command, deps)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			page, err := authenticated.ListCurrentUserPlaylists(command.Context(), opts.max, offset)
			if err != nil {
				return classify(err)
			}
			rendered := output.RenderPlaylists(page.Items, fields)
			if opts.id {
				rendered = output.RenderPlaylistIDs(page.Items)
			}
			if err := writeOutput(command, rendered); err != nil {
				return err
			}
			if page.HasNext {
				if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", pagetoken.Encode(playlistPageScope, page.Offset+page.Limit)); err != nil {
					return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
				}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.IntVarP(&opts.max, "max", "m", 10, "Maximum results (1-50)")
	flags.StringVar(&opts.nextPageToken, "next-page-token", "", "Opaque continuation token")
	addOutputFlags(command, &opts.options, "IDs")
	return command
}

func newGet(deps Dependencies) *cobra.Command {
	var opts options
	command := &cobra.Command{
		Use: "get <spotify-id-uri-or-url>", Short: "Get one Spotify playlist",
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			id, err := spotifyref.Parse(args[0], spotifyref.Playlist)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			var fields []output.PlaylistField
			if !opts.id {
				fields, err = output.SelectPlaylistFields(opts.fields, opts.extended, opts.artwork)
				if err != nil {
					return exitcode.New(exitcode.Usage, err)
				}
			}
			authenticated, err := openSession(command, deps)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			playlist, err := authenticated.GetPlaylist(command.Context(), id)
			if err != nil {
				return classify(err)
			}
			rendered := output.RenderPlaylist(playlist, fields)
			if opts.id {
				rendered = output.RenderPlaylistIDs([]client.Playlist{playlist})
			}
			return writeOutput(command, rendered)
		},
	}
	addOutputFlags(command, &opts, "ID")
	return command
}

func addOutputFlags(command *cobra.Command, opts *options, idNoun string) {
	flags := command.Flags()
	flags.BoolVar(&opts.id, "id", false, "Emit only playlist "+idNoun)
	flags.StringVar(&opts.fields, "fields", "", "Comma-separated output fields")
	flags.BoolVar(&opts.extended, "extended", false, "Add less-frequent playlist fields")
	flags.BoolVar(&opts.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
}

func openSession(command *cobra.Command, deps Dependencies) (Session, error) {
	backendFlag := command.Flags().Lookup(credstore.BackendFlagName)
	backendSet := backendFlag != nil && backendFlag.Changed
	backend := ""
	if deps.Backend != nil {
		backend = *deps.Backend
	}
	if err := credentials.ValidateExplicitBackend(backend, backendSet); err != nil {
		return nil, exitcode.New(exitcode.Usage, err)
	}
	if deps.OpenSession == nil {
		return nil, exitcode.New(exitcode.Generic, errors.New("authenticated session is unavailable"))
	}
	authenticated, err := deps.OpenSession(command.Context(), backend, backendSet)
	if err != nil {
		return nil, exitcode.New(exitcode.Config, err)
	}
	for _, scope := range []string{auth.ScopePlaylistReadCollaborative, auth.ScopePlaylistReadPrivate} {
		if !slices.Contains(authenticated.Scopes(), scope) {
			_ = authenticated.Close()
			return nil, exitcode.New(exitcode.Config, fmt.Errorf("spotify authorization lacks %s; run sptfy init --overwrite", scope))
		}
	}
	return authenticated, nil
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

func writeOutput(command *cobra.Command, rendered string) error {
	if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing playlist output failed"))
	}
	return nil
}

func noArgs(use string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return exitcode.New(exitcode.Usage, errors.New(use+" takes no arguments"))
		}
		return nil
	}
}
