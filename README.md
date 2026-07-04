# TokiToki Windows

A native Windows tray app for TokiToki. The app is a small Go executable that
uses the shared `github.com/labx/tokitoki-agent/pkg/agentlib` package for local
AI usage scanning and upload.

## Architecture

```text
tracklm-windows.exe
  ├─ native Windows tray and settings UI
  ├─ launch-at-login registry integration
  ├─ recursive Claude Code / Codex directory watcher
  ├─ periodic sync scheduler
  └─ agentlib sync engine from ../tracklm-goagent
```

The Windows client does not bundle or spawn a separate agent process. It builds
one executable and shares the same `~/.tokitoki` state as the CLI.

## Build

```sh
make build
```

The release executable is written to:

```text
dist/tracklm-windows-amd64.exe
```

For compatibility, the default amd64 build also writes:

```text
dist/tracklm-windows.exe
```

`make` without a target also runs `make build`.

Build Windows on ARM:

```sh
make build-arm64
```

That writes:

```text
dist/tracklm-windows-arm64.exe
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
