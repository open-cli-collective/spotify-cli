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
