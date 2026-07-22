// Command sptfy is the Spotify CLI entrypoint and production composition root.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/open-cli-collective/spotify-cli/internal/cmd/root"
	"github.com/open-cli-collective/spotify-cli/internal/config"
	"github.com/open-cli-collective/spotify-cli/internal/credentials"
	"github.com/open-cli-collective/spotify-cli/internal/exitcode"
)

func main() {
	if code := run(); code != exitcode.Success {
		os.Exit(code)
	}
}

func run() int {
	cmd := root.New(root.Dependencies{
		In:          os.Stdin,
		Out:         os.Stdout,
		ErrOut:      os.Stderr,
		Scope:       statedir.Scope{Name: config.Service},
		Cache:       statedir.Cache{Tool: config.Tool},
		Data:        statedir.Data{Tool: config.Tool},
		OpenStore:   credentials.ProductionOpener(promptFilePassphrase),
		Interactive: term.IsTerminal(int(os.Stdin.Fd())),
		OpenBrowser: browser.OpenURL,
		HTTPClient:  spotifyHTTPClient(),
	})
	return executeCommand(cmd)
}

func spotifyHTTPClient() *http.Client {
	return &http.Client{
		Transport: http.DefaultTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func executeCommand(cmd *cobra.Command) int {
	if err := cmd.Execute(); err != nil {
		if !exitcode.Quiet(err) {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err)
		}
		return exitcode.Code(err)
	}
	return exitcode.Success
}

func promptFilePassphrase() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("encrypted-file backend needs a TTY prompt or SPOTIFY_CLI_KEYRING_PASSPHRASE")
	}
	_, _ = fmt.Fprint(os.Stderr, "Keyring passphrase: ")
	value, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", errors.New("reading keyring passphrase failed")
	}
	if len(value) == 0 {
		return "", errors.New("keyring passphrase must not be empty")
	}
	return string(value), nil
}
