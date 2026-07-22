// Package version exposes build metadata stamped into release binaries.
package version

import "fmt"

var (
	// Version is the release version.
	Version = "dev"
	// Commit is the source commit.
	Commit = "unknown"
	// Date is the build timestamp.
	Date = "unknown"
)

// Info formats the build metadata for CLI output.
func Info() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, Date)
}
