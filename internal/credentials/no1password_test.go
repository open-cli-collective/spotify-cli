//go:build keyring_no1password

package credentials

import (
	"errors"
	"testing"

	"github.com/open-cli-collective/cli-common/credstore"

	"github.com/open-cli-collective/spotify-cli/internal/config"
)

func TestOnePasswordBackendsFailClosedWhenDisabled(t *testing.T) {
	for _, backend := range []string{"op", "op-connect", "op-desktop"} {
		_, err := ProductionOpener(nil)(OpenRequest{
			Config: config.Default(), Backend: backend, BackendSet: true,
		})
		if !errors.Is(err, credstore.ErrBackendNotImplemented) {
			t.Fatalf("backend %q: error = %v, want ErrBackendNotImplemented", backend, err)
		}
	}
}
