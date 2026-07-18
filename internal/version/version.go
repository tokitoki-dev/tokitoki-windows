// Package version contains release and build metadata for the Windows app.
package version

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
	return "Version " + Version
}
