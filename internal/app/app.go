// Package app coordinates the Windows tray app services.
package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib"
	"github.com/tokitoki-dev/tokitoki-windows/internal/datadirs"
	"github.com/tokitoki-dev/tokitoki-windows/internal/settings"
	"github.com/tokitoki-dev/tokitoki-windows/internal/syncer"
	"github.com/tokitoki-dev/tokitoki-windows/internal/watcher"
)

const (
	syncInterval  = 30 * time.Minute
	watchDebounce = 2 * time.Second

	dashboardLoginTimeout = 10 * time.Second
)

// App owns the long-lived Windows client services.
type App struct {
	client     *agentlib.Client
	settings   *settings.Store
	syncer     *syncer.Syncer
	watcher    *watcher.Watcher
	logger *slog.Logger

	// prefsMu guards the cached user preferences below.
	prefsMu     sync.RWMutex
	tracking    bool
	autoUpdates bool

	ctx context.Context
}

// New creates an App.
func New(logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	client, err := agentlib.New(agentlib.Options{Logger: logger})
	if err != nil {
		return nil, err
	}

	store := settings.NewStore(client.DataDir())
	app := &App{
		client:   client,
		settings: store,
		logger:   logger,
	}
	app.syncer = syncer.New(client, app.syncOptions, logger)
	app.watcher = watcher.New(watchDebounce, app.syncer.Trigger, logger)
	return app, nil
}

// Start begins background sync, periodic sync, and filesystem monitoring.
func (a *App) Start(ctx context.Context) error {
	a.ctx = ctx
	config, err := a.settings.Load()
	if err != nil {
		return err
	}
	a.setTracking(!config.TrackingDisabled)
	a.prefsMu.Lock()
	a.autoUpdates = !config.AutomaticUpdatesDisabled
	a.prefsMu.Unlock()

	a.syncer.Start(ctx)
	a.syncer.Periodically(ctx, syncInterval)
	if err := a.RestartMonitoring(); err != nil {
		return err
	}
	a.syncer.Trigger()
	return nil
}

// Stop stops filesystem monitoring.
func (a *App) Stop() {
	if a.watcher != nil {
		a.watcher.Stop()
	}
}

// TrackingEnabled reports whether monitoring and syncing are active.
func (a *App) TrackingEnabled() bool {
	a.prefsMu.RLock()
	defer a.prefsMu.RUnlock()
	return a.tracking
}

// savePref applies mutate to the persisted settings. Save rewrites the whole
// file, so every preference must be read back first or saving one would erase
// the others.
func (a *App) savePref(mutate func(*settings.Settings)) error {
	current, err := a.settings.Load()
	if err != nil {
		return err
	}
	mutate(&current)
	return a.settings.Save(current)
}

// AutomaticUpdatesEnabled reports whether the background update check runs.
func (a *App) AutomaticUpdatesEnabled() bool {
	a.prefsMu.RLock()
	defer a.prefsMu.RUnlock()
	return a.autoUpdates
}

// SetAutomaticUpdatesEnabled persists the automatic-update switch. The
// background checker consults it before every run, so no restart is needed.
func (a *App) SetAutomaticUpdatesEnabled(enabled bool) error {
	if err := a.savePref(func(s *settings.Settings) {
		s.AutomaticUpdatesDisabled = !enabled
	}); err != nil {
		return err
	}
	a.prefsMu.Lock()
	a.autoUpdates = enabled
	a.prefsMu.Unlock()
	return nil
}

// SetTrackingEnabled persists the tracking switch and applies it: off stops
// filesystem monitoring and makes sync runs no-ops, on resumes both and
// syncs immediately to catch up on whatever happened while paused.
func (a *App) SetTrackingEnabled(enabled bool) error {
	if err := a.savePref(func(s *settings.Settings) {
		s.TrackingDisabled = !enabled
	}); err != nil {
		return err
	}
	a.setTracking(enabled)
	if !enabled {
		a.watcher.Stop()
		return nil
	}
	if err := a.RestartMonitoring(); err != nil {
		return err
	}
	a.SyncNow()
	return nil
}

// SyncNow requests an immediate sync.
func (a *App) SyncNow() {
	a.syncer.Trigger()
}

// DashboardTarget returns the URL the Dashboard action should open: a signed
// one-time login link when the server will mint one, so the browser lands
// already signed in; otherwise the plain server URL. Callers must not invoke
// this on the UI thread — it talks to the network.
func (a *App) DashboardTarget(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, dashboardLoginTimeout)
	defer cancel()
	url, err := a.client.DashboardURL(ctx)
	if err != nil {
		a.logger.Debug("dashboard login link unavailable", "error", err)
		return agentlib.BaseURL()
	}
	return url
}

// APIKey returns the configured API key.
func (a *App) APIKey() (string, error) {
	return a.client.GetAPIKey()
}

// SetAPIKey saves the configured API key.
func (a *App) SetAPIKey(apiKey string) error {
	if err := a.client.SetAPIKey(apiKey); err != nil {
		return err
	}
	a.SyncNow()
	return nil
}

// RestartMonitoring restarts filesystem monitoring. With tracking off there
// is nothing to watch: Start with no paths just stops the current watcher.
func (a *App) RestartMonitoring() error {
	if a.ctx == nil {
		return nil
	}
	var paths []string
	if a.TrackingEnabled() {
		paths = datadirs.WatchPaths()
	}
	return a.watcher.Start(a.ctx, paths)
}

// syncOptions resolves what a sync run should scan. Tracking off — or no API
// key yet, as on macOS — means nothing: the syncer already treats an empty
// provider set as a no-op, so neither pause needs a second mechanism.
func (a *App) syncOptions() agentlib.SyncOptions {
	if !a.TrackingEnabled() {
		return agentlib.SyncOptions{}
	}
	if _, err := a.client.GetAPIKey(); err != nil {
		if !errors.Is(err, agentlib.ErrMissingAPIKey) {
			a.logger.Warn("check api key", "error", err)
		}
		return agentlib.SyncOptions{}
	}
	return datadirs.Resolve().SyncOptions()
}

func (a *App) setTracking(enabled bool) {
	a.prefsMu.Lock()
	defer a.prefsMu.Unlock()
	a.tracking = enabled
}
