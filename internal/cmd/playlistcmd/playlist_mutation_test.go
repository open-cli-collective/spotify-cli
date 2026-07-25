package playlistcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/open-cli-collective/spotify-cli/internal/auth"
	"github.com/open-cli-collective/spotify-cli/internal/client"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

const (
	trackID1 = "abcdefghijklmnopqrstuv"
	trackID2 = "ZYXWVUTSRQPONMLKJIHGFE"
	trackID3 = "1111111111111111111111"
)

type addCall struct {
	uris     []string
	position *int
}

type prefixFailingWriter struct {
	buffer bytes.Buffer
	limit  int
	err    error
}

func (writer *prefixFailingWriter) Write(value []byte) (int, error) {
	accepted := min(max(writer.limit-writer.buffer.Len(), 0), len(value))
	if accepted > 0 {
		_, _ = writer.buffer.Write(value[:accepted])
	}
	return accepted, writer.err
}

func (writer *prefixFailingWriter) String() string { return writer.buffer.String() }

type mutationSession struct {
	scopes       []string
	closed       bool
	gets         []client.Playlist
	getCalls     int
	pages        map[int]client.PlaylistItemPage
	pageCalls    []int
	addCalls     []addCall
	removeCalls  []string
	mutationErr  error
	failAddCall  int
	addSnapshots []string
}

func mutationScopes() []string {
	return []string{
		auth.ScopePlaylistModifyPrivate, auth.ScopePlaylistModifyPublic,
		auth.ScopePlaylistReadCollaborative, auth.ScopePlaylistReadPrivate,
	}
}

func playlistState(total int, snapshot string) client.Playlist {
	return client.Playlist{ID: playlistID, ItemCount: &client.PlaylistItemCount{Total: total}, SnapshotID: snapshot}
}

func trackItem(id string) client.PlaylistItem {
	return client.PlaylistItem{Type: "track", ID: id, URI: "spotify:track:" + id}
}

func (session *mutationSession) Close() error     { session.closed = true; return nil }
func (session *mutationSession) Scopes() []string { return session.scopes }
func (session *mutationSession) GetPlaylist(_ context.Context, _ string) (client.Playlist, error) {
	if len(session.gets) == 0 {
		return client.Playlist{}, session.mutationErr
	}
	index := min(session.getCalls, len(session.gets)-1)
	session.getCalls++
	return session.gets[index], nil
}
func (session *mutationSession) ListPlaylistItems(_ context.Context, _ string, limit, offset int) (client.PlaylistItemPage, error) {
	session.pageCalls = append(session.pageCalls, offset)
	page, ok := session.pages[offset]
	if !ok {
		return client.PlaylistItemPage{}, session.mutationErr
	}
	page.Offset = offset
	page.Limit = limit
	return page, nil
}
func (session *mutationSession) AddPlaylistItems(_ context.Context, _ string, uris []string, position *int) (string, error) {
	call := addCall{uris: append([]string(nil), uris...)}
	if position != nil {
		value := *position
		call.position = &value
	}
	session.addCalls = append(session.addCalls, call)
	if session.failAddCall == len(session.addCalls) {
		return "", session.mutationErr
	}
	if len(session.addSnapshots) >= len(session.addCalls) {
		return session.addSnapshots[len(session.addCalls)-1], nil
	}
	return fmt.Sprintf("snapshot-%d", len(session.addCalls)), nil
}
func (session *mutationSession) RemovePlaylistItemsByURI(_ context.Context, _ string, uri, snapshot string) (string, error) {
	session.removeCalls = append(session.removeCalls, uri+":"+snapshot)
	if session.mutationErr != nil {
		return "", session.mutationErr
	}
	return "final-snapshot", nil
}

func TestPlaylistItemsAddPositionsOrderDuplicatesAndChunking(t *testing.T) {
	for _, test := range []struct {
		name         string
		positionArgs []string
		wantPosition int
	}{
		{name: "start", positionArgs: []string{"--position", "0"}, wantPosition: 0},
		{name: "middle", positionArgs: []string{"--position", "2"}, wantPosition: 2},
		{name: "end", positionArgs: []string{"--position", "3"}, wantPosition: 3},
		{name: "append", wantPosition: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &mutationSession{scopes: mutationScopes(), gets: []client.Playlist{playlistState(3, "before")}}
			secondReference := "spotify:track:" + trackID1
			if test.name == "middle" {
				secondReference = "https://open.spotify.com/track/" + trackID1
			}
			args := []string{"playlists", "items", "add", playlistID, trackID1, secondReference}
			args = append(args, test.positionArgs...)
			stdout, stderr, opens, err := executeAdd(session, args...)
			if err != nil || stderr != "" || opens != 1 || stdout != fmt.Sprintf("added\t%s\t%d\t2\tsnapshot-1\n", playlistID, test.wantPosition) ||
				len(session.addCalls) != 1 || strings.Join(session.addCalls[0].uris, ",") != "spotify:track:"+trackID1+",spotify:track:"+trackID1 {
				t.Fatalf("stdout=%q stderr=%q opens=%d calls=%+v error=%v", stdout, stderr, opens, session.addCalls, err)
			}
			if test.name == "append" && session.addCalls[0].position != nil || test.name != "append" && (session.addCalls[0].position == nil || *session.addCalls[0].position != test.wantPosition) {
				t.Fatalf("position=%v", session.addCalls[0].position)
			}
		})
	}

	references := make([]string, 101)
	for index := range references {
		if index%2 == 0 {
			references[index] = trackID1
		} else {
			references[index] = trackID2
		}
	}
	wantURIs := make([]string, len(references))
	for index, reference := range references {
		wantURIs[index] = "spotify:track:" + reference
	}
	for _, test := range []struct {
		name              string
		requestedPosition *int
		wantStart         int
		wantPositions     []*int
	}{
		{name: "positioned", requestedPosition: intPointer(2), wantStart: 2, wantPositions: []*int{intPointer(2), intPointer(102)}},
		{name: "append", wantStart: 4, wantPositions: []*int{nil, nil}},
	} {
		t.Run("chunked "+test.name, func(t *testing.T) {
			session := &mutationSession{scopes: mutationScopes(), gets: []client.Playlist{playlistState(4, "before")}, addSnapshots: []string{"chunk-1", "chunk-2"}}
			result, err := addPlaylistItems(context.Background(), session, playlistID, wantURIs, test.requestedPosition)
			if err != nil || result != (addResult{StartPosition: test.wantStart, Count: 101, SnapshotID: "chunk-2"}) || len(session.addCalls) != 2 ||
				!slices.Equal(session.addCalls[0].uris, wantURIs[:100]) || !slices.Equal(session.addCalls[1].uris, wantURIs[100:]) ||
				!equalIntPointers(session.addCalls[0].position, test.wantPositions[0]) || !equalIntPointers(session.addCalls[1].position, test.wantPositions[1]) {
				t.Fatalf("result=%+v calls=%+v error=%v", result, session.addCalls, err)
			}
		})
	}
}

func intPointer(value int) *int { return &value }

func equalIntPointers(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func TestPlaylistItemsAddValidatesBeforeSessionAndReportsPartialSuccess(t *testing.T) {
	for _, args := range [][]string{
		{"playlists", "items", "add", playlistID},
		{"playlists", "items", "add", "bad", trackID1},
		{"playlists", "items", "add", playlistID, trackID1, "bad"},
		{"playlists", "items", "add", playlistID, trackID1, "--position", "-1"},
	} {
		_, _, opens, err := executeAdd(&mutationSession{}, args...)
		if exitcode.Code(err) != exitcode.Usage || opens != 0 {
			t.Fatalf("args=%v opens=%d error=%v", args, opens, err)
		}
	}
	overEnd := &mutationSession{scopes: mutationScopes(), gets: []client.Playlist{playlistState(3, "before")}}
	stdout, stderr, opens, err := executeAdd(overEnd, "playlists", "items", "add", playlistID, trackID1, "--position", "4")
	if exitcode.Code(err) != exitcode.Usage || stdout != "" || stderr != "" || opens != 1 || len(overEnd.addCalls) != 0 {
		t.Fatalf("over-end stdout=%q stderr=%q opens=%d calls=%v error=%v", stdout, stderr, opens, overEnd.addCalls, err)
	}

	references := make([]string, 101)
	for index := range references {
		references[index] = trackID1
	}
	session := &mutationSession{
		scopes: mutationScopes(), gets: []client.Playlist{playlistState(0, "before")},
		failAddCall: 2, mutationErr: client.ErrUpstream, addSnapshots: []string{"chunk-1"},
	}
	stdout, stderr, _, err = executeAdd(session, append([]string{"playlists", "items", "add", playlistID}, references...)...)
	var partial *PartialMutationError
	if exitcode.Code(err) != exitcode.Upstream || !errors.As(err, &partial) || !errors.Is(err, client.ErrUpstream) ||
		stdout != "added\t"+playlistID+"\t0\t100\tchunk-1\n" || stderr != "" || len(session.addCalls) != 2 {
		t.Fatalf("stdout=%q stderr=%q calls=%d error=%v", stdout, stderr, len(session.addCalls), err)
	}
	if partial.Cause != client.ErrUpstream || partial.StartPosition != 0 || partial.CompletedCount != 100 || partial.SnapshotID != "chunk-1" {
		t.Fatalf("partial=%+v", partial)
	}
}

func TestPlaylistItemsAddUncertainOutcomeRequiresReconciliation(t *testing.T) {
	for _, test := range []struct {
		name             string
		references       []string
		failAddCall      int
		addSnapshots     []string
		wantCurrent      int
		wantConfirmed    int
		wantUncertain    int
		wantLastSnapshot string
	}{
		{name: "first chunk", references: []string{trackID1}, failAddCall: 1, wantCurrent: 2, wantUncertain: 1},
		{name: "later chunk", references: make([]string, 101), failAddCall: 2, addSnapshots: []string{"chunk\n1"}, wantCurrent: 102, wantConfirmed: 100, wantUncertain: 1, wantLastSnapshot: "chunk\n1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index := range test.references {
				test.references[index] = trackID1
			}
			providerCause := errors.New("provider-secret")
			uncertainCause := &client.MutationOutcomeUncertainError{
				Cause: providerCause, Method: "POST", Path: "/playlists/" + playlistID + "/items",
			}
			session := &mutationSession{
				scopes: mutationScopes(), gets: []client.Playlist{playlistState(3, "before")},
				failAddCall: test.failAddCall, mutationErr: uncertainCause, addSnapshots: test.addSnapshots,
			}
			var stdout bytes.Buffer
			args := append([]string{"playlists", "items", "add", playlistID}, test.references...)
			args = append(args, "--position", "2")
			stderr, err := executeMutationAtBoundary(Dependencies{OpenAddSession: func(context.Context, string, bool) (AddSession, error) {
				return session, nil
			}}, &stdout, args...)
			var reconcile *MutationReconciliationError
			var clientUncertain *client.MutationOutcomeUncertainError
			if exitcode.Code(err) != exitcode.Upstream || !errors.As(err, &reconcile) || !errors.As(err, &clientUncertain) ||
				!errors.Is(err, providerCause) || stdout.String() != "" || stderr != err.Error()+"\n" ||
				strings.Contains(err.Error(), "provider-secret") || strings.Contains(err.Error(), "\n") {
				t.Fatalf("stdout=%q stderr=%q error=%v", stdout.String(), stderr, err)
			}
			if reconcile.Operation != "add" || reconcile.PlaylistID != playlistID || reconcile.StartPosition != 2 ||
				reconcile.CurrentPosition != test.wantCurrent || reconcile.ConfirmedCount != test.wantConfirmed ||
				reconcile.UncertainCount != test.wantUncertain || reconcile.LastConfirmedSnapshot != test.wantLastSnapshot || reconcile.Cause != uncertainCause {
				t.Fatalf("reconciliation=%+v", reconcile)
			}
		})
	}
}

func TestPlaylistItemsRemoveUncertainOutcomeRequiresReconciliation(t *testing.T) {
	providerCause := errors.New("provider-secret")
	uncertainCause := &client.MutationOutcomeUncertainError{
		Cause: providerCause, Method: "DELETE", Path: "/playlists/" + playlistID + "/items",
	}
	session := &mutationSession{
		scopes: mutationScopes(), gets: []client.Playlist{playlistState(1, "before\nsnapshot"), playlistState(1, "before\nsnapshot")},
		pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1)}}}, mutationErr: uncertainCause,
	}
	var stdout bytes.Buffer
	stderr, err := executeMutationAtBoundary(Dependencies{OpenRemoveSession: func(context.Context, string, bool) (RemoveSession, error) {
		return session, nil
	}}, &stdout, "playlists", "items", "remove", playlistID, "0")
	var reconcile *MutationReconciliationError
	var clientUncertain *client.MutationOutcomeUncertainError
	if exitcode.Code(err) != exitcode.Upstream || !errors.As(err, &reconcile) || !errors.As(err, &clientUncertain) ||
		!errors.Is(err, providerCause) || stdout.String() != "" || stderr != err.Error()+"\n" ||
		strings.Contains(err.Error(), "provider-secret") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("stdout=%q stderr=%q error=%v", stdout.String(), stderr, err)
	}
	if reconcile.Operation != "remove" || reconcile.PlaylistID != playlistID || reconcile.Position != 0 ||
		reconcile.TrackID != trackID1 || reconcile.PriorSnapshot != "before\nsnapshot" || reconcile.Cause != uncertainCause {
		t.Fatalf("reconciliation=%+v", reconcile)
	}
}

func TestPlaylistItemsAddOutputFailurePreservesAppliedOutcome(t *testing.T) {
	writerFailure := errors.New("writer-secret")
	writer := &prefixFailingWriter{limit: 6, err: writerFailure}
	session := &mutationSession{scopes: mutationScopes(), gets: []client.Playlist{playlistState(0, "before")}}
	stderr, err := executeMutationAtBoundary(Dependencies{OpenAddSession: func(context.Context, string, bool) (AddSession, error) {
		return session, nil
	}}, writer, "playlists", "items", "add", playlistID, trackID1)
	wantRecord := "added\t" + playlistID + "\t0\t1\tsnapshot-1"
	var outputErr *PostMutationOutputError
	if exitcode.Code(err) != exitcode.Generic || !errors.As(err, &outputErr) || !errors.Is(err, writerFailure) ||
		writer.String() != wantRecord[:6] || stderr != "playlist mutation applied but output failed; recovery record: "+wantRecord+"\n" {
		t.Fatalf("stdout=%q stderr=%q error=%v", writer.String(), stderr, err)
	}
	if outputErr.AppliedOutcome != wantRecord || outputErr.MutationError != nil || outputErr.Cause != writerFailure || strings.Contains(err.Error(), "writer-secret") {
		t.Fatalf("output error=%+v", outputErr)
	}
}

func TestPlaylistItemsRemoveOutputFailurePreservesAppliedOutcome(t *testing.T) {
	writerFailure := errors.New("writer-secret")
	writer := &prefixFailingWriter{limit: 8, err: writerFailure}
	session := &mutationSession{
		scopes: mutationScopes(), gets: []client.Playlist{playlistState(1, "before"), playlistState(1, "before")},
		pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1)}}},
	}
	stderr, err := executeMutationAtBoundary(Dependencies{OpenRemoveSession: func(context.Context, string, bool) (RemoveSession, error) {
		return session, nil
	}}, writer, "playlists", "items", "remove", playlistID, "0")
	wantRecord := "removed\t" + playlistID + "\t0\t" + trackID1 + "\tfinal-snapshot"
	var outputErr *PostMutationOutputError
	if exitcode.Code(err) != exitcode.Generic || !errors.As(err, &outputErr) || !errors.Is(err, writerFailure) ||
		writer.String() != wantRecord[:8] || stderr != "playlist mutation applied but output failed; recovery record: "+wantRecord+"\n" {
		t.Fatalf("stdout=%q stderr=%q error=%v", writer.String(), stderr, err)
	}
	if outputErr.AppliedOutcome != wantRecord || outputErr.MutationError != nil || outputErr.Cause != writerFailure || strings.Contains(err.Error(), "writer-secret") {
		t.Fatalf("output error=%+v", outputErr)
	}
}

func TestPlaylistItemsPartialAddOutputFailurePreservesBothCauses(t *testing.T) {
	writerFailure := errors.New("writer-secret")
	providerFailure := errors.New("provider-secret")
	writer := &prefixFailingWriter{limit: 7, err: writerFailure}
	references := make([]string, 101)
	for index := range references {
		references[index] = trackID1
	}
	session := &mutationSession{
		scopes: mutationScopes(), gets: []client.Playlist{playlistState(0, "before")},
		failAddCall: 2, mutationErr: providerFailure, addSnapshots: []string{"chunk-1"},
	}
	stderr, err := executeMutationAtBoundary(Dependencies{OpenAddSession: func(context.Context, string, bool) (AddSession, error) {
		return session, nil
	}}, writer, append([]string{"playlists", "items", "add", playlistID}, references...)...)
	wantRecord := "added\t" + playlistID + "\t0\t100\tchunk-1"
	var outputErr *PostMutationOutputError
	var partial *PartialMutationError
	if exitcode.Code(err) != exitcode.Upstream || !errors.As(err, &outputErr) || !errors.As(err, &partial) ||
		!errors.Is(err, writerFailure) || !errors.Is(err, providerFailure) || writer.String() != wantRecord[:7] ||
		stderr != "playlist mutation applied but output failed; recovery record: "+wantRecord+"\n" {
		t.Fatalf("stdout=%q stderr=%q error=%v", writer.String(), stderr, err)
	}
	if outputErr.AppliedOutcome != wantRecord || outputErr.MutationError != partial || outputErr.Cause != writerFailure ||
		strings.Contains(err.Error(), "writer-secret") || strings.Contains(err.Error(), "provider-secret") {
		t.Fatalf("output error=%+v partial=%+v", outputErr, partial)
	}
}

func executeMutationAtBoundary(deps Dependencies, stdout io.Writer, args ...string) (string, error) {
	command := &cobra.Command{Use: "sptfy", SilenceErrors: true, SilenceUsage: true}
	command.AddCommand(New(deps))
	var stderr bytes.Buffer
	command.SetOut(stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)
	err := command.Execute()
	if err != nil && !exitcode.Quiet(err) {
		_, _ = fmt.Fprintln(&stderr, err)
	}
	return stderr.String(), err
}

func TestPlaylistItemsRemoveUniquePositionAndInverseRecord(t *testing.T) {
	items := []client.PlaylistItem{trackItem(trackID1), trackItem(trackID2), trackItem(trackID3)}
	session := &mutationSession{
		scopes: mutationScopes(), gets: []client.Playlist{playlistState(3, "before"), playlistState(3, "before")},
		pages: map[int]client.PlaylistItemPage{0: {Items: items}},
	}
	stdout, stderr, opens, err := executeRemove(session, "playlists", "items", "remove", playlistID, "1")
	want := "removed\t" + playlistID + "\t1\t" + trackID2 + "\tfinal-snapshot\n"
	if err != nil || stdout != want || stderr != "" || opens != 1 || fmt.Sprint(session.pageCalls) != "[0]" ||
		fmt.Sprint(session.removeCalls) != "[spotify:track:"+trackID2+":before]" || session.getCalls != 2 || !session.closed {
		t.Fatalf("stdout=%q stderr=%q opens=%d pages=%v remove=%v gets=%d closed=%t error=%v", stdout, stderr, opens, session.pageCalls, session.removeCalls, session.getCalls, session.closed, err)
	}
}

func TestPlaylistItemsRemoveRejectsUnsafeTargetsBeforeDelete(t *testing.T) {
	duplicateItems := make([]client.PlaylistItem, 51)
	for index := range duplicateItems {
		duplicateItems[index] = trackItem(trackID2)
		duplicateItems[index].ID = fmt.Sprintf("%022d", index)
		duplicateItems[index].URI = "spotify:track:" + duplicateItems[index].ID
	}
	duplicateItems[0] = trackItem(trackID1)
	duplicateItems[50] = trackItem(trackID1)
	for _, test := range []struct {
		name     string
		position string
		gets     []client.Playlist
		pages    map[int]client.PlaylistItemPage
	}{
		{name: "out of range", position: "3", gets: []client.Playlist{playlistState(3, "before")}},
		{name: "episode", position: "0", gets: []client.Playlist{playlistState(1, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{{Type: "episode", ID: trackID1, URI: "spotify:episode:" + trackID1}}}}},
		{name: "local", position: "0", gets: []client.Playlist{playlistState(1, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{{Type: "local"}}}}},
		{name: "unavailable", position: "0", gets: []client.Playlist{playlistState(1, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{{Type: "unavailable"}}}}},
		{name: "future", position: "0", gets: []client.Playlist{playlistState(1, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{{Type: "audiobook", ID: trackID1, URI: "spotify:track:" + trackID1}}}}},
		{name: "duplicate", position: "0", gets: []client.Playlist{playlistState(51, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: duplicateItems[:50], HasNext: true}, 50: {Items: duplicateItems[50:]}}},
		{name: "duplicate missing uri", position: "0", gets: []client.Playlist{playlistState(2, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1), {Type: "track", ID: trackID1}}}}},
		{name: "duplicate mismatched uri", position: "0", gets: []client.Playlist{playlistState(2, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1), {Type: "track", ID: trackID1, URI: "spotify:track:" + trackID2}}}}},
		{name: "stale snapshot", position: "0", gets: []client.Playlist{playlistState(1, "before"), playlistState(1, "changed")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1)}}}},
		{name: "stale count", position: "0", gets: []client.Playlist{playlistState(1, "before"), playlistState(2, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1)}}}},
		{name: "page total mismatch", position: "0", gets: []client.Playlist{playlistState(1, "before")}, pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1)}, HasNext: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &mutationSession{scopes: mutationScopes(), gets: test.gets, pages: test.pages}
			position, _ := strconv.Atoi(test.position)
			_, err := removePlaylistItem(context.Background(), session, playlistID, position)
			if err == nil || len(session.removeCalls) != 0 {
				t.Fatalf("removes=%v error=%v", session.removeCalls, err)
			}
		})
	}
}

func TestPlaylistMutationScopeGuardRequiresAllFourScopes(t *testing.T) {
	for _, missing := range mutationScopes() {
		scopes := make([]string, 0, 3)
		for _, scope := range mutationScopes() {
			if scope != missing {
				scopes = append(scopes, scope)
			}
		}
		for _, test := range []struct {
			args []string
			add  bool
		}{
			{args: []string{"playlists", "items", "add", playlistID, trackID1}, add: true},
			{args: []string{"playlists", "items", "remove", playlistID, "0"}},
		} {
			session := &mutationSession{scopes: scopes}
			var stdout, stderr string
			var opens int
			var err error
			if test.add {
				stdout, stderr, opens, err = executeAdd(session, test.args...)
			} else {
				stdout, stderr, opens, err = executeRemove(session, test.args...)
			}
			if exitcode.Code(err) != exitcode.Config || stdout != "" || stderr != "" || opens != 1 || session.getCalls != 0 ||
				!session.closed || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "init --overwrite") {
				t.Fatalf("missing=%s args=%v stdout=%q stderr=%q opens=%d closed=%t error=%v", missing, test.args, stdout, stderr, opens, session.closed, err)
			}
		}
	}
}

func TestPlaylistRemoveMutationFailureEmitsNothing(t *testing.T) {
	session := &mutationSession{
		scopes: mutationScopes(), gets: []client.Playlist{playlistState(1, "before"), playlistState(1, "before")},
		pages: map[int]client.PlaylistItemPage{0: {Items: []client.PlaylistItem{trackItem(trackID1)}}}, mutationErr: errors.New("failure"),
	}
	stdout, stderr, _, err := executeRemove(session, "playlists", "items", "remove", playlistID, "0")
	if exitcode.Code(err) != exitcode.Upstream || stdout != "" || stderr != "" || len(session.removeCalls) != 1 {
		t.Fatalf("stdout=%q stderr=%q removes=%v error=%v", stdout, stderr, session.removeCalls, err)
	}
}
