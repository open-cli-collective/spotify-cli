package playlistcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/pagetoken"
)

const playlistID = "0123456789ABCDEFGHIJKL"

type fakeSession struct {
	scopes  []string
	calls   []string
	hasNext bool
	empty   bool
	closed  bool
	readErr error
}

func (session *fakeSession) Close() error {
	session.closed = true
	return nil
}
func (session *fakeSession) Scopes() []string { return session.scopes }
func (session *fakeSession) ListCurrentUserPlaylists(_ context.Context, limit, offset int) (client.PlaylistPage, error) {
	session.calls = append(session.calls, fmt.Sprintf("list:%d:%d", limit, offset))
	if session.readErr != nil {
		return client.PlaylistPage{}, session.readErr
	}
	if session.empty {
		return client.PlaylistPage{Limit: limit, Offset: offset}, nil
	}
	return client.PlaylistPage{
		Items: []client.Playlist{{
			ID: playlistID, Name: "Mix", Owner: client.PlaylistOwner{ID: "owner-1", DisplayName: "Ada"},
			ItemCount: &client.PlaylistItemCount{Total: 3}, Collaborative: true,
			URI: "spotify:playlist:" + playlistID, ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/playlist/" + playlistID},
			SnapshotID: "snapshot", Description: "Description", Images: []client.Image{{URL: "https://image"}},
		}},
		Limit: limit, Offset: offset, HasNext: session.hasNext,
	}, nil
}
func (session *fakeSession) GetPlaylist(_ context.Context, id string) (client.Playlist, error) {
	session.calls = append(session.calls, "get:"+id)
	if session.readErr != nil {
		return client.Playlist{}, session.readErr
	}
	return client.Playlist{
		ID: id, Name: "Mix", Owner: client.PlaylistOwner{ID: "owner-1", DisplayName: "Ada"},
		ItemCount: &client.PlaylistItemCount{Total: 3}, Collaborative: true,
	}, nil
}
func (session *fakeSession) ListPlaylistItems(_ context.Context, id string, limit, offset int) (client.PlaylistItemPage, error) {
	session.calls = append(session.calls, fmt.Sprintf("items:%s:%d:%d", id, limit, offset))
	if session.readErr != nil {
		return client.PlaylistItemPage{}, session.readErr
	}
	if session.empty {
		return client.PlaylistItemPage{Limit: limit, Offset: offset}, nil
	}
	duration := 61000
	explicit := true
	return client.PlaylistItemPage{
		Items: []client.PlaylistItem{
			{Type: "track", ID: "abcdefghijklmnopqrstuv", Name: "Song", Artists: []client.Artist{{ID: "artist-1", Name: "Ada"}}, AlbumID: "album-1", AlbumName: "Album", DurationMS: &duration, URI: "spotify:track:abcdefghijklmnopqrstuv", URL: "https://track", AddedAt: "2026-07-24T12:00:00Z", AddedByID: "owner-1", DiscNumber: 1, TrackNumber: 2, Explicit: &explicit, Restriction: "market", Images: []client.Image{{URL: "https://image"}}},
			{Type: "unavailable"},
		},
		Limit: limit, Offset: offset, HasNext: session.hasNext,
	}, nil
}
func TestPlaylistListShapeFlags(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "selected fields", args: []string{"--fields", "playlist,description"}, want: "PLAYLIST | DESCRIPTION\nMix | Description\n"},
		{name: "extended", args: []string{"--extended"}, want: "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE | URI | URL | SNAPSHOT_ID | DESCRIPTION\n" + playlistID + " | Mix | owner-1 | Ada | 3 | - | true | spotify:playlist:" + playlistID + " | https://open.spotify.com/playlist/" + playlistID + " | snapshot | Description\n"},
		{name: "artwork", args: []string{"--include-artwork"}, want: "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE | ARTWORK\n" + playlistID + " | Mix | owner-1 | Ada | 3 | - | true | -x- https://image\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, opens, err := execute(&fakeSession{scopes: playlistScopes()}, append([]string{"playlists", "list"}, test.args...)...)
			if err != nil || stdout != test.want || stderr != "" || opens != 1 {
				t.Fatalf("stdout=%q stderr=%q opens=%d error=%v", stdout, stderr, opens, err)
			}
		})
	}
}

func playlistScopes() []string {
	return []string{auth.ScopePlaylistReadCollaborative, auth.ScopePlaylistReadPrivate}
}

func TestPlaylistListOutputFlagsAndPagination(t *testing.T) {
	session := &fakeSession{scopes: playlistScopes(), hasNext: true}
	stdout, stderr, opens, err := execute(session, "playlists", "list", "--next-page-token", pagetoken.Encode(playlistPageScope, 10))
	want := "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE\n" +
		playlistID + " | Mix | owner-1 | Ada | 3 | - | true\n"
	wantErr := "More results available (next: " + pagetoken.Encode(playlistPageScope, 20) + ")\n"
	if err != nil || stdout != want || stderr != wantErr || opens != 1 || fmt.Sprint(session.calls) != "[list:10:10]" {
		t.Fatalf("stdout=%q stderr=%q opens=%d calls=%v error=%v", stdout, stderr, opens, session.calls, err)
	}

	stdout, stderr, _, err = execute(&fakeSession{scopes: playlistScopes()}, "playlists", "list", "--id", "--fields", "invalid", "--extended", "--include-artwork")
	if err != nil || stdout != playlistID+"\n" || stderr != "" {
		t.Fatalf("id stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}

	stdout, stderr, _, err = execute(&fakeSession{scopes: playlistScopes(), empty: true}, "playlists", "list")
	if err != nil || stdout != "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE\n" || stderr != "" {
		t.Fatalf("empty stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
}

func TestPlaylistGetAcceptedReferencesAndDetailOutput(t *testing.T) {
	for _, reference := range []string{
		playlistID,
		"spotify:playlist:" + playlistID,
		"https://open.spotify.com/playlist/" + playlistID,
	} {
		session := &fakeSession{scopes: playlistScopes()}
		stdout, stderr, opens, err := execute(session, "playlists", "get", reference)
		want := playlistID + "  Mix\nOwner ID: owner-1   Owner: Ada\nItem Count: 3   Public: -\nCollaborative: true\n"
		if err != nil || stdout != want || stderr != "" || opens != 1 || fmt.Sprint(session.calls) != "[get:"+playlistID+"]" {
			t.Fatalf("reference=%q stdout=%q stderr=%q opens=%d calls=%v error=%v", reference, stdout, stderr, opens, session.calls, err)
		}
	}

	stdout, _, _, err := execute(&fakeSession{scopes: playlistScopes()}, "playlists", "get", playlistID, "--fields", "playlist,owner_id")
	if err != nil || stdout != playlistID+"  Mix\nOwner ID: owner-1\n" {
		t.Fatalf("fields stdout=%q error=%v", stdout, err)
	}
	stdout, _, _, err = execute(&fakeSession{scopes: playlistScopes()}, "playlists", "get", playlistID, "--id", "--fields", "invalid")
	if err != nil || stdout != playlistID+"\n" {
		t.Fatalf("id stdout=%q error=%v", stdout, err)
	}
}

func TestPlaylistCommandsValidateBeforeSession(t *testing.T) {
	for _, args := range [][]string{
		{"playlists", "list", "extra"},
		{"playlists", "list", "--max", "0"},
		{"playlists", "list", "--max", "51"},
		{"playlists", "list", "--fields", "invalid"},
		{"playlists", "list", "--next-page-token", "invalid"},
		{"playlists", "list", "--next-page-token", pagetoken.Encode("library-tracks", 10)},
		{"playlists", "get"},
		{"playlists", "get", "bad"},
		{"playlists", "get", "spotify:album:" + playlistID},
		{"playlists", "get", playlistID, "--fields", "invalid"},
	} {
		_, _, opens, err := execute(&fakeSession{}, args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v opens=%d code=%d error=%v", args, opens, exitcode.Code(err), err)
		}
	}
	for _, max := range []string{"1", "50"} {
		session := &fakeSession{scopes: playlistScopes()}
		_, _, opens, err := execute(session, "playlists", "list", "--max", max)
		if err != nil || opens != 1 || fmt.Sprint(session.calls) != "[list:"+max+":0]" {
			t.Fatalf("max=%s opens=%d calls=%v error=%v", max, opens, session.calls, err)
		}
	}
}

func TestPlaylistArgumentMessages(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"playlists", "extra"}, want: "playlists takes no arguments"},
		{args: []string{"playlists", "items", "extra"}, want: "items takes no arguments"},
		{args: []string{"playlists", "list", "extra"}, want: "list takes no arguments"},
		{args: []string{"playlists", "get"}, want: "accepts 1 arg(s), received 0"},
		{args: []string{"playlists", "items", "list"}, want: "accepts 1 arg(s), received 0"},
		{args: []string{"playlists", "items", "add", playlistID}, want: "requires at least 2 arg(s), only received 1"},
		{args: []string{"playlists", "items", "remove", playlistID}, want: "accepts 2 arg(s), received 1"},
		{args: []string{"playlists", "items", "update", playlistID}, want: "accepts 2 arg(s), received 1"},
	} {
		_, _, opens, err := execute(&fakeSession{}, test.args...)
		if err == nil || err.Error() != test.want || opens != 0 {
			t.Fatalf("args=%v error=%v opens=%d", test.args, err, opens)
		}
	}
}

func TestPlaylistScopeGuardRequiresBothScopesWithOverwriteHint(t *testing.T) {
	for _, scopes := range [][]string{nil, {auth.ScopePlaylistReadCollaborative}, {auth.ScopePlaylistReadPrivate}} {
		for _, args := range [][]string{{"playlists", "list"}, {"playlists", "get", playlistID}, {"playlists", "items", "list", playlistID}} {
			session := &fakeSession{scopes: scopes}
			stdout, stderr, opens, err := execute(session, args...)
			if exitcode.Code(err) != exitcode.Config || stdout != "" || stderr != "" || opens != 1 || len(session.calls) != 0 || !session.closed ||
				!bytes.Contains([]byte(err.Error()), []byte("sptfy init --overwrite")) {
				t.Fatalf("scopes=%v args=%v stdout=%q stderr=%q opens=%d calls=%v closed=%t error=%v", scopes, args, stdout, stderr, opens, session.calls, session.closed, err)
			}
		}
	}
}

func TestPlaylistReadErrorsAreClassified(t *testing.T) {
	for _, test := range []struct {
		err  error
		code int
	}{
		{err: client.ErrForbidden, code: exitcode.Config},
		{err: client.ErrInvalidResponse, code: exitcode.Upstream},
	} {
		_, _, _, err := execute(&fakeSession{scopes: playlistScopes(), readErr: test.err}, "playlists", "list")
		if exitcode.Code(err) != test.code || !errors.Is(err, test.err) {
			t.Fatalf("source=%v code=%d error=%v", test.err, exitcode.Code(err), err)
		}
	}
}

func TestPlaylistItemsListAcceptedReferencesAndPagination(t *testing.T) {
	for _, reference := range []string{
		playlistID,
		"spotify:playlist:" + playlistID,
		"https://open.spotify.com/playlist/" + playlistID,
	} {
		session := &fakeSession{scopes: playlistScopes(), hasNext: true}
		stdout, stderr, opens, err := execute(session, "playlists", "items", "list", reference, "--max", "2", "--next-page-token", pagetoken.Encode(playlistItemPageScope(playlistID), 10))
		want := "Playlist ID: " + playlistID + "\n" +
			"POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" +
			"10 | track | abcdefghijklmnopqrstuv | Song | artist-1 | Ada | album-1 | Album | 1:01\n" +
			"11 | unavailable | - | - | - | - | - | - | -\n"
		wantErr := "More results available (next: " + pagetoken.Encode(playlistItemPageScope(playlistID), 12) + ")\n"
		if err != nil || stdout != want || stderr != wantErr || opens != 1 || fmt.Sprint(session.calls) != "[items:"+playlistID+":2:10]" {
			t.Fatalf("reference=%q stdout=%q stderr=%q opens=%d calls=%v error=%v", reference, stdout, stderr, opens, session.calls, err)
		}
	}
}

func TestPlaylistItemsListShapeFlagsAndEmptyOutput(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "selected", args: []string{"--fields", "item,type,item"}, want: "Playlist ID: " + playlistID + "\nITEM | TYPE\nSong | track\n- | unavailable\n"},
		{name: "artwork", args: []string{"--include-artwork", "--fields", "position,artwork"}, want: "Playlist ID: " + playlistID + "\nPOSITION | ARTWORK\n0 | -x- https://image\n1 | -\n"},
		{name: "extended", args: []string{"--extended"}, want: "Playlist ID: " + playlistID + "\nPOSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION | URI | URL | ADDED_AT | ADDED_BY_ID | DISC_NUMBER | TRACK_NUMBER | EXPLICIT | RESTRICTION\n0 | track | abcdefghijklmnopqrstuv | Song | artist-1 | Ada | album-1 | Album | 1:01 | spotify:track:abcdefghijklmnopqrstuv | https://track | 2026-07-24T12:00:00Z | owner-1 | 1 | 2 | true | market\n1 | unavailable | - | - | - | - | - | - | - | - | - | - | - | - | - | - | -\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, opens, err := execute(&fakeSession{scopes: playlistScopes()}, append([]string{"playlists", "items", "list", playlistID}, test.args...)...)
			if err != nil || stdout != test.want || stderr != "" || opens != 1 {
				t.Fatalf("stdout=%q stderr=%q opens=%d error=%v", stdout, stderr, opens, err)
			}
		})
	}

	stdout, stderr, _, err := execute(&fakeSession{scopes: playlistScopes()}, "playlists", "items", "list", playlistID, "--id", "--fields", "invalid", "--extended", "--include-artwork")
	if err != nil || stdout != "abcdefghijklmnopqrstuv\n" || stderr != "" {
		t.Fatalf("id stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
	stdout, stderr, _, err = execute(&fakeSession{scopes: playlistScopes(), empty: true}, "playlists", "items", "list", playlistID)
	wantEmpty := "Playlist ID: " + playlistID + "\nPOSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n"
	if err != nil || stdout != wantEmpty || stderr != "" {
		t.Fatalf("empty stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
}

func TestPlaylistItemsListValidatesBeforeSession(t *testing.T) {
	otherPlaylistID := "ZYXWVUTSRQPONMLKJIHGFE"
	for _, args := range [][]string{
		{"playlists", "items", "extra"},
		{"playlists", "items", "list"},
		{"playlists", "items", "list", "bad"},
		{"playlists", "items", "list", "spotify:album:" + playlistID},
		{"playlists", "items", "list", playlistID, "--max", "0"},
		{"playlists", "items", "list", playlistID, "--max", "51"},
		{"playlists", "items", "list", playlistID, "--fields", "invalid"},
		{"playlists", "items", "list", playlistID, "--next-page-token", "invalid"},
		{"playlists", "items", "list", playlistID, "--next-page-token", pagetoken.Encode(playlistPageScope, 10)},
		{"playlists", "items", "list", playlistID, "--next-page-token", pagetoken.Encode(playlistItemPageScope(otherPlaylistID), 10)},
	} {
		_, _, opens, err := execute(&fakeSession{}, args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v opens=%d code=%d error=%v", args, opens, exitcode.Code(err), err)
		}
	}
	for _, max := range []string{"1", "50"} {
		session := &fakeSession{scopes: playlistScopes()}
		_, _, opens, err := execute(session, "playlists", "items", "list", playlistID, "--max", max)
		if err != nil || opens != 1 || fmt.Sprint(session.calls) != "[items:"+playlistID+":"+max+":0]" {
			t.Fatalf("max=%s opens=%d calls=%v error=%v", max, opens, session.calls, err)
		}
	}
}

func execute(session ReadSession, args ...string) (string, string, int, error) {
	opens := 0
	stdout, stderr, err := executeWithDependencies(Dependencies{OpenReadSession: func(context.Context, string, bool) (ReadSession, error) {
		opens++
		return session, nil
	}}, args...)
	return stdout, stderr, opens, err
}

func executeAdd(session AddSession, args ...string) (string, string, int, error) {
	opens := 0
	stdout, stderr, err := executeWithDependencies(Dependencies{OpenAddSession: func(context.Context, string, bool) (AddSession, error) {
		opens++
		return session, nil
	}}, args...)
	return stdout, stderr, opens, err
}

func executeRemove(session RemoveSession, args ...string) (string, string, int, error) {
	opens := 0
	stdout, stderr, err := executeWithDependencies(Dependencies{OpenRemoveSession: func(context.Context, string, bool) (RemoveSession, error) {
		opens++
		return session, nil
	}}, args...)
	return stdout, stderr, opens, err
}

func executeWithDependencies(deps Dependencies, args ...string) (string, string, error) {
	command := &cobra.Command{Use: "sptfy"}
	command.AddCommand(New(deps))
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}
