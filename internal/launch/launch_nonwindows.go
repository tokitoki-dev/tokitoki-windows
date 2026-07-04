//go:build !windows

// Package launch manages Windows launch-at-login integration.
package launch

// IsEnabled reports whether the current executable is registered for login.
func IsEnabled() bool {
	return false
}

// SetEnabled registers or unregisters the current executable for login.
func SetEnabled(enabled bool) error {
	return nil
}
