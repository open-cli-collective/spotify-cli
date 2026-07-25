package cmdutil

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
)

func TestOpenSessionWithoutBackendFlag(t *testing.T) {
	var backend string
	var backendSet bool
	got, err := OpenSession(&cobra.Command{}, func(_ context.Context, value string, explicit bool) (int, error) {
		backend, backendSet = value, explicit
		return 1, nil
	})
	if err != nil || got != 1 || backend != "" || backendSet {
		t.Fatalf("session=%d backend=%q explicit=%t error=%v", got, backend, backendSet, err)
	}
}
