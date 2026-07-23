//go:build windows

package ui

import (
	"github.com/lxn/walk"
	"github.com/tokitoki-dev/tokitoki-windows/internal/logo"
)

// walk owns the tray's shell registration (NewNotifyIcon + SetIcon +
// SetVisible); this file only renders the glyph walk shows. The mark is
// monochrome and generated on the fly — white on the default dark taskbar,
// dark on a light one — so there is no bitmap asset to ship or theme.

// newTrayIcon renders the Tokitoki mark for the current taskbar theme, sized
// for the notification area at the given DPI so it stays crisp on high-DPI
// displays. The caller owns the returned icon and disposes it once it is no
// longer the icon walk is showing.
func newTrayIcon(dpi int, lightTaskbar bool) (*walk.Icon, error) {
	glyph := logo.White
	if lightTaskbar {
		glyph = logo.Dark
	}
	if dpi <= 0 {
		dpi = 96
	}
	size := 16 * dpi / 96
	return walk.NewIconFromImageForDPI(logo.Mark(size, glyph), dpi)
}
