// Command sptfy is the Spotify CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/open-cli-collective/spotify-cli/internal/cmd/root"
)

func main() {
	if err := root.New().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
