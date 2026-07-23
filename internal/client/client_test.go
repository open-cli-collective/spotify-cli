package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMeReturnsStableIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/me" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account_id":"stable-account","display_name":"A Listener","id":"spotify-id","uri":"spotify:user:spotify-id"}`))
	}))
	defer server.Close()

	got, err := (Client{HTTPClient: server.Client(), BaseURL: server.URL + "/v1"}).Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "stable-account" || got.DisplayName != "A Listener" || got.ID != "spotify-id" || got.URI != "spotify:user:spotify-id" {
		t.Fatalf("user = %+v", got)
	}
}

func TestMeClassifiesStatusWithoutReturningBody(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrForbidden},
		{http.StatusTooManyRequests, ErrUpstream},
		{http.StatusInternalServerError, ErrUpstream},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			const sentinel = "response-body-secret-sentinel"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, sentinel)
			}))
			defer server.Close()
			_, err := (Client{HTTPClient: server.Client(), BaseURL: server.URL}).Me(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("error leaked response body: %v", err)
			}
		})
	}
}

func TestMeRejectsMalformedOversizedAndUnstableResponses(t *testing.T) {
	tests := []string{
		`not-json`,
		`{"display_name":"missing account"}`,
		`{"account_id":"` + strings.Repeat("x", maxResponseBytes) + `"}`,
	}
	for _, body := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		_, err := (Client{HTTPClient: server.Client(), BaseURL: server.URL}).Me(context.Background())
		server.Close()
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body length %d: error = %v, want ErrInvalidResponse", len(body), err)
		}
	}
}

func TestMeClassifiesTransportFailure(t *testing.T) {
	cause := errors.New("transport-detail-sentinel")
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}
	_, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).Me(context.Background())
	if !errors.Is(err, ErrUpstream) || !errors.Is(err, cause) || strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("error = %v, want safe ErrUpstream preserving cause", err)
	}
}

func TestMePreservesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	_, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).Me(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestCatalogGetUsesOneExactPath(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		name string
		path string
		get  func(Client) error
	}{
		{name: "track", path: "/v1/tracks/" + id, get: func(value Client) error { _, err := value.GetTrack(context.Background(), id); return err }},
		{name: "album", path: "/v1/albums/" + id, get: func(value Client) error { _, err := value.GetAlbum(context.Background(), id); return err }},
		{name: "artist", path: "/v1/artists/" + id, get: func(value Client) error { _, err := value.GetArtist(context.Background(), id); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				calls++
				if request.Method != http.MethodGet || request.URL.Path != test.path || request.URL.RawQuery != "" {
					t.Fatalf("request = %s %s", request.Method, request.URL.RequestURI())
				}
				_, _ = io.WriteString(w, `{"id":"`+id+`"}`)
			}))
			defer server.Close()
			if err := test.get(Client{HTTPClient: server.Client(), BaseURL: server.URL + "/v1"}); err != nil || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestCatalogGetValidatesInputAndResponse(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, `{}`), nil
	})}
	spotify := Client{HTTPClient: httpClient}
	for _, id := range []string{" ", "short", "0123456789ABCDEFGHIJK!", "0123456789ABCDEFGHIJKL?market=US"} {
		if _, err := spotify.GetTrack(context.Background(), id); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
			t.Fatalf("invalid ID %q error=%v calls=%d", id, err, calls)
		}
	}
	if _, err := spotify.GetAlbum(context.Background(), "0123456789ABCDEFGHIJKL"); !errors.Is(err, ErrInvalidResponse) || calls != 1 {
		t.Fatalf("missing response ID error=%v calls=%d", err, calls)
	}
}

func TestCatalogGetInheritsBoundedTransportBehavior(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		code int
		want error
	}{
		{name: "unauthorized", code: http.StatusUnauthorized, want: ErrUnauthorized},
		{name: "forbidden", code: http.StatusForbidden, want: ErrForbidden},
		{name: "malformed", body: "not-json", code: http.StatusOK, want: ErrInvalidResponse},
		{name: "oversized", body: `{"id":"` + strings.Repeat("x", maxResponseBytes) + `"}`, code: http.StatusOK, want: ErrInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(test.code, test.body), nil
			})}
			_, err := (Client{HTTPClient: httpClient}).GetArtist(context.Background(), "0123456789ABCDEFGHIJKL")
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}

	calls := 0
	spotify := Client{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(http.StatusServiceUnavailable, ""), nil
			}
			return response(http.StatusOK, `{"id":"0123456789ABCDEFGHIJKL"}`), nil
		})},
		Wait: func(context.Context, time.Duration) error { return nil },
	}
	if _, err := spotify.GetTrack(context.Background(), "0123456789ABCDEFGHIJKL"); err != nil || calls != 2 {
		t.Fatalf("retry error=%v calls=%d", err, calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spotify.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	if _, err := spotify.GetTrack(ctx, "0123456789ABCDEFGHIJKL"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestCatalogTraversalUsesExactPathsAndDecodesPages(t *testing.T) {
	const (
		albumID  = "0123456789ABCDEFGHIJKL"
		artistID = "abcdefghijklmnopqrstuv"
	)
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		query := request.URL.Query()
		switch request.URL.Path {
		case "/v1/albums/" + albumID + "/tracks":
			if request.Method != http.MethodGet || query.Get("limit") != "50" || query.Get("offset") != "50" || len(query) != 2 {
				t.Fatalf("album tracks request=%s %s", request.Method, request.URL.RequestURI())
			}
			return response(http.StatusOK, `{"items":[{"id":"track-1","name":"First","artists":[{"id":"artist-1","name":"One"},{"id":"artist-2","name":"Two"}],"disc_number":1,"track_number":1},{"id":"track-2","name":"Second","artists":[{"id":"artist-2","name":"Two"}],"disc_number":2,"track_number":1}],"limit":50,"offset":50,"total":52,"next":"https://evil.invalid/follow-me"}`), nil
		case "/v1/artists/" + artistID + "/albums":
			if request.Method != http.MethodGet || query.Get("limit") != "10" || query.Get("offset") != "10" || len(query) != 2 {
				t.Fatalf("artist albums request=%s %s", request.Method, request.URL.RequestURI())
			}
			return response(http.StatusOK, `{"items":[{"id":"album-1","name":"Album","artists":[{"id":"artist-1","name":"One"},{"id":"artist-2","name":"Two"}],"images":[{"url":"https://image","width":640,"height":640}]}],"limit":10,"offset":10,"total":11,"next":"https://evil.invalid/follow-me"}`), nil
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
			return nil, nil
		}
	})}
	spotify := Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}
	tracks, err := spotify.ListAlbumTracks(context.Background(), albumID, 50, 50)
	if err != nil || len(tracks.Items) != 2 || tracks.Items[0].Artists[1].ID != "artist-2" ||
		tracks.Items[1].DiscNumber != 2 || !tracks.HasNext {
		t.Fatalf("tracks=%+v error=%v", tracks, err)
	}
	albums, err := spotify.ListArtistAlbums(context.Background(), artistID, 10, 10)
	if err != nil || len(albums.Items) != 1 || albums.Items[0].Artists[1].Name != "Two" ||
		albums.Items[0].Images[0].URL != "https://image" || !albums.HasNext || calls != 2 {
		t.Fatalf("albums=%+v calls=%d error=%v", albums, calls, err)
	}
}

func TestCatalogTraversalRejectsInvalidInputsAndPages(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, `{}`), nil
	})}
	spotify := Client{HTTPClient: httpClient}
	for _, call := range []func() error{
		func() error { _, err := spotify.ListAlbumTracks(context.Background(), "bad", 10, 0); return err },
		func() error { _, err := spotify.ListAlbumTracks(context.Background(), id, 51, 0); return err },
		func() error { _, err := spotify.ListAlbumTracks(context.Background(), id, 10, -1); return err },
		func() error { _, err := spotify.ListArtistAlbums(context.Background(), id, 11, 0); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
			t.Fatalf("error=%v calls=%d", err, calls)
		}
	}

	for _, page := range []string{
		`{}`,
		`{"items":null,"limit":1,"offset":0,"total":0}`,
		`{"items":[],"limit":2,"offset":0,"total":0}`,
		`{"items":[],"limit":1,"offset":1,"total":0}`,
		`{"items":[],"limit":1,"offset":0,"total":-1}`,
		`{"items":[{},{}],"limit":1,"offset":0,"total":2}`,
		`{"items":[{"id":" "}],"limit":1,"offset":0,"total":1}`,
	} {
		httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, page), nil
		})
		if _, err := spotify.ListAlbumTracks(context.Background(), id, 1, 0); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("page=%s error=%v", page, err)
		}
		if _, err := spotify.ListArtistAlbums(context.Background(), id, 1, 0); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("page=%s error=%v", page, err)
		}
	}
}

func TestCatalogTraversalInheritsTransportBehavior(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, test := range []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrForbidden},
	} {
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(test.status, "secret-canary"), nil
		})}
		_, err := (Client{HTTPClient: httpClient}).ListArtistAlbums(context.Background(), id, 1, 0)
		if !errors.Is(err, test.want) || strings.Contains(err.Error(), "canary") {
			t.Fatalf("status=%d error=%v", test.status, err)
		}
	}

	calls := 0
	spotify := Client{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(http.StatusServiceUnavailable, ""), nil
			}
			return response(http.StatusOK, `{"items":[],"limit":1,"offset":0,"total":0,"next":null}`), nil
		})},
		Wait: func(context.Context, time.Duration) error { return nil },
	}
	if _, err := spotify.ListAlbumTracks(context.Background(), id, 1, 0); err != nil || calls != 2 {
		t.Fatalf("retry error=%v calls=%d", err, calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spotify.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	if _, err := spotify.ListAlbumTracks(ctx, id, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestSearchTracksEncodesQueryAndDecodesBreadcrumbs(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/search" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("q") != `  artist:"Björk" 東京  ` || query.Get("type") != "track" || query.Get("limit") != "10" || query.Get("offset") != "20" {
			t.Fatalf("query = %v", query)
		}
		return response(http.StatusOK, `{"tracks":{"items":[{"id":"track","name":"Song","artists":[{"id":"artist","name":"Artist"}],"album":{"id":"album","name":"Album","images":[{"url":"https://image","width":640,"height":640}]},"duration_ms":123000}],"limit":10,"offset":20,"total":31,"next":"https://api.spotify.com/v1/search?offset=30"}}`), nil
	})}
	page, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).SearchTracks(context.Background(), `  artist:"Björk" 東京  `, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Artists[0].ID != "artist" || page.Items[0].Album.ID != "album" || !page.HasNext || page.Offset != 20 {
		t.Fatalf("page = %+v", page)
	}
}

func TestSearchAlbumsEncodesQueryAndDecodesBreadcrumbs(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.URL.Host != "api.spotify.invalid" || request.URL.Path != "/v1/search" || query.Get("q") != `artist:"Björk"` || query.Get("type") != "album" || query.Get("limit") != "1" || query.Get("offset") != "10" {
			t.Fatalf("request = %s %s query=%v", request.Method, request.URL.Path, query)
		}
		return response(http.StatusOK, `{"albums":{"items":[{"id":"album","name":"Debut","artists":[{"id":"artist","name":"Björk"}],"release_date":"1993-07-05","release_date_precision":"day","total_tracks":12,"album_type":"album","uri":"spotify:album:album","external_urls":{"spotify":"https://open.spotify.com/album/album"},"images":[{"url":"https://image","width":640,"height":640}],"restrictions":{"reason":"market"}}],"limit":1,"offset":10,"total":12,"next":"https://evil.invalid/follow-me"}}`), nil
	})}
	page, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).SearchAlbums(context.Background(), `artist:"Björk"`, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Artists[0].ID != "artist" || page.Items[0].ReleaseDate != "1993-07-05" || !page.HasNext || page.Offset != 10 || calls != 1 {
		t.Fatalf("page = %+v calls=%d", page, calls)
	}
}

func TestSearchArtistsEncodesQueryAndDecodesPage(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.URL.Host != "api.spotify.invalid" || request.URL.Path != "/v1/search" || query.Get("q") != "Björk 東京" || query.Get("type") != "artist" || query.Get("limit") != "1" || query.Get("offset") != "0" {
			t.Fatalf("request = %s %s query=%v", request.Method, request.URL.Path, query)
		}
		return response(http.StatusOK, `{"artists":{"items":[{"id":"artist","name":"Björk","uri":"spotify:artist:artist","external_urls":{"spotify":"https://open.spotify.com/artist/artist"},"images":[{"url":"https://image","width":320,"height":320}]}],"limit":1,"offset":0,"total":1,"next":"https://evil.invalid/follow-me"}}`), nil
	})}
	page, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).SearchArtists(context.Background(), "Björk 東京", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "artist" || !page.HasNext || calls != 1 {
		t.Fatalf("page = %+v calls=%d", page, calls)
	}
}

func TestCatalogSearchRejectsMalformedPages(t *testing.T) {
	for _, malformed := range []struct {
		name string
		page string
	}{
		{name: "missing page"},
		{name: "missing items", page: `{"limit":1,"offset":0,"total":0}`},
		{name: "null items", page: `{"items":null,"limit":1,"offset":0,"total":0}`},
		{name: "wrong offset", page: `{"items":[],"limit":1,"offset":1,"total":0}`},
		{name: "wrong limit", page: `{"items":[],"limit":2,"offset":0,"total":0}`},
		{name: "negative total", page: `{"items":[],"limit":1,"offset":0,"total":-1}`},
		{name: "too many items", page: `{"items":[{},{}],"limit":1,"offset":0,"total":2}`},
		{name: "missing item ID", page: `{"items":[{}],"limit":1,"offset":0,"total":1}`},
		{name: "whitespace item ID", page: `{"items":[{"id":" \t\n"}],"limit":1,"offset":0,"total":1}`},
	} {
		for _, surface := range []string{"albums", "artists"} {
			t.Run(surface+" "+malformed.name, func(t *testing.T) {
				body := `{}`
				if malformed.page != "" {
					body = `{"` + surface + `":` + malformed.page + `}`
				}
				httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, body), nil })}
				client := Client{HTTPClient: httpClient}
				var err error
				if surface == "albums" {
					_, err = client.SearchAlbums(context.Background(), "q", 1, 0)
				} else {
					_, err = client.SearchArtists(context.Background(), "q", 1, 0)
				}
				if !errors.Is(err, ErrInvalidResponse) {
					t.Fatalf("body=%s error=%v", body, err)
				}
			})
		}
	}
}

func TestSearchTracksRetriesOnlyDocumentedStatuses(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		wantDelays []time.Duration
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, retryAfter: "2", wantDelays: []time.Duration{2 * time.Second, 2 * time.Second}},
		{name: "bad gateway", status: http.StatusBadGateway, wantDelays: []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}},
		{name: "service unavailable", status: http.StatusServiceUnavailable, wantDelays: []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, wantDelays: []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var delays []time.Duration
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				result := response(test.status, "response-body-canary")
				result.Header.Set("Retry-After", test.retryAfter)
				return result, nil
			})}
			client := Client{HTTPClient: httpClient, Wait: func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			}}
			_, err := client.SearchTracks(context.Background(), "query", 10, 0)
			if !errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), "canary") || calls != 3 || !reflect.DeepEqual(delays, test.wantDelays) {
				t.Fatalf("error=%v calls=%d delays=%v", err, calls, delays)
			}
		})
	}
}

func TestSearchTracksRecoversAfterRetryableResponse(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		wantDelay  time.Duration
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, retryAfter: "2", wantDelay: 2 * time.Second},
		{name: "service unavailable", status: http.StatusServiceUnavailable, wantDelay: 250 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var delays []time.Duration
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					result := response(test.status, "")
					result.Header.Set("Retry-After", test.retryAfter)
					return result, nil
				}
				return response(http.StatusOK, `{"tracks":{"items":[{"id":"recovered"}],"limit":10,"offset":0,"total":1,"next":null}}`), nil
			})}
			spotifyClient := Client{HTTPClient: httpClient, Wait: func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			}}
			page, err := spotifyClient.SearchTracks(context.Background(), "query", 10, 0)
			if err != nil || calls != 2 || len(page.Items) != 1 || page.Items[0].ID != "recovered" || !reflect.DeepEqual(delays, []time.Duration{test.wantDelay}) {
				t.Fatalf("page=%+v error=%v calls=%d delays=%v", page, err, calls, delays)
			}
		})
	}
}

func TestSearchTracksRejectsInvalidRetryAfterWithoutRetry(t *testing.T) {
	for _, value := range []string{"", "-1", "not-a-number", "301", "999999999999999999999999"} {
		calls := 0
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			result := response(http.StatusTooManyRequests, "")
			result.Header.Set("Retry-After", value)
			return result, nil
		})}
		_, err := (Client{HTTPClient: httpClient}).SearchTracks(context.Background(), "query", 10, 0)
		if !errors.Is(err, ErrUpstream) || calls != 1 {
			t.Fatalf("Retry-After %q: error=%v calls=%d", value, err, calls)
		}
	}
}

func TestSearchTracksCancelsRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusServiceUnavailable, ""), nil
	})}
	client := Client{HTTPClient: httpClient, Wait: func(ctx context.Context, _ time.Duration) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}}
	_, err := client.SearchTracks(ctx, "query", 10, 0)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestSearchTracksRejectsInconsistentPaging(t *testing.T) {
	for _, body := range []string{
		`{"tracks":{"items":[],"limit":9,"offset":0,"total":0,"next":null}}`,
		`{"tracks":{"limit":10,"offset":0,"total":0,"next":null}}`,
		`{"tracks":{"items":null,"limit":10,"offset":0,"total":0,"next":null}}`,
	} {
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})}
		_, err := (Client{HTTPClient: httpClient}).SearchTracks(context.Background(), "query", 10, 0)
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
