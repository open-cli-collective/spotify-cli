package spotifyref

import "testing"

func TestParse(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		name  string
		kind  Kind
		value string
		want  string
	}{
		{name: "raw track ID", kind: Track, value: id, want: id},
		{name: "album URI", kind: Album, value: "spotify:album:" + id, want: id},
		{name: "artist URL", kind: Artist, value: "https://open.spotify.com/artist/" + id, want: id},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.value, test.kind)
			if err != nil || got != test.want {
				t.Fatalf("Parse() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestParseRejectsMalformedWrongKindAndHostileURLs(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, value := range []string{
		"", "short", "0123456789ABCDEFGHIJK!", " spotify:track:" + id,
		"spotify:album:" + id, "spotify:track:" + id + ":extra",
		"http://open.spotify.com/track/" + id,
		"https://evil.example/track/" + id,
		"https://open.spotify.com.evil.example/track/" + id,
		"https://user@open.spotify.com/track/" + id,
		"https://open.spotify.com:443/track/" + id,
		"https://open.spotify.com/album/" + id,
		"https://open.spotify.com/track/" + id + "/extra",
		"https://open.spotify.com/track/" + id + "?si=secret",
		"https://open.spotify.com/track/" + id + "?",
		"https://open.spotify.com/track/" + id + "#fragment",
		"https://open.spotify.com/track/" + id + "#",
		"https://open.spotify.com/track/%30" + id[1:],
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := Parse(value, Track); err == nil {
				t.Fatalf("Parse(%q) succeeded", value)
			}
		})
	}
}
