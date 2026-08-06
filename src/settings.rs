//! Preferences persisted at `%USERPROFILE%\.tokitoki\windows-settings.json`
//! (mirrors Go `internal/settings`).
//!
//! Writes are atomic: serialize to a temp file in the same directory, then
//! rename over the destination so a crash never leaves a torn file.

use std::{
    fs, io,
    path::{Path, PathBuf},
    sync::Mutex,
};

use serde::{Deserialize, Serialize};

const FILE_NAME: &str = "windows-settings.json";

/// Persisted preferences.
///
/// Both flags are inverted so the zero value (absent file or field) means the
/// feature is ON — the exact schema the Go app writes. Unknown fields (such as
/// the legacy `enabled_providers` list) still parse and are ignored.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Settings {
    /// `true` disables session tracking (watching + syncing).
    #[serde(default, skip_serializing_if = "is_false")]
    pub tracking_disabled: bool,
    /// `true` disables automatic app-update checks.
    #[serde(default, skip_serializing_if = "is_false")]
    pub automatic_updates_disabled: bool,
}

#[expect(clippy::trivially_copy_pass_by_ref, reason = "serde requires &T")]
fn is_false(value: &bool) -> bool {
    !*value
}

/// Failure while loading or saving [`Settings`].
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// Filesystem read/write/rename failed.
    #[error("settings file: {0}")]
    Io(#[from] io::Error),
    /// The settings file held invalid JSON.
    #[error("settings decode: {0}")]
    Decode(#[from] serde_json::Error),
}

/// Serialized access to the settings file.
pub struct Store {
    path: PathBuf,
    lock: Mutex<()>,
}

impl Store {
    /// Creates a store rooted at `data_dir` (normally
    /// [`crate::agent_cli::data_dir`]).
    #[must_use]
    pub fn new(data_dir: &Path) -> Self {
        Self {
            path: data_dir.join(FILE_NAME),
            lock: Mutex::new(()),
        }
    }

    /// Loads the settings; a missing file yields the defaults.
    ///
    /// # Errors
    /// Returns [`Error`] when the file exists but cannot be read or parsed.
    pub fn load(&self) -> Result<Settings, Error> {
        let _guard = self
            .lock
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        match fs::read(&self.path) {
            Ok(bytes) => Ok(serde_json::from_slice(&bytes)?),
            Err(err) if err.kind() == io::ErrorKind::NotFound => Ok(Settings::default()),
            Err(err) => Err(err.into()),
        }
    }

    /// Atomically replaces the settings file with `settings`.
    ///
    /// # Errors
    /// Returns [`Error`] when the directory cannot be created or the
    /// temp-write/rename fails.
    pub fn save(&self, settings: Settings) -> Result<(), Error> {
        let _guard = self
            .lock
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let dir = self.path.parent().unwrap_or(Path::new("."));
        fs::create_dir_all(dir)?;

        let mut body = serde_json::to_vec_pretty(&settings)?;
        body.push(b'\n');

        let tmp = dir.join(format!(".windows-settings.tmp.{}", std::process::id()));
        fs::write(&tmp, &body)?;
        fs::rename(&tmp, &self.path).inspect_err(|_| {
            // Best effort: don't leave the temp file behind on failure.
            let _ = fs::remove_file(&tmp);
        })?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn store() -> (tempfile::TempDir, Store) {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::new(dir.path());
        (dir, store)
    }

    mod load {
        use super::*;

        #[test]
        fn returns_defaults_when_file_missing() {
            let (_dir, store) = store();
            assert_eq!(store.load().unwrap(), Settings::default());
        }

        #[test]
        fn ignores_legacy_enabled_providers_field() {
            let (dir, store) = store();
            fs::write(
                dir.path().join(FILE_NAME),
                r#"{"tracking_disabled":true,"enabled_providers":["claude"]}"#,
            )
            .unwrap();
            assert!(store.load().unwrap().tracking_disabled);
        }

        #[test]
        fn fails_on_invalid_json() {
            let (dir, store) = store();
            fs::write(dir.path().join(FILE_NAME), "not json").unwrap();
            assert!(matches!(store.load().unwrap_err(), Error::Decode(_)));
        }
    }

    mod save {
        use super::*;

        #[test]
        fn writes_empty_object_for_defaults() {
            let (dir, store) = store();
            store.save(Settings::default()).unwrap();
            let body = fs::read_to_string(dir.path().join(FILE_NAME)).unwrap();
            assert_eq!(body, "{}\n");
        }

        #[test]
        fn round_trips_disabled_flags() {
            let (_dir, store) = store();
            let settings = Settings {
                tracking_disabled: true,
                automatic_updates_disabled: true,
            };
            store.save(settings).unwrap();
            assert_eq!(store.load().unwrap(), settings);
        }

        #[test]
        fn leaves_no_temp_file_behind() {
            let (dir, store) = store();
            store.save(Settings::default()).unwrap();
            let leftovers: Vec<_> = fs::read_dir(dir.path())
                .unwrap()
                .filter_map(Result::ok)
                .filter(|e| e.file_name().to_string_lossy().starts_with('.'))
                .collect();
            assert!(leftovers.is_empty(), "found leftovers: {leftovers:?}");
        }
    }
}
