//go:build windows

package ui

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

const dwmwaUseImmersiveDarkMode = 20

var (
	dwmapi                   = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttrib   = dwmapi.NewProc("DwmSetWindowAttribute")
	user32                   = syscall.NewLazyDLL("user32.dll")
	procSetProcessDPIContext = user32.NewProc("SetProcessDpiAwarenessContext")
)

func applyWindowTheme(hwnd win.HWND) {
	enableDarkTitleBar(hwnd)
}

func enableProcessDPIAwareness() {
	const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)
	procSetProcessDPIContext.Call(dpiAwarenessContextPerMonitorAwareV2)
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

func enableDarkTitleBar(hwnd win.HWND) {
	enabled := int32(1)
	procDwmSetWindowAttrib.Call(
		uintptr(hwnd),
		uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&enabled)),
		unsafe.Sizeof(enabled),
	)
}
