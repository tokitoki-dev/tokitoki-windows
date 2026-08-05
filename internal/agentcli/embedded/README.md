# Embedded CLI

Release builds place the pinned `tokitoki.exe` and its `VERSION` here —
`scripts/fetch-cli-release.ps1` downloads the release asset, verifies it
against the pins in `scripts/cli-release-pins.ps1`, and `go:embed` bakes it
into the app. Both files are gitignored; this README keeps the directory
present so dev builds compile with nothing bundled.
