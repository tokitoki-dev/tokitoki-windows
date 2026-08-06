//! One short-lived shared-CLI process per operation.

use std::{
    io::Read,
    path::Path,
    process::{Command, Stdio},
    thread,
    time::Duration,
};

use wait_timeout::ChildExt;

use super::{Error, EXIT_NO_API_KEY, OP_TIMEOUT, SYNC_TIMEOUT, UPDATE_TIMEOUT};
use crate::{data_dirs::ProviderDirs, syncer::SyncClient};

const STDERR_LIMIT: usize = 200;

/// Invokes the shared CLI; stateless, cheap to clone.
#[derive(Clone, Copy, Debug, Default)]
pub struct Client;

impl Client {
    /// Creates a client.
    #[must_use]
    pub fn new() -> Self {
        Self
    }

    /// Runs `sync --provider-dir p=d ...`. A `None`/empty argument set is a
    /// no-op.
    ///
    /// # Errors
    /// Returns [`Error`] when the CLI fails, times out, or prints something
    /// other than `{"ok":true}`.
    pub fn sync(&self, dirs: &ProviderDirs) -> Result<(), Error> {
        let Some(pairs) = super::sync_args(dirs) else {
            return Ok(());
        };
        let mut args = vec!["sync".to_owned()];
        args.extend(pairs);
        run_ok("sync", &args, SYNC_TIMEOUT)
    }

    /// Runs `get key` and returns the trimmed key.
    ///
    /// # Errors
    /// Returns [`Error::MissingApiKey`] when no key is configured, or another
    /// [`Error`] on CLI failure.
    pub fn api_key(&self) -> Result<String, Error> {
        let args = ["get".to_owned(), "key".to_owned()];
        Ok(run("get key", &args, OP_TIMEOUT)?.trim().to_owned())
    }

    /// Runs `set key <key>`.
    ///
    /// # Errors
    /// Returns [`Error`] on CLI failure; the key never appears in the error.
    pub fn set_api_key(&self, key: &str) -> Result<(), Error> {
        let args = ["set".to_owned(), "key".to_owned(), key.to_owned()];
        run_ok("set key", &args, OP_TIMEOUT)
    }

    /// Runs `get dashboard-url` and returns the minted login URL.
    ///
    /// # Errors
    /// Returns [`Error::UnexpectedOutput`] when the output is not an
    /// `http`/`https` URL, or another [`Error`] on CLI failure.
    pub fn dashboard_url(&self) -> Result<url::Url, Error> {
        let label = "get dashboard-url";
        let args = ["get".to_owned(), "dashboard-url".to_owned()];
        let output = run(label, &args, OP_TIMEOUT)?;
        let parsed =
            url::Url::parse(output.trim()).map_err(|_| Error::UnexpectedOutput { label })?;
        if matches!(parsed.scheme(), "http" | "https") {
            Ok(parsed)
        } else {
            Err(Error::UnexpectedOutput { label })
        }
    }

    /// Runs `update` so the shared CLI replaces itself with the latest release.
    ///
    /// # Errors
    /// Returns [`Error`] on CLI failure or timeout.
    pub fn update(&self) -> Result<(), Error> {
        run_ok("update", &["update".to_owned()], UPDATE_TIMEOUT)
    }
}

impl SyncClient for Client {
    fn sync(&self, dirs: &ProviderDirs) -> Result<(), Error> {
        Client::sync(self, dirs)
    }
}

/// Runs an operation and requires `{"ok":true}` on stdout.
fn run_ok(label: &'static str, args: &[String], timeout: Duration) -> Result<(), Error> {
    let output = run(label, args, timeout)?;
    let ok = serde_json::from_str::<serde_json::Value>(output.trim())
        .ok()
        .and_then(|value| value.get("ok").and_then(serde_json::Value::as_bool))
        .unwrap_or(false);
    if ok {
        Ok(())
    } else {
        Err(Error::UnexpectedOutput { label })
    }
}

/// Spawns the shared CLI with a hidden console and captures stdout.
///
/// Errors carry only `label`, never argv — argv may contain the API key.
fn run(label: &'static str, args: &[String], timeout: Duration) -> Result<String, Error> {
    let binary = super::shared_binary()?;
    run_binary(&binary, label, args, timeout)
}

pub(crate) fn run_binary(
    binary: &Path,
    label: &'static str,
    args: &[String],
    timeout: Duration,
) -> Result<String, Error> {
    let mut command = Command::new(binary);
    command
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    hide_console(&mut command);

    let mut child = command
        .spawn()
        .map_err(|source| Error::Launch { label, source })?;

    // Drain both pipes on their own threads while waiting: a full pipe would
    // otherwise deadlock the child before the timeout fires.
    let stdout_pipe = child.stdout.take();
    let stderr_pipe = child.stderr.take();
    let stdout_reader = thread::spawn(move || drain(stdout_pipe));
    let stderr_reader = thread::spawn(move || drain(stderr_pipe));

    let status = match child.wait_timeout(timeout) {
        Ok(Some(status)) => status,
        Ok(None) => {
            let _ = child.kill();
            let _ = child.wait();
            return Err(Error::Timeout { label });
        }
        Err(source) => {
            let _ = child.kill();
            return Err(Error::Launch { label, source });
        }
    };

    let stdout = stdout_reader.join().unwrap_or_default();
    let stderr = stderr_reader.join().unwrap_or_default();

    if status.code() == Some(EXIT_NO_API_KEY) {
        return Err(Error::MissingApiKey);
    }
    if !status.success() {
        return Err(Error::Failed {
            label,
            detail: truncate(&String::from_utf8_lossy(&stderr)),
        });
    }
    Ok(String::from_utf8_lossy(&stdout).into_owned())
}

fn drain(pipe: Option<impl Read>) -> Vec<u8> {
    let mut buffer = Vec::new();
    if let Some(mut pipe) = pipe {
        let _ = pipe.read_to_end(&mut buffer);
    }
    buffer
}

#[cfg(windows)]
fn hide_console(command: &mut Command) {
    use std::os::windows::process::CommandExt;
    const CREATE_NO_WINDOW: u32 = 0x0800_0000;
    command.creation_flags(CREATE_NO_WINDOW);
}

#[cfg(not(windows))]
fn hide_console(_command: &mut Command) {}

/// Truncates to 200 chars, mirroring the Go `msg[:197] + "..."` behavior.
fn truncate(message: &str) -> String {
    let trimmed = message.trim();
    if trimmed.chars().count() <= STDERR_LIMIT {
        return trimmed.to_owned();
    }
    let head: String = trimmed.chars().take(STDERR_LIMIT - 3).collect();
    format!("{head}...")
}

#[cfg(test)]
mod tests {
    use super::*;

    mod truncate {
        use super::*;

        #[test]
        fn keeps_short_messages_untouched() {
            assert_eq!(truncate("boom"), "boom");
        }

        #[test]
        fn caps_long_messages_at_two_hundred_chars() {
            let long = "x".repeat(500);
            let result = truncate(&long);
            assert_eq!(result.chars().count(), STDERR_LIMIT);
        }

        #[test]
        fn appends_ellipsis_when_truncating() {
            let long = "x".repeat(500);
            assert!(truncate(&long).ends_with("..."));
        }
    }

    mod run_binary {
        use super::*;

        #[test]
        fn missing_binary_maps_to_launch_error() {
            let missing = Path::new("definitely-not-a-real-binary-12345");
            let err = run_binary(missing, "probe", &[], Duration::from_secs(1)).unwrap_err();
            assert!(
                matches!(err, Error::Launch { label: "probe", .. }),
                "got {err}"
            );
        }
    }
}
