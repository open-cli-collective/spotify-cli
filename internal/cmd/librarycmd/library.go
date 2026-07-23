// Package librarycmd implements saved-track library operations.
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

const pageScope = "library-tracks"

// Session is the authenticated capability required by library commands.
type Session interface {
	Close() error
	Scopes() []string
	ListSavedTracks(context.Context, int, int) (client.SavedTrackPage, error)
	CheckSavedTracks(context.Context, []string) ([]bool, error)
	SaveSavedTracks(context.Context, []string) error
	RemoveSavedTracks(context.Context, []string) error
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

type trackReference struct {
	reference string
	id        string
	uri       string
}

// New constructs the saved library command tree.
func New(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "library", Short: "Manage the Spotify library", Args: noArgs("library")}
	tracks := &cobra.Command{Use: "tracks", Short: "Manage saved tracks", Args: noArgs("tracks")}
	tracks.AddCommand(newList(deps), newCheck(deps), newMutation(deps, "add"), newMutation(deps, "remove"))
	command.AddCommand(tracks)
	return command
}

func newList(deps Dependencies) *cobra.Command {
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
			offset, err := pagetoken.Decode(pageScope, opts.nextPageToken, math.MaxInt-50)
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
				if _, err := fmt.Fprintf(command.ErrOrStderr(), "More results available (next: %s)\n", pagetoken.Encode(pageScope, page.Offset+page.Limit)); err != nil {
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

func newCheck(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use: "check <track-reference>...", Short: "Check whether tracks are saved",
		Args: minimumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			references, err := parseReferences(args)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			authenticated, err := openSession(command, deps, auth.ScopeUserLibraryRead)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			saved, err := authenticated.CheckSavedTracks(command.Context(), trackURIs(references))
			if err != nil {
				return classify(err)
			}
			if len(saved) != len(references) {
				return exitcode.New(exitcode.Upstream, client.ErrInvalidResponse)
			}
			checks := make([]output.SavedTrackCheck, len(references))
			for index, reference := range references {
				checks[index] = output.SavedTrackCheck{Reference: reference.reference, ID: reference.id, Saved: saved[index]}
			}
			if _, err := io.WriteString(command.OutOrStdout(), output.RenderSavedTrackChecks(checks)); err != nil {
				return exitcode.New(exitcode.Generic, errors.New("writing saved-track checks failed"))
			}
			return nil
		},
	}
}

func newMutation(deps Dependencies, verb string) *cobra.Command {
	return &cobra.Command{
		Use: verb + " <track-reference>...", Short: verb + " tracks in the library",
		Args: minimumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			references, err := parseReferences(args)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			authenticated, err := openSession(command, deps, auth.ScopeUserLibraryModify)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			uris := trackURIs(references)
			if verb == "add" {
				err = authenticated.SaveSavedTracks(command.Context(), uris)
			} else {
				err = authenticated.RemoveSavedTracks(command.Context(), uris)
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

func parseReferences(args []string) ([]trackReference, error) {
	result := make([]trackReference, 0, len(args))
	seen := make(map[string]struct{}, len(args))
	for _, reference := range args {
		id, err := spotifyref.Parse(reference, spotifyref.Track)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, trackReference{reference: reference, id: id, uri: "spotify:track:" + id})
	}
	return result, nil
}

func trackURIs(references []trackReference) []string {
	uris := make([]string, len(references))
	for index, reference := range references {
		uris[index] = reference.uri
	}
	return uris
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
