//! Platform-neutral service coordinator (mirrors Go `internal/app`).
//!
//! Owns the shared-CLI client, settings store, syncer, and watcher; the UI
//! layer talks only to [`App`].

use std::{
    sync::{
        mpsc::{self, RecvTimeoutError, SyncSender},
        Arc, Mutex, PoisonError, RwLock,
    },
    thread,
    time::Duration,
};

use crate::{
    agent_cli::{self, Client},
    data_dirs,
    settings::{Settings, Store},
    syncer::Syncer,
    watcher::Watcher,
};

/// Periodic full-sync interval.
pub const SYNC_INTERVAL: Duration = Duration::from_secs(30 * 60);
/// Filesystem-event debounce window.
pub const WATCH_DEBOUNCE: Duration = Duration::from_secs(2);
/// How often the shared CLI self-update runs.
const CLI_UPDATE_INTERVAL: Duration = Duration::from_secs(24 * 60 * 60);

/// Cached preference flags (the settings file stores their inversions).
#[derive(Clone, Copy)]
struct Prefs {
    tracking: bool,
    auto_updates: bool,
}

impl Default for Prefs {
    fn default() -> Self {
        Self {
            tracking: true,
            auto_updates: true,
        }
    }
}

/// The service coordinator. Create with [`App::new`], then call
/// [`App::start`] once; the UI drives everything else.
pub struct App {
    client: Client,
    store: Store,
    syncer: Arc<Syncer>,
    watcher: Watcher,
    prefs: Arc<RwLock<Prefs>>,
    cli_update_stop: Mutex<Option<SyncSender<()>>>,
}

impl App {
    /// Wires client, settings store, syncer (with its 30 min ticker), and
    /// watcher together. Nothing is monitored until [`App::start`].
    #[must_use]
    pub fn new() -> Arc<Self> {
        let client = Client::new();
        let prefs = Arc::new(RwLock::new(Prefs::default()));
        let store = Store::new(&agent_cli::data_dir().unwrap_or_else(|_| ".".into()));

        let options_prefs = Arc::clone(&prefs);
        let mut syncer = Syncer::start(
            Arc::new(client),
            Box::new(move || read(&options_prefs).tracking.then(data_dirs::resolve)),
        );
        syncer.periodically(SYNC_INTERVAL);
        let syncer = Arc::new(syncer);

        let trigger = Arc::clone(&syncer);
        let watcher = Watcher::new(WATCH_DEBOUNCE, move || trigger.trigger());

        Arc::new(Self {
            client,
            store,
            syncer,
            watcher,
            prefs,
            cli_update_stop: Mutex::new(None),
        })
    }

    /// Loads settings, starts monitoring, seeds the shared CLI (synchronously
    /// — the Settings dialog reads the key right after), triggers the first
    /// sync, and starts the daily CLI self-update loop.
    pub fn start(&self) {
        let settings = self.store.load().unwrap_or_else(|err| {
            tracing::warn!("load settings: {err}");
            Settings::default()
        });
        *write(&self.prefs) = Prefs {
            tracking: !settings.tracking_disabled,
            auto_updates: !settings.automatic_updates_disabled,
        };

        self.restart_monitoring();

        if let Err(err) = agent_cli::bootstrap() {
            tracing::warn!("bootstrap shared CLI: {err}");
        }
        self.syncer.trigger();
        self.start_cli_update_loop();
    }

    /// Stops monitoring and the CLI self-update loop. The syncer worker stops
    /// when the `App` is dropped.
    pub fn stop(&self) {
        self.watcher.stop();
        lock(&self.cli_update_stop).take();
    }

    /// Whether session tracking is on.
    #[must_use]
    pub fn tracking_enabled(&self) -> bool {
        read(&self.prefs).tracking
    }

    /// Toggles tracking: off stops the watcher; on restarts monitoring and
    /// syncs immediately.
    pub fn set_tracking_enabled(&self, enabled: bool) {
        write(&self.prefs).tracking = enabled;
        self.save_pref(|settings| settings.tracking_disabled = !enabled);
        if enabled {
            self.restart_monitoring();
            self.syncer.trigger();
        } else {
            self.watcher.stop();
        }
    }

    /// Whether automatic app updates are on.
    #[must_use]
    pub fn automatic_updates_enabled(&self) -> bool {
        read(&self.prefs).auto_updates
    }

    /// Persists the automatic-updates preference.
    pub fn set_automatic_updates_enabled(&self, enabled: bool) {
        write(&self.prefs).auto_updates = enabled;
        self.save_pref(|settings| settings.automatic_updates_disabled = !enabled);
    }

    /// Requests a sync now (coalesced).
    pub fn sync_now(&self) {
        self.syncer.trigger();
    }

    /// Resolves the dashboard target: a minted login URL, falling back to the
    /// plain base URL. Blocking — never call on the UI thread.
    #[must_use]
    pub fn dashboard_target(&self) -> String {
        match self.client.dashboard_url() {
            Ok(url) => String::from(url),
            Err(err) => {
                tracing::debug!("dashboard url: {err}");
                agent_cli::base_url()
            }
        }
    }

    /// Reads the configured API key via the shared CLI.
    ///
    /// # Errors
    /// [`agent_cli::Error::MissingApiKey`] when none is configured; other
    /// [`agent_cli::Error`]s on CLI failure.
    pub fn api_key(&self) -> Result<String, agent_cli::Error> {
        self.client.api_key()
    }

    /// Stores a new API key, then syncs immediately.
    ///
    /// # Errors
    /// Returns [`agent_cli::Error`] when the CLI rejects the key.
    pub fn set_api_key(&self, key: &str) -> Result<(), agent_cli::Error> {
        self.client.set_api_key(key)?;
        self.sync_now();
        Ok(())
    }

    /// Points the watcher at the current provider watch paths.
    pub fn restart_monitoring(&self) {
        let paths = data_dirs::watch_paths();
        if let Err(err) = self.watcher.start(&paths) {
            tracing::warn!("start watcher: {err}");
        }
    }

    /// Re-loads the settings file before mutating so concurrent edits are not
    /// clobbered (Save rewrites the whole JSON).
    fn save_pref(&self, mutate: impl FnOnce(&mut Settings)) {
        let mut settings = self.store.load().unwrap_or_else(|err| {
            tracing::warn!("reload settings: {err}");
            Settings::default()
        });
        mutate(&mut settings);
        if let Err(err) = self.store.save(settings) {
            tracing::warn!("save settings: {err}");
        }
    }

    /// Runs `tokitoki update` now and then daily; errors are debug-only noise.
    fn start_cli_update_loop(&self) {
        let (stop_tx, stop_rx) = mpsc::sync_channel::<()>(0);
        let client = self.client;
        let spawned = thread::Builder::new()
            .name("cli-update".to_owned())
            .spawn(move || loop {
                if let Err(err) = client.update() {
                    tracing::debug!("shared CLI update: {err}");
                }
                match stop_rx.recv_timeout(CLI_UPDATE_INTERVAL) {
                    Err(RecvTimeoutError::Timeout) => {}
                    Ok(()) | Err(RecvTimeoutError::Disconnected) => return,
                }
            });
        if spawned.is_ok() {
            *lock(&self.cli_update_stop) = Some(stop_tx);
        }
    }
}

fn read<T>(lock: &RwLock<T>) -> std::sync::RwLockReadGuard<'_, T> {
    lock.read().unwrap_or_else(PoisonError::into_inner)
}

fn write<T>(lock: &RwLock<T>) -> std::sync::RwLockWriteGuard<'_, T> {
    lock.write().unwrap_or_else(PoisonError::into_inner)
}

fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}
