// Package spotifyref parses typed Spotify catalog references without network access.
package spotifyref

import (
	"errors"
	"net/url"
	"strings"
)

// Kind is one supported Spotify catalog resource kind.
type Kind string

// Supported catalog resource kinds.
const (
	Track  Kind = "track"
	Album  Kind = "album"
	Artist Kind = "artist"
)

var errInvalid = errors.New("invalid Spotify catalog reference")

// Parse returns the ID from a raw ID, Spotify URI, or canonical Spotify URL.
func Parse(value string, kind Kind) (string, error) {
	if ValidID(value) {
		return value, nil
	}
	if prefix := "spotify:" + string(kind) + ":"; strings.HasPrefix(value, prefix) && ValidID(strings.TrimPrefix(value, prefix)) {
		return strings.TrimPrefix(value, prefix), nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "open.spotify.com" || parsed.User != nil || strings.ContainsAny(value, "?#") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errInvalid
	}
	prefix := "/" + string(kind) + "/"
	if !strings.HasPrefix(parsed.Path, prefix) || parsed.EscapedPath() != parsed.Path {
		return "", errInvalid
	}
	id := strings.TrimPrefix(parsed.Path, prefix)
	if !ValidID(id) {
		return "", errInvalid
	}
	return id, nil
}

// ValidID reports whether value has Spotify's canonical catalog ID shape.
func ValidID(value string) bool {
	if len(value) != 22 {
		return false
	}
	for index := range value {
		character := value[index]
		if character < '0' || character > '9' {
			if character < 'A' || character > 'Z' {
				if character < 'a' || character > 'z' {
					return false
				}
			}
		}
	}
	return true
}
