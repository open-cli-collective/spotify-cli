package exitcode

import (
	"errors"
	"testing"
)

func TestTypedErrors(t *testing.T) {
	base := errors.New("boom")
	err := New(Usage, base)
	if Code(err) != Usage || Quiet(err) || !errors.Is(err, base) {
		t.Fatalf("typed error did not preserve code/error: %#v", err)
	}
	quiet := NewQuiet(Config, base)
	if Code(quiet) != Config || !Quiet(quiet) {
		t.Fatalf("quiet error = code %d quiet %v", Code(quiet), Quiet(quiet))
	}
	if Code(base) != Generic {
		t.Fatalf("plain error code = %d, want %d", Code(base), Generic)
	}
}
