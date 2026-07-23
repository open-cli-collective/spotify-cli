package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestSavedTrackListUsesFixedPathAndValidatesPage(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "api.spotify.invalid" ||
			request.URL.Path != "/v1/me/tracks" || request.URL.Query().Get("limit") != "10" ||
			request.URL.Query().Get("offset") != "20" || len(request.URL.Query()) != 2 {
			t.Fatalf("request=%s %s", request.Method, request.URL.String())
		}
		return response(http.StatusOK, `{"items":[{"added_at":"2026-07-23T12:00:00Z","track":{"id":"`+id+`"}}],"limit":10,"offset":20,"total":31,"next":"https://evil.invalid/follow"}`), nil
	})}
	page, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).ListSavedTracks(context.Background(), 10, 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].Track.ID != id || !page.HasNext {
		t.Fatalf("page=%+v error=%v", page, err)
	}

	for _, body := range []string{
		`{}`,
		`{"items":[],"limit":11,"offset":20,"total":0}`,
		`{"items":[],"limit":10,"offset":21,"total":0}`,
		`{"items":[],"limit":10,"offset":20,"total":-1}`,
		`{"items":[{"track":{"id":"bad"}}],"limit":10,"offset":20,"total":1}`,
		`{"items":[{"track":{"id":"0123456789ABCDEFGHIJKL"}}],"limit":10,"offset":20,"total":1}`,
	} {
		httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})
		if _, err := (Client{HTTPClient: httpClient}).ListSavedTracks(context.Background(), 10, 20); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}

func TestSavedTrackMutationsChunkAtForty(t *testing.T) {
	for _, count := range []int{40, 41, 80, 81} {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			t.Run(fmt.Sprintf("%s/%d", method, count), func(t *testing.T) {
				var sizes []int
				httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.Method != method || request.URL.Path != "/v1/me/library" || len(request.URL.Query()) != 1 {
						t.Fatalf("request=%s %s", request.Method, request.URL.String())
					}
					sizes = append(sizes, len(strings.Split(request.URL.Query().Get("uris"), ",")))
					return response(http.StatusNoContent, ""), nil
				})}
				spotify := Client{HTTPClient: httpClient}
				var err error
				if method == http.MethodPut {
					err = spotify.SaveSavedTracks(context.Background(), libraryURIs(count))
				} else {
					err = spotify.RemoveSavedTracks(context.Background(), libraryURIs(count))
				}
				if err != nil || len(sizes) != (count+39)/40 {
					t.Fatalf("sizes=%v error=%v", sizes, err)
				}
				for _, size := range sizes {
					if size < 1 || size > 40 {
						t.Fatalf("sizes=%v", sizes)
					}
				}
			})
		}
	}
}

func TestSavedTrackOperationsChunkAtForty(t *testing.T) {
	for _, count := range []int{40, 41, 80, 81} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			uris := libraryURIs(count)
			var sizes []int
			isSaved := func(uri string) bool {
				index, _ := strconv.Atoi(strings.TrimPrefix(uri, "spotify:track:"))
				return index%7 == 0 || index%11 == 3
			}
			httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host != "api.spotify.invalid" || request.URL.Path != "/v1/me/library/contains" ||
					request.Method != http.MethodGet || len(request.URL.Query()) != 1 {
					t.Fatalf("request=%s %s", request.Method, request.URL.String())
				}
				chunk := strings.Split(request.URL.Query().Get("uris"), ",")
				size := len(chunk)
				sizes = append(sizes, size)
				values := make([]string, size)
				for index, uri := range chunk {
					values[index] = strconv.FormatBool(isSaved(uri))
				}
				return response(http.StatusOK, "["+strings.Join(values, ",")+"]"), nil
			})}
			got, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).CheckSavedTracks(context.Background(), uris)
			wantCalls := (count + 39) / 40
			wantResults := make([]bool, count)
			for index, uri := range uris {
				wantResults[index] = isSaved(uri)
			}
			if err != nil || !slices.Equal(got, wantResults) || len(sizes) != wantCalls {
				t.Fatalf("count=%d sizes=%v results=%v want=%v error=%v", count, sizes, got, wantResults, err)
			}
			for index, size := range sizes {
				want := min(40, count-index*40)
				if size != want {
					t.Fatalf("count=%d sizes=%v", count, sizes)
				}
			}
		})
	}
}

func TestSavedTrackCheckRejectsResponseLengthMismatch(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `[true]`), nil
	})}
	_, err := (Client{HTTPClient: httpClient}).CheckSavedTracks(context.Background(), libraryURIs(2))
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("error=%v", err)
	}
}

func TestSavedTrackMutationsUseFixedMethodsAndStopOnLaterFailure(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != method || request.URL.Host != "api.spotify.invalid" ||
					request.URL.Path != "/v1/me/library" || len(strings.Split(request.URL.Query().Get("uris"), ",")) > 40 {
					t.Fatalf("request=%s %s", request.Method, request.URL.String())
				}
				if calls == 2 {
					return response(http.StatusInternalServerError, "secret"), nil
				}
				return response(http.StatusNoContent, ""), nil
			})}
			spotify := Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}
			var err error
			if method == http.MethodPut {
				err = spotify.SaveSavedTracks(context.Background(), libraryURIs(81))
			} else {
				err = spotify.RemoveSavedTracks(context.Background(), libraryURIs(81))
			}
			if !errors.Is(err, ErrUpstream) || calls != 2 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestSavedTrackOperationsRejectInvalidInputBeforeRequest(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusNoContent, ""), nil
	})}
	spotify := Client{HTTPClient: httpClient}
	for _, call := range []func() error{
		func() error { _, err := spotify.CheckSavedTracks(context.Background(), nil); return err },
		func() error { return spotify.SaveSavedTracks(context.Background(), []string{"0123456789ABCDEFGHIJKL"}) },
		func() error {
			return spotify.RemoveSavedTracks(context.Background(), []string{"spotify:album:0123456789ABCDEFGHIJKL"})
		},
	} {
		if err := call(); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
}

func libraryURIs(count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = fmt.Sprintf("spotify:track:%022d", index)
	}
	return result
}
