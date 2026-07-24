//go:build windows

package ui

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaSystemBackdropType   = 38
	dwmsbtMainWindow          = 2 // Mica
)

var (
	dwmapi                   = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttrib   = dwmapi.NewProc("DwmSetWindowAttribute")
	user32                   = syscall.NewLazyDLL("user32.dll")
	procSetProcessDPIContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSPIForDpi            = user32.NewProc("SystemParametersInfoForDpi")
	procSetLayeredWindowAttr = user32.NewProc("SetLayeredWindowAttributes")
)

// lwaAlpha selects the alpha argument of SetLayeredWindowAttributes.
const lwaAlpha = 0x2

// makeOwnerPhantom turns the tray's main window into an owner nobody sees. It
// has to exist — it owns the dialogs and carries the tray's messages — but it
// must never appear. walk re-shows a dialog's owner as that dialog closes and
// enforces a minimum window size, so it cannot simply be shrunk out of sight;
// it is made fully transparent instead, and kept out of the taskbar and the
// focus order. Centering it also centers the dialogs, which walk positions
// relative to their owner.
func makeOwnerPhantom(form *walk.MainWindow) {
	hwnd := form.Handle()
	style := win.GetWindowLong(hwnd, win.GWL_EXSTYLE)
	win.SetWindowLong(hwnd, win.GWL_EXSTYLE, style|
		win.WS_EX_LAYERED|win.WS_EX_TOOLWINDOW|win.WS_EX_TRANSPARENT|win.WS_EX_NOACTIVATE)
	procSetLayeredWindowAttr.Call(uintptr(hwnd), 0, 0, lwaAlpha)

	width := int(win.GetSystemMetrics(win.SM_CXSCREEN))
	height := int(win.GetSystemMetrics(win.SM_CYSCREEN))
	if width <= 0 || height <= 0 {
		return
	}
	// Collapse to walk's enforced minimum, then center whatever that turns
	// out to be.
	_ = form.SetBoundsPixels(walk.Rectangle{X: width / 2, Y: height / 2, Width: 1, Height: 1})
	bounds := form.BoundsPixels()
	_ = form.SetBoundsPixels(walk.Rectangle{
		X:      width/2 - bounds.Width/2,
		Y:      height/2 - bounds.Height/2,
		Width:  bounds.Width,
		Height: bounds.Height,
	})
}

func applyWindowTheme(hwnd win.HWND) {
	applyDialogTheme(hwnd)
}

// applyDialogTheme gives a top-level window the chrome the system theme
// asks for: a dark title bar when apps are dark, and the Mica backdrop on
// Windows 11 22H2+. Every DWM call fails soft on systems that predate its
// attribute.
func applyDialogTheme(hwnd win.HWND) {
	dark := int32(0)
	if !appsUseLightTheme() {
		dark = 1
	}
	setWindowAttribute(hwnd, dwmwaUseImmersiveDarkMode, dark)
	setWindowAttribute(hwnd, dwmwaSystemBackdropType, dwmsbtMainWindow)
}

func setWindowAttribute(hwnd win.HWND, attribute uint32, value int32) {
	procDwmSetWindowAttrib.Call(
		uintptr(hwnd),
		uintptr(attribute),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
}

// appsUseLightTheme reads the theme that governs window chrome and app
// content — AppsUseLightTheme, distinct from the taskbar's
// SystemUsesLightTheme. A missing value means light, the Windows default.
func appsUseLightTheme() bool {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return true
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return true
	}
	return value != 0
}

func enableProcessDPIAwareness() {
	const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)
	procSetProcessDPIContext.Call(dpiAwarenessContextPerMonitorAwareV2)
}

// Dark menus and scrollbars come from uxtheme's dark engine, reachable only
// through unnamed ordinal exports — the same ones Notepad++, wxWidgets and
// every other dark-capable Win32 app ships on. They are stable since
// Windows 10 1903 but still undocumented, so everything here degrades to
// the classic light look when a lookup fails.
const (
	ordinalSetPreferredAppMode = 135
	ordinalFlushMenuThemes     = 136
	appModeAllowDark           = 1
)

var procFlushMenuThemes uintptr

// enableDarkMenus opts the process into dark context menus on builds that
// have the dark engine. AllowDark follows the system theme: dark menus on a
// dark system, light on a light one — never forced.
func enableDarkMenus() {
	major, _, build := windows.RtlGetNtVersionNumbers()
	build &= 0xFFFF // the raw value carries flag bits
	if major < 10 || (major == 10 && build < 18362) {
		return
	}

	uxtheme, err := windows.LoadLibraryEx("uxtheme.dll", 0, windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return
	}
	setPreferredAppMode, err := windows.GetProcAddressByOrdinal(uxtheme, ordinalSetPreferredAppMode)
	if err != nil {
		return
	}
	syscall.SyscallN(setPreferredAppMode, appModeAllowDark)

	if proc, err := windows.GetProcAddressByOrdinal(uxtheme, ordinalFlushMenuThemes); err == nil {
		procFlushMenuThemes = proc
		flushMenuThemes()
	}
}

// flushMenuThemes re-resolves menu theming; call it when the system theme
// flips so open and future menus pick up the new mode.
func flushMenuThemes() {
	if procFlushMenuThemes != 0 {
		syscall.SyscallN(procFlushMenuThemes)
	}
}

var themeWatchPrevProc uintptr

// watchTaskbarTheme invokes onChange on every WM_SETTINGCHANGE broadcast —
// the message a theme flip arrives as on each top-level window. The callback
// must therefore be cheap and idempotent; deciding whether anything actually
// changed is its job. walk offers no hook for arbitrary messages, so the
// hidden main window's proc is chained. onChange runs on the UI thread.
func watchTaskbarTheme(hwnd win.HWND, onChange func()) {
	callback := syscall.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
		if msg == win.WM_SETTINGCHANGE {
			onChange()
		}
		return win.CallWindowProc(themeWatchPrevProc, hwnd, msg, wParam, lParam)
	})
	themeWatchPrevProc = win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, callback)
}

// messageFont returns the user's dialog font, the one
// SPI_GETNONCLIENTMETRICS designates for message text. Hardcoding "Segoe UI
// 9pt" happens to match a stock install and silently ignores accessibility
// scaling and font substitution; asking the system is the difference.
// Values are read at 96 DPI — walk scales fonts per monitor itself.
func messageFont() (family string, pointSize int) {
	family, pointSize = "Segoe UI", 9

	var metrics win.NONCLIENTMETRICS
	metrics.CbSize = uint32(unsafe.Sizeof(metrics))
	if procSPIForDpi.Find() != nil {
		return family, pointSize
	}
	ret, _, _ := procSPIForDpi.Call(
		win.SPI_GETNONCLIENTMETRICS,
		uintptr(metrics.CbSize),
		uintptr(unsafe.Pointer(&metrics)),
		0,
		96,
	)
	if ret == 0 {
		return family, pointSize
	}

	if name := syscall.UTF16ToString(metrics.LfMessageFont.LfFaceName[:]); name != "" {
		family = name
	}
	if height := metrics.LfMessageFont.LfHeight; height < 0 {
		pointSize = int(-height) * 72 / 96
	}
	return family, pointSize
}
