// Package searchcmd implements Spotify resource search commands.
package searchcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/cmdutil"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
	"github.com/open-cli-collective/spotify-cli/internal/pagetoken"
)

const (
	defaultMax = 10
	maxResults = 10
	maxOffset  = 1000
)

// Session is the authenticated capability required by search commands.
type Session interface {
	Close() error
	SearchTracks(context.Context, string, int, int) (client.TrackPage, error)
	SearchAlbums(context.Context, string, int, int) (client.AlbumPage, error)
	SearchArtists(context.Context, string, int, int) (client.ArtistPage, error)
}

// SessionOpener opens the authenticated capability required by search commands.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the authenticated effect used by search commands.
type Dependencies struct {
	OpenSession SessionOpener
}

// New constructs the search command group.
func New(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "search", Short: "Search Spotify", Args: cmdutil.NoArgs("search"),
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newTrack(deps), newAlbum(deps), newArtist(deps))
	return command
}

type searchOptions struct {
	max           int
	nextPageToken string
	id            bool
	fields        string
	extended      bool
	artwork       bool
}

func newTrack(deps Dependencies) *cobra.Command {
	options := searchOptions{}
	command := &cobra.Command{
		Use:   "track <query>",
		Short: "Search tracks (at most 10 results per page)",
		Args:  cmdutil.ExactArgs(1, "search track requires exactly one query"),
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

func runTrack(command *cobra.Command, deps Dependencies, query string, options searchOptions) error {
	offset, err := validateSearch(query, options, "track")
	if err != nil {
		return err
	}
	var fields []output.TrackField
	if !options.id {
		fields, err = output.SelectTrackFields(options.fields, options.extended, options.artwork)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
	}
	authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
	if err != nil {
		return err
	}
	defer func() { _ = authenticated.Close() }()
	page, err := authenticated.SearchTracks(command.Context(), query, options.max, offset)
	if err != nil {
		return exitcode.New(cmdutil.Classify(err), err)
	}
	rendered := output.RenderTracks(page.Items, fields)
	if options.id {
		rendered = output.RenderTrackIDs(page.Items)
	}
	return writeSearchOutput(command, rendered, "track", page.Offset, page.Limit, page.HasNext)
}

func newAlbum(deps Dependencies) *cobra.Command {
	options := searchOptions{}
	command := &cobra.Command{
		Use:   "album <query>",
		Short: "Search albums (at most 10 results per page)",
		Args:  cmdutil.ExactArgs(1, "search album requires exactly one query"),
		RunE:  func(command *cobra.Command, args []string) error { return runAlbum(command, deps, args[0], options) },
	}
	flags := command.Flags()
	flags.IntVarP(&options.max, "max", "m", defaultMax, "Maximum results (1-10; Spotify development-mode cap)")
	flags.StringVar(&options.nextPageToken, "next-page-token", "", "Opaque continuation token")
	flags.BoolVar(&options.id, "id", false, "Emit only album IDs")
	flags.StringVar(&options.fields, "fields", "", "Comma-separated output columns")
	flags.BoolVar(&options.extended, "extended", false, "Add less-frequent album columns")
	flags.BoolVar(&options.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
}

func runAlbum(command *cobra.Command, deps Dependencies, query string, options searchOptions) error {
	offset, err := validateSearch(query, options, "album")
	if err != nil {
		return err
	}
	var fields []output.AlbumField
	if !options.id {
		fields, err = output.SelectAlbumFields(options.fields, options.extended, options.artwork)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
	}
	authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
	if err != nil {
		return err
	}
	defer func() { _ = authenticated.Close() }()
	page, err := authenticated.SearchAlbums(command.Context(), query, options.max, offset)
	if err != nil {
		return exitcode.New(cmdutil.Classify(err), err)
	}
	rendered := output.RenderAlbums(page.Items, fields)
	if options.id {
		rendered = output.RenderAlbumIDs(page.Items)
	}
	return writeSearchOutput(command, rendered, "album", page.Offset, page.Limit, page.HasNext)
}

func newArtist(deps Dependencies) *cobra.Command {
	options := searchOptions{}
	command := &cobra.Command{
		Use:   "artist <query>",
		Short: "Search artists (at most 10 results per page)",
		Args:  cmdutil.ExactArgs(1, "search artist requires exactly one query"),
		RunE:  func(command *cobra.Command, args []string) error { return runArtist(command, deps, args[0], options) },
	}
	flags := command.Flags()
	flags.IntVarP(&options.max, "max", "m", defaultMax, "Maximum results (1-10; Spotify development-mode cap)")
	flags.StringVar(&options.nextPageToken, "next-page-token", "", "Opaque continuation token")
	flags.BoolVar(&options.id, "id", false, "Emit only artist IDs")
	flags.StringVar(&options.fields, "fields", "", "Comma-separated output columns")
	flags.BoolVar(&options.extended, "extended", false, "Add less-frequent artist columns")
	flags.BoolVar(&options.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
}

func runArtist(command *cobra.Command, deps Dependencies, query string, options searchOptions) error {
	offset, err := validateSearch(query, options, "artist")
	if err != nil {
		return err
	}
	var fields []output.ArtistField
	if !options.id {
		fields, err = output.SelectArtistFields(options.fields, options.extended, options.artwork)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
	}
	authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
	if err != nil {
		return err
	}
	defer func() { _ = authenticated.Close() }()
	page, err := authenticated.SearchArtists(command.Context(), query, options.max, offset)
	if err != nil {
		return exitcode.New(cmdutil.Classify(err), err)
	}
	rendered := output.RenderArtists(page.Items, fields)
	if options.id {
		rendered = output.RenderArtistIDs(page.Items)
	}
	return writeSearchOutput(command, rendered, "artist", page.Offset, page.Limit, page.HasNext)
}

func validateSearch(query string, options searchOptions, surface string) (int, error) {
	if strings.TrimSpace(query) == "" {
		return 0, exitcode.New(exitcode.Usage, errors.New("search query must not be blank"))
	}
	if options.max < 1 || options.max > maxResults {
		return 0, exitcode.New(exitcode.Usage, fmt.Errorf("--max must be between 1 and %d", maxResults))
	}
	offset, err := decodePageToken(surface, options.nextPageToken)
	if err != nil {
		return 0, exitcode.New(exitcode.Usage, err)
	}
	return offset, nil
}

func writeSearchOutput(command *cobra.Command, rendered, surface string, offset, limit int, hasNext bool) error {
	if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing search output failed"))
	}
	nextOffset := offset + limit
	if hasNext && nextOffset <= maxOffset {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", encodePageToken(surface, nextOffset)); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
		}
	}
	return nil
}

func encodePageToken(surface string, offset int) string {
	return pagetoken.Encode(surface, offset)
}

func decodePageToken(surface, value string) (int, error) {
	if len(value) > 64 {
		return 0, errors.New("invalid --next-page-token")
	}
	offset, err := pagetoken.Decode(surface, value, maxOffset)
	if err != nil {
		return 0, errors.New("invalid --next-page-token")
	}
	return offset, nil
}
