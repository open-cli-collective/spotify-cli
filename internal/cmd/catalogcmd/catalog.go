// Package catalogcmd implements Spotify single-resource catalog reads.
package catalogcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/cmdutil"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
	"github.com/open-cli-collective/spotify-cli/internal/pagetoken"
	"github.com/open-cli-collective/spotify-cli/internal/spotifyref"
)

const defaultMax = 10

// Session is the authenticated capability required by catalog commands.
type Session interface {
	Close() error
	GetTrack(context.Context, string) (client.Track, error)
	GetAlbum(context.Context, string) (client.Album, error)
	GetArtist(context.Context, string) (client.Artist, error)
	ListAlbumTracks(context.Context, string, int, int) (client.TrackPage, error)
	ListArtistAlbums(context.Context, string, int, int) (client.AlbumPage, error)
}

// SessionOpener opens the authenticated capability required by catalog commands.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the authenticated effect used by catalog commands.
type Dependencies struct {
	OpenSession SessionOpener
}

type options struct {
	id       bool
	fields   string
	extended bool
	artwork  bool
}

type listOptions struct {
	max           int
	nextPageToken string
	id            bool
	fields        string
	extended      bool
	artwork       bool
}

// New constructs the three catalog command groups.
func New(deps Dependencies) []*cobra.Command {
	return []*cobra.Command{
		newGroup("tracks", "track", newTrack(deps)),
		newGroup("albums", "album", newAlbum(deps), relationshipGroup("tracks", newAlbumTracks(deps))),
		newGroup("artists", "artist", newArtist(deps), relationshipGroup("albums", newArtistAlbums(deps))),
	}
}

func newGroup(use, alias string, children ...*cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use: use, Aliases: []string{alias}, Short: "Read Spotify " + use,
		Args: cmdutil.NoArgs(use),
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(children...)
	return command
}

func relationshipGroup(use string, child *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use: use, Short: "Traverse Spotify " + use, Args: cmdutil.NoArgs(use),
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(child)
	return command
}

func newTrack(deps Dependencies) *cobra.Command {
	var opts options
	command := getCommand("track", &opts, func(command *cobra.Command, reference string) error {
		id, err := spotifyref.Parse(reference, spotifyref.Track)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		var fields []output.TrackField
		if !opts.id {
			fields, err = output.SelectTrackFields(opts.fields, opts.extended, opts.artwork)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
		}
		authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		track, err := authenticated.GetTrack(command.Context(), id)
		if err != nil {
			return exitcode.New(cmdutil.Classify(err), err)
		}
		rendered := output.RenderTrack(track, fields)
		if opts.id {
			rendered = output.RenderTrackIDs([]client.Track{track})
		}
		return writeOutput(command, rendered)
	})
	return command
}

func newAlbum(deps Dependencies) *cobra.Command {
	var opts options
	return getCommand("album", &opts, func(command *cobra.Command, reference string) error {
		id, err := spotifyref.Parse(reference, spotifyref.Album)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		var fields []output.AlbumField
		if !opts.id {
			fields, err = output.SelectAlbumFields(opts.fields, opts.extended, opts.artwork)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
		}
		authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		album, err := authenticated.GetAlbum(command.Context(), id)
		if err != nil {
			return exitcode.New(cmdutil.Classify(err), err)
		}
		rendered := output.RenderAlbum(album, fields)
		if opts.id {
			rendered = output.RenderAlbumIDs([]client.Album{album})
		}
		return writeOutput(command, rendered)
	})
}

func newArtist(deps Dependencies) *cobra.Command {
	var opts options
	return getCommand("artist", &opts, func(command *cobra.Command, reference string) error {
		id, err := spotifyref.Parse(reference, spotifyref.Artist)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		var fields []output.ArtistField
		if !opts.id {
			fields, err = output.SelectArtistFields(opts.fields, opts.extended, opts.artwork)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
		}
		authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		artist, err := authenticated.GetArtist(command.Context(), id)
		if err != nil {
			return exitcode.New(cmdutil.Classify(err), err)
		}
		rendered := output.RenderArtist(artist, fields)
		if opts.id {
			rendered = output.RenderArtistIDs([]client.Artist{artist})
		}
		return writeOutput(command, rendered)
	})
}

func newAlbumTracks(deps Dependencies) *cobra.Command {
	var opts listOptions
	return listCommand("album tracks", "track", 50, false, &opts, func(command *cobra.Command, reference string) error {
		id, err := spotifyref.Parse(reference, spotifyref.Album)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		if opts.max < 1 || opts.max > 50 {
			return exitcode.New(exitcode.Usage, errors.New("--max must be between 1 and 50"))
		}
		var fields []output.TrackField
		if !opts.id {
			fields, err = output.SelectAlbumTrackFields(opts.fields, opts.extended)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
		}
		scope := "album-tracks:" + id
		offset, err := decodeTraversalToken(scope, opts.nextPageToken, 50)
		if err != nil {
			return err
		}
		authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		page, err := authenticated.ListAlbumTracks(command.Context(), id, opts.max, offset)
		if err != nil {
			return exitcode.New(cmdutil.Classify(err), err)
		}
		rendered := output.RenderTracks(page.Items, fields)
		if opts.id {
			rendered = output.RenderTrackIDs(page.Items)
		} else {
			rendered = "Album ID: " + id + "\n" + rendered
		}
		return writeListOutput(command, rendered, scope, page.Offset, page.Limit, page.HasNext)
	})
}

func newArtistAlbums(deps Dependencies) *cobra.Command {
	var opts listOptions
	return listCommand("artist albums", "album", 10, true, &opts, func(command *cobra.Command, reference string) error {
		id, err := spotifyref.Parse(reference, spotifyref.Artist)
		if err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		if opts.max < 1 || opts.max > 10 {
			return exitcode.New(exitcode.Usage, errors.New("--max must be between 1 and 10"))
		}
		var fields []output.AlbumField
		if !opts.id {
			fields, err = output.SelectAlbumFields(opts.fields, opts.extended, opts.artwork)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
		}
		scope := "artist-albums:" + id
		offset, err := decodeTraversalToken(scope, opts.nextPageToken, 10)
		if err != nil {
			return err
		}
		authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		page, err := authenticated.ListArtistAlbums(command.Context(), id, opts.max, offset)
		if err != nil {
			return exitcode.New(cmdutil.Classify(err), err)
		}
		rendered := output.RenderAlbums(page.Items, fields)
		if opts.id {
			rendered = output.RenderAlbumIDs(page.Items)
		} else {
			rendered = "Artist ID: " + id + "\n" + rendered
		}
		return writeListOutput(command, rendered, scope, page.Offset, page.Limit, page.HasNext)
	})
}

func listCommand(parent, resource string, maxResults int, artwork bool, opts *listOptions, run func(*cobra.Command, string) error) *cobra.Command {
	command := &cobra.Command{
		Use: "list <spotify-id-uri-or-url>", Short: "List Spotify " + parent,
		Args: cmdutil.ExactArgs(1, ""),
		RunE: func(command *cobra.Command, args []string) error { return run(command, args[0]) },
	}
	flags := command.Flags()
	flags.IntVarP(&opts.max, "max", "m", defaultMax, fmt.Sprintf("Maximum results (1-%d)", maxResults))
	flags.StringVar(&opts.nextPageToken, "next-page-token", "", "Opaque continuation token")
	flags.BoolVar(&opts.id, "id", false, "Emit only "+resource+" IDs")
	flags.StringVar(&opts.fields, "fields", "", "Comma-separated output fields")
	flags.BoolVar(&opts.extended, "extended", false, "Add less-frequent "+resource+" fields")
	if artwork {
		flags.BoolVar(&opts.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	}
	return command
}

func decodeTraversalToken(scope, value string, pageLimit int) (int, error) {
	offset, err := pagetoken.Decode(scope, value, math.MaxInt-pageLimit)
	if err != nil {
		return 0, exitcode.New(exitcode.Usage, errors.New("invalid --next-page-token"))
	}
	return offset, nil
}

func getCommand(resource string, opts *options, run func(*cobra.Command, string) error) *cobra.Command {
	command := &cobra.Command{
		Use: "get <spotify-id-uri-or-url>", Short: "Get one Spotify " + resource,
		Args: cmdutil.ExactArgs(1, ""),
		RunE: func(command *cobra.Command, args []string) error { return run(command, args[0]) },
	}
	flags := command.Flags()
	flags.BoolVar(&opts.id, "id", false, "Emit only the "+resource+" ID")
	flags.StringVar(&opts.fields, "fields", "", "Comma-separated output fields")
	flags.BoolVar(&opts.extended, "extended", false, "Add less-frequent "+resource+" fields")
	flags.BoolVar(&opts.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
}

func writeOutput(command *cobra.Command, rendered string) error {
	if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing catalog output failed"))
	}
	return nil
}

func writeListOutput(command *cobra.Command, rendered, scope string, offset, limit int, hasNext bool) error {
	if err := writeOutput(command, rendered); err != nil {
		return err
	}
	if hasNext {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", pagetoken.Encode(scope, offset+limit)); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
		}
	}
	return nil
}
