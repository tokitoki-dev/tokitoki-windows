//go:build windows

// Package launch manages Windows launch-at-login integration.
package launch

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "TokiToki"
)

// IsEnabled reports whether the current executable is registered for login.
func IsEnabled() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	value, _, err := key.GetStringValue(valueName)
	if err != nil {
		return false
	}
	value = strings.Trim(value, `"`)
	return strings.EqualFold(value, executable)
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
	return key.SetStringValue(valueName, strconv.Quote(executable))
}
