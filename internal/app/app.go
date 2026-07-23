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
	logger     *slog.Logger
	trackingMu sync.RWMutex
	tracking   bool
	ctx        context.Context
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
	a.trackingMu.RLock()
	defer a.trackingMu.RUnlock()
	return a.tracking
}

// SetTrackingEnabled persists the tracking switch and applies it: off stops
// filesystem monitoring and makes sync runs no-ops, on resumes both and
// syncs immediately to catch up on whatever happened while paused.
func (a *App) SetTrackingEnabled(enabled bool) error {
	if err := a.settings.Save(settings.Settings{TrackingDisabled: !enabled}); err != nil {
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
	a.trackingMu.Lock()
	defer a.trackingMu.Unlock()
	a.tracking = enabled
}
