package output

import (
	"fmt"
	"slices"
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
	allTrackFields          = slices.Concat(defaultTrackFields, extendedTrackFields, []TrackField{TrackArtwork})
	allAlbumTrackFields     = slices.Concat(defaultAlbumTrackFields, extendedTrackFields)
	defaultSavedTrackFields = slices.Concat([]TrackField{TrackAddedAt}, defaultTrackFields)
	allSavedTrackFields     = slices.Concat([]TrackField{TrackAddedAt}, allTrackFields)
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
	return selectFields(csv, fields, allTrackFields, "track")
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
	return selectFields(csv, fields, allSavedTrackFields, "track")
}

// SelectAlbumTrackFields selects only fields present in simplified album-track responses.
func SelectAlbumTrackFields(csv string, extended bool) ([]TrackField, error) {
	fields := append([]TrackField(nil), defaultAlbumTrackFields...)
	if extended {
		fields = append(fields, extendedTrackFields...)
	}
	return selectFields(csv, fields, allAlbumTrackFields, "track")
}

// RenderTracks renders one pipe-delimited table, including the header for an empty page.
func RenderTracks(tracks []client.Track, fields []TrackField) string {
	return renderTable(tracks, fields, trackCell)
}

// RenderTrackIDs renders one primary identifier per line without a header.
func RenderTrackIDs(tracks []client.Track) string {
	return renderIDs(tracks, func(track client.Track) string { return track.ID })
}

// RenderSavedTracks renders saved tracks with their library timestamps.
func RenderSavedTracks(items []client.SavedTrack, fields []TrackField) string {
	return renderTable(items, fields, savedTrackCell)
}

// RenderSavedTrackIDs renders one saved track ID per line.
func RenderSavedTrackIDs(items []client.SavedTrack) string {
	return renderIDs(items, func(item client.SavedTrack) string { return item.Track.ID })
}

func savedTrackCell(item client.SavedTrack, field TrackField) string {
	if field == TrackAddedAt {
		return cell(item.AddedAt)
	}
	return trackCell(item.Track, field)
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
	//nolint:exhaustive // TrackAddedAt is handled by savedTrackCell.
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
