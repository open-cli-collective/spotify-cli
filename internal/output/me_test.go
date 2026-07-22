package output

import (
	"reflect"
	"testing"

	"github.com/open-cli-collective/spotify-cli/internal/client"
)

func TestRenderMeText(t *testing.T) {
	got := RenderMeText(client.User{AccountID: "account", ID: "spotify-id", URI: "spotify:user:spotify-id"}, []string{"user-read-private", "alpha"})
	want := "account_id\taccount\ndisplay_name\t-\nspotify_id\tspotify-id\nuri\tspotify:user:spotify-id\nscopes\talpha,user-read-private\n"
	if got != want {
		t.Fatalf("output:\n%s\nwant:\n%s", got, want)
	}
}

func TestMeResult(t *testing.T) {
	got := NewMeResult(client.User{AccountID: "account", DisplayName: "Name"}, []string{"z", "a"})
	want := MeResult{AccountID: "account", DisplayName: "Name", Scopes: []string{"a", "z"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}
