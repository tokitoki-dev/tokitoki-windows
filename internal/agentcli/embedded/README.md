# Embedded CLI

Release builds place the pinned CLI here as `tokitoki.exe.gz` plus its
`VERSION` — `scripts/fetch-cli-release.ps1` downloads the release asset,
verifies it against the pins in `scripts/cli-release-pins.ps1`, compresses
it, and `go:embed` bakes it into the app; seeding decompresses it. Both
files are gitignored; this README keeps the directory present so dev builds
compile with nothing bundled.
