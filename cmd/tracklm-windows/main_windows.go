//go:build windows

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/labx/tracklm-windows/internal/app"
	"github.com/labx/tracklm-windows/internal/ui"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
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
}
