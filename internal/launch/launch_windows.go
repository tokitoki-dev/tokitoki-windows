//go:build windows

// Package launch manages Windows launch-at-login integration.
package launch

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "Tokitoki"
)

// quotePath wraps an executable path in double quotes so a Run entry survives
// spaces in the path. Only quotes — not strconv.Quote, which would also escape
// the backslashes into "C:\\...", a form the entry must not hold.
func quotePath(path string) string {
	return `"` + path + `"`
}

// IsEnabled reports whether Tokitoki is registered to launch at login. It
// tests only for the entry's presence, not the path it holds: whether the
// user asked for autostart does not depend on which folder the app runs
// from. Reconcile keeps that path pointed at the current executable.
func IsEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	_, _, err = key.GetStringValue(valueName)
	return err == nil
}

// Reconcile repoints an existing launch-at-login entry at the current
// executable. Windows stores an absolute path, so moving the app would leave
// the entry aimed at a location that no longer runs, and autostart would
// silently break. Call once at startup: with no entry (autostart off) or a
// path that already matches, it does nothing.
func Reconcile() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer key.Close()

	current, _, err := key.GetStringValue(valueName)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	if strings.EqualFold(strings.Trim(current, `"`), executable) {
		return nil
	}
	return key.SetStringValue(valueName, quotePath(executable))
}

// SetEnabled registers or unregisters the current executable for login.
func SetEnabled(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enabled {
		if err := key.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return key.SetStringValue(valueName, quotePath(executable))
}
