package output

import "testing"

func TestRenderSavedChecksSanitizesAndPreservesOrder(t *testing.T) {
	got := RenderSavedChecks([]SavedCheck{
		{Reference: "spotify:album:first", ID: "first", Saved: true},
		{Reference: "second\nvalue", ID: "second", Saved: false},
	})
	want := "REFERENCE | ID | SAVED\n" +
		"spotify:album:first | first | true\n" +
		"second value | second | false\n"
	if got != want {
		t.Fatalf("rendered=%q want=%q", got, want)
	}
}
