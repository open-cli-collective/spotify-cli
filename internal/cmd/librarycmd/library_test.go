package librarycmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
	"github.com/open-cli-collective/spotify-cli/internal/pagetoken"
)

const trackID = "0123456789ABCDEFGHIJKL"

type fakeSession struct {
	scopes     []string
	calls      []string
	hasNext    bool
	mutateErr  error
	checkSaved []bool
	empty      bool
}

func (session *fakeSession) Close() error     { return nil }
func (session *fakeSession) Scopes() []string { return session.scopes }
func (session *fakeSession) ListSavedTracks(_ context.Context, limit, offset int) (client.SavedTrackPage, error) {
	session.calls = append(session.calls, fmt.Sprintf("list:%d:%d", limit, offset))
	if session.empty {
		return client.SavedTrackPage{Limit: limit, Offset: offset}, nil
	}
	return client.SavedTrackPage{
		Items: []client.SavedTrack{{
			AddedAt: "2026-07-23T12:00:00Z",
			Track: client.Track{
				ID: trackID, Name: "Song", Artists: []client.Artist{{ID: "artist-1", Name: "Artist"}},
				Album: client.Album{ID: "album-1", Name: "Album"}, DurationMS: 61000,
			},
		}},
		Limit: limit, Offset: offset, HasNext: session.hasNext,
	}, nil
}

func TestListEmptyAndSelectedFields(t *testing.T) {
	stdout, stderr, _, err := execute(&fakeSession{scopes: []string{auth.ScopeUserLibraryRead}, empty: true},
		"library", "tracks", "list")
	if err != nil || stdout != "ADDED_AT | ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" || stderr != "" {
		t.Fatalf("empty stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
	stdout, stderr, _, err = execute(&fakeSession{scopes: []string{auth.ScopeUserLibraryRead}},
		"library", "tracks", "list", "--fields", "added_at,track,album_id")
	want := "ADDED_AT | TRACK | ALBUM_ID\n2026-07-23T12:00:00Z | Song | album-1\n"
	if err != nil || stdout != want || stderr != "" {
		t.Fatalf("fields stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
}
func (session *fakeSession) CheckSavedTracks(_ context.Context, uris []string) ([]bool, error) {
	session.calls = append(session.calls, "check:"+fmt.Sprint(uris))
	if session.checkSaved != nil {
		return session.checkSaved, nil
	}
	result := make([]bool, len(uris))
	for index := range result {
		result[index] = index%2 == 0
	}
	return result, nil
}
func (session *fakeSession) SaveSavedTracks(_ context.Context, uris []string) error {
	session.calls = append(session.calls, "add:"+fmt.Sprint(uris))
	return session.mutateErr
}
func (session *fakeSession) RemoveSavedTracks(_ context.Context, uris []string) error {
	session.calls = append(session.calls, "remove:"+fmt.Sprint(uris))
	return session.mutateErr
}

func TestListExactOutputFlagsAndContinuationRouting(t *testing.T) {
	session := &fakeSession{scopes: []string{auth.ScopeUserLibraryRead}, hasNext: true}
	stdout, stderr, opens, err := execute(session, "library", "tracks", "list", "--next-page-token", pagetoken.Encode(pageScope, 10))
	want := "ADDED_AT | ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n" +
		"2026-07-23T12:00:00Z | " + trackID + " | Song | artist-1 | Artist | album-1 | Album | 1:01\n"
	if err != nil || stdout != want || stderr != "More results available (next: "+pagetoken.Encode(pageScope, 20)+")\n" ||
		opens != 1 || fmt.Sprint(session.calls) != "[list:10:10]" {
		t.Fatalf("stdout=%q stderr=%q opens=%d calls=%v error=%v", stdout, stderr, opens, session.calls, err)
	}

	stdout, stderr, _, err = execute(&fakeSession{scopes: []string{auth.ScopeUserLibraryRead}}, "library", "tracks", "list",
		"--id", "--fields", "invalid", "--extended", "--include-artwork")
	if err != nil || stdout != trackID+"\n" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
}

func TestCheckReferenceFormsDeduplicateInFirstSeenOrder(t *testing.T) {
	second := "abcdefghijklmnopqrstuv"
	session := &fakeSession{scopes: []string{auth.ScopeUserLibraryRead}}
	stdout, stderr, opens, err := execute(session, "library", "tracks", "check",
		"spotify:track:"+trackID, "https://open.spotify.com/track/"+second, trackID)
	want := "REFERENCE | ID | SAVED\n" +
		"spotify:track:" + trackID + " | " + trackID + " | true\n" +
		"https://open.spotify.com/track/" + second + " | " + second + " | false\n"
	if err != nil || stdout != want || stderr != "" || opens != 1 || len(session.calls) != 1 {
		t.Fatalf("stdout=%q stderr=%q opens=%d calls=%v error=%v", stdout, stderr, opens, session.calls, err)
	}
}

func TestBatchValidationHappensBeforeSession(t *testing.T) {
	for _, verb := range []string{"check", "add", "remove"} {
		stdout, stderr, opens, err := execute(&fakeSession{}, "library", "tracks", verb, trackID, "bad")
		if exitcode.Code(err) != exitcode.Usage || stdout != "" || stderr != "" || opens != 0 {
			t.Fatalf("verb=%s stdout=%q stderr=%q opens=%d error=%v", verb, stdout, stderr, opens, err)
		}
	}
}

func TestScopeGuardPrecedesResourceRequestWithOverwriteHint(t *testing.T) {
	for _, test := range []struct {
		verb, scope string
	}{
		{verb: "list", scope: auth.ScopeUserLibraryRead},
		{verb: "check", scope: auth.ScopeUserLibraryRead},
		{verb: "add", scope: auth.ScopeUserLibraryModify},
		{verb: "remove", scope: auth.ScopeUserLibraryModify},
	} {
		session := &fakeSession{scopes: []string{auth.ScopeUserReadPrivate}}
		args := []string{"library", "tracks", test.verb}
		if test.verb != "list" {
			args = append(args, trackID)
		}
		_, _, opens, err := execute(session, args...)
		want := "spotify authorization lacks " + test.scope + "; run sptfy init --overwrite"
		if exitcode.Code(err) != exitcode.Config || err.Error() != want || opens != 1 || len(session.calls) != 0 {
			t.Fatalf("verb=%s error=%v opens=%d calls=%v", test.verb, err, opens, session.calls)
		}
	}
}

func TestMutationsEmitOnlyAfterCompleteSuccess(t *testing.T) {
	for _, test := range []struct {
		verb, want string
	}{
		{verb: "add", want: "added\t1\n"},
		{verb: "remove", want: "removed\t1\n"},
	} {
		session := &fakeSession{scopes: []string{auth.ScopeUserLibraryModify}}
		stdout, stderr, _, err := execute(session, "library", "tracks", test.verb,
			trackID, "spotify:track:"+trackID, "https://open.spotify.com/track/"+trackID)
		wantCall := test.verb + ":[spotify:track:" + trackID + "]"
		if err != nil || stdout != test.want || stderr != "" ||
			len(session.calls) != 1 || session.calls[0] != wantCall {
			t.Fatalf("verb=%s stdout=%q stderr=%q calls=%v error=%v", test.verb, stdout, stderr, session.calls, err)
		}

		session = &fakeSession{scopes: []string{auth.ScopeUserLibraryModify}, mutateErr: client.ErrUpstream}
		stdout, _, _, err = execute(session, "library", "tracks", test.verb, trackID)
		if !errors.Is(err, client.ErrUpstream) || stdout != "" {
			t.Fatalf("verb=%s stdout=%q error=%v", test.verb, stdout, err)
		}
	}
}

func TestListValidatesBeforeSession(t *testing.T) {
	for _, args := range [][]string{
		{"library", "tracks", "list", "--max", "0"},
		{"library", "tracks", "list", "--max", "51"},
		{"library", "tracks", "list", "--fields", "invalid"},
		{"library", "tracks", "list", "--next-page-token", "bad"},
	} {
		_, _, opens, err := execute(&fakeSession{}, args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v opens=%d error=%v", args, opens, err)
		}
	}
}

func TestListAcceptsExactMaxBoundariesAndRejectsForeignTokenBeforeSession(t *testing.T) {
	for _, max := range []string{"1", "50"} {
		session := &fakeSession{scopes: []string{auth.ScopeUserLibraryRead}}
		_, _, opens, err := execute(session, "library", "tracks", "list", "--max", max)
		if err != nil || opens != 1 || fmt.Sprint(session.calls) != "[list:"+max+":0]" {
			t.Fatalf("max=%s opens=%d calls=%v error=%v", max, opens, session.calls, err)
		}
	}

	session := &fakeSession{scopes: []string{auth.ScopeUserLibraryRead}}
	_, _, opens, err := execute(session, "library", "tracks", "list",
		"--next-page-token", pagetoken.Encode("search-tracks", 10))
	if exitcode.Code(err) != exitcode.Usage || opens != 0 || len(session.calls) != 0 {
		t.Fatalf("foreign token opens=%d calls=%v error=%v", opens, session.calls, err)
	}
}

func execute(session *fakeSession, args ...string) (string, string, int, error) {
	opens := 0
	command := &cobra.Command{Use: "sptfy"}
	command.AddCommand(New(Dependencies{OpenSession: func(context.Context, string, bool) (Session, error) {
		opens++
		return session, nil
	}}))
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), opens, err
}
