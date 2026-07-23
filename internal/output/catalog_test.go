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
		ID: "artist-1", Name: "Björk | live\ncut", URI: "spotify:artist:artist-1",
		ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/artist/artist-1"}, Images: []client.Image{{URL: "https://image", Width: nil, Height: intPointer(320)}},
	}
	tests := []struct {
		name     string
		csv      string
		extended bool
		artwork  bool
		want     string
	}{
		{name: "default", want: "ID | ARTIST\nartist-1 | Björk live cut\n"},
		{name: "fields and artwork", csv: "artist,artwork", want: "ARTIST | ARTWORK\nBjörk live cut | -x320 https://image\n"},
		{name: "extended", extended: true, want: "ID | ARTIST | URI | URL\nartist-1 | Björk live cut | spotify:artist:artist-1 | https://open.spotify.com/artist/artist-1\n"},
		{name: "include artwork", artwork: true, want: "ID | ARTIST | ARTWORK\nartist-1 | Björk live cut | -x320 https://image\n"},
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

func TestRenderCatalogDetails(t *testing.T) {
	track := client.Track{
		ID: "track-1", Name: "Song | live\ncut", DurationMS: 123000,
		Artists: []client.Artist{{ID: "artist-1", Name: "Björk"}},
		Album:   client.Album{ID: "album-1", Name: "Debut", Images: []client.Image{{URL: "https://image", Width: intPointer(640), Height: intPointer(640)}}},
		URI:     "spotify:track:track-1", ExternalURLs: client.ExternalURLs{Spotify: "https://open.spotify.com/track/track-1"},
		DiscNumber: 1, TrackNumber: 2, Explicit: true,
	}
	album := track.Album
	album.Artists = track.Artists
	album.Name = "Debut | live\ncut"
	album.ReleaseDate = "1993"
	album.ReleaseDatePrecision = "year"
	album.TotalTracks = 12
	album.AlbumType = "album"
	album.URI = "spotify:album:album-1"
	album.ExternalURLs = client.ExternalURLs{Spotify: "https://open.spotify.com/album/album-1"}
	artist := track.Artists[0]
	artist.Name = "Björk | live\ncut"
	artist.URI = "spotify:artist:artist-1"
	artist.ExternalURLs = client.ExternalURLs{Spotify: "https://open.spotify.com/artist/artist-1"}
	artist.Images = []client.Image{{URL: "https://artist-image", Width: intPointer(320), Height: intPointer(320)}}

	trackFields, _ := SelectTrackFields("", false, false)
	if got, want := RenderTrack(track, trackFields), "track-1  Song live cut\nArtist IDs: artist-1   Artists: Björk\nAlbum ID: album-1   Album: Debut\nDuration: 2:03\n"; got != want {
		t.Fatalf("track detail=%q want=%q", got, want)
	}
	albumFields, _ := SelectAlbumFields("", false, false)
	if got, want := RenderAlbum(album, albumFields), "album-1  Debut live cut\nArtist IDs: artist-1   Artists: Björk\nRelease Date: 1993   Total Tracks: 12\n"; got != want {
		t.Fatalf("album detail=%q want=%q", got, want)
	}
	artistFields, _ := SelectArtistFields("", false, false)
	if got, want := RenderArtist(artist, artistFields), "artist-1  Björk live cut\n"; got != want {
		t.Fatalf("artist detail=%q want=%q", got, want)
	}

	fields, _ := SelectTrackFields("id,track,uri,artwork", false, false)
	if got, want := RenderTrack(track, fields), "track-1  Song live cut\nURI: spotify:track:track-1   Artwork: 640x640 https://image\n"; got != want {
		t.Fatalf("selected detail=%q want=%q", got, want)
	}
	fields, _ = SelectTrackFields("", true, true)
	want := "track-1  Song live cut\n" +
		"Artist IDs: artist-1   Artists: Björk\n" +
		"Album ID: album-1   Album: Debut\n" +
		"Duration: 2:03   URI: spotify:track:track-1\n" +
		"URL: https://open.spotify.com/track/track-1   Disc Number: 1\n" +
		"Track Number: 2   Explicit: true\n" +
		"Restriction: -   Artwork: 640x640 https://image\n"
	if got := RenderTrack(track, fields); got != want {
		t.Fatalf("extended detail=%q want=%q", got, want)
	}
	albumFields, _ = SelectAlbumFields("album,uri,artwork", true, false)
	if got, want := RenderAlbum(album, albumFields), "album-1  Debut live cut\nURI: spotify:album:album-1   Artwork: 640x640 https://image\n"; got != want {
		t.Fatalf("selected album detail=%q want=%q", got, want)
	}
	artistFields, _ = SelectArtistFields("", true, true)
	want = "artist-1  Björk live cut\n" +
		"URI: spotify:artist:artist-1   URL: https://open.spotify.com/artist/artist-1\n" +
		"Artwork: 320x320 https://artist-image\n"
	if got := RenderArtist(artist, artistFields); got != want {
		t.Fatalf("extended artist detail=%q want=%q", got, want)
	}
}
