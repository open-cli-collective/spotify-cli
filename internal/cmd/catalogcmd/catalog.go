// Package catalogcmd implements Spotify single-resource catalog reads.
package catalogcmd

import (
	"context"
	"errors"
	"io"

	"github.com/open-cli-collective/cli-common/credstore"
	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
	"github.com/open-cli-collective/spotify-cli/internal/spotifyref"
)

// Session is the authenticated capability required by catalog commands.
type Session interface {
	Close() error
	GetTrack(context.Context, string) (client.Track, error)
	GetAlbum(context.Context, string) (client.Album, error)
	GetArtist(context.Context, string) (client.Artist, error)
}

// SessionOpener opens the authenticated capability required by catalog commands.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the authenticated effect used by catalog commands.
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

// New constructs the three catalog command groups.
func New(deps Dependencies) []*cobra.Command {
	return []*cobra.Command{
		newGroup("tracks", "track", newTrack(deps)),
		newGroup("albums", "album", newAlbum(deps)),
		newGroup("artists", "artist", newArtist(deps)),
	}
}

func newGroup(use, alias string, child *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use: use, Aliases: []string{alias}, Short: "Read Spotify " + use,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return exitcode.New(exitcode.Usage, errors.New(use+" takes no arguments"))
			}
			return nil
		},
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
		authenticated, err := openSession(command, deps)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		track, err := authenticated.GetTrack(command.Context(), id)
		if err != nil {
			return classify(err)
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
		authenticated, err := openSession(command, deps)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		album, err := authenticated.GetAlbum(command.Context(), id)
		if err != nil {
			return classify(err)
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
		authenticated, err := openSession(command, deps)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		artist, err := authenticated.GetArtist(command.Context(), id)
		if err != nil {
			return classify(err)
		}
		rendered := output.RenderArtist(artist, fields)
		if opts.id {
			rendered = output.RenderArtistIDs([]client.Artist{artist})
		}
		return writeOutput(command, rendered)
	})
}

func getCommand(resource string, opts *options, run func(*cobra.Command, string) error) *cobra.Command {
	command := &cobra.Command{
		Use: "get <spotify-id-uri-or-url>", Short: "Get one Spotify " + resource,
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error { return run(command, args[0]) },
	}
	flags := command.Flags()
	flags.BoolVar(&opts.id, "id", false, "Emit only the "+resource+" ID")
	flags.StringVar(&opts.fields, "fields", "", "Comma-separated output fields")
	flags.BoolVar(&opts.extended, "extended", false, "Add less-frequent "+resource+" fields")
	flags.BoolVar(&opts.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
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
		return exitcode.New(exitcode.Generic, errors.New("writing catalog output failed"))
	}
	return nil
}
