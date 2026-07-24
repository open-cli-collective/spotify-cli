// Package playlistcmd implements current-user playlist reads.
package playlistcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

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

type authenticatedSession interface {
	Close() error
	Scopes() []string
}

// ReadSession is the authenticated capability required by playlist reads.
type ReadSession interface {
	authenticatedSession
	ListCurrentUserPlaylists(context.Context, int, int) (client.PlaylistPage, error)
	GetPlaylist(context.Context, string) (client.Playlist, error)
	ListPlaylistItems(context.Context, string, int, int) (client.PlaylistItemPage, error)
}

// AddSession is the authenticated capability required by playlist additions.
type AddSession interface {
	authenticatedSession
	GetPlaylist(context.Context, string) (client.Playlist, error)
	AddPlaylistItems(context.Context, string, []string, *int) (string, error)
}

// RemoveSession is the authenticated capability required by playlist removals.
type RemoveSession interface {
	authenticatedSession
	GetPlaylist(context.Context, string) (client.Playlist, error)
	ListPlaylistItems(context.Context, string, int, int) (client.PlaylistItemPage, error)
	RemovePlaylistItemsByURI(context.Context, string, string, string) (string, error)
}

// ReadSessionOpener opens an authenticated session for playlist reads.
type ReadSessionOpener func(context.Context, string, bool) (ReadSession, error)

// AddSessionOpener opens an authenticated session for adding playlist items.
type AddSessionOpener func(context.Context, string, bool) (AddSession, error)

// RemoveSessionOpener opens an authenticated session for removing playlist items.
type RemoveSessionOpener func(context.Context, string, bool) (RemoveSession, error)

// Dependencies contains the authenticated effect used by playlist commands.
type Dependencies struct {
	OpenReadSession   ReadSessionOpener
	OpenAddSession    AddSessionOpener
	OpenRemoveSession RemoveSessionOpener
	Backend           *string
}

type addResult struct {
	StartPosition int
	Count         int
	SnapshotID    string
}

// PartialMutationError reports an accepted prefix of a failed mutation.
type PartialMutationError struct {
	Cause          error
	StartPosition  int
	CompletedCount int
	SnapshotID     string
}

func (err *PartialMutationError) Error() string {
	return fmt.Sprintf("playlist add stopped after %d items: %v", err.CompletedCount, err.Cause)
}

// Unwrap returns the provider failure that stopped the mutation.
func (err *PartialMutationError) Unwrap() error { return err.Cause }

// PostMutationOutputError reports a failed write after a mutation took effect.
type PostMutationOutputError struct {
	Cause          error
	AppliedOutcome string
	MutationError  error
}

func (err *PostMutationOutputError) Error() string {
	return "playlist mutation applied but output failed; recovery record: " + err.AppliedOutcome
}

// Unwrap returns the output failure and any mutation failure.
func (err *PostMutationOutputError) Unwrap() []error {
	if err.MutationError == nil {
		return []error{err.Cause}
	}
	return []error{err.Cause, err.MutationError}
}

// MutationReconciliationError reports a mutation whose applied outcome is unknown.
type MutationReconciliationError struct {
	Cause                 error
	Operation             string
	PlaylistID            string
	StartPosition         int
	CurrentPosition       int
	ConfirmedCount        int
	UncertainCount        int
	LastConfirmedSnapshot string
	Position              int
	TrackID               string
	PriorSnapshot         string
}

func (err *MutationReconciliationError) Error() string {
	if err.Operation == "remove" {
		return fmt.Sprintf(
			"playlist remove outcome uncertain; inspect and reconcile before retrying: playlist=%s position=%d track=%s prior_snapshot=%s",
			safeErrorMetadata(err.PlaylistID), err.Position, safeErrorMetadata(err.TrackID), safeErrorMetadata(err.PriorSnapshot),
		)
	}
	return fmt.Sprintf(
		"playlist add outcome uncertain; inspect and reconcile before retrying: playlist=%s start=%d current=%d confirmed=%d uncertain=%d last_snapshot=%s",
		safeErrorMetadata(err.PlaylistID), err.StartPosition, err.CurrentPosition, err.ConfirmedCount, err.UncertainCount,
		safeErrorMetadata(err.LastConfirmedSnapshot),
	)
}

// Unwrap returns the outcome-uncertain client failure.
func (err *MutationReconciliationError) Unwrap() error { return err.Cause }

type removeResult struct {
	Position   int
	TrackID    string
	SnapshotID string
}

var (
	errAddPositionExceeds = errors.New("--position exceeds the playlist item count")
	errRemoveOutside      = errors.New("position is outside the playlist")
	errRemoveNonTrack     = errors.New("position does not contain a removable Spotify track")
	errRemoveDuplicate    = errors.New("track occurs more than once; exact-position removal is unavailable")
	errRemoveChanged      = errors.New("playlist changed before removal; retry with a fresh position")
)

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
		Use: "playlists", Aliases: []string{"playlist"}, Short: "Manage Spotify playlists",
		Args: noArgs("playlists"), RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newList(deps), newGet(deps), newItems(deps))
	return command
}

func newItems(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "items", Short: "Manage ordered Spotify playlist items", Args: noArgs("items"),
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newItemsList(deps), newItemsAdd(deps), newItemsRemove(deps))
	return command
}

func newItemsAdd(deps Dependencies) *cobra.Command {
	position := -1
	command := &cobra.Command{
		Use: "add <playlist-reference> <track-reference>...", Short: "Add tracks to a Spotify playlist",
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(2)(command, args); err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			playlistID, err := spotifyref.Parse(args[0], spotifyref.Playlist)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			uris := make([]string, len(args)-1)
			for index, reference := range args[1:] {
				id, parseErr := spotifyref.Parse(reference, spotifyref.Track)
				if parseErr != nil {
					return exitcode.New(exitcode.Usage, parseErr)
				}
				uris[index] = "spotify:track:" + id
			}
			if position < 0 && command.Flags().Changed("position") {
				return exitcode.New(exitcode.Usage, errors.New("--position must be nonnegative"))
			}
			authenticated, err := openSession(command, deps.Backend, deps.OpenAddSession, auth.ScopePlaylistModifyPrivate, auth.ScopePlaylistModifyPublic)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			var requestedPosition *int
			if command.Flags().Changed("position") {
				requestedPosition = &position
			}
			result, err := addPlaylistItems(command.Context(), authenticated, playlistID, uris, requestedPosition)
			if result.Count > 0 {
				return writeMutationOutput(command, output.RenderPlaylistItemsAdded(playlistID, result.StartPosition, result.Count, result.SnapshotID), err)
			}
			if err != nil {
				return classifyMutation(err)
			}
			return nil
		},
	}
	command.Flags().IntVar(&position, "position", -1, "Zero-based insertion position (default append)")
	return command
}

func newItemsRemove(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use: "remove <playlist-reference> <zero-based-position>", Short: "Remove one track from a Spotify playlist",
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(command, args); err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			playlistID, err := spotifyref.Parse(args[0], spotifyref.Playlist)
			if err != nil {
				return exitcode.New(exitcode.Usage, err)
			}
			position, err := strconv.Atoi(args[1])
			if err != nil || position < 0 {
				return exitcode.New(exitcode.Usage, errors.New("position must be a nonnegative integer"))
			}
			authenticated, err := openSession(command, deps.Backend, deps.OpenRemoveSession, auth.ScopePlaylistModifyPrivate, auth.ScopePlaylistModifyPublic)
			if err != nil {
				return err
			}
			defer func() { _ = authenticated.Close() }()
			result, err := removePlaylistItem(command.Context(), authenticated, playlistID, position)
			if err != nil {
				return classifyMutation(err)
			}
			return writeMutationOutput(command, output.RenderPlaylistItemRemoved(playlistID, result.Position, result.TrackID, result.SnapshotID), nil)
		},
	}
	return command
}

func addPlaylistItems(ctx context.Context, spotify AddSession, playlistID string, uris []string, position *int) (addResult, error) {
	playlist, err := spotify.GetPlaylist(ctx, playlistID)
	if err != nil {
		return addResult{}, err
	}
	if playlist.ItemCount == nil {
		return addResult{}, client.ErrInvalidResponse
	}
	startPosition := playlist.ItemCount.Total
	if position != nil {
		if *position > playlist.ItemCount.Total {
			return addResult{}, errAddPositionExceeds
		}
		startPosition = *position
	}
	finalSnapshot := ""
	completed := 0
	for start := 0; start < len(uris); start += 100 {
		end := min(start+100, len(uris))
		var chunkPosition *int
		if position != nil {
			value := *position + start
			chunkPosition = &value
		}
		snapshot, addErr := spotify.AddPlaylistItems(ctx, playlistID, uris[start:end], chunkPosition)
		if addErr != nil {
			var uncertain *client.MutationOutcomeUncertainError
			if errors.As(addErr, &uncertain) {
				return addResult{}, &MutationReconciliationError{
					Cause: addErr, Operation: "add", PlaylistID: playlistID,
					StartPosition: startPosition, CurrentPosition: startPosition + completed,
					ConfirmedCount: completed, UncertainCount: end - start, LastConfirmedSnapshot: finalSnapshot,
				}
			}
			if completed == 0 {
				return addResult{}, addErr
			}
			result := addResult{StartPosition: startPosition, Count: completed, SnapshotID: finalSnapshot}
			return result, &PartialMutationError{
				Cause: addErr, StartPosition: startPosition, CompletedCount: completed, SnapshotID: finalSnapshot,
			}
		}
		finalSnapshot = snapshot
		completed += end - start
	}
	return addResult{StartPosition: startPosition, Count: len(uris), SnapshotID: finalSnapshot}, nil
}

func removePlaylistItem(ctx context.Context, spotify RemoveSession, playlistID string, position int) (removeResult, error) {
	before, err := spotify.GetPlaylist(ctx, playlistID)
	if err != nil {
		return removeResult{}, err
	}
	if before.ItemCount == nil || strings.TrimSpace(before.SnapshotID) == "" {
		return removeResult{}, client.ErrInvalidResponse
	}
	total := before.ItemCount.Total
	if position >= total {
		return removeResult{}, errRemoveOutside
	}
	items := make([]client.PlaylistItem, 0, total)
	for offset := 0; offset < total; {
		page, err := spotify.ListPlaylistItems(ctx, playlistID, 50, offset)
		if err != nil {
			return removeResult{}, err
		}
		if page.Total != total || page.Offset != offset || len(page.Items) == 0 {
			return removeResult{}, client.ErrInvalidResponse
		}
		items = append(items, page.Items...)
		offset += len(page.Items)
	}
	if len(items) != total {
		return removeResult{}, client.ErrInvalidResponse
	}
	target := items[position]
	uri := "spotify:track:" + target.ID
	if target.Type != "track" || !spotifyref.ValidID(target.ID) || target.URI != uri {
		return removeResult{}, errRemoveNonTrack
	}
	occurrences := 0
	for _, item := range items {
		if item.Type == "track" && item.ID == target.ID {
			occurrences++
		}
	}
	if occurrences != 1 {
		return removeResult{}, errRemoveDuplicate
	}
	current, err := spotify.GetPlaylist(ctx, playlistID)
	if err != nil {
		return removeResult{}, err
	}
	if current.ItemCount == nil || current.ItemCount.Total != total || current.SnapshotID != before.SnapshotID {
		return removeResult{}, errRemoveChanged
	}
	finalSnapshot, err := spotify.RemovePlaylistItemsByURI(ctx, playlistID, uri, before.SnapshotID)
	if err != nil {
		var uncertain *client.MutationOutcomeUncertainError
		if errors.As(err, &uncertain) {
			return removeResult{}, &MutationReconciliationError{
				Cause: err, Operation: "remove", PlaylistID: playlistID, Position: position,
				TrackID: target.ID, PriorSnapshot: before.SnapshotID,
			}
		}
		return removeResult{}, err
	}
	return removeResult{Position: position, TrackID: target.ID, SnapshotID: finalSnapshot}, nil
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
			authenticated, err := openSession(command, deps.Backend, deps.OpenReadSession)
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
			authenticated, err := openSession(command, deps.Backend, deps.OpenReadSession)
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
			authenticated, err := openSession(command, deps.Backend, deps.OpenReadSession)
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

func openSession[T authenticatedSession](command *cobra.Command, backendValue *string, opener func(context.Context, string, bool) (T, error), additionalScopes ...string) (T, error) {
	var zero T
	backendFlag := command.Flags().Lookup(credstore.BackendFlagName)
	backendSet := backendFlag != nil && backendFlag.Changed
	backend := ""
	if backendValue != nil {
		backend = *backendValue
	}
	if err := credentials.ValidateExplicitBackend(backend, backendSet); err != nil {
		return zero, exitcode.New(exitcode.Usage, err)
	}
	if opener == nil {
		return zero, exitcode.New(exitcode.Generic, errors.New("authenticated session is unavailable"))
	}
	authenticated, err := opener(command.Context(), backend, backendSet)
	if err != nil {
		return zero, exitcode.New(exitcode.Config, err)
	}
	requiredScopes := append([]string{auth.ScopePlaylistReadCollaborative, auth.ScopePlaylistReadPrivate}, additionalScopes...)
	for _, scope := range requiredScopes {
		if !slices.Contains(authenticated.Scopes(), scope) {
			_ = authenticated.Close()
			return zero, exitcode.New(exitcode.Config, fmt.Errorf("spotify authorization lacks %s; run sptfy init --overwrite", scope))
		}
	}
	return authenticated, nil
}

func classifyMutation(err error) error {
	switch {
	case errors.Is(err, errAddPositionExceeds), errors.Is(err, errRemoveOutside),
		errors.Is(err, errRemoveNonTrack), errors.Is(err, errRemoveDuplicate):
		return exitcode.New(exitcode.Usage, err)
	default:
		return classify(err)
	}
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

func writeMutationOutput(command *cobra.Command, rendered string, mutationErr error) error {
	if _, err := io.WriteString(command.OutOrStdout(), rendered); err != nil {
		outputErr := &PostMutationOutputError{
			Cause: err, AppliedOutcome: strings.TrimSuffix(rendered, "\n"), MutationError: mutationErr,
		}
		if mutationErr != nil {
			return exitcode.New(exitcode.Code(classifyMutation(mutationErr)), outputErr)
		}
		return exitcode.New(exitcode.Generic, outputErr)
	}
	if mutationErr != nil {
		return classifyMutation(mutationErr)
	}
	return nil
}

func safeErrorMetadata(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	if value = strings.TrimSpace(value); value == "" {
		return "-"
	}
	return value
}

func noArgs(use string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return exitcode.New(exitcode.Usage, errors.New(use+" takes no arguments"))
		}
		return nil
	}
}
