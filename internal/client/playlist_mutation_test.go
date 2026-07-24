package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	mutationPlaylistID = "0123456789ABCDEFGHIJKL"
	mutationTrackID    = "abcdefghijklmnopqrstuv"
)

func TestAddPlaylistItemsUsesCurrentEndpointAndBody(t *testing.T) {
	position := 2
	var calls int
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodPost || request.URL.Path != "/v1/playlists/"+mutationPlaylistID+"/items" ||
			request.URL.RawQuery != "" || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request=%s %s content-type=%q", request.Method, request.URL.RequestURI(), request.Header.Get("Content-Type"))
		}
		var body struct {
			URIs     []string `json:"uris"`
			Position *int     `json:"position"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.URIs) != 2 || body.URIs[0] != "spotify:track:"+mutationTrackID || body.URIs[1] != "spotify:track:"+mutationTrackID ||
			body.Position == nil || *body.Position != position {
			t.Fatalf("body=%+v", body)
		}
		return response(http.StatusCreated, `{"snapshot_id":"next-snapshot"}`), nil
	})}
	snapshot, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).AddPlaylistItems(
		context.Background(), mutationPlaylistID,
		[]string{"spotify:track:" + mutationTrackID, "spotify:track:" + mutationTrackID}, &position,
	)
	if err != nil || snapshot != "next-snapshot" || calls != 1 {
		t.Fatalf("snapshot=%q calls=%d error=%v", snapshot, calls, err)
	}
}

func TestAddPlaylistItemsAcceptsExact100URIRequestBody(t *testing.T) {
	want := make([]string, 100)
	for index := range want {
		want[index] = "spotify:track:" + mutationTrackID
	}
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		var body struct {
			URIs []string `json:"uris"`
		}
		if request.Method != http.MethodPost || request.URL.Path != "/v1/playlists/"+mutationPlaylistID+"/items" ||
			json.NewDecoder(request.Body).Decode(&body) != nil || !slices.Equal(body.URIs, want) {
			t.Fatalf("request=%s %s URI count=%d", request.Method, request.URL.RequestURI(), len(body.URIs))
		}
		return response(http.StatusCreated, `{"snapshot_id":"hundred"}`), nil
	})}
	snapshot, err := (Client{HTTPClient: httpClient}).AddPlaylistItems(context.Background(), mutationPlaylistID, want, nil)
	if err != nil || snapshot != "hundred" || calls != 1 {
		t.Fatalf("snapshot=%q calls=%d error=%v", snapshot, calls, err)
	}
}

func TestPlaylistMutationHTTPStatusesAreTypedAndNeverRetried(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		for _, test := range []struct {
			status    int
			uncertain bool
		}{
			{status: http.StatusBadRequest},
			{status: http.StatusTooManyRequests},
			{status: http.StatusInternalServerError, uncertain: true},
			{status: http.StatusBadGateway, uncertain: true},
			{status: http.StatusServiceUnavailable, uncertain: true},
			{status: http.StatusGatewayTimeout, uncertain: true},
		} {
			t.Run(method+" "+http.StatusText(test.status), func(t *testing.T) {
				calls := 0
				httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(request.Body)
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodPost && (strings.Contains(string(body), "position") || strings.Contains(string(body), "tracks")) {
						t.Fatalf("body=%s", body)
					}
					return response(test.status, ""), nil
				})}
				spotify := Client{HTTPClient: httpClient}
				var err error
				if method == http.MethodPost {
					_, err = spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, []string{"spotify:track:" + mutationTrackID}, nil)
				} else {
					_, err = spotify.RemovePlaylistItemAtPosition(context.Background(), mutationPlaylistID, "spotify:track:"+mutationTrackID, 2, "before")
				}
				if test.uncertain {
					assertMutationUncertain(t, err, method, ErrUpstream)
				} else {
					assertMutationRejected(t, err, method, test.status, ErrUpstream)
				}
				if calls != 1 {
					t.Fatalf("calls=%d error=%v", calls, err)
				}
			})
		}
	}
}

func TestRemovePlaylistItemsByURIUsesCurrentItemsShapeAndSnapshot(t *testing.T) {
	var calls int
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodDelete || request.URL.Path != "/v1/playlists/"+mutationPlaylistID+"/items" ||
			strings.Contains(request.URL.Path, "/tracks") || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request=%s %s", request.Method, request.URL.RequestURI())
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, old := body["tracks"]; old || len(body) != 2 || string(body["snapshot_id"]) != `"before-snapshot"` ||
			string(body["items"]) != `[{"uri":"spotify:track:`+mutationTrackID+`"}]` {
			t.Fatalf("body=%v", body)
		}
		return response(http.StatusOK, `{"snapshot_id":"after-snapshot"}`), nil
	})}
	snapshot, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).RemovePlaylistItemsByURI(
		context.Background(), mutationPlaylistID, "spotify:track:"+mutationTrackID, "before-snapshot",
	)
	if err != nil || snapshot != "after-snapshot" || calls != 1 {
		t.Fatalf("snapshot=%q calls=%d error=%v", snapshot, calls, err)
	}
}

func TestRemovePlaylistItemAtPositionUsesSpecificOccurrenceAndSnapshot(t *testing.T) {
	var calls int
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodDelete || request.URL.Path != "/v1/playlists/"+mutationPlaylistID+"/items" ||
			request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request=%s %s", request.Method, request.URL.RequestURI())
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 2 || string(body["snapshot_id"]) != `"after-add"` ||
			string(body["items"]) != `[{"uri":"spotify:track:`+mutationTrackID+`","positions":[2]}]` {
			t.Fatalf("body=%v", body)
		}
		return response(http.StatusOK, `{"snapshot_id":"after-remove"}`), nil
	})}
	snapshot, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).RemovePlaylistItemAtPosition(
		context.Background(), mutationPlaylistID, "spotify:track:"+mutationTrackID, 2, "after-add",
	)
	if err != nil || snapshot != "after-remove" || calls != 1 {
		t.Fatalf("snapshot=%q calls=%d error=%v", snapshot, calls, err)
	}
}

func TestPlaylistMutationsDoNotRetryTransportErrors(t *testing.T) {
	transportErr := errors.New("transport failed after write")
	for _, test := range []struct {
		name   string
		method string
		call   func(Client) error
	}{
		{name: "add", method: http.MethodPost, call: func(spotify Client) error {
			_, err := spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, []string{"spotify:track:" + mutationTrackID}, nil)
			return err
		}},
		{name: "remove", method: http.MethodDelete, call: func(spotify Client) error {
			_, err := spotify.RemovePlaylistItemsByURI(context.Background(), mutationPlaylistID, "spotify:track:"+mutationTrackID, "before")
			return err
		}},
		{name: "remove position", method: http.MethodDelete, call: func(spotify Client) error {
			_, err := spotify.RemovePlaylistItemAtPosition(context.Background(), mutationPlaylistID, "spotify:track:"+mutationTrackID, 2, "before")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, transportErr
			})}
			err := test.call(Client{HTTPClient: httpClient})
			assertMutationUncertain(t, err, test.method, transportErr)
			if !errors.Is(err, ErrUpstream) || calls != 1 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestPlaylistMutationMarksPostSendContextFailureUncertain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, context.Canceled
	})}
	_, err := (Client{HTTPClient: httpClient}).AddPlaylistItems(ctx, mutationPlaylistID, []string{"spotify:track:" + mutationTrackID}, nil)
	assertMutationUncertain(t, err, http.MethodPost, context.Canceled)
}

func TestPlaylistMutationsMarkInvalidSuccessfulResponsesUncertain(t *testing.T) {
	for _, test := range []struct {
		name     string
		method   string
		response func() *http.Response
		cause    error
	}{
		{name: "post malformed", method: http.MethodPost, response: func() *http.Response { return response(http.StatusCreated, `{`) }, cause: ErrInvalidResponse},
		{name: "delete empty", method: http.MethodDelete, response: func() *http.Response { return response(http.StatusOK, ``) }, cause: ErrInvalidResponse},
		{name: "post truncated", method: http.MethodPost, response: func() *http.Response {
			return &http.Response{StatusCode: http.StatusCreated, Header: make(http.Header), Body: &truncatedMutationBody{}}
		}, cause: io.ErrUnexpectedEOF},
		{name: "delete oversized", method: http.MethodDelete, response: func() *http.Response {
			return response(http.StatusOK, strings.Repeat("x", maxResponseBytes+1))
		}, cause: ErrInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return test.response(), nil
			})}
			spotify := Client{HTTPClient: httpClient}
			var err error
			if test.method == http.MethodPost {
				_, err = spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, []string{"spotify:track:" + mutationTrackID}, nil)
			} else {
				_, err = spotify.RemovePlaylistItemsByURI(context.Background(), mutationPlaylistID, "spotify:track:"+mutationTrackID, "before")
			}
			assertMutationUncertain(t, err, test.method, test.cause)
			if !errors.Is(err, ErrInvalidResponse) || calls != 1 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestPlaylistMutationsRejectInvalidInputsAndMarkMissingSnapshotsUncertain(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, `{}`), nil
	})}
	spotify := Client{HTTPClient: httpClient}
	validURI := "spotify:track:" + mutationTrackID
	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = validURI
	}
	negative := -1
	for _, call := range []func() error{
		func() error {
			_, err := spotify.AddPlaylistItems(context.Background(), "bad", []string{validURI}, nil)
			return err
		},
		func() error {
			_, err := spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, nil, nil)
			return err
		},
		func() error {
			_, err := spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, tooMany, nil)
			return err
		},
		func() error {
			_, err := spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, []string{"spotify:episode:" + mutationTrackID}, nil)
			return err
		},
		func() error {
			_, err := spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, []string{validURI}, &negative)
			return err
		},
		func() error {
			_, err := spotify.RemovePlaylistItemsByURI(context.Background(), "bad", validURI, "snapshot")
			return err
		},
		func() error {
			_, err := spotify.RemovePlaylistItemsByURI(context.Background(), mutationPlaylistID, "bad", "snapshot")
			return err
		},
		func() error {
			_, err := spotify.RemovePlaylistItemsByURI(context.Background(), mutationPlaylistID, validURI, " ")
			return err
		},
		func() error {
			_, err := spotify.RemovePlaylistItemAtPosition(context.Background(), mutationPlaylistID, validURI, -1, "snapshot")
			return err
		},
	} {
		err := call()
		var uncertain *MutationOutcomeUncertainError
		if !errors.Is(err, ErrInvalidResponse) || errors.As(err, &uncertain) || calls != 0 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
	_, err := (Client{BaseURL: "://"}).AddPlaylistItems(context.Background(), mutationPlaylistID, []string{validURI}, nil)
	assertMutationRejected(t, err, http.MethodPost, 0, ErrUpstream)
	_, err = spotify.AddPlaylistItems(context.Background(), mutationPlaylistID, []string{validURI}, nil)
	assertMutationUncertain(t, err, http.MethodPost, ErrInvalidResponse)
	if calls != 1 {
		t.Fatalf("add response calls=%d error=%v", calls, err)
	}
	_, err = spotify.RemovePlaylistItemsByURI(context.Background(), mutationPlaylistID, validURI, "snapshot")
	assertMutationUncertain(t, err, http.MethodDelete, ErrInvalidResponse)
	if calls != 2 {
		t.Fatalf("remove response calls=%d error=%v", calls, err)
	}
}

func assertMutationUncertain(t *testing.T, err error, method string, cause error) {
	t.Helper()
	var uncertain *MutationOutcomeUncertainError
	if !errors.As(err, &uncertain) || uncertain.Method != method || uncertain.Path != "/playlists/"+mutationPlaylistID+"/items" ||
		!errors.Is(err, cause) || !errors.Is(uncertain.Cause, cause) ||
		err.Error() != "spotify mutation outcome is uncertain; inspect current state before retrying" {
		t.Fatalf("uncertain=%+v error=%v", uncertain, err)
	}
}

func assertMutationRejected(t *testing.T, err error, method string, statusCode int, cause error) {
	t.Helper()
	var rejected *MutationRejectedError
	var uncertain *MutationOutcomeUncertainError
	if !errors.As(err, &rejected) || errors.As(err, &uncertain) || rejected.Method != method ||
		rejected.Path != "/playlists/"+mutationPlaylistID+"/items" || rejected.StatusCode != statusCode ||
		!errors.Is(err, cause) || err.Error() != cause.Error() {
		t.Fatalf("rejected=%+v error=%v", rejected, err)
	}
}

type truncatedMutationBody struct{ sent bool }

func (body *truncatedMutationBody) Read(value []byte) (int, error) {
	if body.sent {
		return 0, io.ErrUnexpectedEOF
	}
	body.sent = true
	return copy(value, `{"snapshot_id":"partial`), nil
}

func (*truncatedMutationBody) Close() error { return nil }
