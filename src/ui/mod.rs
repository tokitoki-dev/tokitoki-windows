//! The Windows front end: hidden window, tray icon, context menu, dialogs,
//! theme handling, and the update announcement flow
//! (mirrors Go `internal/ui`).
//!
//! Everything runs on one UI thread owning the Win32 message loop; background
//! work (CLI calls, update checks) happens on worker threads that post
//! `WM_APP`-range messages back.

#[cfg(windows)]
mod imp;
#[cfg(windows)]
mod settings_dialog;
#[cfg(windows)]
mod task_dialog;
#[cfg(windows)]
mod theme;
#[cfg(windows)]
mod tray;
#[cfg(windows)]
mod updater;

use std::{path::PathBuf, sync::Arc};

use crate::app::App;

/// UI-layer failure (window/class creation, tray registration).
#[derive(Debug, thiserror::Error)]
#[error("ui: {0}")]
pub struct Error(pub String);

/// Runs the tray UI until the user quits. Blocks on the Win32 message loop.
///
/// # Errors
/// Returns [`Error`] when the hidden window or tray icon cannot be created,
/// or on non-Windows platforms.
pub fn run(app: Arc<App>) -> Result<(), Error> {
    #[cfg(windows)]
    {
        imp::run(app)
    }
    #[cfg(not(windows))]
    {
        drop(app);
        Err(Error("tokitoki-windows only runs on Windows".to_owned()))
    }
}

/// After [`run`] returns: the exe to relaunch when the user accepted an
/// update restart. The caller must release the instance lock first.
#[must_use]
pub fn pending_restart() -> Option<PathBuf> {
    #[cfg(windows)]
    {
        imp::pending_restart()
    }
    #[cfg(not(windows))]
    {
        None
    }
}
