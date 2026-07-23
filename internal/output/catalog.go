package output

import (
	"fmt"
	"strings"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

// AlbumField identifies one stable album table column.
type AlbumField string

// Stable album output fields.
const (
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
	defaultAlbumFields  = []AlbumField{AlbumID, AlbumName, AlbumArtistIDs, AlbumArtists, AlbumReleaseDate, AlbumTotalTracks}
	extendedAlbumFields = []AlbumField{AlbumURI, AlbumURL, AlbumType, AlbumReleaseDatePrecision, AlbumRestriction}
	allAlbumFields      = append(append(append([]AlbumField(nil), defaultAlbumFields...), extendedAlbumFields...), AlbumArtwork)
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
	if strings.TrimSpace(csv) == "" {
		return fields, nil
	}
	defaults := fields
	fields = nil
	seen := map[AlbumField]bool{}
	for _, raw := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		name := AlbumField(strings.ToUpper(trimmed))
		if !validAlbumField(name) {
			return nil, fmt.Errorf("unknown album field %q; valid fields: %s", trimmed, albumFieldNames())
		}
		if !seen[name] {
			fields = append(fields, name)
			seen[name] = true
		}
	}
	if len(fields) == 0 {
		return defaults, nil
	}
	return fields, nil
}

// RenderAlbums renders one pipe-delimited table, including the header for an empty page.
func RenderAlbums(albums []client.Album, fields []AlbumField) string {
	var rendered strings.Builder
	headers := make([]string, len(fields))
	for index, field := range fields {
		headers[index] = string(field)
	}
	rendered.WriteString(strings.Join(headers, " | "))
	rendered.WriteByte('\n')
	for _, album := range albums {
		cells := make([]string, len(fields))
		for index, field := range fields {
			cells[index] = albumCell(album, field)
		}
		rendered.WriteString(strings.Join(cells, " | "))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderAlbumIDs renders one primary identifier per line without a header.
func RenderAlbumIDs(albums []client.Album) string {
	var rendered strings.Builder
	for _, album := range albums {
		rendered.WriteString(cell(album.ID))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

func albumCell(album client.Album, field AlbumField) string {
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

func validAlbumField(field AlbumField) bool {
	for _, candidate := range allAlbumFields {
		if candidate == field {
			return true
		}
	}
	return false
}

func albumFieldNames() string {
	values := make([]string, len(allAlbumFields))
	for index, field := range allAlbumFields {
		values[index] = string(field)
	}
	return strings.Join(values, ", ")
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
	allArtistFields      = append(append(append([]ArtistField(nil), defaultArtistFields...), extendedArtistFields...), ArtistArtwork)
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
	if strings.TrimSpace(csv) == "" {
		return fields, nil
	}
	defaults := fields
	fields = nil
	seen := map[ArtistField]bool{}
	for _, raw := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		name := ArtistField(strings.ToUpper(trimmed))
		if !validArtistField(name) {
			return nil, fmt.Errorf("unknown artist field %q; valid fields: %s", trimmed, artistFieldNames())
		}
		if !seen[name] {
			fields = append(fields, name)
			seen[name] = true
		}
	}
	if len(fields) == 0 {
		return defaults, nil
	}
	return fields, nil
}

// RenderArtists renders one pipe-delimited table, including the header for an empty page.
func RenderArtists(artists []client.Artist, fields []ArtistField) string {
	var rendered strings.Builder
	headers := make([]string, len(fields))
	for index, field := range fields {
		headers[index] = string(field)
	}
	rendered.WriteString(strings.Join(headers, " | "))
	rendered.WriteByte('\n')
	for _, artist := range artists {
		cells := make([]string, len(fields))
		for index, field := range fields {
			cells[index] = artistCell(artist, field)
		}
		rendered.WriteString(strings.Join(cells, " | "))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderArtistIDs renders one primary identifier per line without a header.
func RenderArtistIDs(artists []client.Artist) string {
	var rendered strings.Builder
	for _, artist := range artists {
		rendered.WriteString(cell(artist.ID))
		rendered.WriteByte('\n')
	}
	return rendered.String()
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

func validArtistField(field ArtistField) bool {
	for _, candidate := range allArtistFields {
		if candidate == field {
			return true
		}
	}
	return false
}

func artistFieldNames() string {
	values := make([]string, len(allArtistFields))
	for index, field := range allArtistFields {
		values[index] = string(field)
	}
	return strings.Join(values, ", ")
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
