# tokitoki-windows-rust

Rust port of [`tokitoki-windows`](../tokitoki-windows) — a native Windows
system-tray agent that watches local AI-coding-assistant data directories
(Claude Code, Codex, Copilot, Gemini, and 11 more) and delegates every
sync/key/update operation to the shared `tokitoki` CLI at
`%USERPROFILE%\.tokitoki\bin\tokitoki.exe`, the same contract the macOS app
and editor plugins follow.

## Module map (Go → Rust)

| Go package           | Rust module        | Status |
|----------------------|--------------------|--------|
| `internal/agentcli`  | `agent_cli`        | ✅ client, sync args, version pinning, embedded-CLI bootstrap |
| `internal/apikey`    | `api_key`          | ✅ server-side key verification |
| `internal/app`       | `app`              | ✅ coordinator, prefs, daily CLI update loop |
| `internal/appupdate` | `app_update`       | ✅ check, trusted transport, sha256 verify, atomic swap, relaunch |
| `internal/datadirs`  | `data_dirs`        | ✅ all 15 providers, env overrides, watch-path union |
| `internal/instance`  | `instance`         | ✅ `Local\TokitokiWindowsTray` named mutex |
| `internal/launch`    | `launch`           | ✅ HKCU Run key, quote/reconcile semantics |
| `internal/logo`      | `logo`             | ✅ SDF clock-mark renderer (ring + hand, 3×3 SSAA) |
| `internal/settings`  | `settings`         | ✅ atomic JSON, inverted flags, legacy-field tolerant |
| `internal/syncer`    | `syncer`           | ✅ capacity-1 coalescing worker + ticker |
| `internal/ui`        | `ui`               | ✅ tray, menu, balloons, theme watch, TaskDialogs, updater flow, Settings window (API-key edit + verify, toggles, update check, dark chrome) |
| `internal/version`   | `version`          | ✅ build-time env injection |
| `internal/watcher`   | `watcher`          | ✅ recursive debounced watching via `notify` |

## Build

```powershell
make            # default: build sibling ../tokitoki-cli from source, gzip it
                # into embedded/, then cargo build --release (CLI embedded)
make clean      # cargo clean + remove the bundled CLI payload
```

`make build` stamps the local CLI with the pinned version from
`scripts/cli-release-pins.ps1` (override: `make CLI_VERSION=x.y.z`); other
knobs: `ARCH=arm64`, `PS=pwsh`.

CI instead runs `scripts/fetch-cli-release.ps1`, which downloads the pinned
release from GitHub, rejects it unless its SHA-256 matches the pin, gzips it
into `embedded/`, and short-circuits when the payload already matches.

A bare `cargo build` with an empty `embedded/` is a dev build: no seeding,
no downloads, ever. Release metadata is injected via env vars read by
`build.rs`:

```powershell
$env:TOKITOKI_VERSION = "0.1.0"; $env:TOKITOKI_COMMIT = "abc123"
$env:TOKITOKI_BUILD_DATE = "2026-01-01T00:00:00Z"; cargo build --release
```

## Checks

```powershell
cargo clippy --all-targets --all-features --locked -- -D warnings
cargo test
cargo fmt --check        # import-group options need nightly: cargo +nightly fmt
```

Lint policy lives in `Cargo.toml` (`clippy::all` = deny, `pedantic` = warn,
`unwrap_used`/`expect_used` = deny outside tests via `clippy.toml`).

## Scaffold TODOs

- **Marquee install progress**: installs run silently in the background; the
  Go app shows a marquee-progress TaskDialog with Cancel.
- **arm64 release builds**: add an `aarch64-pc-windows-msvc` job mirroring
  the Go release workflow.

## Behavior constants (parity with Go)

Data dir `%USERPROFILE%\.tokitoki` · settings `windows-settings.json` ·
sync every 30 min · watch debounce 2 s · CLI update daily · app update check
5 s after launch, then daily · sync timeout 2 min · short CLI ops 15 s ·
base URL `TOKITOKI_BASE_URL` (default `https://tokitoki.dev`).
