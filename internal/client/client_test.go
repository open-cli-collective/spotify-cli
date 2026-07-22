package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
