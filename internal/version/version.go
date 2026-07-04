// Package version contains build metadata shown in the Windows settings panel.
package version

import "fmt"

var (
	// Version is the release version injected by the build.
	Version = "dev"

	// Commit is the source revision injected by the build.
	Commit = "local"

	// BuildDate is the UTC build timestamp injected by the build.
	BuildDate = "unknown"
)

// Summary returns a compact user-facing version string.
func Summary() string {
	if Commit == "" || Commit == "local" {
		return fmt.Sprintf("Version %s", Version)
	}
	return fmt.Sprintf("Version %s (%s)", Version, Commit)
}
