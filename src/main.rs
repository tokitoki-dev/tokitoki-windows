//! Entry point (mirrors `cmd/tokitoki-windows/main_windows.go`):
//! cleanup leftovers → single-instance guard → autostart reconcile →
//! app start → tray UI loop → optional update relaunch.

// Release builds detach from the console (Go's `-H windowsgui`); debug builds
// keep it for logs.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::{process::ExitCode, sync::Arc};

use tokitoki_windows::{app::App, app_update, instance, launch, ui};

fn main() -> ExitCode {
    init_logging();

    // Delete the .old binary a previous update left behind.
    app_update::cleanup_leftovers();

    let lock = match instance::acquire() {
        Ok(Some(lock)) => lock,
        // Another instance is running: exit silently, like the Go app.
        Ok(None) => return ExitCode::SUCCESS,
        Err(err) => {
            tracing::error!("single-instance guard: {err}");
            return ExitCode::FAILURE;
        }
    };

    // Repoint a stale autostart entry at this exe; failure is only a warning.
    if let Err(err) = launch::reconcile() {
        tracing::warn!("reconcile launch entry: {err}");
    }

    let app = App::new();
    app.start();

    let result = ui::run(Arc::clone(&app));
    app.stop();

    if let Err(err) = result {
        tracing::error!("{err}");
        return ExitCode::FAILURE;
    }

    if let Some(target) = ui::pending_restart() {
        // The child checks the same mutex: release it first or the relaunched
        // app will see a live instance and quit.
        lock.release();
        if let Err(err) = app_update::relaunch(&target) {
            tracing::error!("relaunch: {err}");
            return ExitCode::FAILURE;
        }
    }
    ExitCode::SUCCESS
}

fn init_logging() {
    use tracing_subscriber::EnvFilter;
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .with_writer(std::io::stderr)
        .init();
}
