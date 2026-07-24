# Tokitoki Windows

A native Windows tray app for Tokitoki. The app is a small Go executable that
uses the shared `github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib` package for local
AI usage scanning and upload.

## Architecture

```text
tokitoki-windows.exe
  ├─ native Windows tray and settings UI
  ├─ launch-at-login registry integration
  ├─ recursive Claude Code / Codex directory watcher
  ├─ periodic sync scheduler
  └─ agentlib sync engine from the tokitoki-cli module
```

The Windows client does not bundle or spawn a separate agent process. It builds
one executable and shares the same `~/.tokitoki` state as the CLI.

All server access uses `TOKITOKI_BASE_URL` and defaults to
`https://tokitoki.dev`. Override it before starting the app when testing a
local or staging server:

```powershell
$env:TOKITOKI_BASE_URL = "http://localhost:9093"
.\tokitoki-windows.exe
```

## The agent library

`agentlib` comes from the published `github.com/tokitoki-dev/tokitoki-cli`
module at the version `go.mod` pins, like any other dependency. This repo
therefore builds on its own — no sibling checkout, no `replace` directive.

To pick up CLI changes, release them from `tokitoki-cli` first, then bump the
version here:

```sh
go get github.com/tokitoki-dev/tokitoki-cli@v0.2.0
go mod tidy
```

To try an unreleased CLI locally, add a temporary replacement — but never
commit it, or a release would link unpublished code:

```sh
go mod edit -replace=github.com/tokitoki-dev/tokitoki-cli=../tokitoki-cli
# undo with:
go mod edit -dropreplace=github.com/tokitoki-dev/tokitoki-cli
```

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
