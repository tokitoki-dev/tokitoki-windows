# Tokitoki for Windows

Tray app that syncs the token usage and cost of your local AI coding agents
to your [Tokitoki](https://tokitoki.dev) dashboard: Claude Code, Codex,
GitHub Copilot, Gemini CLI and
[a dozen more](https://github.com/tokitoki-dev/tokitoki-cli#supported-tools).
Every AI session shows up next to your coding time, grouped by project, so
you can see what a feature actually cost.

One exe. The app itself is half a megabyte of C17 with no installer, no
runtime and no framework; it links against the DLLs already in Windows.

## Install

1. Download `tokitoki-windows-amd64.exe` (or `arm64`) from the
   [latest release](https://github.com/tokitoki-dev/tokitoki-windows/releases/latest)
   and put it wherever you keep your tools.
2. Run it. Tokitoki appears in the tray.
3. Open **Settings** from the tray icon, paste the API key from
   [tokitoki.dev/settings](https://tokitoki.dev/settings), and turn on
   **Launch at login**.

The exe is not code-signed yet, so SmartScreen may ask once. Releases after
v0.2.0 ship a `checksums.txt` next to the binaries; compare before you click
through:

```powershell
Get-FileHash tokitoki-windows-amd64.exe -Algorithm SHA256
```

## What it does

- Syncs on launch, every 30 minutes, and two seconds after your agents write
  new session data. It watches their data folders, so a Claude Code session
  is on the dashboard before you get back to it.
- Settings: API key with a **Verify Key** check, launch at login, automatic
  updates with a manual **Check for Updates**.
- Updates download in the background, are verified by SHA-256 and swapped in
  atomically.
- Bundles [tokitoki-cli](https://github.com/tokitoki-dev/tokitoki-cli) and
  keeps a shared copy under `%USERPROFILE%\.tokitoki` that the VS Code
  extension and other Tokitoki clients reuse. Nothing runs between syncs.
- Survives an Explorer restart: the tray icon comes back on its own.

## Why C

The same agent exists in Go and Rust. Measured amd64 release builds, each
embedding the same 4.74 MB CLI:

| Implementation | Full exe | App body |
|---|---|---|
| **C** | **5.28 MB** | **≈ 0.54 MB** |
| Rust | 8.78 MB | ≈ 4.0 MB (2.3 MB with opt-level=z) |
| Go | 15.02 MB | ≈ 10.4 MB |

C is small because it externalizes TLS, crypto and Unicode to the OS instead
of bundling them. The only vendored dependency is miniz for gzip.

## Privacy

The app reads token counts, model names and timestamps from your agents'
local data and uploads that metadata over HTTPS with your API key. Never
your code. Delete your data anytime from the dashboard.

## Other clients

[VS Code](https://github.com/tokitoki-dev/tokitoki-vscode) ·
[macOS](https://github.com/tokitoki-dev/tokitoki-macos) ·
[CLI](https://github.com/tokitoki-dev/tokitoki-cli) for servers and scripts.
The overview lives at [github.com/tokitoki-dev](https://github.com/tokitoki-dev).

## Development

MSVC (VS Build Tools / VS 18+). All tasks locate `VsDevCmd.bat` themselves.

```powershell
make            # build ../tokitoki-cli, gzip into embedded/, compile release
make debug      # console-subsystem exe with symbols, no CLI bundling
make test       # unit tests (util + domain): fixtures in tests/fixtures/
make clean
```

Work on `dev`; releases are tagged from `main`. Source layout, release
flags, CLI bundling and the behavior constants shared with the Go and Rust
builds are in [ARCHITECTURE.md](ARCHITECTURE.md).

## License

[Apache License 2.0](LICENSE)
