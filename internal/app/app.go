// Package app coordinates the Windows tray app services.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokitoki-dev/tokitoki-windows/internal/agentcli"
	"github.com/tokitoki-dev/tokitoki-windows/internal/datadirs"
	"github.com/tokitoki-dev/tokitoki-windows/internal/settings"
	"github.com/tokitoki-dev/tokitoki-windows/internal/syncer"
	"github.com/tokitoki-dev/tokitoki-windows/internal/watcher"
)

const (
	syncInterval  = 30 * time.Minute
	watchDebounce = 2 * time.Second

	// cliUpdateInterval paces `tokitoki update` after the launch run — rule
	// three of the shared-CLI contract: the CLI owns its own freshness, the
	// app only asks on a slow timer.
	cliUpdateInterval = 24 * time.Hour
)

// App owns the long-lived Windows client services.
type App struct {
	client     *agentcli.Client
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

	dataDir, err := agentcli.DataDir()
	if err != nil {
		return nil, err
	}

	app := &App{
		client:   agentcli.NewClient(logger),
		settings: settings.NewStore(dataDir),
		logger:   logger,
	}
	app.syncer = syncer.New(app.client, app.syncOptions, logger)
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
	// The shared CLI must exist before anything invokes it: the startup
	// Settings dialog reads the key right after Start returns, so seeding
	// runs here, not in a goroutine it would race. The common case — shared
	// CLI already current — costs one version query.
	agentcli.Bootstrap(ctx, a.logger)
	a.syncer.Trigger()
	go a.updateSharedCLI(ctx)
	return nil
}

// updateSharedCLI keeps the shared CLI fresh by delegating to the CLI
// itself: `tokitoki update` at launch, then daily. Failure costs a log line;
// a dev machine without a shared CLI just logs and tries again tomorrow.
func (a *App) updateSharedCLI(ctx context.Context) {
	for {
		if err := a.client.Update(ctx); err != nil {
			a.logger.Debug("shared CLI update", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cliUpdateInterval):
		}
	}
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
	url, err := a.client.DashboardURL(ctx)
	if err != nil {
		a.logger.Debug("dashboard login link unavailable", "error", err)
		return agentcli.BaseURL()
	}
	return url
}

// APIKey returns the configured API key.
func (a *App) APIKey() (string, error) {
	return a.client.APIKey(context.Background())
}

// SetAPIKey saves the configured API key.
func (a *App) SetAPIKey(apiKey string) error {
	if err := a.client.SetAPIKey(context.Background(), apiKey); err != nil {
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

// syncOptions resolves what a sync run should scan. Tracking off means
// nothing: the syncer already treats an empty provider set as a no-op. A
// missing API key is deliberately not checked here — scanning is offline
// work, events queue locally, and the CLI skips the upload half on its own
// until a key is saved.
func (a *App) syncOptions() map[string][]string {
	if !a.TrackingEnabled() {
		return nil
	}
	return datadirs.Resolve()
}

func (a *App) setTracking(enabled bool) {
	a.prefsMu.Lock()
	defer a.prefsMu.Unlock()
	a.tracking = enabled
}
