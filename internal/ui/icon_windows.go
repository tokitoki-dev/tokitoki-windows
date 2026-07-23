//go:build windows

package ui

import (
	"github.com/lxn/walk"
	"golang.org/x/sys/windows/registry"
)

const appIconResourceID = 2

// newAppIcon returns the branded application icon embedded in the
// executable's resources; windows and the taskbar use it.
func newAppIcon() (*walk.Icon, error) {
	return walk.NewIconFromResourceId(appIconResourceID)
}

// taskbarUsesLightTheme reads the theme that governs the taskbar and tray:
// SystemUsesLightTheme, not AppsUseLightTheme. A missing value means the
// dark taskbar Windows 10 shipped with.
func taskbarUsesLightTheme() bool {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return false
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("SystemUsesLightTheme")
	return err == nil && value == 1
}
