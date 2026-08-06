//! Build metadata injected by `build.rs` (mirrors Go `internal/version`).
//!
//! Release builds set `TOKITOKI_VERSION` / `TOKITOKI_COMMIT` /
//! `TOKITOKI_BUILD_DATE` in the environment; local builds fall back to
//! `dev` / `local` / `unknown`.

/// Application version, e.g. `0.1.0`, or `dev` for local builds.
pub const VERSION: &str = env!("TOKITOKI_VERSION");
/// Git commit the binary was built from, or `local`.
pub const COMMIT: &str = env!("TOKITOKI_COMMIT");
/// UTC build timestamp (RFC 3339), or `unknown`.
pub const BUILD_DATE: &str = env!("TOKITOKI_BUILD_DATE");

/// Human-readable version line shown in the Settings dialog.
///
/// ```
/// assert!(tokitoki_windows::version::summary().starts_with("Version "));
/// ```
#[must_use]
pub fn summary() -> String {
    format!("Version {VERSION}")
}
