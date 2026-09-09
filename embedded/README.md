# Embedded shared CLI

Release builds bundle a pinned `tokitoki` CLI here so first launch can seed
`%USERPROFILE%\.tokitoki\bin\tokitoki.exe` without a network round-trip:

- `tokitoki.exe.gz` — gzip of the pinned CLI release for the target arch
- `VERSION` — the pinned version, plain text, no trailing newline

Both files are **gitignored** and fetched at build time (see
`scripts/fetch-cli-release.ps1` in the Go project for the pin/verify flow).
When they are absent — every dev build — `build.rs` omits the `embedded_cli`
cfg and `agent_cli::bootstrap` does nothing: no seeding, no downloads, ever.
