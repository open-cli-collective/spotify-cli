package catalogcmd

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/pagetoken"
)

type fakeSession struct {
	calls     []string
	childName string
	hasNext   bool
	empty     bool
}

func (session *fakeSession) Close() error { return nil }
func (session *fakeSession) GetTrack(_ context.Context, id string) (client.Track, error) {
	session.calls = append(session.calls, "track:"+id)
	return client.Track{
		ID: id, Name: "Song", Artists: []client.Artist{{ID: "artist-1", Name: "Artist"}},
		Album: client.Album{ID: "album-1", Name: "Album"}, DurationMS: 61000,
	}, nil
}
func (session *fakeSession) GetAlbum(_ context.Context, id string) (client.Album, error) {
	session.calls = append(session.calls, "album:"+id)
	return client.Album{
		ID: id, Name: "Album", Artists: []client.Artist{{ID: "artist-1", Name: "Artist"}},
		ReleaseDate: "2026", ReleaseDatePrecision: "year", TotalTracks: 10, AlbumType: "album",
		URI: "spotify:album:" + id, ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/album/" + id},
		Images: []client.Image{{URL: "https://album-image"}},
	}, nil
}
func (session *fakeSession) GetArtist(_ context.Context, id string) (client.Artist, error) {
	session.calls = append(session.calls, "artist:"+id)
	return client.Artist{
		ID: id, Name: "Artist", URI: "spotify:artist:" + id,
		ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/artist/" + id},
		Images:       []client.Image{{URL: "https://artist-image"}},
	}, nil
}
func (session *fakeSession) ListAlbumTracks(_ context.Context, id string, limit, offset int) (client.TrackPage, error) {
	session.calls = append(session.calls, "album-tracks:"+id+":"+strconv.Itoa(limit)+":"+strconv.Itoa(offset))
	if session.empty {
		return client.TrackPage{Limit: limit, Offset: offset}, nil
	}
	name := "Song"
	if session.childName != "" {
		name = session.childName
	}
	return client.TrackPage{
		Items: []client.Track{{
			ID: "track-1", Name: name, Artists: []client.Artist{{ID: "artist-1", Name: "Artist"}},
			DurationMS: 61000, URI: "spotify:track:track-1",
			ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/track/track-1"},
			DiscNumber:   2, TrackNumber: 3, Explicit: true, Restrictions: client.Restriction{Reason: "market"},
		}},
		Limit: limit, Offset: offset, Total: offset + 1, HasNext: session.hasNext,
	}, nil
}
func (session *fakeSession) ListArtistAlbums(_ context.Context, id string, limit, offset int) (client.AlbumPage, error) {
	session.calls = append(session.calls, "artist-albums:"+id+":"+strconv.Itoa(limit)+":"+strconv.Itoa(offset))
	if session.empty {
		return client.AlbumPage{Limit: limit, Offset: offset}, nil
	}
	name := "Album"
	if session.childName != "" {
		name = session.childName
	}
	return client.AlbumPage{
		Items: []client.Album{{
			ID: "album-1", Name: name, Artists: []client.Artist{{ID: "artist-1", Name: "Artist"}},
			ReleaseDate: "2026", TotalTracks: 10, URI: "spotify:album:album-1",
			Images: []client.Image{{URL: "https://album-image"}},
		}},
		Limit: limit, Offset: offset, Total: offset + 1, HasNext: session.hasNext,
	}, nil
}

func TestCatalogGetExactOutputAndAcceptedReferences(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, resource := range []struct {
		group string
		kind  string
		want  string
		call  string
	}{
		{group: "tracks", kind: "track", want: id + "  Song\nArtist IDs: artist-1   Artists: Artist\nAlbum ID: album-1   Album: Album\nDuration: 1:01\n", call: "track:" + id},
		{group: "albums", kind: "album", want: id + "  Album\nArtist IDs: artist-1   Artists: Artist\nRelease Date: 2026   Total Tracks: 10\n", call: "album:" + id},
		{group: "artists", kind: "artist", want: id + "  Artist\n", call: "artist:" + id},
	} {
		for _, reference := range []struct {
			name  string
			value string
		}{
			{name: "raw ID", value: id},
			{name: "URI", value: "spotify:" + resource.kind + ":" + id},
			{name: "URL", value: "https://open.spotify.com/" + resource.kind + "/" + id},
		} {
			t.Run(resource.group+" "+reference.name, func(t *testing.T) {
				stdout, stderr, opens, session, err := execute(resource.group, "get", reference.value)
				if err != nil || stdout != resource.want || stderr != "" || opens != 1 || len(session.calls) != 1 || session.calls[0] != resource.call {
					t.Fatalf("stdout=%q stderr=%q opens=%d calls=%v error=%v", stdout, stderr, opens, session.calls, err)
				}
			})
		}
	}
}

func TestCatalogGetOutputFlags(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	stdout, _, _, _, err := execute("tracks", "get", id, "--id", "--fields", "invalid", "--extended", "--include-artwork")
	if err != nil || stdout != id+"\n" {
		t.Fatalf("stdout=%q error=%v", stdout, err)
	}
	stdout, _, _, _, err = execute("tracks", "get", id, "--fields", "track,album_id")
	if err != nil || stdout != id+"  Song\nAlbum ID: album-1\n" {
		t.Fatalf("stdout=%q error=%v", stdout, err)
	}
	stdout, _, _, _, err = execute("albums", "get", id, "--extended", "--include-artwork")
	want := id + "  Album\n" +
		"Artist IDs: artist-1   Artists: Artist\n" +
		"Release Date: 2026   Total Tracks: 10\n" +
		"URI: spotify:album:" + id + "   URL: https://open.spotify.com/album/" + id + "\n" +
		"Album Type: album   Release Date Precision: year\n" +
		"Restriction: -   Artwork: -x- https://album-image\n"
	if err != nil || stdout != want {
		t.Fatalf("album stdout=%q error=%v", stdout, err)
	}
	stdout, _, _, _, err = execute("albums", "get", id, "--fields", "album,uri,artwork")
	if err != nil || stdout != id+"  Album\nURI: spotify:album:"+id+"   Artwork: -x- https://album-image\n" {
		t.Fatalf("album fields stdout=%q error=%v", stdout, err)
	}
	stdout, _, _, _, err = execute("artists", "get", id, "--extended", "--include-artwork")
	want = id + "  Artist\n" +
		"URI: spotify:artist:" + id + "   URL: https://open.spotify.com/artist/" + id + "\n" +
		"Artwork: -x- https://artist-image\n"
	if err != nil || stdout != want {
		t.Fatalf("artist stdout=%q error=%v", stdout, err)
	}
	stdout, _, _, _, err = execute("artists", "get", id, "--fields", "artist,url,artwork")
	if err != nil || stdout != id+"  Artist\nURL: https://open.spotify.com/artist/"+id+"   Artwork: -x- https://artist-image\n" {
		t.Fatalf("artist fields stdout=%q error=%v", stdout, err)
	}
}

func TestCatalogGetValidatesBeforeSession(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, args := range [][]string{
		{"tracks", "get"}, {"tracks", "get", id, id}, {"tracks", "get", "bad"},
		{"tracks", "get", "spotify:album:" + id}, {"tracks", "get", id, "--fields", "invalid"},
		{"tracks", "get", "https://user@open.spotify.com/track/" + id},
	} {
		_, _, opens, _, err := execute(args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v error=%v code=%d opens=%d", args, err, exitcode.Code(err), opens)
		}
	}
}

func TestCatalogGroupsHaveOnlySingularAliasesAndNoJSON(t *testing.T) {
	command := &cobra.Command{Use: "sptfy"}
	command.AddCommand(New(Dependencies{})...)
	for _, name := range []string{"tracks", "albums", "artists"} {
		group, _, err := command.Find([]string{name})
		if err != nil || len(group.Aliases) != 1 {
			t.Fatalf("%s aliases=%v error=%v", name, group.Aliases, err)
		}
	}
	_, _, opens, _, err := execute("tracks", "get", "0123456789ABCDEFGHIJKL", "--json")
	if err == nil || opens != 0 {
		t.Fatalf("error=%v opens=%d", err, opens)
	}
}

func TestCatalogGetHelp(t *testing.T) {
	stdout, stderr, opens, _, err := execute("tracks", "get", "--help")
	want := "Get one Spotify track\n\n" +
		"Usage:\n  sptfy tracks get <spotify-id-uri-or-url> [flags]\n\n" +
		"Flags:\n" +
		"      --extended          Add less-frequent track fields\n" +
		"      --fields string     Comma-separated output fields\n" +
		"  -h, --help              help for get\n" +
		"      --id                Emit only the track ID\n" +
		"      --include-artwork   Add Spotify artwork dimensions and URLs\n"
	if err != nil || stdout != want || stderr != "" || opens != 0 {
		t.Fatalf("stdout=%q stderr=%q opens=%d error=%v", stdout, stderr, opens, err)
	}
}

func TestCatalogTraversalExactOutputAndAcceptedReferences(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		group, relation, kind, parent, want string
	}{
		{
			group: "albums", relation: "tracks", kind: "album", parent: "Album",
			want: "Album ID: " + id + "\nID | TRACK | ARTIST_IDS | ARTISTS | DURATION\ntrack-1 | Song | artist-1 | Artist | 1:01\n",
		},
		{
			group: "artists", relation: "albums", kind: "artist", parent: "Artist",
			want: "Artist ID: " + id + "\nID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS\nalbum-1 | Album | artist-1 | Artist | 2026 | 10\n",
		},
	} {
		for _, reference := range []string{id, "spotify:" + test.kind + ":" + id, "https://open.spotify.com/" + test.kind + "/" + id} {
			stdout, stderr, opens, session, err := execute(test.group, test.relation, "list", reference)
			wantCall := test.kind + "-" + test.relation + ":" + id + ":10:0"
			if err != nil || stdout != test.want || stderr != "" || opens != 1 || len(session.calls) != 1 || session.calls[0] != wantCall {
				t.Fatalf("%s %q stdout=%q stderr=%q opens=%d calls=%v error=%v", test.parent, reference, stdout, stderr, opens, session.calls, err)
			}
		}
	}
}

func TestCatalogTraversalOutputShapes(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"albums", "tracks", "list", id, "--id", "--fields", "invalid", "--extended"}, want: "track-1\n"},
		{args: []string{"albums", "tracks", "list", id, "--fields", "track,artist_ids"}, want: "Album ID: " + id + "\nTRACK | ARTIST_IDS\nSong | artist-1\n"},
		{args: []string{"albums", "tracks", "list", id, "--extended"}, want: "Album ID: " + id + "\nID | TRACK | ARTIST_IDS | ARTISTS | DURATION | URI | URL | DISC_NUMBER | TRACK_NUMBER | EXPLICIT | RESTRICTION\ntrack-1 | Song | artist-1 | Artist | 1:01 | spotify:track:track-1 | https://open.spotify.com/track/track-1 | 2 | 3 | true | market\n"},
		{args: []string{"artists", "albums", "list", id, "--id", "--fields", "invalid", "--include-artwork"}, want: "album-1\n"},
		{args: []string{"artists", "albums", "list", id, "--fields", "album,artwork"}, want: "Artist ID: " + id + "\nALBUM | ARTWORK\nAlbum | -x- https://album-image\n"},
		{args: []string{"artists", "albums", "list", id, "--include-artwork"}, want: "Artist ID: " + id + "\nID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS | ARTWORK\nalbum-1 | Album | artist-1 | Artist | 2026 | 10 | -x- https://album-image\n"},
	} {
		stdout, stderr, _, _, err := execute(test.args...)
		if err != nil || stdout != test.want || stderr != "" {
			t.Fatalf("args=%v stdout=%q stderr=%q error=%v", test.args, stdout, stderr, err)
		}
	}
}

func TestCatalogTraversalSanitizesChildNames(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"albums", "tracks", "list", id, "--fields", "track"}, want: "Album ID: " + id + "\nTRACK\nBjörk Live Cut\n"},
		{args: []string{"artists", "albums", "list", id, "--fields", "album"}, want: "Artist ID: " + id + "\nALBUM\nBjörk Live Cut\n"},
	} {
		stdout, stderr, _, _, err := executeWithSession(&fakeSession{childName: "Björk | Live\nCut"}, test.args...)
		if err != nil || stdout != test.want || stderr != "" {
			t.Fatalf("args=%v stdout=%q stderr=%q error=%v", test.args, stdout, stderr, err)
		}
	}
}

func TestCatalogTraversalEmptyPagesKeepParentAndTableHeaders(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"albums", "tracks", "list", id}, want: "Album ID: " + id + "\nID | TRACK | ARTIST_IDS | ARTISTS | DURATION\n"},
		{args: []string{"artists", "albums", "list", id}, want: "Artist ID: " + id + "\nID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS\n"},
	} {
		stdout, stderr, _, _, err := executeWithSession(&fakeSession{empty: true}, test.args...)
		if err != nil || stdout != test.want || stderr != "" {
			t.Fatalf("args=%v stdout=%q stderr=%q error=%v", test.args, stdout, stderr, err)
		}
	}
}

func TestCatalogTraversalPaginationIsParentBoundAndUsesStderr(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		args              []string
		scope, call, want string
	}{
		{
			args: []string{"albums", "tracks", "list", id}, scope: "album-tracks:" + id,
			call: "album-tracks:" + id + ":10:10",
			want: "Album ID: " + id + "\nID | TRACK | ARTIST_IDS | ARTISTS | DURATION\ntrack-1 | Song | artist-1 | Artist | 1:01\n",
		},
		{
			args: []string{"artists", "albums", "list", id}, scope: "artist-albums:" + id,
			call: "artist-albums:" + id + ":10:10",
			want: "Artist ID: " + id + "\nID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS\nalbum-1 | Album | artist-1 | Artist | 2026 | 10\n",
		},
	} {
		authenticated := &fakeSession{hasNext: true}
		token := pagetoken.Encode(test.scope, 10)
		args := append(test.args, "--next-page-token", token)
		stdout, stderr, opens, session, err := executeWithSession(authenticated, args...)
		wantErr := "More results available (next: " + pagetoken.Encode(test.scope, 20) + ")\n"
		if err != nil || stdout != test.want || stderr != wantErr || opens != 1 ||
			len(session.calls) != 1 || session.calls[0] != test.call {
			t.Fatalf("args=%v stdout=%q stderr=%q opens=%d calls=%v error=%v", args, stdout, stderr, opens, session.calls, err)
		}
	}
}

func TestCatalogTraversalValidatesBeforeSession(t *testing.T) {
	const (
		id      = "0123456789ABCDEFGHIJKL"
		otherID = "abcdefghijklmnopqrstuv"
	)
	for _, args := range [][]string{
		{"albums", "tracks", "list", "bad"},
		{"albums", "tracks", "list", id, "--max", "0"},
		{"albums", "tracks", "list", id, "--max", "51"},
		{"artists", "albums", "list", id, "--max", "0"},
		{"artists", "albums", "list", id, "--max", "11"},
		{"albums", "tracks", "list", id, "--fields", "album"},
		{"albums", "tracks", "list", id, "--fields", "artwork"},
		{"albums", "tracks", "list", id, "--next-page-token", "invalid"},
		{"albums", "tracks", "list", id, "--next-page-token", pagetoken.Encode("artist-albums:"+id, 10)},
		{"albums", "tracks", "list", id, "--next-page-token", pagetoken.Encode("album-tracks:"+otherID, 10)},
		{"artists", "albums", "list", id, "--next-page-token", pagetoken.Encode("artist-albums:"+otherID, 10)},
	} {
		_, _, opens, _, err := execute(args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v error=%v code=%d opens=%d", args, err, exitcode.Code(err), opens)
		}
	}
	_, _, opens, _, err := execute("albums", "tracks", "list", id, "--include-artwork")
	if err == nil || opens != 0 {
		t.Fatalf("unavailable artwork flag error=%v opens=%d", err, opens)
	}
	for _, test := range []struct {
		args []string
		call string
	}{
		{args: []string{"albums", "tracks", "list", id, "--max", "50"}, call: "album-tracks:" + id + ":50:0"},
		{args: []string{"artists", "albums", "list", id, "--max", "10"}, call: "artist-albums:" + id + ":10:0"},
	} {
		_, _, opens, session, err := execute(test.args...)
		if err != nil || opens != 1 || len(session.calls) != 1 || session.calls[0] != test.call {
			t.Fatalf("args=%v opens=%d calls=%v error=%v", test.args, opens, session.calls, err)
		}
	}
}

func TestCatalogTraversalHelpDoesNotOpenSessionOrAdvertiseUnavailableFields(t *testing.T) {
	stdout, stderr, opens, _, err := execute("albums", "tracks", "list", "--help")
	if err != nil || stderr != "" || opens != 0 || bytes.Contains([]byte(stdout), []byte("include-artwork")) {
		t.Fatalf("stdout=%q stderr=%q opens=%d error=%v", stdout, stderr, opens, err)
	}
	stdout, stderr, opens, _, err = execute("artists", "albums", "list", "--help")
	if err != nil || stderr != "" || opens != 0 || !bytes.Contains([]byte(stdout), []byte("include-artwork")) {
		t.Fatalf("stdout=%q stderr=%q opens=%d error=%v", stdout, stderr, opens, err)
	}
}

func execute(args ...string) (string, string, int, *fakeSession, error) {
	return executeWithSession(&fakeSession{}, args...)
}

func executeWithSession(authenticated *fakeSession, args ...string) (string, string, int, *fakeSession, error) {
	opens := 0
	command := &cobra.Command{Use: "sptfy"}
	command.AddCommand(New(Dependencies{OpenSession: func(context.Context, string, bool) (Session, error) {
		opens++
		return authenticated, nil
	}})...)
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), opens, authenticated, err
}
