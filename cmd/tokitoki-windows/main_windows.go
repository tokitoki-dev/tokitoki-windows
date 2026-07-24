//go:build windows

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tokitoki-dev/tokitoki-windows/internal/app"
	"github.com/tokitoki-dev/tokitoki-windows/internal/appupdate"
	"github.com/tokitoki-dev/tokitoki-windows/internal/instance"
	"github.com/tokitoki-dev/tokitoki-windows/internal/launch"
	"github.com/tokitoki-dev/tokitoki-windows/internal/ui"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	appupdate.CleanupLeftovers()
	lock, acquired, err := instance.Acquire()
	if err != nil {
		logger.Error("acquire instance lock", "error", err)
		os.Exit(1)
	}
	if !acquired {
		return
	}
	defer lock.Close()

	// If the app was moved, its launch-at-login entry still names the old
	// path; point it back at where we actually run. Best-effort — a failure
	// here only leaves autostart stale, it must not stop the app.
	if err := launch.Reconcile(); err != nil {
		logger.Warn("reconcile launch-at-login", "error", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	trayApp, err := app.New(logger)
	if err != nil {
		logger.Error("create app", "error", err)
		os.Exit(1)
	}
	defer trayApp.Stop()

	if err := trayApp.Start(ctx); err != nil {
		logger.Error("start app", "error", err)
		os.Exit(1)
	}

	if err := ui.Run(ctx, trayApp, logger); err != nil {
		logger.Error("run ui", "error", err)
		os.Exit(1)
	}

	// An accepted update restart: the new process may only be spawned after
	// this one has released the instance lock, or it would see a live
	// instance and quit. Stop and Close are idempotent, so the deferred
	// calls above stay harmless.
	if target, ok := ui.PendingRestart(); ok {
		trayApp.Stop()
		_ = lock.Close()
		if err := appupdate.Relaunch(target); err != nil {
			logger.Error("relaunch after update", "error", err)
		}
	}
}
