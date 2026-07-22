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
