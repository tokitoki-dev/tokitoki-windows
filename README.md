# Tokitoki Windows

A native Windows tray app for Tokitoki. The app is a small Go executable that
drives the shared `tokitoki` CLI for all scanning and uploading — the same
binary the macOS app and every editor plugin invoke.

## Architecture

```text
tokitoki-windows.exe
  ├─ native Windows tray and settings UI
  ├─ launch-at-login registry integration
  ├─ recursive Claude Code / Codex directory watcher
  ├─ periodic sync scheduler
  └─ shared tokitoki CLI at %USERPROFILE%\.tokitoki\bin\tokitoki.exe
       └─ one short-lived invocation per operation
```

The app links none of the CLI's Go packages. Each operation — sync, reading
or writing the API key, minting a dashboard login link — invokes the shared
CLI once and parses its standard output, so every Tokitoki front-end on a
machine runs exactly the same logic against the same `~/.tokitoki` state.

All server access uses `TOKITOKI_BASE_URL` and defaults to
`https://tokitoki.dev`. Override it before starting the app when testing a
local or staging server:

```powershell
$env:TOKITOKI_BASE_URL = "http://localhost:9093"
.\tokitoki-windows.exe
```

## The shared CLI

The app follows the shared-CLI contract in `tokitoki-cli/README.md`, the same
three rules `AgentProcess.swift` implements on macOS:

1. **Resolve the shared binary** at
   `%USERPROFILE%\.tokitoki\bin\tokitoki.exe` for every invocation.
2. **Seed, never download.** Release builds embed the pinned CLI release via
   `go:embed` (`internal/agentcli/embedded/`). At startup the app seeds the
   shared path when it is missing or older than the embedded copy — staged
   and renamed into place, never a downgrade, never a network fetch.
3. **Delegate freshness to the CLI.** The app runs `tokitoki update` at
   launch and daily; the CLI owns the whole check-download-verify-swap
   sequence for itself.

The embedded CLI is pinned in `scripts/cli-release-pins.ps1` — a tag plus a
SHA-256 per architecture, reviewed together. `scripts/fetch-cli-release.ps1`
downloads the release asset, verifies the digest, the embedded `GOARCH`, and
(host arch permitting) the version the binary reports, then drops it into the
embed directory. The build task runs it automatically.

Dev builds (`go build` without the fetched asset) embed nothing and seed
nothing: they use whatever shared CLI the machine already has. To develop
against unreleased CLI changes, build the sibling checkout straight into the
shared path:

```powershell
go build -o "$env:USERPROFILE\.tokitoki\bin\tokitoki.exe" ../tokitoki-cli/cmd/tokitoki
```

To move a release to a newer CLI, tag and release it in `tokitoki-cli` first,
then update `scripts/cli-release-pins.ps1` with the new tag and the
`checksums.txt` digests from that release.

## Build

```sh
make build
```

The release executable is written to:

```text
dist/tokitoki-windows-amd64.exe
```

For compatibility, the default amd64 build also writes:

```text
dist/tokitoki-windows.exe
```

`make` without a target also runs `make build`.

Build Windows on ARM:

```sh
make build-arm64
```

That writes:

```text
dist/tokitoki-windows-arm64.exe
```

Build both release architectures:

```sh
make build-all
```

Set release metadata:

```sh
make build-all VERSION=1.0.0 COMMIT=$(git rev-parse --short HEAD)
```

Development builds keep a console for diagnostics:

```sh
make debug
```

If the manifest resource needs to be regenerated:

```sh
make generate
```

## Release

Releases are cut by tag. Pushing `vX.Y.Z` runs `.github/workflows/release.yml`,
which tests, builds both architectures, checks each binary really is the
architecture it claims, and publishes a GitHub release carrying:

```text
tokitoki-windows-amd64.exe
tokitoki-windows-arm64.exe
```

Those names are what the server's asset matcher reads, so it can hand each
machine the right build. The unsuffixed `dist/tokitoki-windows.exe` produced by
local builds is deliberately not published.

The tag must point at a commit on `main` — the workflow refuses otherwise — so
merge first, then:

```sh
git switch main
git merge --ff-only dev
git push origin main
git tag v0.1.0
git push origin v0.1.0
```

A published GitHub release does not ship anything to users on its own; rolling
it out still happens in `/admin/releases`.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
