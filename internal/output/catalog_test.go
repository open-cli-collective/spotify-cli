package output

import (
	"strings"
	"testing"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

func TestRenderAlbumsShapes(t *testing.T) {
	album := client.Album{
		ID: "album-1", Name: "Debut | live\ncut", Artists: []client.Artist{{ID: "artist-1", Name: "Björk"}, {ID: "artist-2", Name: "Guest"}},
		ReleaseDate: "1993-07-05", TotalTracks: 12, AlbumType: "album", URI: "spotify:album:album-1",
		ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/album/album-1"}, Restrictions: client.Restriction{Reason: "market"},
		Images: []client.Image{{URL: "https://image", Width: intPointer(640), Height: intPointer(640)}, {URL: "https://unknown"}},
	}
	tests := []struct {
		name     string
		csv      string
		extended bool
		artwork  bool
		want     string
	}{
		{name: "default", want: "ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS\nalbum-1 | Debut live cut | artist-1,artist-2 | Björk,Guest | 1993-07-05 | 12\n"},
		{name: "fields and artwork", csv: "album,artwork,ALBUM", want: "ALBUM | ARTWORK\nDebut live cut | 640x640 https://image,-x- https://unknown\n"},
		{name: "extended", extended: true, want: "ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS | URI | URL | ALBUM_TYPE | RELEASE_DATE_PRECISION | RESTRICTION\nalbum-1 | Debut live cut | artist-1,artist-2 | Björk,Guest | 1993-07-05 | 12 | spotify:album:album-1 | https://open.spotify.com/album/album-1 | album | - | market\n"},
		{name: "include artwork", artwork: true, want: "ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS | ARTWORK\nalbum-1 | Debut live cut | artist-1,artist-2 | Björk,Guest | 1993-07-05 | 12 | 640x640 https://image,-x- https://unknown\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields, err := SelectAlbumFields(test.csv, test.extended, test.artwork)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderAlbums([]client.Album{album}, fields); got != test.want {
				t.Fatalf("rendered=%q want=%q", got, test.want)
			}
		})
	}
	if got := RenderAlbumIDs([]client.Album{{ID: "one"}, {ID: "two"}}); got != "one\ntwo\n" {
		t.Fatalf("IDs = %q", got)
	}
	if _, err := SelectAlbumFields("nope", false, false); err == nil || !strings.Contains(err.Error(), "valid fields: ID, ALBUM") {
		t.Fatalf("error = %v", err)
	}
}

func TestRenderArtistsShapes(t *testing.T) {
	artist := client.Artist{
		ID: "artist-1", Name: "Björk | live\ncut", Genres: []string{"art pop", "electronic"}, URI: "spotify:artist:artist-1",
		ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/artist/artist-1"}, Images: []client.Image{{URL: "https://image", Width: nil, Height: intPointer(320)}},
	}
	tests := []struct {
		name     string
		csv      string
		extended bool
		artwork  bool
		want     string
	}{
		{name: "default", want: "ID | ARTIST | GENRES\nartist-1 | Björk live cut | art pop,electronic\n"},
		{name: "fields and artwork", csv: "artist,artwork", want: "ARTIST | ARTWORK\nBjörk live cut | -x320 https://image\n"},
		{name: "extended", extended: true, want: "ID | ARTIST | GENRES | URI | URL\nartist-1 | Björk live cut | art pop,electronic | spotify:artist:artist-1 | https://open.spotify.com/artist/artist-1\n"},
		{name: "include artwork", artwork: true, want: "ID | ARTIST | GENRES | ARTWORK\nartist-1 | Björk live cut | art pop,electronic | -x320 https://image\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields, err := SelectArtistFields(test.csv, test.extended, test.artwork)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderArtists([]client.Artist{artist}, fields); got != test.want {
				t.Fatalf("rendered=%q want=%q", got, test.want)
			}
		})
	}
	if got := RenderArtistIDs([]client.Artist{{ID: "one"}, {ID: "two"}}); got != "one\ntwo\n" {
		t.Fatalf("IDs = %q", got)
	}
	if _, err := SelectArtistFields("nope", false, false); err == nil || !strings.Contains(err.Error(), "valid fields: ID, ARTIST") {
		t.Fatalf("error = %v", err)
	}
}
