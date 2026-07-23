package output

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

// TrackField identifies one stable track table column.
type TrackField string

// Stable track output fields.
const (
	TrackID          TrackField = "ID"
	TrackName        TrackField = "TRACK"
	TrackArtistIDs   TrackField = "ARTIST_IDS"
	TrackArtists     TrackField = "ARTISTS"
	TrackAlbumID     TrackField = "ALBUM_ID"
	TrackAlbum       TrackField = "ALBUM"
	TrackDuration    TrackField = "DURATION"
	TrackURI         TrackField = "URI"
	TrackURL         TrackField = "URL"
	TrackDiscNumber  TrackField = "DISC_NUMBER"
	TrackTrackNumber TrackField = "TRACK_NUMBER"
	TrackExplicit    TrackField = "EXPLICIT"
	TrackRestriction TrackField = "RESTRICTION"
	TrackArtwork     TrackField = "ARTWORK"
	TrackAddedAt     TrackField = "ADDED_AT"
)

var (
	defaultTrackFields      = []TrackField{TrackID, TrackName, TrackArtistIDs, TrackArtists, TrackAlbumID, TrackAlbum, TrackDuration}
	defaultAlbumTrackFields = []TrackField{TrackID, TrackName, TrackArtistIDs, TrackArtists, TrackDuration}
	extendedTrackFields     = []TrackField{TrackURI, TrackURL, TrackDiscNumber, TrackTrackNumber, TrackExplicit, TrackRestriction}
	allTrackFields          = append(append(append([]TrackField(nil), defaultTrackFields...), extendedTrackFields...), TrackArtwork)
	allAlbumTrackFields     = append(append([]TrackField(nil), defaultAlbumTrackFields...), extendedTrackFields...)
	defaultSavedTrackFields = append([]TrackField{TrackAddedAt}, defaultTrackFields...)
	allSavedTrackFields     = append([]TrackField{TrackAddedAt}, allTrackFields...)
)

// SelectTrackFields applies the family-wide default, widening, then override precedence.
func SelectTrackFields(csv string, extended, artwork bool) ([]TrackField, error) {
	fields := append([]TrackField(nil), defaultTrackFields...)
	if extended {
		fields = append(fields, extendedTrackFields...)
	}
	if artwork {
		fields = append(fields, TrackArtwork)
	}
	return selectTrackFields(csv, fields, allTrackFields)
}

// SelectSavedTrackFields applies saved-track list field precedence.
func SelectSavedTrackFields(csv string, extended, artwork bool) ([]TrackField, error) {
	fields := append([]TrackField(nil), defaultSavedTrackFields...)
	if extended {
		fields = append(fields, extendedTrackFields...)
	}
	if artwork {
		fields = append(fields, TrackArtwork)
	}
	return selectTrackFields(csv, fields, allSavedTrackFields)
}

// SelectAlbumTrackFields selects only fields present in simplified album-track responses.
func SelectAlbumTrackFields(csv string, extended bool) ([]TrackField, error) {
	fields := append([]TrackField(nil), defaultAlbumTrackFields...)
	if extended {
		fields = append(fields, extendedTrackFields...)
	}
	return selectTrackFields(csv, fields, allAlbumTrackFields)
}

func selectTrackFields(csv string, defaults, allowed []TrackField) ([]TrackField, error) {
	if strings.TrimSpace(csv) == "" {
		return defaults, nil
	}
	var fields []TrackField
	seen := map[TrackField]bool{}
	for _, raw := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		name := TrackField(strings.ToUpper(trimmed))
		if !validTrackField(name, allowed) {
			return nil, fmt.Errorf("unknown track field %q; valid fields: %s", trimmed, trackFieldNames(allowed))
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

// RenderTracks renders one pipe-delimited table, including the header for an empty page.
func RenderTracks(tracks []client.Track, fields []TrackField) string {
	var rendered strings.Builder
	headers := make([]string, len(fields))
	for index, field := range fields {
		headers[index] = string(field)
	}
	rendered.WriteString(strings.Join(headers, " | "))
	rendered.WriteByte('\n')
	for _, track := range tracks {
		cells := make([]string, len(fields))
		for index, field := range fields {
			cells[index] = trackCell(track, field)
		}
		rendered.WriteString(strings.Join(cells, " | "))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderTrackIDs renders one primary identifier per line without a header.
func RenderTrackIDs(tracks []client.Track) string {
	var rendered strings.Builder
	for _, track := range tracks {
		rendered.WriteString(cell(track.ID))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderSavedTracks renders saved tracks with their library timestamps.
func RenderSavedTracks(items []client.SavedTrack, fields []TrackField) string {
	var rendered strings.Builder
	headers := make([]string, len(fields))
	for index, field := range fields {
		headers[index] = string(field)
	}
	rendered.WriteString(strings.Join(headers, " | "))
	rendered.WriteByte('\n')
	for _, item := range items {
		cells := make([]string, len(fields))
		for index, field := range fields {
			if field == TrackAddedAt {
				cells[index] = cell(item.AddedAt)
			} else {
				cells[index] = trackCell(item.Track, field)
			}
		}
		rendered.WriteString(strings.Join(cells, " | "))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderSavedTrackIDs renders one saved track ID per line.
func RenderSavedTrackIDs(items []client.SavedTrack) string {
	tracks := make([]client.Track, len(items))
	for index, item := range items {
		tracks[index] = item.Track
	}
	return RenderTrackIDs(tracks)
}

// RenderTrack renders one track as an identity header and paired attributes.
func RenderTrack(track client.Track, fields []TrackField) string {
	attributes := make([]detailAttribute, 0, len(fields))
	for _, field := range fields {
		if field != TrackID && field != TrackName {
			attributes = append(attributes, detailAttribute{key: detailKey(string(field)), value: trackCell(track, field)})
		}
	}
	return renderDetail(track.ID, track.Name, attributes)
}

func trackCell(track client.Track, field TrackField) string {
	switch field {
	case TrackID:
		return cell(track.ID)
	case TrackName:
		return cell(track.Name)
	case TrackArtistIDs:
		return cell(joinArtists(track.Artists, func(artist client.Artist) string { return artist.ID }))
	case TrackArtists:
		return cell(joinArtists(track.Artists, func(artist client.Artist) string { return artist.Name }))
	case TrackAlbumID:
		return cell(track.Album.ID)
	case TrackAlbum:
		return cell(track.Album.Name)
	case TrackDuration:
		return duration(track.DurationMS)
	case TrackURI:
		return cell(track.URI)
	case TrackURL:
		return cell(track.ExternalURLs.Spotify)
	case TrackDiscNumber:
		return positiveInt(track.DiscNumber)
	case TrackTrackNumber:
		return positiveInt(track.TrackNumber)
	case TrackExplicit:
		return strconv.FormatBool(track.Explicit)
	case TrackRestriction:
		return cell(track.Restrictions.Reason)
	case TrackArtwork:
		return renderArtwork(track.Album.Images)
	case TrackAddedAt:
		return "-"
	default:
		return "-"
	}
}

func joinArtists(artists []client.Artist, value func(client.Artist) string) string {
	result := make([]string, 0, len(artists))
	for _, artist := range artists {
		result = append(result, sanitize(value(artist)))
	}
	return strings.Join(result, ",")
}

func duration(milliseconds int) string {
	if milliseconds < 0 {
		milliseconds = 0
	}
	total := milliseconds / 1000
	if total >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", total/3600, total%3600/60, total%60)
	}
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func positiveInt(value int) string {
	if value <= 0 {
		return "-"
	}
	return strconv.Itoa(value)
}

func dimension(value *int) string {
	if value == nil {
		return "-"
	}
	return strconv.Itoa(*value)
}

func cell(value string) string {
	value = sanitize(value)
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func sanitize(value string) string {
	value = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(value)
	return strings.ReplaceAll(value, " | ", " ")
}

func validTrackField(field TrackField, allowed []TrackField) bool {
	for _, candidate := range allowed {
		if candidate == field {
			return true
		}
	}
	return false
}

func trackFieldNames(allowed []TrackField) string {
	values := make([]string, len(allowed))
	for index, field := range allowed {
		values[index] = string(field)
	}
	return strings.Join(values, ", ")
}
