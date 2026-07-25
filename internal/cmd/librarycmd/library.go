// Package librarycmd implements saved-library operations.
package librarycmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/cmd/cmdutil"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/output"
	"github.com/open-cli-collective/spotify-cli/internal/pagetoken"
	"github.com/open-cli-collective/spotify-cli/internal/spotifyref"
)

const (
	trackPageScope = "library-tracks"
	albumPageScope = "library-albums"
)

// Session is the authenticated capability required by library commands.
type Session interface {
	Close() error
	Scopes() []string
	ListSavedTracks(context.Context, int, int) (client.SavedTrackPage, error)
	ListSavedAlbums(context.Context, int, int) (client.SavedAlbumPage, error)
	CheckSavedItems(context.Context, spotifyref.Kind, []string) ([]bool, error)
	SaveSavedItems(context.Context, spotifyref.Kind, []string) error
	RemoveSavedItems(context.Context, spotifyref.Kind, []string) error
}

// SessionOpener opens the authenticated capability required by library commands.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the authenticated effect used by library commands.
type Dependencies struct {
	OpenSession SessionOpener
}

type listOptions struct {
	max           int
	nextPageToken string
	id            bool
	fields        string
	extended      bool
	artwork       bool
}

type libraryReference struct {
	reference string
	id        string
}

// New constructs the saved library command tree.
func New(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "library", Short: "Manage the Spotify library", Args: cmdutil.NoArgs("library")}
	tracks := &cobra.Command{Use: "tracks", Short: "Manage saved tracks", Args: cmdutil.NoArgs("tracks")}
	tracks.AddCommand(newTrackList(deps), newCheck(deps, spotifyref.Track), newMutation(deps, spotifyref.Track, "add"), newMutation(deps, spotifyref.Track, "remove"))
	albums := &cobra.Command{Use: "albums", Short: "Manage saved albums", Args: cmdutil.NoArgs("albums")}
	albums.AddCommand(newAlbumList(deps), newCheck(deps, spotifyref.Album), newMutation(deps, spotifyref.Album, "add"), newMutation(deps, spotifyref.Album, "remove"))
	command.AddCommand(tracks, albums)
	return command
}

func newTrackList(deps Dependencies) *cobra.Command {
	opts := &listOptions{}
	var fields []output.TrackField
	prepare := func() error {
		if !opts.id {
			var err error
			fields, err = output.SelectSavedTrackFields(opts.fields, opts.extended, opts.artwork)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
		}
		return nil
	}
	return listCommand("List saved tracks", "track", trackPageScope, opts, prepare, func(command *cobra.Command, offset int) error {
		authenticated, err := openSession(command, deps, auth.ScopeUserLibraryRead)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		page, err := authenticated.ListSavedTracks(command.Context(), opts.max, offset)
		if err != nil {
			return exitcode.New(cmdutil.Classify(err), err)
		}
		var rendered string
		if opts.id {
			rendered = output.RenderSavedTrackIDs(page.Items)
		} else {
			rendered = output.RenderSavedTracks(page.Items, fields)
		}
		return writeListOutput(command, rendered, "tracks", trackPageScope, page.Offset, page.Limit, page.HasNext)
	})
}

func newAlbumList(deps Dependencies) *cobra.Command {
	opts := &listOptions{}
	var fields []output.AlbumField
	prepare := func() error {
		if !opts.id {
			var err error
			fields, err = output.SelectSavedAlbumFields(opts.fields, opts.extended, opts.artwork)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
		}
		return nil
	}
	return listCommand("List saved albums", "album", albumPageScope, opts, prepare, func(command *cobra.Command, offset int) error {
		authenticated, err := openSession(command, deps, auth.ScopeUserLibraryRead)
		if err != nil {
			return err
		}
		defer func() { _ = authenticated.Close() }()
		page, err := authenticated.ListSavedAlbums(command.Context(), opts.max, offset)
		if err != nil {
			return exitcode.New(cmdutil.Classify(err), err)
		}
		var rendered string
		if opts.id {
			rendered = output.RenderSavedAlbumIDs(page.Items)
		} else {
			rendered = output.RenderSavedAlbums(page.Items, fields)
		}
		return writeListOutput(command, rendered, "albums", albumPageScope, page.Offset, page.Limit, page.HasNext)
	})
}

func listCommand(short, resource, pageScope string, opts *listOptions, prepare func() error, run func(*cobra.Command, int) error) *cobra.Command {
	command := &cobra.Command{
		Use: "list", Short: short, Args: cmdutil.NoArgs("list"),
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.max < 1 || opts.max > 50 {
				return exitcode.New(exitcode.Usage, errors.New("--max must be between 1 and 50"))
			}
			if err := prepare(); err != nil {
				return err
			}
			offset, err := pagetoken.Decode(pageScope, opts.nextPageToken)
			if err != nil {
				return exitcode.New(exitcode.Usage, errors.New("invalid --next-page-token"))
			}
			return run(command, offset)
		},
	}
	flags := command.Flags()
	flags.IntVarP(&opts.max, "max", "m", 10, "Maximum results (1-50)")
	flags.StringVar(&opts.nextPageToken, "next-page-token", "", "Opaque continuation token")
	flags.BoolVar(&opts.id, "id", false, "Emit only "+resource+" IDs")
	flags.StringVar(&opts.fields, "fields", "", "Comma-separated output fields")
	flags.BoolVar(&opts.extended, "extended", false, "Add less-frequent "+resource+" fields")
	flags.BoolVar(&opts.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
}

func writeListOutput(command *cobra.Command, rendered, plural, pageScope string, offset, limit int, hasNext bool) error {
	if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
		return exitcode.New(exitcode.Generic, errors.New("writing saved "+plural+" failed"))
	}
	if hasNext {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", pagetoken.Encode(pageScope, offset+limit)); err != nil {
			return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
		}
	}
	return nil
}

func newCheck(deps Dependencies, kind spotifyref.Kind) *cobra.Command {
	plural := resourcePlural(kind)
	return &cobra.Command{
		Use: "check <" + string(kind) + "-reference>...", Short: "Check whether " + plural + " are saved",
		Args: cmdutil.MinimumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			references, err := parseReferences(args, kind)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			authenticated, err := openSession(command, deps, auth.ScopeUserLibraryRead)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			saved, err := authenticated.CheckSavedItems(command.Context(), kind, referenceIDs(references))
			if err != nil {
				return exitcode.New(cmdutil.Classify(err), err)
			}
			if len(saved) != len(references) {
				return exitcode.New(exitcode.Upstream, client.ErrInvalidResponse)
			}
			checks := make([]output.SavedCheck, len(references))
			for index, reference := range references {
				checks[index] = output.SavedCheck{Reference: reference.reference, ID: reference.id, Saved: saved[index]}
			}
			if _, err := io.WriteString(command.OutOrStdout(), output.RenderSavedChecks(checks)); err != nil {
				return exitcode.New(exitcode.Generic, errors.New("writing saved-item checks failed"))
			}
			return nil
		},
	}
}

func newMutation(deps Dependencies, kind spotifyref.Kind, verb string) *cobra.Command {
	plural := resourcePlural(kind)
	return &cobra.Command{
		Use: verb + " <" + string(kind) + "-reference>...", Short: verb + " " + plural + " in the library",
		Args: cmdutil.MinimumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			references, err := parseReferences(args, kind)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			authenticated, err := openSession(command, deps, auth.ScopeUserLibraryModify)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			ids := referenceIDs(references)
			if verb == "add" {
				err = authenticated.SaveSavedItems(command.Context(), kind, ids)
			} else {
				err = authenticated.RemoveSavedItems(command.Context(), kind, ids)
			}
			if err != nil {
				return exitcode.New(cmdutil.Classify(err), err)
			}
			result := "added"
			if verb == "remove" {
				result = "removed"
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%d\n", result, len(references)); err != nil {
				return exitcode.New(exitcode.Generic, errors.New("writing library mutation result failed"))
			}
			return nil
		},
	}
}

func parseReferences(args []string, kind spotifyref.Kind) ([]libraryReference, error) {
	result := make([]libraryReference, 0, len(args))
	seen := make(map[string]struct{}, len(args))
	for _, reference := range args {
		id, err := spotifyref.Parse(reference, kind)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, libraryReference{reference: reference, id: id})
	}
	return result, nil
}

func referenceIDs(references []libraryReference) []string {
	ids := make([]string, len(references))
	for index, reference := range references {
		ids[index] = reference.id
	}
	return ids
}

func resourcePlural(kind spotifyref.Kind) string {
	if kind == spotifyref.Album {
		return "albums"
	}
	return "tracks"
}

func openSession(command *cobra.Command, deps Dependencies, requiredScope string) (Session, error) {
	authenticated, err := cmdutil.OpenSession(command, deps.OpenSession)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(authenticated.Scopes(), requiredScope) {
		_ = authenticated.Close()
		return nil, exitcode.New(exitcode.Config, fmt.Errorf("spotify authorization lacks %s; run sptfy init --overwrite", requiredScope))
	}
	return authenticated, nil
}
