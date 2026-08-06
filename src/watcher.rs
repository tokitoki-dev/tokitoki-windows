//! Recursive debounced filesystem watcher (mirrors Go `internal/watcher`).
//!
//! Any burst of create/write/remove/rename events collapses into a single
//! `on_change` call once the paths have been quiet for the debounce window.
//! The `notify` Windows backend (`ReadDirectoryChangesW`) watches directory
//! trees recursively on its own, so new session subdirectories are picked up
//! without manual re-walking.

use std::{
    path::PathBuf,
    sync::{
        mpsc::{self, RecvTimeoutError},
        Arc, Mutex,
    },
    thread,
    time::Duration,
};

use notify::{EventKind, RecursiveMode, Watcher as _};

const DEFAULT_DEBOUNCE: Duration = Duration::from_secs(2);

/// Debounced change callback.
pub type OnChange = Arc<dyn Fn() + Send + Sync>;

struct Active {
    // Dropping the backend disconnects the event channel, which in turn
    // stops the debounce thread.
    backend: notify::RecommendedWatcher,
    thread: thread::JoinHandle<()>,
}

/// Watches directory trees and fires a debounced callback.
pub struct Watcher {
    debounce: Duration,
    on_change: OnChange,
    active: Mutex<Option<Active>>,
}

impl Watcher {
    /// Creates a stopped watcher. A zero `debounce` falls back to 2 s.
    pub fn new(debounce: Duration, on_change: impl Fn() + Send + Sync + 'static) -> Self {
        Self {
            debounce: if debounce.is_zero() {
                DEFAULT_DEBOUNCE
            } else {
                debounce
            },
            on_change: Arc::new(on_change),
            active: Mutex::new(None),
        }
    }

    /// (Re)starts watching `paths` recursively; an empty list just stops.
    ///
    /// # Errors
    /// Returns a [`notify::Error`] when the backend cannot be created. Paths
    /// that fail to register are logged and skipped, like the Go original.
    pub fn start(&self, paths: &[PathBuf]) -> Result<(), notify::Error> {
        self.stop();
        if paths.is_empty() {
            return Ok(());
        }

        let (event_tx, event_rx) = mpsc::channel::<()>();
        let mut backend =
            notify::recommended_watcher(move |result: Result<notify::Event, notify::Error>| {
                match result {
                    Ok(event) if is_relevant(event.kind) => {
                        let _ = event_tx.send(());
                    }
                    Ok(_) => {}
                    Err(err) => tracing::debug!("watch error: {err}"),
                }
            })?;

        for path in paths {
            if let Err(err) = backend.watch(path, RecursiveMode::Recursive) {
                tracing::debug!("watch {}: {err}", path.display());
            }
        }

        let debounce = self.debounce;
        let on_change = Arc::clone(&self.on_change);
        let thread = thread::Builder::new()
            .name("watcher-debounce".to_owned())
            .spawn(move || debounce_loop(&event_rx, debounce, &on_change))
            .map_err(notify::Error::io)?;

        *lock(&self.active) = Some(Active { backend, thread });
        Ok(())
    }

    /// Stops watching; blocks until the debounce thread exits.
    pub fn stop(&self) {
        let active = lock(&self.active).take();
        if let Some(active) = active {
            drop(active.backend);
            let _ = active.thread.join();
        }
    }
}

impl Drop for Watcher {
    fn drop(&mut self) {
        self.stop();
    }
}

fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    mutex
        .lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner)
}

fn is_relevant(kind: EventKind) -> bool {
    matches!(
        kind,
        EventKind::Create(_) | EventKind::Modify(_) | EventKind::Remove(_) | EventKind::Any
    )
}

fn debounce_loop(events: &mpsc::Receiver<()>, debounce: Duration, on_change: &OnChange) {
    // Outer recv blocks until the first event of a burst; the inner loop then
    // restarts the timer on every further event until things go quiet.
    while events.recv().is_ok() {
        loop {
            match events.recv_timeout(debounce) {
                Ok(()) => {}
                Err(RecvTimeoutError::Timeout) => {
                    on_change();
                    break;
                }
                Err(RecvTimeoutError::Disconnected) => return,
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::{AtomicUsize, Ordering};

    use super::*;

    #[test]
    fn burst_of_writes_fires_one_callback() {
        let dir = tempfile::tempdir().unwrap();
        let fired = Arc::new(AtomicUsize::new(0));
        let counter = Arc::clone(&fired);
        let watcher = Watcher::new(Duration::from_millis(100), move || {
            counter.fetch_add(1, Ordering::SeqCst);
        });
        watcher.start(&[dir.path().to_path_buf()]).unwrap();

        for i in 0..5 {
            std::fs::write(dir.path().join(format!("f{i}")), b"x").unwrap();
        }
        thread::sleep(Duration::from_millis(600));

        assert_eq!(fired.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn empty_path_list_is_a_no_op() {
        let watcher = Watcher::new(Duration::ZERO, || {});
        watcher.start(&[]).unwrap();
        watcher.stop();
    }

    #[test]
    fn new_subdirectory_contents_are_picked_up() {
        let dir = tempfile::tempdir().unwrap();
        let fired = Arc::new(AtomicUsize::new(0));
        let counter = Arc::clone(&fired);
        let watcher = Watcher::new(Duration::from_millis(100), move || {
            counter.fetch_add(1, Ordering::SeqCst);
        });
        watcher.start(&[dir.path().to_path_buf()]).unwrap();

        let sub = dir.path().join("session-1");
        std::fs::create_dir(&sub).unwrap();
        thread::sleep(Duration::from_millis(400));
        std::fs::write(sub.join("log"), b"x").unwrap();
        thread::sleep(Duration::from_millis(400));

        assert!(fired.load(Ordering::SeqCst) >= 1);
    }
}
