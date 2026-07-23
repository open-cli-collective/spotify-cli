package output

import (
	"strings"
	"testing"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

func TestRenderTracksDefault(t *testing.T) {
	fields, err := SelectTrackFields("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	track := client.Track{
		ID: "track-1", Name: "Song | live\ncut", DurationMS: 3_723_000,
		Artists: []client.Artist{{ID: "artist-1", Name: "Ada |\nLovelace"}, {ID: "artist-2", Name: "B|B"}},
		Album:   client.Album{ID: "album-1", Name: "Record"},
	}
	want := "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" +
		"track-1 | Song live cut | artist-1,artist-2 | Ada Lovelace,B|B | album-1 | Record | 1:02:03\n"
	if got := RenderTracks([]client.Track{track}, fields); got != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", got, want)
	}
}

func TestTrackFieldPrecedenceAndArtwork(t *testing.T) {
	fields, err := SelectTrackFields("track,artwork,TRACK", true, false)
	if err != nil {
		t.Fatal(err)
	}
	track := client.Track{Name: "Song", Album: client.Album{Images: []client.Image{{Width: intPointer(640), Height: intPointer(640), URL: "https://i.example/large"}, {URL: "https://i.example/unknown"}}}}
	want := "TRACK | ARTWORK\nSong | 640x640 https://i.example/large,-x- https://i.example/unknown\n"
	if got := RenderTracks([]client.Track{track}, fields); got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}

	widened, err := SelectTrackFields(", ,", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(widened) != len(defaultTrackFields)+len(extendedTrackFields)+1 {
		t.Fatalf("widened fields = %v", widened)
	}
}

func intPointer(value int) *int { return &value }

func TestTrackProjectionRejectsUnknownAndIDsHaveNoHeader(t *testing.T) {
	if _, err := SelectTrackFields("nope", false, false); err == nil || !strings.Contains(err.Error(), "valid fields: ID, TRACK") {
		t.Fatalf("error = %v", err)
	}
	if got := RenderTrackIDs([]client.Track{{ID: "one"}, {ID: "two"}}); got != "one\ntwo\n" {
		t.Fatalf("IDs = %q", got)
	}
	fields, _ := SelectTrackFields("", false, false)
	if got := RenderTracks(nil, fields); got != "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" {
		t.Fatalf("empty table = %q", got)
	}
}

func TestAlbumTrackFieldsExcludeUnavailableAlbumAndArtworkMetadata(t *testing.T) {
	fields, err := SelectAlbumTrackFields("", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderTracks(nil, fields); got != "ID | TRACK | ARTIST_IDS | ARTISTS | DURATION\n" {
		t.Fatalf("default=%q", got)
	}
	fields, err = SelectAlbumTrackFields("", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderTracks(nil, fields); got != "ID | TRACK | ARTIST_IDS | ARTISTS | DURATION | URI | URL | DISC_NUMBER | TRACK_NUMBER | EXPLICIT | RESTRICTION\n" {
		t.Fatalf("extended=%q", got)
	}
	for _, csv := range []string{"album_id", "album", "artwork"} {
		if _, err := SelectAlbumTrackFields(csv, false); err == nil {
			t.Fatalf("field %q accepted", csv)
		}
	}
}
