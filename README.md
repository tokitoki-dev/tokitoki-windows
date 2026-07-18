# TokiToki Windows

A native Windows tray app for TokiToki. The app is a small Go executable that
uses the shared `github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib` package for local
AI usage scanning and upload.

## Architecture

```text
tokitoki-windows.exe
  ├─ native Windows tray and settings UI
  ├─ launch-at-login registry integration
  ├─ recursive Claude Code / Codex directory watcher
  ├─ periodic sync scheduler
  └─ agentlib sync engine from ../tokitoki-cli
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

## Source layout

This project imports the agent library through this local replacement:

```text
replace github.com/tokitoki-dev/tokitoki-cli => ../tokitoki-cli
```

For local builds, keep both projects next to each other:

```text
tokitoki/
  tokitoki-cli/
  tokitoki-windows/
```

If `tokitoki-windows` is copied alone, `make` cannot resolve `agentlib` and Go
will report that the replacement path cannot be found.

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

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
