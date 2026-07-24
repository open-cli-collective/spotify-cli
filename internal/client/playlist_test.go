package client

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
)

const playlistID = "0123456789ABCDEFGHIJKL"

func TestListCurrentUserPlaylistsUsesFixedPathAndValidatesPage(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.Host != "api.spotify.invalid" ||
			request.URL.Path != "/v1/me/playlists" || request.URL.Query().Get("limit") != "1" ||
			request.URL.Query().Get("offset") != "20" || len(request.URL.Query()) != 2 {
			t.Fatalf("request=%s %s", request.Method, request.URL.String())
		}
		return response(http.StatusOK, `{"items":[{"id":"`+playlistID+`","name":"Mix","owner":{"id":"owner-1","display_name":"Ada"},"items":{"total":3},"public":null,"collaborative":true,"description":"desc","uri":"spotify:playlist:`+playlistID+`","external_urls":{"spotify":"https://open.spotify.com/playlist/`+playlistID+`"},"images":[{"url":"https://image","width":640,"height":640}],"snapshot_id":"snapshot"}],"limit":1,"offset":20,"total":21,"next":"https://evil.invalid/follow"}`), nil
	})}
	page, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).ListCurrentUserPlaylists(context.Background(), 1, 20)
	if err != nil || calls != 1 || len(page.Items) != 1 || page.Items[0].ID != playlistID ||
		page.Items[0].Owner.DisplayName != "Ada" || page.Items[0].ItemCount == nil || page.Items[0].ItemCount.Total != 3 ||
		page.Items[0].Public != nil || !page.Items[0].Collaborative || !page.HasNext {
		t.Fatalf("page=%+v calls=%d error=%v", page, calls, err)
	}
}

func TestListCurrentUserPlaylistsAcceptsBoundariesAndRejectsMalformedData(t *testing.T) {
	for _, limit := range []int{1, 50} {
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, `{"items":[],"limit":`+strconv.Itoa(limit)+`,"offset":0,"total":0,"next":null}`), nil
		})}
		if _, err := (Client{HTTPClient: httpClient}).ListCurrentUserPlaylists(context.Background(), limit, 0); err != nil {
			t.Fatalf("limit=%d error=%v", limit, err)
		}
	}

	for _, body := range []string{
		`{}`,
		`{"items":null,"limit":1,"offset":0,"total":0}`,
		`{"items":[],"limit":2,"offset":0,"total":0}`,
		`{"items":[],"limit":1,"offset":1,"total":0}`,
		`{"items":[],"limit":1,"offset":0,"total":-1}`,
		`{"items":[{},{}],"limit":1,"offset":0,"total":2}`,
		`{"items":[{"id":"bad","items":{"total":0}}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"id":"0123456789ABCDEFGHIJKL"}],"limit":1,"offset":0,"total":1}`,
		`{"items":[{"id":"0123456789ABCDEFGHIJKL","items":{"total":-1}}],"limit":1,"offset":0,"total":1}`,
	} {
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})}
		if _, err := (Client{HTTPClient: httpClient}).ListCurrentUserPlaylists(context.Background(), 1, 0); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}

func TestPlaylistReadsRejectInvalidInputsBeforeRequest(t *testing.T) {
	calls := 0
	spotify := Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, `{}`), nil
	})}}
	for _, call := range []func() error{
		func() error { _, err := spotify.ListCurrentUserPlaylists(context.Background(), 0, 0); return err },
		func() error { _, err := spotify.ListCurrentUserPlaylists(context.Background(), 51, 0); return err },
		func() error { _, err := spotify.ListCurrentUserPlaylists(context.Background(), 1, -1); return err },
		func() error { _, err := spotify.GetPlaylist(context.Background(), "bad"); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
}

func TestGetPlaylistUsesFixedPathAndValidatesResponse(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.Host != "api.spotify.invalid" || request.URL.Path != "/v1/playlists/"+playlistID || request.URL.RawQuery != "" {
			t.Fatalf("request=%s %s", request.Method, request.URL.String())
		}
		return response(http.StatusOK, `{"id":"`+playlistID+`","name":"Mix","items":{"total":3},"public":false}`), nil
	})}
	playlist, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).GetPlaylist(context.Background(), playlistID)
	if err != nil || calls != 1 || playlist.ID != playlistID || playlist.Public == nil || *playlist.Public {
		t.Fatalf("playlist=%+v calls=%d error=%v", playlist, calls, err)
	}

	for _, body := range []string{
		`{}`,
		`{"id":"bad","items":{"total":0}}`,
		`{"id":"abcdefghijklmnopqrstuv","items":{"total":0}}`,
		`{"id":"0123456789ABCDEFGHIJKL"}`,
		`{"id":"0123456789ABCDEFGHIJKL","items":{"total":-1}}`,
	} {
		httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})
		if _, err := (Client{HTTPClient: httpClient}).GetPlaylist(context.Background(), playlistID); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}
