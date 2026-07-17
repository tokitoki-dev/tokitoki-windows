//go:build !windows

// Package ui implements the native Windows tray and dialogs.
package ui

import (
	"context"
	"log/slog"

	coreapp "github.com/tokitoki-dev/tokitoki-windows/internal/app"
)

// Run is a no-op outside Windows.
func Run(context.Context, *coreapp.App, *slog.Logger) error {
	return nil
}
