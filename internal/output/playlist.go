package output

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

// PlaylistField identifies one stable playlist table column.
type PlaylistField string

// Stable playlist output fields.
const (
	PlaylistID            PlaylistField = "ID"
	PlaylistName          PlaylistField = "PLAYLIST"
	PlaylistOwnerID       PlaylistField = "OWNER_ID"
	PlaylistOwner         PlaylistField = "OWNER"
	PlaylistItemCount     PlaylistField = "ITEM_COUNT"
	PlaylistPublic        PlaylistField = "PUBLIC"
	PlaylistCollaborative PlaylistField = "COLLABORATIVE"
	PlaylistURI           PlaylistField = "URI"
	PlaylistURL           PlaylistField = "URL"
	PlaylistSnapshotID    PlaylistField = "SNAPSHOT_ID"
	PlaylistDescription   PlaylistField = "DESCRIPTION"
	PlaylistArtwork       PlaylistField = "ARTWORK"
)

var (
	defaultPlaylistFields  = []PlaylistField{PlaylistID, PlaylistName, PlaylistOwnerID, PlaylistOwner, PlaylistItemCount, PlaylistPublic, PlaylistCollaborative}
	extendedPlaylistFields = []PlaylistField{PlaylistURI, PlaylistURL, PlaylistSnapshotID, PlaylistDescription}
	allPlaylistFields      = append(append(append([]PlaylistField(nil), defaultPlaylistFields...), extendedPlaylistFields...), PlaylistArtwork)
)

// SelectPlaylistFields applies default, widening, artwork, then explicit-field precedence.
func SelectPlaylistFields(csv string, extended, includeArtwork bool) ([]PlaylistField, error) {
	fields := append([]PlaylistField(nil), defaultPlaylistFields...)
	if extended {
		fields = append(fields, extendedPlaylistFields...)
	}
	if includeArtwork {
		fields = append(fields, PlaylistArtwork)
	}
	if strings.TrimSpace(csv) == "" {
		return fields, nil
	}
	defaults := fields
	fields = nil
	seen := map[PlaylistField]bool{}
	for _, raw := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		field := PlaylistField(strings.ToUpper(trimmed))
		if !containsPlaylistField(field) {
			return nil, fmt.Errorf("unknown playlist field %q; valid fields: %s", trimmed, playlistFieldNames())
		}
		if !seen[field] {
			fields = append(fields, field)
			seen[field] = true
		}
	}
	if len(fields) == 0 {
		return defaults, nil
	}
	return fields, nil
}

// RenderPlaylists renders one pipe-delimited table, including the header for an empty page.
func RenderPlaylists(playlists []client.Playlist, fields []PlaylistField) string {
	var rendered strings.Builder
	headers := make([]string, len(fields))
	for index, field := range fields {
		headers[index] = string(field)
	}
	rendered.WriteString(strings.Join(headers, " | "))
	rendered.WriteByte('\n')
	for _, playlist := range playlists {
		cells := make([]string, len(fields))
		for index, field := range fields {
			cells[index] = playlistCell(playlist, field)
		}
		rendered.WriteString(strings.Join(cells, " | "))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderPlaylistIDs renders one primary identifier per line without a header.
func RenderPlaylistIDs(playlists []client.Playlist) string {
	var rendered strings.Builder
	for _, playlist := range playlists {
		rendered.WriteString(cell(playlist.ID))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderPlaylist renders one playlist as an identity header and paired attributes.
func RenderPlaylist(playlist client.Playlist, fields []PlaylistField) string {
	attributes := make([]detailAttribute, 0, len(fields))
	for _, field := range fields {
		if field != PlaylistID && field != PlaylistName {
			attributes = append(attributes, detailAttribute{key: detailKey(string(field)), value: playlistCell(playlist, field)})
		}
	}
	return renderDetail(playlist.ID, playlist.Name, attributes)
}

func playlistCell(playlist client.Playlist, field PlaylistField) string {
	switch field {
	case PlaylistID:
		return cell(playlist.ID)
	case PlaylistName:
		return cell(playlist.Name)
	case PlaylistOwnerID:
		return cell(playlist.Owner.ID)
	case PlaylistOwner:
		return cell(playlist.Owner.DisplayName)
	case PlaylistItemCount:
		if playlist.ItemCount == nil {
			return "-"
		}
		return strconv.Itoa(playlist.ItemCount.Total)
	case PlaylistPublic:
		if playlist.Public == nil {
			return "-"
		}
		return strconv.FormatBool(*playlist.Public)
	case PlaylistCollaborative:
		return strconv.FormatBool(playlist.Collaborative)
	case PlaylistURI:
		return cell(playlist.URI)
	case PlaylistURL:
		return cell(playlist.ExternalURLs.Spotify)
	case PlaylistSnapshotID:
		return cell(playlist.SnapshotID)
	case PlaylistDescription:
		return cell(playlist.Description)
	case PlaylistArtwork:
		return renderArtwork(playlist.Images)
	default:
		return "-"
	}
}

func containsPlaylistField(field PlaylistField) bool {
	for _, candidate := range allPlaylistFields {
		if candidate == field {
			return true
		}
	}
	return false
}

func playlistFieldNames() string {
	values := make([]string, len(allPlaylistFields))
	for index, field := range allPlaylistFields {
		values[index] = string(field)
	}
	return strings.Join(values, ", ")
}

// PlaylistItemField identifies one stable playlist-item table column.
type PlaylistItemField string

// Stable playlist-item output fields.
const (
	PlaylistItemPosition    PlaylistItemField = "POSITION"
	PlaylistItemType        PlaylistItemField = "TYPE"
	PlaylistItemID          PlaylistItemField = "ID"
	PlaylistItemName        PlaylistItemField = "ITEM"
	PlaylistItemArtistIDs   PlaylistItemField = "ARTIST_IDS"
	PlaylistItemArtists     PlaylistItemField = "ARTISTS"
	PlaylistItemAlbumID     PlaylistItemField = "ALBUM_ID"
	PlaylistItemAlbum       PlaylistItemField = "ALBUM"
	PlaylistItemDuration    PlaylistItemField = "DURATION"
	PlaylistItemURI         PlaylistItemField = "URI"
	PlaylistItemURL         PlaylistItemField = "URL"
	PlaylistItemAddedAt     PlaylistItemField = "ADDED_AT"
	PlaylistItemAddedByID   PlaylistItemField = "ADDED_BY_ID"
	PlaylistItemDiscNumber  PlaylistItemField = "DISC_NUMBER"
	PlaylistItemTrackNumber PlaylistItemField = "TRACK_NUMBER"
	PlaylistItemExplicit    PlaylistItemField = "EXPLICIT"
	PlaylistItemRestriction PlaylistItemField = "RESTRICTION"
	PlaylistItemArtwork     PlaylistItemField = "ARTWORK"
)

var (
	defaultPlaylistItemFields = []PlaylistItemField{
		PlaylistItemPosition, PlaylistItemType, PlaylistItemID, PlaylistItemName,
		PlaylistItemArtistIDs, PlaylistItemArtists, PlaylistItemAlbumID, PlaylistItemAlbum, PlaylistItemDuration,
	}
	extendedPlaylistItemFields = []PlaylistItemField{
		PlaylistItemURI, PlaylistItemURL, PlaylistItemAddedAt, PlaylistItemAddedByID,
		PlaylistItemDiscNumber, PlaylistItemTrackNumber, PlaylistItemExplicit, PlaylistItemRestriction,
	}
	allPlaylistItemFields = append(append(append([]PlaylistItemField(nil), defaultPlaylistItemFields...), extendedPlaylistItemFields...), PlaylistItemArtwork)
)

// SelectPlaylistItemFields applies default, widening, artwork, then explicit-field precedence.
func SelectPlaylistItemFields(csv string, extended, includeArtwork bool) ([]PlaylistItemField, error) {
	fields := append([]PlaylistItemField(nil), defaultPlaylistItemFields...)
	if extended {
		fields = append(fields, extendedPlaylistItemFields...)
	}
	if includeArtwork {
		fields = append(fields, PlaylistItemArtwork)
	}
	if strings.TrimSpace(csv) == "" {
		return fields, nil
	}
	defaults := fields
	fields = nil
	seen := map[PlaylistItemField]bool{}
	for _, raw := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		field := PlaylistItemField(strings.ToUpper(trimmed))
		if !containsPlaylistItemField(field) {
			return nil, fmt.Errorf("unknown playlist item field %q; valid fields: %s", trimmed, playlistItemFieldNames())
		}
		if !seen[field] {
			fields = append(fields, field)
			seen[field] = true
		}
	}
	if len(fields) == 0 {
		return defaults, nil
	}
	return fields, nil
}

// RenderPlaylistItems renders one ordered pipe-delimited table.
func RenderPlaylistItems(items []client.PlaylistItem, offset int, fields []PlaylistItemField) string {
	var rendered strings.Builder
	headers := make([]string, len(fields))
	for index, field := range fields {
		headers[index] = string(field)
	}
	rendered.WriteString(strings.Join(headers, " | "))
	rendered.WriteByte('\n')
	for index, item := range items {
		cells := make([]string, len(fields))
		for fieldIndex, field := range fields {
			cells[fieldIndex] = playlistItemCell(item, offset+index, field)
		}
		rendered.WriteString(strings.Join(cells, " | "))
		rendered.WriteByte('\n')
	}
	return rendered.String()
}

// RenderPlaylistItemIDs renders only present Spotify item IDs.
func RenderPlaylistItemIDs(items []client.PlaylistItem) string {
	var rendered strings.Builder
	for _, item := range items {
		if item.ID != "" {
			rendered.WriteString(cell(item.ID))
			rendered.WriteByte('\n')
		}
	}
	return rendered.String()
}

// RenderPlaylistItemsAdded renders one reversible playlist-add record.
func RenderPlaylistItemsAdded(playlistID string, position, count int, snapshotID string) string {
	return strings.Join([]string{"added", mutationCell(playlistID), strconv.Itoa(position), strconv.Itoa(count), mutationCell(snapshotID)}, "\t") + "\n"
}

// RenderPlaylistItemRemoved renders one reversible playlist-remove record.
func RenderPlaylistItemRemoved(playlistID string, position int, trackID, snapshotID string) string {
	return strings.Join([]string{"removed", mutationCell(playlistID), strconv.Itoa(position), mutationCell(trackID), mutationCell(snapshotID)}, "\t") + "\n"
}

// RenderPlaylistItemUpdated renders one reversible playlist-replacement record.
func RenderPlaylistItemUpdated(playlistID string, position int, oldTrackID, newTrackID, snapshotID string) string {
	return strings.Join([]string{"updated", mutationCell(playlistID), strconv.Itoa(position), mutationCell(oldTrackID), mutationCell(newTrackID), mutationCell(snapshotID)}, "\t") + "\n"
}

func mutationCell(value string) string {
	return strings.ReplaceAll(cell(value), "\t", " ")
}

func playlistItemCell(item client.PlaylistItem, position int, field PlaylistItemField) string {
	switch field {
	case PlaylistItemPosition:
		return strconv.Itoa(position)
	case PlaylistItemType:
		return cell(item.Type)
	case PlaylistItemID:
		return cell(item.ID)
	case PlaylistItemName:
		return cell(item.Name)
	case PlaylistItemArtistIDs:
		return cell(joinArtists(item.Artists, func(artist client.Artist) string { return artist.ID }))
	case PlaylistItemArtists:
		return cell(joinArtists(item.Artists, func(artist client.Artist) string { return artist.Name }))
	case PlaylistItemAlbumID:
		return cell(item.AlbumID)
	case PlaylistItemAlbum:
		return cell(item.AlbumName)
	case PlaylistItemDuration:
		if item.DurationMS == nil {
			return "-"
		}
		return duration(*item.DurationMS)
	case PlaylistItemURI:
		return cell(item.URI)
	case PlaylistItemURL:
		return cell(item.URL)
	case PlaylistItemAddedAt:
		return cell(item.AddedAt)
	case PlaylistItemAddedByID:
		return cell(item.AddedByID)
	case PlaylistItemDiscNumber:
		return positiveInt(item.DiscNumber)
	case PlaylistItemTrackNumber:
		return positiveInt(item.TrackNumber)
	case PlaylistItemExplicit:
		if item.Explicit == nil {
			return "-"
		}
		return strconv.FormatBool(*item.Explicit)
	case PlaylistItemRestriction:
		return cell(item.Restriction)
	case PlaylistItemArtwork:
		return renderArtwork(item.Images)
	default:
		return "-"
	}
}

func containsPlaylistItemField(field PlaylistItemField) bool {
	for _, candidate := range allPlaylistItemFields {
		if candidate == field {
			return true
		}
	}
	return false
}

func playlistItemFieldNames() string {
	values := make([]string, len(allPlaylistItemFields))
	for index, field := range allPlaylistItemFields {
		values[index] = string(field)
	}
	return strings.Join(values, ", ")
}
