package output

import (
	"strings"
	"testing"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

func TestPlaylistFieldsAndRendering(t *testing.T) {
	public := true
	playlist := client.Playlist{
		ID: "playlist-1", Name: "Mix | live\ncut",
		Owner:     client.PlaylistOwner{ID: "owner-1", DisplayName: "Ada | DJ"},
		ItemCount: &client.PlaylistItemCount{Total: 3}, Public: &public, Collaborative: true,
		URI: "spotify:playlist:playlist-1", ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/playlist/playlist-1"},
		SnapshotID: "snapshot", Description: "First | line\nSecond",
		Images: []client.Image{{URL: "https://image", Width: intPointer(640), Height: intPointer(640)}},
	}
	for _, test := range []struct {
		name     string
		csv      string
		extended bool
		artwork  bool
		want     string
	}{
		{name: "default", want: "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE\nplaylist-1 | Mix live cut | owner-1 | Ada DJ | 3 | true | true\n"},
		{name: "fields deduplicate", csv: "playlist,description,PLAYLIST,artwork", want: "PLAYLIST | DESCRIPTION | ARTWORK\nMix live cut | First line Second | 640x640 https://image\n"},
		{name: "extended", extended: true, want: "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE | URI | URL | SNAPSHOT_ID | DESCRIPTION\nplaylist-1 | Mix live cut | owner-1 | Ada DJ | 3 | true | true | spotify:playlist:playlist-1 | https://open.spotify.com/playlist/playlist-1 | snapshot | First line Second\n"},
		{name: "artwork", artwork: true, want: "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE | ARTWORK\nplaylist-1 | Mix live cut | owner-1 | Ada DJ | 3 | true | true | 640x640 https://image\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields, err := SelectPlaylistFields(test.csv, test.extended, test.artwork)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderPlaylists([]client.Playlist{playlist}, fields); got != test.want {
				t.Fatalf("rendered=%q want=%q", got, test.want)
			}
		})
	}
	if got := RenderPlaylistIDs([]client.Playlist{{ID: "one"}, {ID: "two"}}); got != "one\ntwo\n" {
		t.Fatalf("ids=%q", got)
	}
	if _, err := SelectPlaylistFields("nope", false, false); err == nil || !strings.Contains(err.Error(), "valid fields: ID, PLAYLIST") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlaylistRenderingHandlesEmptyAndNullableFields(t *testing.T) {
	fields, err := SelectPlaylistFields("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	header := "ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE\n"
	if got := RenderPlaylists(nil, fields); got != header {
		t.Fatalf("empty=%q", got)
	}
	playlist := client.Playlist{ID: "playlist-1"}
	want := header + "playlist-1 | - | - | - | - | - | false\n"
	if got := RenderPlaylists([]client.Playlist{playlist}, fields); got != want {
		t.Fatalf("nullable=%q want=%q", got, want)
	}
}

func TestRenderPlaylistDetailKeepsIdentityOnlyInHeader(t *testing.T) {
	playlist := client.Playlist{
		ID: "playlist-1", Name: "Mix | live\ncut", Owner: client.PlaylistOwner{ID: "owner-1", DisplayName: "Ada"},
		ItemCount: &client.PlaylistItemCount{Total: 3}, Description: "First\nSecond",
	}
	fields, err := SelectPlaylistFields("id,playlist,owner_id,item_count,description", false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "playlist-1  Mix live cut\nOwner ID: owner-1   Item Count: 3\nDescription: First Second\n"
	if got := RenderPlaylist(playlist, fields); got != want {
		t.Fatalf("detail=%q want=%q", got, want)
	}
}
