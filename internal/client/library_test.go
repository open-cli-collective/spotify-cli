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

	"github.com/open-cli-collective/spotify-cli/internal/spotifyref"
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
					err = spotify.SaveSavedItems(context.Background(), spotifyref.Track, libraryIDs(count))
				} else {
					err = spotify.RemoveSavedItems(context.Background(), spotifyref.Track, libraryIDs(count))
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

func TestQueryOnlySavedItemMutationOutcomesAreTypedAndNeverRetried(t *testing.T) {
	const id = "0123456789ABCDEFGHIJKL"
	for _, operation := range []struct {
		name   string
		method string
		call   func(Client) error
	}{
		{name: "save track", method: http.MethodPut, call: func(spotify Client) error {
			return spotify.SaveSavedItems(context.Background(), spotifyref.Track, []string{id})
		}},
		{name: "remove track", method: http.MethodDelete, call: func(spotify Client) error {
			return spotify.RemoveSavedItems(context.Background(), spotifyref.Track, []string{id})
		}},
		{name: "save album", method: http.MethodPut, call: func(spotify Client) error {
			return spotify.SaveSavedItems(context.Background(), spotifyref.Album, []string{id})
		}},
		{name: "remove album", method: http.MethodDelete, call: func(spotify Client) error {
			return spotify.RemoveSavedItems(context.Background(), spotifyref.Album, []string{id})
		}},
	} {
		for _, test := range []struct {
			status    int
			uncertain bool
		}{
			{status: http.StatusTooManyRequests},
			{status: http.StatusServiceUnavailable, uncertain: true},
		} {
			t.Run(operation.name+"/"+http.StatusText(test.status), func(t *testing.T) {
				calls := 0
				httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					calls++
					if request.Method != operation.method || request.URL.Path != "/v1/me/library" ||
						request.URL.Query().Get("uris") == "" || request.Header.Get("Content-Type") != "" {
						t.Fatalf("request=%s %s content-type=%q", request.Method, request.URL.String(), request.Header.Get("Content-Type"))
					}
					return response(test.status, "secret"), nil
				})}
				err := operation.call(Client{HTTPClient: httpClient})
				var uncertain *MutationOutcomeUncertainError
				var rejected *MutationRejectedError
				if test.uncertain {
					if !errors.As(err, &uncertain) || errors.As(err, &rejected) || uncertain.Method != operation.method ||
						!strings.HasPrefix(uncertain.Path, "/me/library?uris=") || !errors.Is(err, ErrUpstream) {
						t.Fatalf("uncertain=%+v error=%v", uncertain, err)
					}
				} else if !errors.As(err, &rejected) || errors.As(err, &uncertain) || rejected.Method != operation.method ||
					rejected.StatusCode != test.status || !strings.HasPrefix(rejected.Path, "/me/library?uris=") ||
					!errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("rejected=%+v error=%v", rejected, err)
				}
				if calls != 1 {
					t.Fatalf("calls=%d error=%v", calls, err)
				}
			})
		}
	}
}

func TestQueryOnlySavedItemMutationPreservesTypedAuthFailures(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{
		{status: http.StatusUnauthorized, want: ErrUnauthorized},
		{status: http.StatusForbidden, want: ErrForbidden},
	} {
		httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(test.status, "secret"), nil
		})}
		err := (Client{HTTPClient: httpClient}).SaveSavedItems(context.Background(), spotifyref.Track, []string{"0123456789ABCDEFGHIJKL"})
		var rejected *MutationRejectedError
		if !errors.As(err, &rejected) || rejected.StatusCode != test.status || !errors.Is(err, test.want) ||
			err.Error() != test.want.Error() || strings.Contains(err.Error(), "secret") {
			t.Fatalf("status=%d rejected=%+v error=%v", test.status, rejected, err)
		}
	}
}

func TestSavedTrackOperationsChunkAtForty(t *testing.T) {
	for _, count := range []int{40, 41, 80, 81} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ids := libraryIDs(count)
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
			got, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).CheckSavedItems(context.Background(), spotifyref.Track, ids)
			wantCalls := (count + 39) / 40
			wantResults := make([]bool, count)
			for index, id := range ids {
				wantResults[index] = isSaved("spotify:track:" + id)
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
	_, err := (Client{HTTPClient: httpClient}).CheckSavedItems(context.Background(), spotifyref.Track, libraryIDs(2))
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
				err = spotify.SaveSavedItems(context.Background(), spotifyref.Track, libraryIDs(81))
			} else {
				err = spotify.RemoveSavedItems(context.Background(), spotifyref.Track, libraryIDs(81))
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
		func() error {
			_, err := spotify.CheckSavedItems(context.Background(), spotifyref.Track, nil)
			return err
		},
		func() error { return spotify.SaveSavedItems(context.Background(), spotifyref.Track, []string{"bad"}) },
		func() error {
			return spotify.RemoveSavedItems(context.Background(), spotifyref.Track, []string{"also-bad"})
		},
	} {
		if err := call(); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
	for _, kind := range []spotifyref.Kind{spotifyref.Artist, ""} {
		for _, call := range []func() error{
			func() error {
				_, err := spotify.CheckSavedItems(context.Background(), kind, []string{"0123456789ABCDEFGHIJKL"})
				return err
			},
			func() error {
				return spotify.SaveSavedItems(context.Background(), kind, []string{"0123456789ABCDEFGHIJKL"})
			},
			func() error {
				return spotify.RemoveSavedItems(context.Background(), kind, []string{"0123456789ABCDEFGHIJKL"})
			},
		} {
			if err := call(); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
				t.Fatalf("kind=%q calls=%d error=%v", kind, calls, err)
			}
		}
	}
}

func TestSavedAlbumListUsesFixedPathAndValidatesPage(t *testing.T) {
	const (
		album  = "0123456789ABCDEFGHIJKL"
		artist = "abcdefghijklmnopqrstuv"
	)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "api.spotify.invalid" ||
			request.URL.Path != "/v1/me/albums" || request.URL.Query().Get("limit") != "10" ||
			request.URL.Query().Get("offset") != "20" || len(request.URL.Query()) != 2 {
			t.Fatalf("request=%s %s", request.Method, request.URL.String())
		}
		return response(http.StatusOK, `{"items":[{"added_at":"2026-07-23T12:00:00Z","album":{"id":"`+album+`","artists":[{"id":"`+artist+`"}]}}],"limit":10,"offset":20,"total":31,"next":"https://evil.invalid/follow"}`), nil
	})}
	page, err := (Client{HTTPClient: httpClient, BaseURL: "https://api.spotify.invalid/v1"}).ListSavedAlbums(context.Background(), 10, 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].Album.ID != album || !page.HasNext {
		t.Fatalf("page=%+v error=%v", page, err)
	}

	for _, body := range []string{
		`{}`,
		`{"items":[],"limit":11,"offset":20,"total":0}`,
		`{"items":[],"limit":10,"offset":21,"total":0}`,
		`{"items":[],"limit":10,"offset":20,"total":-1}`,
		`{"items":[{"added_at":"2026-07-23T12:00:00Z","album":{"id":"bad","artists":[{"id":"abcdefghijklmnopqrstuv"}]}}],"limit":10,"offset":20,"total":1}`,
		`{"items":[{"added_at":"2026-07-23T12:00:00Z","album":{"id":"0123456789ABCDEFGHIJKL","artists":[]}}],"limit":10,"offset":20,"total":1}`,
		`{"items":[{"added_at":"2026-07-23T12:00:00Z","album":{"id":"0123456789ABCDEFGHIJKL","artists":[{"id":"bad"}]}}],"limit":10,"offset":20,"total":1}`,
		`{"items":[{"album":{"id":"0123456789ABCDEFGHIJKL","artists":[{"id":"abcdefghijklmnopqrstuv"}]}}],"limit":10,"offset":20,"total":1}`,
	} {
		httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})
		if _, err := (Client{HTTPClient: httpClient}).ListSavedAlbums(context.Background(), 10, 20); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}

func TestSavedAlbumOperationsUseGenericLibraryChunks(t *testing.T) {
	for _, count := range []int{40, 41, 80, 81} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ids := libraryIDs(count)
			isSaved := func(uri string) bool {
				index, _ := strconv.Atoi(strings.TrimPrefix(uri, "spotify:album:"))
				return index%5 == 0 || index%13 == 2
			}
			var checkSizes []int
			checkClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet || request.URL.Path != "/v1/me/library/contains" {
					t.Fatalf("request=%s %s", request.Method, request.URL.String())
				}
				chunk := strings.Split(request.URL.Query().Get("uris"), ",")
				checkSizes = append(checkSizes, len(chunk))
				values := make([]string, len(chunk))
				for index, uri := range chunk {
					values[index] = strconv.FormatBool(isSaved(uri))
				}
				return response(http.StatusOK, "["+strings.Join(values, ",")+"]"), nil
			})}
			got, err := (Client{HTTPClient: checkClient}).CheckSavedItems(context.Background(), spotifyref.Album, ids)
			want := make([]bool, count)
			for index, id := range ids {
				want[index] = isSaved("spotify:album:" + id)
			}
			if err != nil || !slices.Equal(got, want) || len(checkSizes) != (count+39)/40 {
				t.Fatalf("check sizes=%v results=%v want=%v error=%v", checkSizes, got, want, err)
			}

			for _, method := range []string{http.MethodPut, http.MethodDelete} {
				var sizes []int
				var seen []string
				httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.Method != method || request.URL.Path != "/v1/me/library" {
						t.Fatalf("request=%s %s", request.Method, request.URL.String())
					}
					chunk := strings.Split(request.URL.Query().Get("uris"), ",")
					sizes = append(sizes, len(chunk))
					seen = append(seen, chunk...)
					return response(http.StatusNoContent, ""), nil
				})}
				spotify := Client{HTTPClient: httpClient}
				if method == http.MethodPut {
					err = spotify.SaveSavedItems(context.Background(), spotifyref.Album, ids)
				} else {
					err = spotify.RemoveSavedItems(context.Background(), spotifyref.Album, ids)
				}
				if err != nil || len(sizes) != (count+39)/40 || !slices.Equal(seen, albumLibraryURIs(count)) {
					t.Fatalf("method=%s sizes=%v seen=%v error=%v", method, sizes, seen, err)
				}
				for index, size := range sizes {
					if size != min(40, count-index*40) {
						t.Fatalf("method=%s sizes=%v", method, sizes)
					}
				}
			}
		})
	}
}

func TestSavedAlbumOperationsRejectWrongKindsAndMalformedResponsesBeforeContinuing(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method == http.MethodGet {
			return response(http.StatusOK, `[true]`), nil
		}
		if calls == 2 {
			return response(http.StatusInternalServerError, "secret"), nil
		}
		return response(http.StatusNoContent, ""), nil
	})}
	spotify := Client{HTTPClient: httpClient}
	for _, call := range []func() error{
		func() error {
			_, err := spotify.CheckSavedItems(context.Background(), spotifyref.Album, nil)
			return err
		},
		func() error {
			return spotify.SaveSavedItems(context.Background(), spotifyref.Album, []string{"bad"})
		},
		func() error {
			return spotify.RemoveSavedItems(context.Background(), spotifyref.Album, []string{"also-bad"})
		},
	} {
		if err := call(); !errors.Is(err, ErrInvalidResponse) || calls != 0 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}

	if _, err := spotify.CheckSavedItems(context.Background(), spotifyref.Album, libraryIDs(41)); !errors.Is(err, ErrInvalidResponse) || calls != 1 {
		t.Fatalf("check calls=%d error=%v", calls, err)
	}
	for _, mutate := range []func(context.Context, spotifyref.Kind, []string) error{spotify.SaveSavedItems, spotify.RemoveSavedItems} {
		calls = 0
		if err := mutate(context.Background(), spotifyref.Album, libraryIDs(81)); !errors.Is(err, ErrUpstream) || calls != 2 {
			t.Fatalf("mutation calls=%d error=%v", calls, err)
		}
	}
}

func libraryIDs(count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = fmt.Sprintf("%022d", index)
	}
	return result
}

func albumLibraryURIs(count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = fmt.Sprintf("spotify:album:%022d", index)
	}
	return result
}
