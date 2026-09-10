# Architecture

C17 port of the Tokitoki Windows tray agent, feature-for-feature parity with
the Go original and the Rust rewrite, built on inbox Windows facilities. The
exe links against system DLLs only (WinHTTP/schannel for TLS, CNG bcrypt for
SHA-256, comctl32/uxtheme/dwmapi for UI). The one vendored dependency is
miniz 3.0.2 (`third_party/miniz`, pinned and digest-recorded) for DEFLATE;
`src/util/inflate.c` layers gzip header/trailer handling with CRC32
verification on top of it.

## Layout

| Area | Files | Notes |
|---|---|---|
| util | `src/util/` | buf, wstr (UTF-8/16 + char-safe truncate), json (narrow shapes, skips legacy fields), sha256 (CNG), http (WinHTTP), inflate (gzip framing + CRC32 over vendored miniz) |
| domain | `src/*.c` | version, settings (atomic JSON), data_dirs (15-provider table), agent_cli (hidden-console CLI runner + key verify + resource-payload seeding), app_update (digest-verified atomic swap), syncer (coalescing), watcher (recursive RDCW + debounce), instance, launch, logo (SDF glyph), app (coordinator) |
| ui | `src/ui/` | tray, theme (cached uxtheme ordinals), task dialogs, full Settings window, updater flow, hidden-window message loop |

Fixes that go beyond the Go/Rust behavior (all found by the Rust release
review): `TaskbarCreated` re-adds the tray icon after an Explorer restart,
`NIM_SETVERSION` makes update balloons clickable, a manual update check
feeds the same pending slot the tray menu installs from, preference writes
are serialized under one lock, and stderr details truncate on UTF-8
character boundaries.

## Release build

Flags: `/std:c17 /W4 /WX /permissive- /utf-8 /O1 /GL /MT` with
`/LTCG /OPT:REF /OPT:ICF`; version injected via `-Version x.y.z` →
`/DTOKITOKI_VERSION`. The CLI payload embeds as an RCDATA resource; absent
payload = dev build (no seeding, no downloads, ever).

CI-style pinned-CLI bundling reuses `scripts/fetch-cli-release.ps1`
(tag + SHA-256 pins in `scripts/cli-release-pins.ps1`).

## Behavior constants (parity)

Data dir `%USERPROFILE%\.tokitoki` · settings `windows-settings.json` ·
mutex `Local\TokitokiWindowsTray` (interops with the Go/Rust builds, verified
live) · sync every 30 min · watch debounce 2 s · CLI update daily · app
update check 5 s after launch, then daily · sync timeout 2 min · short CLI
ops 15 s · base URL `TOKITOKI_BASE_URL` (default `https://tokitoki.dev`).
