//! Background app-update schedule and the install/restart flow
//! (mirrors Go `internal/ui/update_windows.go`).
//!
//! The loop keeps ticking while automatic updates are off (so re-enabling
//! works without a restart), exits permanently on a dev build, and announces
//! any given version at most once.

use std::{sync::atomic::Ordering, thread, time::Duration};

use windows::Win32::{
    Foundation::{LPARAM, WPARAM},
    UI::WindowsAndMessaging::{PostMessageW, WM_CLOSE},
};

use super::{
    imp::{lock, Ui, WM_UPDATE_AVAILABLE},
    task_dialog,
};
use crate::{agent_cli, app_update, version};

const FIRST_CHECK_DELAY: Duration = Duration::from_secs(5);
const CHECK_INTERVAL: Duration = Duration::from_secs(24 * 60 * 60);

/// Starts the periodic update check.
pub(super) fn spawn(ui: &'static Ui) {
    thread::Builder::new()
        .name("app-update".to_owned())
        .spawn(move || {
            let mut offered: Option<String> = None;
            thread::sleep(FIRST_CHECK_DELAY);
            loop {
                if ui.app.automatic_updates_enabled() && !check_once(ui, &mut offered) {
                    return;
                }
                thread::sleep(CHECK_INTERVAL);
            }
        })
        .ok();
}

/// One check; returns `false` when the loop should stop (dev build).
fn check_once(ui: &'static Ui, offered: &mut Option<String>) -> bool {
    match app_update::check(&agent_cli::base_url(), version::VERSION) {
        Err(app_update::Error::DevBuild) => {
            tracing::debug!("development build; update checks disabled");
            false
        }
        Err(err) => {
            tracing::debug!("update check: {err}");
            true
        }
        Ok(None) => true,
        Ok(Some(update)) => {
            if offered.as_deref() != Some(update.version.as_str()) {
                *offered = Some(update.version.clone());
                *lock(&ui.pending_update) = Some(update);
                // SAFETY: posting to our own window from a worker thread.
                unsafe {
                    let _ =
                        PostMessageW(Some(ui.hwnd()), WM_UPDATE_AVAILABLE, WPARAM(0), LPARAM(0));
                }
            }
            true
        }
    }
}

/// Offers the pending update; on acceptance downloads, installs, and prompts
/// for a restart. Re-entry is blocked while an install is in flight.
pub(super) fn offer_install(ui: &'static Ui) {
    if ui.installing.swap(true, Ordering::SeqCst) {
        return;
    }
    thread::Builder::new()
        .name("app-install".to_owned())
        .spawn(move || {
            install_flow(ui);
            ui.installing.store(false, Ordering::SeqCst);
        })
        .ok();
}

fn install_flow(ui: &'static Ui) {
    let Some(update) = lock(&ui.pending_update).clone() else {
        return;
    };

    let accepted = task_dialog::choice(
        &format!("Version {} is available", update.version),
        &format!(
            "You have {}. Tokitoki keeps running while the update downloads and installs.",
            version::VERSION
        ),
        &["Install now", "Not now"],
    );
    if accepted != Some(0) {
        return;
    }

    if let Err(err) = app_update::install(&update) {
        task_dialog::error("Update failed", &err.to_string());
        return;
    }
    lock(&ui.pending_update).take();

    let restart = task_dialog::choice(
        &format!("Version {} is installed", update.version),
        "Restarting takes a few seconds…",
        &["Restart now", "Later"],
    );
    if restart == Some(0) {
        if let Ok(exe) = std::env::current_exe() {
            ui.request_restart(exe);
        }
        // WM_CLOSE → DefWindowProc destroys the window → WM_DESTROY quits the
        // loop; main then relaunches after releasing the instance mutex.
        // SAFETY: posting to our own window from a worker thread.
        unsafe {
            let _ = PostMessageW(Some(ui.hwnd()), WM_CLOSE, WPARAM(0), LPARAM(0));
        }
    }
}
