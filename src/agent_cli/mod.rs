//! Shared-CLI resolution and invocation (mirrors Go `internal/agentcli`).
//!
//! The tray app contains no sync/upload logic of its own: every operation is
//! delegated to the shared `tokitoki` CLI at
//! `%USERPROFILE%\.tokitoki\bin\tokitoki.exe`, spawned once per call with a
//! hidden console — the same contract the macOS app and editor plugins follow.
//!
//! Errors deliberately never contain argv: `set key <key>` would leak the
//! API key into logs.

mod bootstrap;
mod client;

use std::{io, path::PathBuf, time::Duration};

pub use bootstrap::bootstrap;
pub use client::Client;

use crate::data_dirs::ProviderDirs;

/// Timeout for a full `sync` run.
pub const SYNC_TIMEOUT: Duration = Duration::from_secs(2 * 60);
/// Timeout for short operations (`get key`, `set key`, `get dashboard-url`).
pub(crate) const OP_TIMEOUT: Duration = Duration::from_secs(15);
/// Timeout for `update` (may download a new CLI release).
pub(crate) const UPDATE_TIMEOUT: Duration = Duration::from_secs(5 * 60);

/// CLI exit code meaning "no API key configured".
pub(crate) const EXIT_NO_API_KEY: i32 = 3;

/// Default server the app talks to when `TOKITOKI_BASE_URL` is unset.
pub const DEFAULT_BASE_URL: &str = "https://tokitoki.dev";

#[cfg(windows)]
const BINARY_NAME: &str = "tokitoki.exe";
#[cfg(not(windows))]
const BINARY_NAME: &str = "tokitoki";

/// Failures from resolving or invoking the shared CLI.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The CLI exited with code 3: no API key is configured yet.
    #[error("api key is not configured")]
    MissingApiKey,
    /// The CLI binary could not be spawned (missing, not executable, ...).
    #[error("{label}: failed to launch shared CLI: {source}")]
    Launch {
        /// Operation label (never argv).
        label: &'static str,
        /// Underlying spawn failure.
        source: io::Error,
    },
    /// The CLI did not finish within the operation's timeout.
    #[error("{label}: shared CLI timed out")]
    Timeout {
        /// Operation label.
        label: &'static str,
    },
    /// The CLI exited non-zero; `detail` holds stderr truncated to 200 chars.
    #[error("{label}: shared CLI failed: {detail}")]
    Failed {
        /// Operation label.
        label: &'static str,
        /// Truncated stderr (never argv).
        detail: String,
    },
    /// The CLI succeeded but printed something other than the expected output.
    #[error("{label}: unexpected shared CLI output")]
    UnexpectedOutput {
        /// Operation label.
        label: &'static str,
    },
    /// The shared data directory could not be created or resolved.
    #[error("shared CLI location: {0}")]
    Location(#[from] io::Error),
}

/// Server base URL: `TOKITOKI_BASE_URL` (trimmed of whitespace and trailing
/// `/`) or [`DEFAULT_BASE_URL`].
#[must_use]
pub fn base_url() -> String {
    std::env::var("TOKITOKI_BASE_URL")
        .ok()
        .map(|value| value.trim().trim_end_matches('/').to_owned())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| DEFAULT_BASE_URL.to_owned())
}

/// The shared data directory `%USERPROFILE%\.tokitoki`, created if absent.
///
/// # Errors
/// Returns an [`io::Error`] when the home directory is unresolvable or the
/// directory cannot be created.
pub fn data_dir() -> io::Result<PathBuf> {
    let home = ["USERPROFILE", "HOME"]
        .iter()
        .find_map(std::env::var_os)
        .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, "home directory not set"))?;
    let dir = PathBuf::from(home).join(".tokitoki");
    std::fs::create_dir_all(&dir)?;
    Ok(dir)
}

/// Path of the shared CLI binary `%USERPROFILE%\.tokitoki\bin\tokitoki.exe`.
///
/// # Errors
/// Propagates [`data_dir`] failures.
pub fn shared_binary() -> io::Result<PathBuf> {
    Ok(data_dir()?.join("bin").join(BINARY_NAME))
}

/// Renders `--provider-dir provider=dir` argument pairs: providers sorted,
/// dirs sorted, empty entries skipped. `None` when there is nothing to scan.
#[must_use]
pub fn sync_args(dirs: &ProviderDirs) -> Option<Vec<String>> {
    let mut args = Vec::new();
    for (provider, provider_dirs) in dirs {
        let mut sorted: Vec<_> = provider_dirs
            .iter()
            .map(|dir| dir.to_string_lossy().into_owned())
            .filter(|dir| !dir.is_empty())
            .collect();
        sorted.sort();
        for dir in sorted {
            args.push("--provider-dir".to_owned());
            args.push(format!("{provider}={dir}"));
        }
    }
    (!args.is_empty()).then_some(args)
}

/// Parses `1.2.3` / `v1.2.3` into its numeric parts. Requires exactly three
/// dot-separated parts; each part contributes its leading digits. `dev`,
/// two-part, or digit-less versions yield `None`.
#[must_use]
pub(crate) fn parse_version(raw: &str) -> Option<[u64; 3]> {
    let trimmed = raw.trim().strip_prefix('v').unwrap_or_else(|| raw.trim());
    let parts: Vec<&str> = trimmed.split('.').collect();
    let [major, minor, patch] = parts.as_slice() else {
        return None;
    };
    Some([
        leading_digits(major)?,
        leading_digits(minor)?,
        leading_digits(patch)?,
    ])
}

fn leading_digits(part: &str) -> Option<u64> {
    let digits: String = part.chars().take_while(char::is_ascii_digit).collect();
    digits.parse().ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    mod sync_args {
        use super::*;

        #[test]
        fn returns_none_for_empty_map() {
            assert_eq!(sync_args(&ProviderDirs::new()), None);
        }

        #[test]
        fn renders_sorted_provider_dir_pairs() {
            let mut dirs = ProviderDirs::new();
            dirs.insert("codex".to_owned(), vec!["b".into(), "a".into()]);
            dirs.insert("claude".to_owned(), vec!["c".into()]);

            let args = sync_args(&dirs).unwrap();

            assert_eq!(
                args,
                vec![
                    "--provider-dir",
                    "claude=c",
                    "--provider-dir",
                    "codex=a",
                    "--provider-dir",
                    "codex=b",
                ]
            );
        }

        #[test]
        fn skips_empty_dir_entries() {
            let mut dirs = ProviderDirs::new();
            dirs.insert("claude".to_owned(), vec![PathBuf::new()]);
            assert_eq!(sync_args(&dirs), None);
        }
    }

    mod parse_version {
        use super::*;

        #[test]
        fn parses_plain_semver() {
            assert_eq!(parse_version("1.2.3"), Some([1, 2, 3]));
        }

        #[test]
        fn strips_leading_v() {
            assert_eq!(parse_version("v0.1.6"), Some([0, 1, 6]));
        }

        #[test]
        fn takes_leading_digits_of_each_part() {
            assert_eq!(parse_version("1.2.3-rc1"), Some([1, 2, 3]));
        }

        #[test]
        fn rejects_dev() {
            assert_eq!(parse_version("dev"), None);
        }

        #[test]
        fn rejects_two_part_versions() {
            assert_eq!(parse_version("1.2"), None);
        }
    }

    mod base_url {
        use super::*;

        #[test]
        fn default_has_no_trailing_slash() {
            assert!(!DEFAULT_BASE_URL.ends_with('/'));
            assert!(base_url().starts_with("http"));
        }
    }
}
