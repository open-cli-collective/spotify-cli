package client

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
)

func TestListPlaylistItemsUsesCurrentFixedPathAndDecodesMixedItems(t *testing.T) {
	const (
		trackID   = "abcdefghijklmnopqrstuv"
		episodeID = "ZYXWVUTSRQPONMLKJIHGFE"
		artistID  = "1111111111111111111111"
		albumID   = "2222222222222222222222"
	)
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.URL.Host != "api.spotify.invalid" ||
			request.URL.Path != "/v1/playlists/"+playlistID+"/items" || query.Get("additional_types") != "episode" ||
			query.Get("limit") != "6" || query.Get("offset") != "20" || len(query) != 3 {
			t.Fatalf("request=%s %s", request.Method, request.URL.String())
		}
		return response(http.StatusOK, `{"items":[
			{"added_at":"2026-07-24T12:00:00Z","added_by":{"id":"owner-1"},"is_local":false,"item":{"type":"track","id":"`+trackID+`","name":"Song","artists":[{"id":"`+artistID+`","name":"Artist"}],"album":{"id":"`+albumID+`","name":"Album","images":[{"url":"https://track-image"}]},"duration_ms":61000,"uri":"spotify:track:`+trackID+`","external_urls":{"spotify":"https://open.spotify.com/track/`+trackID+`"},"disc_number":1,"track_number":2,"explicit":true,"restrictions":{"reason":"market"}}},
			{"is_local":false,"item":{"type":"episode","id":"`+episodeID+`","name":"Episode","duration_ms":62000,"uri":"spotify:episode:`+episodeID+`","external_urls":{"spotify":"https://open.spotify.com/episode/`+episodeID+`"},"explicit":false,"images":[{"url":"https://episode-image"}]}},
			{"is_local":true,"item":{"type":"track","name":"Local song","duration_ms":63000,"uri":"spotify:local:Artist:Album:Song:63"}},
			{"is_local":false,"item":null},
			{"is_local":false,"item":{"type":"audiobook","name":"Future | item\nline","duration_ms":64000}},
			{"is_local":false,"item":{"name":"Untyped"}}
		],"limit":6,"offset":20,"total":27,"next":"https://evil.invalid/follow"}`), nil
	})}
	page, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).ListPlaylistItems(context.Background(), playlistID, 6, 20)
	if err != nil || calls != 1 || len(page.Items) != 6 || page.Offset != 20 || page.Total != 27 || !page.HasNext {
		t.Fatalf("page=%+v calls=%d error=%v", page, calls, err)
	}
	track, episode, local, unavailable, unknown, untyped := page.Items[0], page.Items[1], page.Items[2], page.Items[3], page.Items[4], page.Items[5]
	if track.Type != "track" || track.ID != trackID || track.Name != "Song" || track.AlbumID != albumID ||
		len(track.Artists) != 1 || track.Artists[0].ID != artistID || track.DurationMS == nil || *track.DurationMS != 61000 ||
		track.AddedByID != "owner-1" || track.Explicit == nil || !*track.Explicit || track.Restriction != "market" || len(track.Images) != 1 {
		t.Fatalf("track=%+v", track)
	}
	if episode.Type != "episode" || episode.ID != episodeID || episode.Name != "Episode" || len(episode.Images) != 1 {
		t.Fatalf("episode=%+v", episode)
	}
	if local.Type != "local" || local.ID != "" || local.Name != "Local song" || local.DurationMS == nil || *local.DurationMS != 63000 {
		t.Fatalf("local=%+v", local)
	}
	if unavailable.Type != "unavailable" || unavailable.ID != "" || unknown.Type != "audiobook" || unknown.Name != "Future | item\nline" ||
		untyped.Type != "unknown" || untyped.Name != "Untyped" {
		t.Fatalf("unavailable=%+v unknown=%+v untyped=%+v", unavailable, unknown, untyped)
	}
}

func TestListPlaylistItemsAcceptsBoundsAndRejectsMalformedData(t *testing.T) {
	for _, limit := range []int{1, 50} {
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, `{"items":[],"limit":`+strconv.Itoa(limit)+`,"offset":0,"total":0,"next":null}`), nil
		})}
		if _, err := (Client{HTTPClient: httpClient}).ListPlaylistItems(context.Background(), playlistID, limit, 0); err != nil {
			t.Fatalf("limit=%d error=%v", limit, err)
		}
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"items":[],"limit":1,"offset":10,"total":3,"next":null}`), nil
	})}
	if _, err := (Client{HTTPClient: httpClient}).ListPlaylistItems(context.Background(), playlistID, 1, 10); err != nil {
		t.Fatalf("empty page beyond total error=%v", err)
	}

	for _, body := range []string{
		`{}`,
		`{"items":null,"limit":1,"offset":0,"total":0}`,
		`{"items":[],"limit":2,"offset":0,"total":0}`,
		`{"items":[],"limit":1,"offset":1,"total":0}`,
		`{"items":[],"limit":1,"offset":0,"total":-1}`,
		`{"items":[{"item":null},{"item":null}],"limit":1,"offset":0,"total":2}`,
		`{"items":[{"item":null}],"limit":1,"offset":0,"total":1,"next":"https://evil.invalid/follow"}`,
		`{"items":[{"item":null}],"limit":1,"offset":0,"total":2,"next":null}`,
		`{"items":[{"item":null}],"limit":1,"offset":1,"total":1,"next":null}`,
		`{"items":[{}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"bad","duration_ms":1}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","artists":[],"album":{"images":[]}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"episode","images":[]}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"episode","id":"ZYXWVUTSRQPONMLKJIHGFE","duration_ms":-1}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"abcdefghijklmnopqrstuv","artists":null,"album":{"images":[]}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"abcdefghijklmnopqrstuv","artists":[],"album":{"id":"2222222222222222222222","images":[]}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"abcdefghijklmnopqrstuv","artists":[{"id":"1111111111111111111111"}],"album":{"images":[]}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"abcdefghijklmnopqrstuv","artists":[{"id":"1111111111111111111111"}],"album":{"id":"bad","images":[]}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"abcdefghijklmnopqrstuv","artists":[{}],"album":{"id":"2222222222222222222222","images":[]}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"abcdefghijklmnopqrstuv","artists":[{"id":"bad"}],"album":{"id":"2222222222222222222222","images":[]}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"track","id":"abcdefghijklmnopqrstuv","artists":[{"id":"1111111111111111111111"}],"album":{"id":"2222222222222222222222","images":null}}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"item":{"type":"episode","id":"ZYXWVUTSRQPONMLKJIHGFE","images":null}}],"limit":1,"offset":0,"total":1}`,
	} {
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})}
		if _, err := (Client{HTTPClient: httpClient}).ListPlaylistItems(context.Background(), playlistID, 1, 0); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}

	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"items":[{"item":null},{"item":null},{"item":null},{"item":null},{"item":null}],"limit":10,"offset":0,"total":20,"next":"https://evil.invalid/follow"}`), nil
	})}
	if _, err := (Client{HTTPClient: httpClient}).ListPlaylistItems(context.Background(), playlistID, 10, 0); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("short continuation page error=%v", err)
	}
}

func TestListPlaylistItemsRejectsInvalidInputsBeforeRequest(t *testing.T) {
	calls := 0
	spotify := Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, `{}`), nil
	})}}
	for _, call := range []func() error{
		func() error { _, err := spotify.ListPlaylistItems(context.Background(), "bad", 1, 0); return err },
		func() error { _, err := spotify.ListPlaylistItems(context.Background(), playlistID, 0, 0); return err },
		func() error { _, err := spotify.ListPlaylistItems(context.Background(), playlistID, 51, 0); return err },
		func() error { _, err := spotify.ListPlaylistItems(context.Background(), playlistID, 1, -1); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
}
