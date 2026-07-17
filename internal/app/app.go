// Package app coordinates the Windows tray app services.
package app

import (
	"context"
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

// Status describes current sync state for UI presentation.
type Status struct {
	Syncing    bool
	LastSyncAt time.Time
	LastError  string
}

// App owns the long-lived Windows client services.
type App struct {
	client       *agentlib.Client
	settings     *settings.Store
	syncer       *syncer.Syncer
	watcher      *watcher.Watcher
	logger       *slog.Logger
	providerLock sync.RWMutex
	providers    []string
	statusLock   sync.RWMutex
	status       Status
	statusSubs   []func(Status)
	ctx          context.Context
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
	app.syncer.SetObserver(app)
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
	a.setProviders(config.EnabledProviders)

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

// EnabledProviders returns selected providers.
func (a *App) EnabledProviders() []string {
	a.providerLock.RLock()
	defer a.providerLock.RUnlock()
	return append([]string{}, a.providers...)
}

// SetEnabledProviders persists selected providers and restarts monitoring.
func (a *App) SetEnabledProviders(providers []string) error {
	providers = settings.NormalizeProviders(providers)
	if len(providers) == 0 {
		return a.settings.Save(settings.Settings{})
	}
	if err := a.settings.Save(settings.Settings{EnabledProviders: providers}); err != nil {
		return err
	}
	a.setProviders(providers)
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

// Status returns the current sync status.
func (a *App) Status() Status {
	a.statusLock.RLock()
	defer a.statusLock.RUnlock()
	return a.status
}

// OnStatusChanged registers a sync status callback.
func (a *App) OnStatusChanged(handler func(Status)) {
	a.statusLock.Lock()
	a.statusSubs = append(a.statusSubs, handler)
	status := a.status
	a.statusLock.Unlock()
	handler(status)
}

// SyncStarted records a running sync.
func (a *App) SyncStarted() {
	a.setStatus(func(status *Status) {
		status.Syncing = true
		status.LastError = ""
	})
}

// SyncSucceeded records a completed sync.
func (a *App) SyncSucceeded(at time.Time) {
	a.setStatus(func(status *Status) {
		status.Syncing = false
		status.LastSyncAt = at
		status.LastError = ""
	})
}

// SyncFailed records a failed sync.
func (a *App) SyncFailed(err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	a.setStatus(func(status *Status) {
		status.Syncing = false
		status.LastError = message
	})
}

// RestartMonitoring restarts filesystem monitoring for enabled providers.
func (a *App) RestartMonitoring() error {
	if a.ctx == nil {
		return nil
	}
	paths := datadirs.WatchPaths(a.EnabledProviders())
	return a.watcher.Start(a.ctx, paths)
}

func (a *App) syncOptions() agentlib.SyncOptions {
	return datadirs.Resolve(a.EnabledProviders()).SyncOptions()
}

func (a *App) setProviders(providers []string) {
	a.providerLock.Lock()
	defer a.providerLock.Unlock()
	a.providers = append([]string{}, providers...)
}

func (a *App) setStatus(update func(*Status)) {
	a.statusLock.Lock()
	update(&a.status)
	status := a.status
	subscribers := append([]func(Status){}, a.statusSubs...)
	a.statusLock.Unlock()

	for _, subscriber := range subscribers {
		subscriber(status)
	}
}
