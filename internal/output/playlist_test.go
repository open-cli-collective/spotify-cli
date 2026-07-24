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

func TestPlaylistItemFieldsAndMixedRendering(t *testing.T) {
	explicit := true
	items := []client.PlaylistItem{
		{Type: "track", ID: "track-1", Name: "Song | live\ncut", Artists: []client.Artist{{ID: "artist-1", Name: "Ada"}}, AlbumID: "album-1", AlbumName: "Album", DurationMS: intPointer(61000), URI: "spotify:track:track-1", URL: "https://track", AddedAt: "2026-07-24T12:00:00Z", AddedByID: "owner-1", DiscNumber: 1, TrackNumber: 2, Explicit: &explicit, Restriction: "market", Images: []client.Image{{URL: "https://track-image"}}},
		{Type: "episode", ID: "episode-1", Name: "Episode", DurationMS: intPointer(62000), Images: []client.Image{{URL: "https://episode-image"}}},
		{Type: "local", Name: "Local", DurationMS: intPointer(63000)},
		{Type: "unavailable"},
		{Type: "future | type\nline", Name: "Future", DurationMS: intPointer(64000)},
		{Type: "unknown", Name: "Untyped"},
	}
	fields, err := SelectPlaylistItemFields("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" +
		"20 | track | track-1 | Song live cut | artist-1 | Ada | album-1 | Album | 1:01\n" +
		"21 | episode | episode-1 | Episode | - | - | - | - | 1:02\n" +
		"22 | local | - | Local | - | - | - | - | 1:03\n" +
		"23 | unavailable | - | - | - | - | - | - | -\n" +
		"24 | future type line | - | Future | - | - | - | - | 1:04\n" +
		"25 | unknown | - | Untyped | - | - | - | - | -\n"
	if got := RenderPlaylistItems(items, 20, fields); got != want {
		t.Fatalf("rendered=%q want=%q", got, want)
	}

	fields, err = SelectPlaylistItemFields("item,TYPE,item,artwork", true, true)
	if err != nil {
		t.Fatal(err)
	}
	want = "ITEM | TYPE | ARTWORK\nSong live cut | track | -x- https://track-image\nEpisode | episode | -x- https://episode-image\nLocal | local | -\n- | unavailable | -\nFuture | future type line | -\nUntyped | unknown | -\n"
	if got := RenderPlaylistItems(items, 0, fields); got != want {
		t.Fatalf("selected=%q want=%q", got, want)
	}
	if got := RenderPlaylistItemIDs(items); got != "track-1\nepisode-1\n" {
		t.Fatalf("ids=%q", got)
	}
}

func TestPlaylistItemExtendedAndEmptyRendering(t *testing.T) {
	fields, err := SelectPlaylistItemFields("", true, true)
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := "POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION | URI | URL | ADDED_AT | ADDED_BY_ID | DISC_NUMBER | TRACK_NUMBER | EXPLICIT | RESTRICTION | ARTWORK\n"
	if got := RenderPlaylistItems(nil, 0, fields); got != wantHeader {
		t.Fatalf("empty=%q", got)
	}
	if _, err := SelectPlaylistItemFields("nope", false, false); err == nil || !strings.Contains(err.Error(), "valid fields: POSITION, TYPE") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlaylistMutationRecordsAreCompactAndSanitized(t *testing.T) {
	if got := RenderPlaylistItemsAdded("playlist\tID", 2, 101, "next\nsnapshot"); got != "added\tplaylist ID\t2\t101\tnext snapshot\n" {
		t.Fatalf("add=%q", got)
	}
	if got := RenderPlaylistItemRemoved("playlist", 2, "track\tID", "next\r\nsnapshot"); got != "removed\tplaylist\t2\ttrack ID\tnext snapshot\n" {
		t.Fatalf("remove=%q", got)
	}
	if got := RenderPlaylistItemUpdated("playlist", 2, "old\tID", "new\nID", "next\r\nsnapshot"); got != "updated\tplaylist\t2\told ID\tnew ID\tnext snapshot\n" {
		t.Fatalf("update=%q", got)
	}
}
