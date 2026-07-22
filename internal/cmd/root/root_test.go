package root

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasPrefix(got, "sptfy dev ") {
		t.Fatalf("version output = %q", got)
	}
}
