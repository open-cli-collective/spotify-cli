package catalogcmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

type fakeSession struct {
	calls []string
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

func execute(args ...string) (string, string, int, *fakeSession, error) {
	opens := 0
	authenticated := &fakeSession{}
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
