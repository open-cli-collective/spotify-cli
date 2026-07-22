// Package output renders typed Spotify values without performing I/O.
package output

import (
	"fmt"
	"slices"
	"strings"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

// MeResult is the control-plane JSON shape for the current identity.
type MeResult struct {
	AccountID   string   `json:"account_id"`
	DisplayName string   `json:"display_name"`
	SpotifyID   string   `json:"spotify_id"`
	URI         string   `json:"uri"`
	Scopes      []string `json:"scopes"`
}

// NewMeResult creates a stable identity result with sorted scopes.
func NewMeResult(user client.User, scopes []string) MeResult {
	scopes = append([]string(nil), scopes...)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	return MeResult{
		AccountID: user.AccountID, DisplayName: user.DisplayName,
		SpotifyID: user.ID, URI: user.URI, Scopes: scopes,
	}
}

// RenderMeText renders the token-efficient text identity shape.
func RenderMeText(user client.User, scopes []string) string {
	result := NewMeResult(user, scopes)
	var rendered strings.Builder
	for _, value := range [][2]string{
		{"account_id", result.AccountID},
		{"display_name", result.DisplayName},
		{"spotify_id", result.SpotifyID},
		{"uri", result.URI},
		{"scopes", strings.Join(result.Scopes, ",")},
	} {
		if value[1] == "" {
			value[1] = "-"
		}
		_, _ = fmt.Fprintf(&rendered, "%s\t%s\n", value[0], value[1])
	}
	return rendered.String()
}
