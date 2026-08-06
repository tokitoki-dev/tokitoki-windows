//! Coalescing sync worker (mirrors Go `internal/syncer`).
//!
//! [`Syncer::trigger`] is a non-blocking send into a channel of capacity 1:
//! any number of triggers that arrive while a sync is running collapse into
//! exactly one follow-up run.

use std::{
    sync::{
        mpsc::{self, Receiver, SyncSender, TrySendError},
        Arc,
    },
    thread,
    time::Duration,
};

use crate::{agent_cli, data_dirs::ProviderDirs};

/// Executes one sync over the given provider directories.
///
/// Implemented by [`agent_cli::Client`]; tests substitute their own.
pub trait SyncClient: Send + Sync + 'static {
    /// Runs a single sync pass.
    ///
    /// # Errors
    /// Returns [`agent_cli::Error`] when the shared CLI fails.
    fn sync(&self, dirs: &ProviderDirs) -> Result<(), agent_cli::Error>;
}

/// Produces the directories to sync; `None` or empty means "skip".
pub type OptionsFn = Box<dyn Fn() -> Option<ProviderDirs> + Send + Sync>;

/// Owns the worker thread; dropping it stops the worker.
pub struct Syncer {
    trigger_tx: SyncSender<()>,
    tickers: Vec<SyncSender<()>>,
    worker: Option<thread::JoinHandle<()>>,
}

impl Syncer {
    /// Starts the worker thread.
    #[must_use]
    pub fn start(client: Arc<dyn SyncClient>, options: OptionsFn) -> Self {
        let (trigger_tx, trigger_rx) = mpsc::sync_channel(1);
        let worker = thread::Builder::new()
            .name("syncer".to_owned())
            .spawn(move || worker_loop(&trigger_rx, &*client, &options))
            .ok();
        Self {
            trigger_tx,
            tickers: Vec::new(),
            worker,
        }
    }

    /// Requests a sync; never blocks. Extra requests while a sync is running
    /// coalesce into one follow-up run.
    pub fn trigger(&self) {
        match self.trigger_tx.try_send(()) {
            Ok(()) | Err(TrySendError::Full(())) => {}
            Err(TrySendError::Disconnected(())) => {
                tracing::debug!("syncer worker is gone; trigger dropped");
            }
        }
    }

    /// Triggers a sync every `interval` until the syncer is dropped.
    /// A zero interval is a no-op.
    pub fn periodically(&mut self, interval: Duration) {
        if interval.is_zero() {
            return;
        }
        let (stop_tx, stop_rx) = mpsc::sync_channel::<()>(0);
        let trigger_tx = self.trigger_tx.clone();
        let spawned = thread::Builder::new()
            .name("syncer-ticker".to_owned())
            .spawn(move || {
                while let Err(mpsc::RecvTimeoutError::Timeout) = stop_rx.recv_timeout(interval) {
                    let _ = trigger_tx.try_send(());
                }
            });
        if spawned.is_ok() {
            self.tickers.push(stop_tx);
        }
    }
}

impl Drop for Syncer {
    fn drop(&mut self) {
        // Disconnect tickers and the trigger channel so both loops exit.
        self.tickers.clear();
        let (empty_tx, _) = mpsc::sync_channel(1);
        self.trigger_tx = empty_tx;
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}

fn worker_loop(trigger_rx: &Receiver<()>, client: &dyn SyncClient, options: &OptionsFn) {
    while trigger_rx.recv().is_ok() {
        sync_once(client, options);
    }
}

fn sync_once(client: &dyn SyncClient, options: &OptionsFn) {
    let Some(dirs) = options().filter(|dirs| !dirs.is_empty()) else {
        tracing::debug!("skip sync; no existing provider directories");
        return;
    };
    match client.sync(&dirs) {
        Ok(()) => tracing::info!("sync completed"),
        Err(err) => tracing::warn!("sync failed: {err}"),
    }
}

#[cfg(test)]
mod tests {
    use std::sync::{
        atomic::{AtomicUsize, Ordering},
        Mutex,
    };

    use super::*;

    struct CountingClient {
        runs: AtomicUsize,
        gate: Mutex<()>,
    }

    impl CountingClient {
        fn new() -> Arc<Self> {
            Arc::new(Self {
                runs: AtomicUsize::new(0),
                gate: Mutex::new(()),
            })
        }
    }

    impl SyncClient for CountingClient {
        fn sync(&self, _dirs: &ProviderDirs) -> Result<(), agent_cli::Error> {
            let _hold = self.gate.lock().unwrap();
            self.runs.fetch_add(1, Ordering::SeqCst);
            Ok(())
        }
    }

    fn one_dir() -> ProviderDirs {
        let mut dirs = ProviderDirs::new();
        dirs.insert("claude".to_owned(), vec!["x".into()]);
        dirs
    }

    #[test]
    fn trigger_runs_one_sync() {
        let client = CountingClient::new();
        let syncer = Syncer::start(client.clone(), Box::new(|| Some(one_dir())));

        syncer.trigger();
        drop(syncer); // joins the worker, so the run has finished

        assert_eq!(client.runs.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn triggers_while_busy_coalesce_into_one_follow_up() {
        let client = CountingClient::new();
        let syncer = Syncer::start(client.clone(), Box::new(|| Some(one_dir())));

        {
            // Hold the gate so the worker blocks inside (or before) the first
            // sync, then pile up triggers: at most one token can queue, so
            // five triggers must collapse into no more than two runs.
            let _hold = client.gate.lock().unwrap();
            for _ in 0..5 {
                syncer.trigger();
            }
        }
        drop(syncer);

        let runs = client.runs.load(Ordering::SeqCst);
        assert!(
            (1..=2).contains(&runs),
            "expected coalescing, got {runs} runs"
        );
    }

    #[test]
    fn skips_sync_when_options_are_empty() {
        let client = CountingClient::new();
        let syncer = Syncer::start(client.clone(), Box::new(|| None));

        syncer.trigger();
        drop(syncer);

        assert_eq!(client.runs.load(Ordering::SeqCst), 0);
    }
}
