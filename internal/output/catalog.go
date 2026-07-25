package output

import (
	"fmt"
	"slices"
	"strings"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

// AlbumField identifies one stable album table column.
type AlbumField string

// Stable album output fields.
const (
	AlbumAddedAt              AlbumField = "ADDED_AT"
	AlbumID                   AlbumField = "ID"
	AlbumName                 AlbumField = "ALBUM"
	AlbumArtistIDs            AlbumField = "ARTIST_IDS"
	AlbumArtists              AlbumField = "ARTISTS"
	AlbumReleaseDate          AlbumField = "RELEASE_DATE"
	AlbumTotalTracks          AlbumField = "TOTAL_TRACKS"
	AlbumURI                  AlbumField = "URI"
	AlbumURL                  AlbumField = "URL"
	AlbumType                 AlbumField = "ALBUM_TYPE"
	AlbumReleaseDatePrecision AlbumField = "RELEASE_DATE_PRECISION"
	AlbumRestriction          AlbumField = "RESTRICTION"
	AlbumArtwork              AlbumField = "ARTWORK"
)

var (
	defaultAlbumFields      = []AlbumField{AlbumID, AlbumName, AlbumArtistIDs, AlbumArtists, AlbumReleaseDate, AlbumTotalTracks}
	extendedAlbumFields     = []AlbumField{AlbumURI, AlbumURL, AlbumType, AlbumReleaseDatePrecision, AlbumRestriction}
	allAlbumFields          = slices.Concat(defaultAlbumFields, extendedAlbumFields, []AlbumField{AlbumArtwork})
	defaultSavedAlbumFields = slices.Concat([]AlbumField{AlbumAddedAt}, defaultAlbumFields)
	allSavedAlbumFields     = slices.Concat([]AlbumField{AlbumAddedAt}, allAlbumFields)
)

// SelectAlbumFields applies the family-wide default, widening, then override precedence.
func SelectAlbumFields(csv string, extended, includeArtwork bool) ([]AlbumField, error) {
	fields := append([]AlbumField(nil), defaultAlbumFields...)
	if extended {
		fields = append(fields, extendedAlbumFields...)
	}
	if includeArtwork {
		fields = append(fields, AlbumArtwork)
	}
	return selectFields(csv, fields, allAlbumFields, "album")
}

// SelectSavedAlbumFields applies saved-album list field precedence.
func SelectSavedAlbumFields(csv string, extended, includeArtwork bool) ([]AlbumField, error) {
	fields := append([]AlbumField(nil), defaultSavedAlbumFields...)
	if extended {
		fields = append(fields, extendedAlbumFields...)
	}
	if includeArtwork {
		fields = append(fields, AlbumArtwork)
	}
	return selectFields(csv, fields, allSavedAlbumFields, "album")
}

// RenderAlbums renders one pipe-delimited table, including the header for an empty page.
func RenderAlbums(albums []client.Album, fields []AlbumField) string {
	return renderTable(albums, fields, albumCell)
}

// RenderAlbumIDs renders one primary identifier per line without a header.
func RenderAlbumIDs(albums []client.Album) string {
	return renderIDs(albums, func(album client.Album) string { return album.ID })
}

// RenderSavedAlbums renders saved albums with their library timestamps.
func RenderSavedAlbums(items []client.SavedAlbum, fields []AlbumField) string {
	return renderTable(items, fields, savedAlbumCell)
}

// RenderSavedAlbumIDs renders one saved album ID per line.
func RenderSavedAlbumIDs(items []client.SavedAlbum) string {
	return renderIDs(items, func(item client.SavedAlbum) string { return item.Album.ID })
}

func savedAlbumCell(item client.SavedAlbum, field AlbumField) string {
	if field == AlbumAddedAt {
		return cell(item.AddedAt)
	}
	return albumCell(item.Album, field)
}

func albumCell(album client.Album, field AlbumField) string {
	//nolint:exhaustive // AlbumAddedAt is handled by savedAlbumCell.
	switch field {
	case AlbumID:
		return cell(album.ID)
	case AlbumName:
		return cell(album.Name)
	case AlbumArtistIDs:
		return cell(joinArtists(album.Artists, func(artist client.Artist) string { return artist.ID }))
	case AlbumArtists:
		return cell(joinArtists(album.Artists, func(artist client.Artist) string { return artist.Name }))
	case AlbumReleaseDate:
		return cell(album.ReleaseDate)
	case AlbumTotalTracks:
		return positiveInt(album.TotalTracks)
	case AlbumURI:
		return cell(album.URI)
	case AlbumURL:
		return cell(album.ExternalURLs.Spotify)
	case AlbumType:
		return cell(album.AlbumType)
	case AlbumReleaseDatePrecision:
		return cell(album.ReleaseDatePrecision)
	case AlbumRestriction:
		return cell(album.Restrictions.Reason)
	case AlbumArtwork:
		return renderArtwork(album.Images)
	default:
		return "-"
	}
}

// ArtistField identifies one stable artist table column.
type ArtistField string

// Stable artist output fields.
const (
	ArtistID      ArtistField = "ID"
	ArtistName    ArtistField = "ARTIST"
	ArtistURI     ArtistField = "URI"
	ArtistURL     ArtistField = "URL"
	ArtistArtwork ArtistField = "ARTWORK"
)

var (
	defaultArtistFields  = []ArtistField{ArtistID, ArtistName}
	extendedArtistFields = []ArtistField{ArtistURI, ArtistURL}
	allArtistFields      = slices.Concat(defaultArtistFields, extendedArtistFields, []ArtistField{ArtistArtwork})
)

// SelectArtistFields applies the family-wide default, widening, then override precedence.
func SelectArtistFields(csv string, extended, includeArtwork bool) ([]ArtistField, error) {
	fields := append([]ArtistField(nil), defaultArtistFields...)
	if extended {
		fields = append(fields, extendedArtistFields...)
	}
	if includeArtwork {
		fields = append(fields, ArtistArtwork)
	}
	return selectFields(csv, fields, allArtistFields, "artist")
}

// RenderArtists renders one pipe-delimited table, including the header for an empty page.
func RenderArtists(artists []client.Artist, fields []ArtistField) string {
	return renderTable(artists, fields, artistCell)
}

// RenderArtistIDs renders one primary identifier per line without a header.
func RenderArtistIDs(artists []client.Artist) string {
	return renderIDs(artists, func(artist client.Artist) string { return artist.ID })
}

func artistCell(artist client.Artist, field ArtistField) string {
	switch field {
	case ArtistID:
		return cell(artist.ID)
	case ArtistName:
		return cell(artist.Name)
	case ArtistURI:
		return cell(artist.URI)
	case ArtistURL:
		return cell(artist.ExternalURLs.Spotify)
	case ArtistArtwork:
		return renderArtwork(artist.Images)
	default:
		return "-"
	}
}

// RenderAlbum renders one album as an identity header and paired attributes.
func RenderAlbum(album client.Album, fields []AlbumField) string {
	attributes := make([]detailAttribute, 0, len(fields))
	for _, field := range fields {
		if field != AlbumID && field != AlbumName {
			attributes = append(attributes, detailAttribute{key: detailKey(string(field)), value: albumCell(album, field)})
		}
	}
	return renderDetail(album.ID, album.Name, attributes)
}

// RenderArtist renders one artist as an identity header and paired attributes.
func RenderArtist(artist client.Artist, fields []ArtistField) string {
	attributes := make([]detailAttribute, 0, len(fields))
	for _, field := range fields {
		if field != ArtistID && field != ArtistName {
			attributes = append(attributes, detailAttribute{key: detailKey(string(field)), value: artistCell(artist, field)})
		}
	}
	return renderDetail(artist.ID, artist.Name, attributes)
}

type detailAttribute struct {
	key   string
	value string
}

func renderDetail(id, name string, attributes []detailAttribute) string {
	var rendered strings.Builder
	_, _ = fmt.Fprintf(&rendered, "%s  %s\n", cell(id), cell(name))
	for index := 0; index < len(attributes); index += 2 {
		_, _ = fmt.Fprintf(&rendered, "%s: %s", attributes[index].key, attributes[index].value)
		if index+1 < len(attributes) {
			_, _ = fmt.Fprintf(&rendered, "   %s: %s", attributes[index+1].key, attributes[index+1].value)
		}
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

func detailKey(field string) string {
	words := strings.Split(field, "_")
	for index, word := range words {
		switch word {
		case "ID", "IDS", "URI", "URL":
			if word == "IDS" {
				words[index] = "IDs"
			}
		default:
			word = strings.ToLower(word)
			words[index] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

func renderArtwork(images []client.Image) string {
	values := make([]string, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.URL) != "" {
			values = append(values, dimension(image.Width)+"x"+dimension(image.Height)+" "+sanitize(image.URL))
		}
	}
	return cell(strings.Join(values, ","))
}
