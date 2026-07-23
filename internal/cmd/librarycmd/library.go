// Package librarycmd implements saved-library operations.
package librarycmd

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

const (
	trackPageScope = "library-tracks"
	albumPageScope = "library-albums"
)

// Session is the authenticated capability required by library commands.
type Session interface {
	Close() error
	Scopes() []string
	ListSavedTracks(context.Context, int, int) (client.SavedTrackPage, error)
	CheckSavedTracks(context.Context, []string) ([]bool, error)
	SaveSavedTracks(context.Context, []string) error
	RemoveSavedTracks(context.Context, []string) error
	ListSavedAlbums(context.Context, int, int) (client.SavedAlbumPage, error)
	CheckSavedAlbums(context.Context, []string) ([]bool, error)
	SaveSavedAlbums(context.Context, []string) error
	RemoveSavedAlbums(context.Context, []string) error
}

// SessionOpener opens the authenticated capability required by library commands.
type SessionOpener func(context.Context, string, bool) (Session, error)

// Dependencies contains the authenticated effect used by library commands.
type Dependencies struct {
	OpenSession SessionOpener
	Backend     *string
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
	uri       string
}

// New constructs the saved library command tree.
func New(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "library", Short: "Manage the Spotify library", Args: noArgs("library")}
	tracks := &cobra.Command{Use: "tracks", Short: "Manage saved tracks", Args: noArgs("tracks")}
	tracks.AddCommand(newTrackList(deps), newCheck(deps, spotifyref.Track), newMutation(deps, spotifyref.Track, "add"), newMutation(deps, spotifyref.Track, "remove"))
	albums := &cobra.Command{Use: "albums", Short: "Manage saved albums", Args: noArgs("albums")}
	albums.AddCommand(newAlbumList(deps), newCheck(deps, spotifyref.Album), newMutation(deps, spotifyref.Album, "add"), newMutation(deps, spotifyref.Album, "remove"))
	command.AddCommand(tracks, albums)
	return command
}

func newTrackList(deps Dependencies) *cobra.Command {
	opts := listOptions{max: 10}
	command := &cobra.Command{
		Use: "list", Short: "List saved tracks", Args: noArgs("list"),
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.max < 1 || opts.max > 50 {
				return exitcode.New(exitcode.Usage, errors.New("--max must be between 1 and 50"))
			}
			var fields []output.TrackField
			var err error
			if !opts.id {
				fields, err = output.SelectSavedTrackFields(opts.fields, opts.extended, opts.artwork)
				if err != nil {
					return exitcode.New(exitcode.Usage, err)
				}
			}
			offset, err := pagetoken.Decode(trackPageScope, opts.nextPageToken, math.MaxInt-50)
			if err != nil {
				return exitcode.New(exitcode.Usage, errors.New("invalid --next-page-token"))
			}
			authenticated, err := openSession(command, deps, auth.ScopeUserLibraryRead)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			page, err := authenticated.ListSavedTracks(command.Context(), opts.max, offset)
			if err != nil {
				return classify(err)
			}
			rendered := output.RenderSavedTracks(page.Items, fields)
			if opts.id {
				rendered = output.RenderSavedTrackIDs(page.Items)
			}
			if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
				return exitcode.New(exitcode.Generic, errors.New("writing saved tracks failed"))
			}
			if page.HasNext {
				if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", pagetoken.Encode(trackPageScope, page.Offset+page.Limit)); err != nil {
					return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
				}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.IntVarP(&opts.max, "max", "m", 10, "Maximum results (1-50)")
	flags.StringVar(&opts.nextPageToken, "next-page-token", "", "Opaque continuation token")
	flags.BoolVar(&opts.id, "id", false, "Emit only track IDs")
	flags.StringVar(&opts.fields, "fields", "", "Comma-separated output fields")
	flags.BoolVar(&opts.extended, "extended", false, "Add less-frequent track fields")
	flags.BoolVar(&opts.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
}

func newAlbumList(deps Dependencies) *cobra.Command {
	opts := listOptions{max: 10}
	command := &cobra.Command{
		Use: "list", Short: "List saved albums", Args: noArgs("list"),
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.max < 1 || opts.max > 50 {
				return exitcode.New(exitcode.Usage, errors.New("--max must be between 1 and 50"))
			}
			var fields []output.AlbumField
			var err error
			if !opts.id {
				fields, err = output.SelectSavedAlbumFields(opts.fields, opts.extended, opts.artwork)
				if err != nil {
					return exitcode.New(exitcode.Usage, err)
				}
			}
			offset, err := pagetoken.Decode(albumPageScope, opts.nextPageToken, math.MaxInt-50)
			if err != nil {
				return exitcode.New(exitcode.Usage, errors.New("invalid --next-page-token"))
			}
			authenticated, err := openSession(command, deps, auth.ScopeUserLibraryRead)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			page, err := authenticated.ListSavedAlbums(command.Context(), opts.max, offset)
			if err != nil {
				return classify(err)
			}
			rendered := output.RenderSavedAlbums(page.Items, fields)
			if opts.id {
				rendered = output.RenderSavedAlbumIDs(page.Items)
			}
			if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
				return exitcode.New(exitcode.Generic, errors.New("writing saved albums failed"))
			}
			if page.HasNext {
				if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", pagetoken.Encode(albumPageScope, page.Offset+page.Limit)); err != nil {
					return exitcode.New(exitcode.Generic, errors.New("writing pagination notice failed"))
				}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.IntVarP(&opts.max, "max", "m", 10, "Maximum results (1-50)")
	flags.StringVar(&opts.nextPageToken, "next-page-token", "", "Opaque continuation token")
	flags.BoolVar(&opts.id, "id", false, "Emit only album IDs")
	flags.StringVar(&opts.fields, "fields", "", "Comma-separated output fields")
	flags.BoolVar(&opts.extended, "extended", false, "Add less-frequent album fields")
	flags.BoolVar(&opts.artwork, "include-artwork", false, "Add Spotify artwork dimensions and URLs")
	return command
}

func newCheck(deps Dependencies, kind spotifyref.Kind) *cobra.Command {
	plural := resourcePlural(kind)
	return &cobra.Command{
		Use: "check <" + string(kind) + "-reference>...", Short: "Check whether " + plural + " are saved",
		Args: minimumArgs(1),
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
			saved, err := checkSaved(command.Context(), authenticated, kind, libraryURIs(references))
			if err != nil {
				return classify(err)
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
		Args: minimumArgs(1),
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
			uris := libraryURIs(references)
			if verb == "add" {
				err = saveItems(command.Context(), authenticated, kind, uris)
			} else {
				err = removeItems(command.Context(), authenticated, kind, uris)
			}
			if err != nil {
				return classify(err)
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
		result = append(result, libraryReference{reference: reference, id: id, uri: "spotify:" + string(kind) + ":" + id})
	}
	return result, nil
}

func libraryURIs(references []libraryReference) []string {
	uris := make([]string, len(references))
	for index, reference := range references {
		uris[index] = reference.uri
	}
	return uris
}

func resourcePlural(kind spotifyref.Kind) string {
	if kind == spotifyref.Album {
		return "albums"
	}
	return "tracks"
}

func checkSaved(ctx context.Context, session Session, kind spotifyref.Kind, uris []string) ([]bool, error) {
	if kind == spotifyref.Album {
		return session.CheckSavedAlbums(ctx, uris)
	}
	return session.CheckSavedTracks(ctx, uris)
}

func saveItems(ctx context.Context, session Session, kind spotifyref.Kind, uris []string) error {
	if kind == spotifyref.Album {
		return session.SaveSavedAlbums(ctx, uris)
	}
	return session.SaveSavedTracks(ctx, uris)
}

func removeItems(ctx context.Context, session Session, kind spotifyref.Kind, uris []string) error {
	if kind == spotifyref.Album {
		return session.RemoveSavedAlbums(ctx, uris)
	}
	return session.RemoveSavedTracks(ctx, uris)
}

func openSession(command *cobra.Command, deps Dependencies, requiredScope string) (Session, error) {
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
	if !slices.Contains(authenticated.Scopes(), requiredScope) {
		_ = authenticated.Close()
		return nil, exitcode.New(exitcode.Config, fmt.Errorf("spotify authorization lacks %s; run sptfy init --overwrite", requiredScope))
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

func noArgs(use string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return exitcode.New(exitcode.Usage, errors.New(use+" takes no arguments"))
		}
		return nil
	}
}

func minimumArgs(count int) func(*cobra.Command, []string) error {
	return func(command *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(count)(command, args); err != nil {
			return exitcode.New(exitcode.Usage, err)
		}
		return nil
	}
}
