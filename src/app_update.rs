//! App self-update: check, download, verify, and atomic binary swap
//! (mirrors Go `internal/appupdate`). There is no external updater process —
//! the running exe renames itself aside and puts the new one in its place.

use std::{
    fs, io,
    io::{Read, Write},
    net::IpAddr,
    path::{Path, PathBuf},
    time::Duration,
};

use serde::Deserialize;
use sha2::{Digest, Sha256};

/// Update channel reported to the server.
pub const CHANNEL: &str = "windows";
const CHECK_TIMEOUT: Duration = Duration::from_secs(15);
const DOWNLOAD_TIMEOUT: Duration = Duration::from_secs(10 * 60);
const OLD_SUFFIX: &str = ".old";
const USER_AGENT: &str = "tokitoki-windows";

/// Failures across the update pipeline.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The running binary is a dev build; updates are disabled.
    #[error("development build; updates are disabled")]
    DevBuild,
    /// The update-check request failed or returned an unusable payload.
    #[error("update check failed: {0}")]
    Check(String),
    /// The download URL uses a transport we refuse (non-https, non-loopback).
    #[error("untrusted update transport: {0}")]
    UntrustedTransport(String),
    /// Downloading the new binary failed.
    #[error("update download failed: {0}")]
    Download(String),
    /// The downloaded size differs from what the server announced.
    #[error("update size mismatch: expected {expected} bytes, got {got}")]
    SizeMismatch {
        /// Bytes the server announced.
        expected: u64,
        /// Bytes actually received.
        got: u64,
    },
    /// The downloaded bytes don't hash to the announced SHA-256.
    #[error("update digest mismatch")]
    DigestMismatch,
    /// Filesystem work (temp file, rename swap) failed.
    #[error("update install failed: {0}")]
    Install(#[from] io::Error),
}

/// An available update as announced by the server.
#[derive(Clone, Debug)]
pub struct Update {
    /// Version string, e.g. `0.2.0`.
    pub version: String,
    /// Absolute download URL (server returns a relative path; we prefix base).
    pub download_url: String,
    /// Expected size in bytes; `0` disables the size check.
    pub size: u64,
    /// Expected SHA-256 hex digest (optionally `sha256:`-prefixed); empty
    /// disables the digest check.
    pub sha256: String,
}

#[derive(Deserialize)]
struct CheckResponse {
    #[serde(default)]
    available: bool,
    #[serde(default)]
    version: String,
    #[serde(default)]
    url: String,
    #[serde(default)]
    size: u64,
    #[serde(default)]
    sha256: String,
}

/// Asks the server whether an update is available for `current`.
///
/// # Errors
/// Returns [`Error::DevBuild`] when `current` is not `x.y.z`-shaped, or
/// [`Error::Check`] when the request fails.
pub fn check(base_url: &str, current: &str) -> Result<Option<Update>, Error> {
    if !is_semver_ish(current) {
        return Err(Error::DevBuild);
    }
    let mut url = url::Url::parse(&format!("{base_url}/api/updates/check"))
        .map_err(|err| Error::Check(err.to_string()))?;
    url.query_pairs_mut()
        .append_pair("channel", CHANNEL)
        .append_pair("platform", "windows")
        .append_pair("arch", arch())
        .append_pair("version", current);

    let response = ureq::AgentBuilder::new()
        .timeout(CHECK_TIMEOUT)
        .user_agent(USER_AGENT)
        .build()
        .get(url.as_str())
        .call()
        .map_err(|err| Error::Check(err.to_string()))?;
    let decoded: CheckResponse = response
        .into_json()
        .map_err(|err| Error::Check(err.to_string()))?;

    if !decoded.available {
        return Ok(None);
    }
    Ok(Some(Update {
        version: decoded.version,
        download_url: format!("{base_url}{}", decoded.url),
        size: decoded.size,
        sha256: decoded.sha256,
    }))
}

/// Downloads and installs `update` next to the running exe, swapping the
/// current binary aside as `<exe>.old`. The app keeps running; the new binary
/// takes effect on relaunch.
///
/// # Errors
/// Returns [`Error`] on untrusted transport, download/verification failure,
/// or a failed rename swap (which is rolled back).
pub fn install(update: &Update) -> Result<(), Error> {
    let exe = std::env::current_exe()?;
    require_trusted_transport(&update.download_url)?;

    // A leftover .old from a previous update would block the swap.
    let old = old_path(&exe);
    let _ = fs::remove_file(&old);

    let staged = download(update, &exe)?;
    swap(&exe, &old, &staged)
}

/// Deletes a leftover `<exe>.old` from a previous update. Best-effort.
pub fn cleanup_leftovers() {
    if let Ok(exe) = std::env::current_exe() {
        let _ = fs::remove_file(old_path(&exe));
    }
}

/// Starts `target` as a detached process; the caller must have released the
/// single-instance mutex first or the child will see a live instance and quit.
///
/// # Errors
/// Returns an [`io::Error`] when the process cannot be spawned.
pub fn relaunch(target: &Path) -> io::Result<()> {
    std::process::Command::new(target).spawn().map(drop)
}

fn old_path(exe: &Path) -> PathBuf {
    let mut name = exe.as_os_str().to_owned();
    name.push(OLD_SUFFIX);
    PathBuf::from(name)
}

/// `x86_64` / `aarch64` → the Go-style arch tokens the server expects.
fn arch() -> &'static str {
    match std::env::consts::ARCH {
        "x86_64" => "amd64",
        "aarch64" => "arm64",
        other => other,
    }
}

fn is_semver_ish(version: &str) -> bool {
    crate::agent_cli::parse_version(version.split(['-', '+']).next().unwrap_or(version)).is_some()
}

/// https is always trusted; plain http only for localhost/loopback (dev).
fn require_trusted_transport(raw_url: &str) -> Result<(), Error> {
    let untrusted = || Error::UntrustedTransport(raw_url.to_owned());
    let parsed = url::Url::parse(raw_url).map_err(|_| untrusted())?;
    match parsed.scheme() {
        "https" => Ok(()),
        "http" => {
            let host = parsed.host_str().ok_or_else(untrusted)?;
            let loopback = host.eq_ignore_ascii_case("localhost")
                || host.parse::<IpAddr>().is_ok_and(|ip| ip.is_loopback());
            loopback.then_some(()).ok_or_else(untrusted)
        }
        _ => Err(untrusted()),
    }
}

fn download(update: &Update, exe: &Path) -> Result<PathBuf, Error> {
    let dir = exe.parent().unwrap_or(Path::new("."));
    let staged = dir.join(format!(".tokitoki-update-{}", std::process::id()));

    let response = ureq::AgentBuilder::new()
        .timeout(DOWNLOAD_TIMEOUT)
        .user_agent(USER_AGENT)
        .build()
        .get(&update.download_url)
        .call()
        .map_err(|err| Error::Download(err.to_string()))?;

    let result = stream_verified(response.into_reader(), &staged, update);
    if result.is_err() {
        let _ = fs::remove_file(&staged);
    }
    result.map(|()| staged)
}

fn stream_verified(mut body: impl Read, staged: &Path, update: &Update) -> Result<(), Error> {
    let mut file = fs::File::create(staged)?;
    let mut hasher = Sha256::new();
    let mut written: u64 = 0;
    let mut buffer = vec![0u8; 64 * 1024];
    loop {
        let read = body
            .read(&mut buffer)
            .map_err(|err| Error::Download(err.to_string()))?;
        if read == 0 {
            break;
        }
        file.write_all(&buffer[..read])?;
        hasher.update(&buffer[..read]);
        written += read as u64;
    }
    file.sync_all()?;
    drop(file);

    if update.size > 0 && written != update.size {
        return Err(Error::SizeMismatch {
            expected: update.size,
            got: written,
        });
    }
    let expected = normalize_digest(&update.sha256);
    if !expected.is_empty() {
        let got = format!("{:x}", hasher.finalize());
        if got != expected {
            return Err(Error::DigestMismatch);
        }
    }
    Ok(())
}

/// Strips an optional `sha256:` prefix and lowercases the hex digest.
fn normalize_digest(digest: &str) -> String {
    let trimmed = digest.trim();
    let stripped = trimmed
        .get(..7)
        .filter(|prefix| prefix.eq_ignore_ascii_case("sha256:"))
        .map_or(trimmed, |_| &trimmed[7..]);
    stripped.to_ascii_lowercase()
}

/// Renames `exe` aside and the staged download into place; rolls the original
/// back if the second rename fails. Windows can't overwrite a running exe but
/// can rename it.
fn swap(exe: &Path, old: &Path, staged: &Path) -> Result<(), Error> {
    fs::rename(exe, old)?;
    if let Err(err) = fs::rename(staged, exe) {
        let _ = fs::rename(old, exe);
        let _ = fs::remove_file(staged);
        return Err(err.into());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    mod is_semver_ish {
        use super::*;

        #[test]
        fn accepts_release_versions() {
            assert!(is_semver_ish("0.1.0"));
        }

        #[test]
        fn accepts_v_prefix() {
            assert!(is_semver_ish("v1.2.3"));
        }

        #[test]
        fn rejects_dev() {
            assert!(!is_semver_ish("dev"));
        }
    }

    mod require_trusted_transport {
        use super::*;

        #[test]
        fn accepts_https() {
            assert!(require_trusted_transport("https://tokitoki.dev/x").is_ok());
        }

        #[test]
        fn accepts_http_localhost() {
            assert!(require_trusted_transport("http://localhost:3000/x").is_ok());
        }

        #[test]
        fn accepts_http_loopback_ip() {
            assert!(require_trusted_transport("http://127.0.0.1/x").is_ok());
        }

        #[test]
        fn rejects_plain_http_hosts() {
            assert!(require_trusted_transport("http://evil.example/x").is_err());
        }
    }

    mod normalize_digest {
        use super::*;

        #[test]
        fn strips_sha256_prefix_case_insensitively() {
            assert_eq!(normalize_digest("SHA256:ABCDEF"), "abcdef");
        }

        #[test]
        fn lowercases_bare_digests() {
            assert_eq!(normalize_digest("ABCDEF"), "abcdef");
        }
    }

    mod swap {
        use super::*;

        #[test]
        fn replaces_exe_and_keeps_backup() {
            let dir = tempfile::tempdir().unwrap();
            let exe = dir.path().join("app.exe");
            let old = dir.path().join("app.exe.old");
            let staged = dir.path().join("staged");
            fs::write(&exe, b"v1").unwrap();
            fs::write(&staged, b"v2").unwrap();

            swap(&exe, &old, &staged).unwrap();

            assert_eq!(fs::read(&exe).unwrap(), b"v2");
        }

        #[test]
        fn rolls_back_when_staged_binary_is_missing() {
            let dir = tempfile::tempdir().unwrap();
            let exe = dir.path().join("app.exe");
            let old = dir.path().join("app.exe.old");
            fs::write(&exe, b"v1").unwrap();

            let result = swap(&exe, &old, &dir.path().join("missing"));

            assert!(result.is_err());
            assert_eq!(fs::read(&exe).unwrap(), b"v1");
        }
    }
}
