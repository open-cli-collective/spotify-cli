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

func TestPlaylistScopeGuardRequiresBothScopesWithOverwriteHint(t *testing.T) {
	for _, scopes := range [][]string{nil, {auth.ScopePlaylistReadCollaborative}, {auth.ScopePlaylistReadPrivate}} {
		for _, args := range [][]string{{"playlists", "list"}, {"playlists", "get", playlistID}} {
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

func execute(session *fakeSession, args ...string) (string, string, int, error) {
	opens := 0
	command := &cobra.Command{Use: "sptfy"}
	command.AddCommand(New(Dependencies{OpenSession: func(context.Context, string, bool) (Session, error) {
		opens++
		return session, nil
	}}))
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), opens, err
}
