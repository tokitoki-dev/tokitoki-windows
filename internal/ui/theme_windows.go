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

func enableDarkTitleBar(hwnd win.HWND) {
	enabled := int32(1)
	procDwmSetWindowAttrib.Call(
		uintptr(hwnd),
		uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&enabled)),
		unsafe.Sizeof(enabled),
	)
}
