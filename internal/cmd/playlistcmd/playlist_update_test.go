package playlistcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

type updateSession struct {
	scopes            []string
	closed            bool
	items             []client.PlaylistItem
	snapshot          string
	gets              int
	pageCalls         []int
	resourceIDs       []string
	operations        []string
	addErr            error
	removeErr         error
	stale             bool
	staleCount        bool
	addSnapshot       string
	addRace           string
	verifyRace        string
	verifyErr         error
	verifyPageErr     error
	postAddGets       int
	finalGapDuplicate bool
}

func (session *updateSession) Close() error     { session.closed = true; return nil }
func (session *updateSession) Scopes() []string { return session.scopes }
func (session *updateSession) GetPlaylist(_ context.Context, playlistID string) (client.Playlist, error) {
	session.resourceIDs = append(session.resourceIDs, "get:"+playlistID)
	session.gets++
	snapshot := session.snapshot
	total := len(session.items)
	if len(session.operations) > 0 {
		session.postAddGets++
		if session.verifyErr != nil && session.postAddGets == 1 {
			return client.Playlist{}, session.verifyErr
		}
		switch session.verifyRace {
		case "first snapshot":
			if session.postAddGets == 1 {
				snapshot = "concurrent"
			}
		case "first count":
			if session.postAddGets == 1 {
				total++
			}
		case "second snapshot":
			if session.postAddGets == 2 {
				snapshot = "concurrent"
			}
		case "second count":
			if session.postAddGets == 2 {
				total++
			}
		}
	}
	if session.stale && session.gets == 2 {
		snapshot = "changed"
	}
	if session.staleCount && session.gets == 2 {
		total++
	}
	return playlistState(total, snapshot), nil
}
func (session *updateSession) ListPlaylistItems(_ context.Context, playlistID string, limit, offset int) (client.PlaylistItemPage, error) {
	session.resourceIDs = append(session.resourceIDs, fmt.Sprintf("list:%s:%d", playlistID, offset))
	session.pageCalls = append(session.pageCalls, offset)
	if len(session.operations) > 0 && session.verifyPageErr != nil {
		return client.PlaylistItemPage{}, session.verifyPageErr
	}
	if len(session.operations) > 0 && session.verifyRace == "sequence" && offset == 0 {
		session.items[len(session.items)-1] = trackItem("9999999999999999999999")
	}
	if len(session.operations) > 0 && session.verifyRace == "metadata" && offset == 0 {
		session.items[len(session.items)-1].Name = "changed display metadata"
	}
	end := min(offset+limit, len(session.items))
	return client.PlaylistItemPage{Items: append([]client.PlaylistItem(nil), session.items[offset:end]...), Offset: offset, Limit: limit, HasNext: end < len(session.items)}, nil
}
func (session *updateSession) AddPlaylistItems(_ context.Context, playlistID string, uris []string, position *int) (string, error) {
	session.resourceIDs = append(session.resourceIDs, "add:"+playlistID)
	session.operations = append(session.operations, fmt.Sprintf("add:%s:%d", strings.Join(uris, ","), *position))
	if session.addErr != nil {
		return "", session.addErr
	}
	if session.addSnapshot == "" {
		session.addSnapshot = "after-add"
	}
	switch session.addRace {
	case "wrong position":
		session.items[*position], session.items[*position+1] = session.items[*position+1], session.items[*position]
	case "duplicate replacement":
		session.items[*position+1] = trackItem(strings.TrimPrefix(uris[0], "spotify:track:"))
	}
	id := strings.TrimPrefix(uris[0], "spotify:track:")
	session.items = append(session.items, client.PlaylistItem{})
	copy(session.items[*position+1:], session.items[*position:])
	session.items[*position] = trackItem(id)
	session.snapshot = session.addSnapshot
	return session.addSnapshot, nil
}
func (session *updateSession) RemovePlaylistItemAtPosition(_ context.Context, playlistID, uri string, position int, snapshot string) (string, error) {
	session.resourceIDs = append(session.resourceIDs, "remove:"+playlistID)
	session.operations = append(session.operations, fmt.Sprintf("remove:%s:%d:%s", uri, position, snapshot))
	if session.removeErr != nil {
		return "", session.removeErr
	}
	if session.finalGapDuplicate {
		session.items = append(session.items, trackItem(strings.TrimPrefix(uri, "spotify:track:")))
	}
	if position < 0 || position >= len(session.items) || session.items[position].URI != uri {
		return "", client.ErrUpstream
	}
	session.items = append(session.items[:position], session.items[position+1:]...)
	return "after-remove", nil
}

func TestPlaylistItemsUpdateReplacesStartMiddleAndEndWithAcceptedReferences(t *testing.T) {
	for _, test := range []struct {
		name              string
		playlistReference string
		trackReference    string
		position          string
		wantPosition      int
		wantOld           string
	}{
		{name: "start raw IDs", playlistReference: playlistID, trackReference: trackID3, position: "0", wantOld: trackID1},
		{name: "middle URIs", playlistReference: "spotify:playlist:" + playlistID, trackReference: "spotify:track:" + trackID3, position: "1", wantPosition: 1, wantOld: trackID2},
		{name: "end URLs", playlistReference: "https://open.spotify.com/playlist/" + playlistID, trackReference: "https://open.spotify.com/track/" + trackID3, position: "2", wantPosition: 2, wantOld: "2222222222222222222222"},
	} {
		t.Run(test.name, func(t *testing.T) {
			lastID := "2222222222222222222222"
			session := &updateSession{scopes: mutationScopes(), snapshot: "before", items: []client.PlaylistItem{trackItem(trackID1), trackItem(trackID2), trackItem(lastID)}}
			stdout, stderr, opens, err := executeUpdate(session, "playlists", "items", "update", test.playlistReference, test.position, "--item", test.trackReference)
			want := fmt.Sprintf("updated\t%s\t%s\t%s\t%s\tafter-remove\n", playlistID, test.position, test.wantOld, trackID3)
			if err != nil || stdout != want || stderr != "" || opens != 1 || !session.closed || session.gets != 4 ||
				fmt.Sprint(session.pageCalls) != "[0 0]" || fmt.Sprint(session.operations) != fmt.Sprintf("[add:spotify:track:%s:%s remove:spotify:track:%s:%d:after-add]", trackID3, test.position, test.wantOld, test.wantPosition+1) {
				t.Fatalf("stdout=%q stderr=%q opens=%d gets=%d pages=%v operations=%v error=%v", stdout, stderr, opens, session.gets, session.pageCalls, session.operations, err)
			}
			if fmt.Sprint(session.resourceIDs) != fmt.Sprintf("[get:%s list:%s:0 get:%s add:%s get:%s list:%s:0 get:%s remove:%s]", playlistID, playlistID, playlistID, playlistID, playlistID, playlistID, playlistID, playlistID) {
				t.Fatalf("resource IDs=%v", session.resourceIDs)
			}
			wantIDs := []string{trackID1, trackID2, lastID}
			wantIDs[test.wantPosition] = trackID3
			for index, item := range session.items {
				if item.ID != wantIDs[index] {
					t.Fatalf("items=%v", session.items)
				}
			}
		})
	}
}

func TestPlaylistItemsUpdateReadsEveryPageAndRejectsUnsafeStateBeforeMutation(t *testing.T) {
	items := make([]client.PlaylistItem, 51)
	for index := range items {
		items[index] = trackItem(fmt.Sprintf("%022d", index))
	}
	session := &updateSession{scopes: mutationScopes(), snapshot: "before", items: items}
	result, err := updatePlaylistItem(context.Background(), session, playlistID, 50, trackID3)
	if err != nil || result.OldTrackID != items[50].ID || fmt.Sprint(session.pageCalls) != "[0 50 0 50]" || len(session.operations) != 2 ||
		fmt.Sprint(session.resourceIDs) != fmt.Sprintf("[get:%s list:%s:0 list:%s:50 get:%s add:%s get:%s list:%s:0 list:%s:50 get:%s remove:%s]", playlistID, playlistID, playlistID, playlistID, playlistID, playlistID, playlistID, playlistID, playlistID, playlistID) {
		t.Fatalf("result=%+v pages=%v operations=%v error=%v", result, session.pageCalls, session.operations, err)
	}

	for _, test := range []struct {
		name        string
		replacement string
		duplicate   string
		wantErr     error
	}{
		{name: "source across page boundary", replacement: trackID3, duplicate: trackID1, wantErr: errRemoveDuplicate},
		{name: "replacement across page boundary", replacement: trackID3, duplicate: trackID3, wantErr: errUpdateDuplicate},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := make([]client.PlaylistItem, 51)
			for index := range items {
				items[index] = trackItem(fmt.Sprintf("%022d", index))
			}
			items[0] = trackItem(trackID1)
			items[50] = trackItem(test.duplicate)
			session := &updateSession{scopes: mutationScopes(), snapshot: "before", items: items}
			_, err := updatePlaylistItem(context.Background(), session, playlistID, 0, test.replacement)
			if !errors.Is(err, test.wantErr) || fmt.Sprint(session.pageCalls) != "[0 50]" || len(session.operations) != 0 ||
				fmt.Sprint(session.resourceIDs) != fmt.Sprintf("[get:%s list:%s:0 list:%s:50]", playlistID, playlistID, playlistID) {
				t.Fatalf("pages=%v resources=%v operations=%v error=%v", session.pageCalls, session.resourceIDs, session.operations, err)
			}
		})
	}

	for _, test := range []struct {
		name        string
		position    int
		replacement string
		items       []client.PlaylistItem
		stale       bool
		staleCount  bool
	}{
		{name: "out of range", position: 3, replacement: trackID3, items: []client.PlaylistItem{trackItem(trackID1)}},
		{name: "episode", replacement: trackID3, items: []client.PlaylistItem{{Type: "episode", ID: trackID1, URI: "spotify:episode:" + trackID1}}},
		{name: "local", replacement: trackID3, items: []client.PlaylistItem{{Type: "local"}}},
		{name: "unavailable", replacement: trackID3, items: []client.PlaylistItem{{Type: "unavailable"}}},
		{name: "future", replacement: trackID3, items: []client.PlaylistItem{{Type: "audiobook", ID: trackID1, URI: "spotify:track:" + trackID1}}},
		{name: "same item", replacement: trackID1, items: []client.PlaylistItem{trackItem(trackID1)}},
		{name: "duplicate source", replacement: trackID3, items: []client.PlaylistItem{trackItem(trackID1), trackItem(trackID1)}},
		{name: "duplicate replacement", replacement: trackID3, items: []client.PlaylistItem{trackItem(trackID1), trackItem(trackID3)}},
		{name: "stale", replacement: trackID3, items: []client.PlaylistItem{trackItem(trackID1)}, stale: true},
		{name: "stale count", replacement: trackID3, items: []client.PlaylistItem{trackItem(trackID1)}, staleCount: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &updateSession{scopes: mutationScopes(), snapshot: "before", items: test.items, stale: test.stale, staleCount: test.staleCount}
			_, err := updatePlaylistItem(context.Background(), session, playlistID, test.position, test.replacement)
			if err == nil || len(session.operations) != 0 {
				t.Fatalf("operations=%v error=%v", session.operations, err)
			}
		})
	}
}

func TestPlaylistItemsUpdateVerifiesConfirmedAddBeforeRemovingOriginal(t *testing.T) {
	readSecret := errors.New("read-secret")
	for _, test := range []struct {
		name          string
		addRace       string
		verifyRace    string
		verifyErr     error
		verifyPageErr error
	}{
		{name: "wrong-position race during add", addRace: "wrong position"},
		{name: "duplicate-replacement race during add", addRace: "duplicate replacement"},
		{name: "first snapshot mismatch", verifyRace: "first snapshot"},
		{name: "first count mismatch", verifyRace: "first count"},
		{name: "sequence mismatch", verifyRace: "sequence"},
		{name: "second snapshot mismatch", verifyRace: "second snapshot"},
		{name: "second count mismatch", verifyRace: "second count"},
		{name: "verification read failure", verifyErr: readSecret},
		{name: "verification page failure", verifyPageErr: readSecret},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &updateSession{
				scopes: mutationScopes(), snapshot: "before",
				items:   []client.PlaylistItem{trackItem(trackID1), trackItem(trackID2), trackItem("2222222222222222222222")},
				addRace: test.addRace, verifyRace: test.verifyRace, verifyErr: test.verifyErr, verifyPageErr: test.verifyPageErr,
			}
			var stdout bytes.Buffer
			stderr, err := executeMutationAtBoundary(Dependencies{OpenUpdateSession: func(context.Context, string, bool) (UpdateSession, error) { return session, nil }}, &stdout,
				"playlists", "items", "update", playlistID, "1", "--item", trackID3)
			var reconcile *ReplacementReconciliationError
			if exitcode.Code(err) != exitcode.Upstream || !errors.As(err, &reconcile) || reconcile.Phase != "verify" ||
				reconcile.PlaylistID != playlistID || reconcile.Position != 1 || reconcile.OldTrackID != trackID2 ||
				reconcile.NewTrackID != trackID3 || reconcile.AddSnapshot != "after-add" || stdout.String() != "" ||
				stderr != err.Error()+"\n" || strings.Contains(err.Error(), "read-secret") || len(session.operations) != 1 {
				t.Fatalf("stdout=%q stderr=%q reconcile=%+v operations=%v error=%v", stdout.String(), stderr, reconcile, session.operations, err)
			}
			if (test.verifyErr != nil || test.verifyPageErr != nil) && !errors.Is(err, readSecret) {
				t.Fatalf("verification read cause was not preserved: %v", err)
			}
			originalPresent := false
			for _, item := range session.items {
				originalPresent = originalPresent || item.ID == trackID2
			}
			if !originalPresent {
				t.Fatalf("original was removed: %v", session.items)
			}
		})
	}
}

func TestPlaylistItemsUpdateIgnoresMutableMetadataDuringAddVerification(t *testing.T) {
	session := &updateSession{
		scopes: mutationScopes(), snapshot: "before", verifyRace: "metadata",
		items: []client.PlaylistItem{trackItem(trackID1), trackItem(trackID2)},
	}
	result, err := updatePlaylistItem(context.Background(), session, playlistID, 1, trackID3)
	if err != nil || result.OldTrackID != trackID2 || len(session.operations) != 2 {
		t.Fatalf("result=%+v operations=%v error=%v", result, session.operations, err)
	}
}

func TestPlaylistItemsUpdateFinalGapDuplicateCannotBeRemoved(t *testing.T) {
	session := &updateSession{
		scopes: mutationScopes(), snapshot: "before", finalGapDuplicate: true,
		items: []client.PlaylistItem{trackItem(trackID1), trackItem(trackID2), trackItem("2222222222222222222222")},
	}
	result, err := updatePlaylistItem(context.Background(), session, playlistID, 1, trackID3)
	oldCount := 0
	for _, item := range session.items {
		if item.ID == trackID2 {
			oldCount++
		}
	}
	if err != nil || result.OldTrackID != trackID2 || oldCount != 1 || len(session.items) != 4 ||
		session.items[1].ID != trackID3 || session.items[2].ID == trackID2 || session.items[3].ID != trackID2 ||
		fmt.Sprint(session.operations) != "[add:spotify:track:"+trackID3+":1 remove:spotify:track:"+trackID2+":2:after-add]" {
		t.Fatalf("result=%+v items=%v operations=%v error=%v", result, session.items, session.operations, err)
	}
}

func TestPlaylistItemsUpdateValidatesBeforeOpeningAndRequiresAllScopes(t *testing.T) {
	for _, args := range [][]string{
		{"playlists", "items", "update", playlistID},
		{"playlists", "items", "update", "bad", "0", "--item", trackID3},
		{"playlists", "items", "update", playlistID, "-1", "--item", trackID3},
		{"playlists", "items", "update", playlistID, "wat", "--item", trackID3},
		{"playlists", "items", "update", playlistID, "0"},
		{"playlists", "items", "update", playlistID, "0", "--item", "bad"},
	} {
		_, _, opens, err := executeUpdate(&updateSession{}, args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v opens=%d error=%v", args, opens, err)
		}
	}
	for _, missing := range mutationScopes() {
		scopes := make([]string, 0, 3)
		for _, scope := range mutationScopes() {
			if scope != missing {
				scopes = append(scopes, scope)
			}
		}
		session := &updateSession{scopes: scopes, snapshot: "before", items: []client.PlaylistItem{trackItem(trackID1)}}
		stdout, stderr, opens, err := executeUpdate(session, "playlists", "items", "update", playlistID, "0", "--item", trackID3)
		if exitcode.Code(err) != exitcode.Config || stdout != "" || stderr != "" || opens != 1 || !session.closed || session.gets != 0 || len(session.operations) != 0 {
			t.Fatalf("missing=%s stdout=%q stderr=%q opens=%d error=%v", missing, stdout, stderr, opens, err)
		}
	}
}

func TestPlaylistItemsUpdateFailureSemantics(t *testing.T) {
	definiteAdd := updateMutationHTTPError(t, http.MethodPost, http.StatusTooManyRequests)
	uncertainAdd := updateMutationHTTPError(t, http.MethodPost, http.StatusServiceUnavailable)
	definiteRemove := updateMutationHTTPError(t, http.MethodDelete, http.StatusBadRequest)
	uncertainRemove := updateMutationHTTPError(t, http.MethodDelete, http.StatusBadGateway)
	for _, test := range []struct {
		name            string
		addErr          error
		removeErr       error
		wantType        string
		wantPhase       string
		wantAddSnapshot string
	}{
		{name: "definite HTTP add", addErr: definiteAdd, wantType: "upstream"},
		{name: "uncertain add", addErr: uncertainAdd, wantType: "reconcile", wantPhase: "add"},
		{name: "untyped add defaults uncertain", addErr: client.ErrUpstream, wantType: "reconcile", wantPhase: "add"},
		{name: "definite HTTP remove", removeErr: definiteRemove, wantType: "partial"},
		{name: "uncertain remove", removeErr: uncertainRemove, wantType: "reconcile", wantPhase: "remove", wantAddSnapshot: "after-add"},
		{name: "untyped remove defaults uncertain", removeErr: client.ErrUpstream, wantType: "reconcile", wantPhase: "remove", wantAddSnapshot: "after-add"},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &updateSession{scopes: mutationScopes(), snapshot: "before\n", items: []client.PlaylistItem{trackItem(trackID1)}, addErr: test.addErr, removeErr: test.removeErr}
			var stdout bytes.Buffer
			stderr, err := executeMutationAtBoundary(Dependencies{OpenUpdateSession: func(context.Context, string, bool) (UpdateSession, error) { return session, nil }}, &stdout,
				"playlists", "items", "update", playlistID, "0", "--item", trackID3)
			if exitcode.Code(err) != exitcode.Upstream || stdout.String() != "" || len(session.operations) != 1+boolInt(test.removeErr != nil) {
				t.Fatalf("stdout=%q stderr=%q operations=%v error=%v", stdout.String(), stderr, session.operations, err)
			}
			switch test.wantType {
			case "upstream":
				if stderr != client.ErrUpstream.Error()+"\n" {
					t.Fatalf("stderr=%q", stderr)
				}
			case "partial":
				var partial *PartialReplacementError
				var rejected *client.MutationRejectedError
				if !errors.As(err, &partial) || !errors.As(err, &rejected) || partial.PlaylistID != playlistID || partial.Position != 0 || partial.OldTrackID != trackID1 || partial.NewTrackID != trackID3 || partial.AddSnapshot != "after-add" ||
					!strings.Contains(err.Error(), "add succeeded") || !strings.Contains(err.Error(), "removal was rejected without applying") ||
					!strings.Contains(err.Error(), "inspect current playlist") || strings.Contains(err.Error(), "both tracks remain") {
					t.Fatalf("partial=%+v error=%v", partial, err)
				}
				if stderr != err.Error()+"\n" {
					t.Fatalf("stderr=%q", stderr)
				}
			case "reconcile":
				var reconcile *ReplacementReconciliationError
				if !errors.As(err, &reconcile) || reconcile.Phase != test.wantPhase || reconcile.AddSnapshot != test.wantAddSnapshot || reconcile.PlaylistID != playlistID || reconcile.Position != 0 || reconcile.OldTrackID != trackID1 || reconcile.NewTrackID != trackID3 || strings.Contains(err.Error(), "\n") || stderr != err.Error()+"\n" {
					t.Fatalf("reconcile=%+v stderr=%q error=%v", reconcile, stderr, err)
				}
			}
		})
	}
}

func updateMutationHTTPError(t *testing.T, method string, statusCode int) error {
	t.Helper()
	calls := 0
	spotify := client.Client{HTTPClient: &http.Client{Transport: updateRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: statusCode, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	var err error
	if method == http.MethodPost {
		_, err = spotify.AddPlaylistItems(context.Background(), playlistID, []string{"spotify:track:" + trackID3}, nil)
	} else {
		_, err = spotify.RemovePlaylistItemAtPosition(context.Background(), playlistID, "spotify:track:"+trackID1, 1, "after-add")
	}
	if err == nil || calls != 1 {
		t.Fatalf("method=%s status=%d calls=%d error=%v", method, statusCode, calls, err)
	}
	return err
}

type updateRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip updateRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestPlaylistItemsUpdateOutputFailurePreservesInverseRecord(t *testing.T) {
	writerFailure := errors.New("writer-secret")
	writer := &prefixFailingWriter{limit: 9, err: writerFailure}
	session := &updateSession{scopes: mutationScopes(), snapshot: "before", items: []client.PlaylistItem{trackItem(trackID1)}}
	stderr, err := executeMutationAtBoundary(Dependencies{OpenUpdateSession: func(context.Context, string, bool) (UpdateSession, error) { return session, nil }}, writer,
		"playlists", "items", "update", playlistID, "0", "--item", trackID3)
	want := "updated\t" + playlistID + "\t0\t" + trackID1 + "\t" + trackID3 + "\tafter-remove"
	var outputErr *PostMutationOutputError
	if exitcode.Code(err) != exitcode.Generic || !errors.As(err, &outputErr) || !errors.Is(err, writerFailure) || outputErr.AppliedOutcome != want || stderr != "playlist mutation applied but output failed; recovery record: "+want+"\n" {
		t.Fatalf("stderr=%q output=%+v error=%v", stderr, outputErr, err)
	}
}

func executeUpdate(session UpdateSession, args ...string) (string, string, int, error) {
	opens := 0
	stdout, stderr, err := executeWithDependencies(Dependencies{OpenUpdateSession: func(context.Context, string, bool) (UpdateSession, error) {
		opens++
		return session, nil
	}}, args...)
	return stdout, stderr, opens, err
}
