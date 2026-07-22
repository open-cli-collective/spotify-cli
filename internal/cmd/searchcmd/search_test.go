package searchcmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/session"
)

func TestTrackSearchExactStreamsAndPagination(t *testing.T) {
	body := `{"tracks":{"items":[{"id":"track-1","name":"Song","artists":[{"id":"artist-1","name":"Artist"}],"album":{"id":"album-1","name":"Album"},"duration_ms":123000}],"limit":10,"offset":0,"total":11,"next":"ignored-provider-url"}}`
	stdout, stderr, opens, err := executeSearch(body, "track", "hello")
	if err != nil {
		t.Fatal(err)
	}
	wantOut := "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\ntrack-1 | Song | artist-1 | Artist | album-1 | Album | 2:03\n"
	if stdout != wantOut || stderr != "More results available (next: djE6dHJhY2s6MTA)\n" || opens != 1 {
		t.Fatalf("stdout=%q stderr=%q opens=%d", stdout, stderr, opens)
	}
}

func TestTrackSearchUsesContinuationOffset(t *testing.T) {
	var offset string
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		offset = request.URL.Query().Get("offset")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"tracks":{"items":[],"limit":10,"offset":10,"total":10,"next":null}}`))}, nil
	})}
	opener := func(context.Context, string, bool) (Session, error) {
		return session.New(client.Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}, nil, nil), nil
	}
	command := New(Dependencies{OpenSession: opener})
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"track", "q", "--next-page-token", encodePageToken(10)})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if offset != "10" {
		t.Fatalf("offset=%q", offset)
	}
}

func TestTrackSearchOutputShapes(t *testing.T) {
	body := `{"tracks":{"items":[{"id":"track-1","name":"Song","artists":[],"album":{"id":"album-1","images":[{"url":"https://image","width":640,"height":640}]},"duration_ms":0,"uri":"spotify:track:track-1","external_urls":{"spotify":"https://open.spotify.com/track/track-1"},"disc_number":1,"track_number":2,"explicit":true,"restrictions":{"reason":"market"}}],"limit":10,"offset":0,"total":1,"next":null}}`
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "IDs override fields", args: []string{"track", "q", "--id", "--fields", "not-real", "--extended"}, want: "track-1\n"},
		{name: "projection", args: []string{"track", "q", "--fields", "track,album_id"}, want: "TRACK | ALBUM_ID\nSong | album-1\n"},
		{name: "artwork", args: []string{"track", "q", "--fields", "artwork"}, want: "ARTWORK\n640x640 https://image\n"},
		{name: "include artwork", args: []string{"track", "q", "--include-artwork"}, want: "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION | ARTWORK\ntrack-1 | Song | - | - | album-1 | - | 0:00 | 640x640 https://image\n"},
		{name: "extended", args: []string{"track", "q", "--extended"}, want: "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION | URI | URL | DISC_NUMBER | TRACK_NUMBER | EXPLICIT | RESTRICTION\ntrack-1 | Song | - | - | album-1 | - | 0:00 | spotify:track:track-1 | https://open.spotify.com/track/track-1 | 1 | 2 | true | market\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, _, err := executeSearch(body, test.args...)
			if err != nil || stdout != test.want || stderr != "" {
				t.Fatalf("stdout=%q stderr=%q error=%v", stdout, stderr, err)
			}
		})
	}
}

func TestTrackSearchAcceptsMaxOne(t *testing.T) {
	body := `{"tracks":{"items":[],"limit":1,"offset":0,"total":0,"next":null}}`
	stdout, stderr, opens, err := executeSearch(body, "track", " q ", "--max", "1")
	if err != nil || stdout != "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" || stderr != "" || opens != 1 {
		t.Fatalf("stdout=%q stderr=%q opens=%d error=%v", stdout, stderr, opens, err)
	}
}

func TestTrackSearchValidatesBeforeSession(t *testing.T) {
	invalid := [][]string{
		{"track", " "},
		{"track", "q", "--max", "0"},
		{"track", "q", "--max", "11"},
		{"track", "q", "--next-page-token", "invalid"},
		{"track", "q", "--next-page-token", strings.Repeat("a", 65)},
		{"track", "q", "--next-page-token", base64.RawURLEncoding.EncodeToString([]byte("v2:track:1"))},
		{"track", "q", "--next-page-token", base64.RawURLEncoding.EncodeToString([]byte("v1:album:1"))},
		{"track", "q", "--next-page-token", base64.RawURLEncoding.EncodeToString([]byte("v1:track:-1"))},
		{"track", "q", "--next-page-token", base64.RawURLEncoding.EncodeToString([]byte("v1:track:1001"))},
		{"track", "q", "--fields", "invalid"},
	}
	for _, args := range invalid {
		_, _, opens, err := executeSearch(``, args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v error=%v code=%d opens=%d", args, err, exitcode.Code(err), opens)
		}
	}
}

func TestTrackSearchClassifiesAPIFailuresWithoutLeakingBodies(t *testing.T) {
	for _, test := range []struct {
		status int
		code   int
	}{
		{status: http.StatusUnauthorized, code: exitcode.Config},
		{status: http.StatusForbidden, code: exitcode.Config},
		{status: http.StatusInternalServerError, code: exitcode.Upstream},
	} {
		stdout, stderr, opens, err := executeSearchResponse(test.status, "response-body-secret-canary", "track", "q")
		if exitcode.Code(err) != test.code || stdout != "" || stderr != "" || opens != 1 || strings.Contains(err.Error(), "secret-canary") {
			t.Fatalf("status=%d stdout=%q stderr=%q opens=%d error=%v code=%d", test.status, stdout, stderr, opens, err, exitcode.Code(err))
		}
	}
}

func TestTrackSearchDoesNotAdvertiseOffsetPastCeiling(t *testing.T) {
	body := `{"tracks":{"items":[],"limit":10,"offset":1000,"total":2000,"next":"ignored-provider-url"}}`
	stdout, stderr, _, err := executeSearch(body, "track", "q", "--next-page-token", encodePageToken(1000))
	if err != nil || stdout != "ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
}

func executeSearch(body string, args ...string) (string, string, int, error) {
	return executeSearchResponse(http.StatusOK, body, args...)
}

func executeSearchResponse(status int, body string, args ...string) (string, string, int, error) {
	opens := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	opener := func(context.Context, string, bool) (Session, error) {
		opens++
		return session.New(client.Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}, nil, nil), nil
	}
	command := New(Dependencies{OpenSession: opener})
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), opens, err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
