//go:build windows

package ui

import (
	"errors"
	"image"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"github.com/tokitoki-dev/tokitoki-windows/internal/logo"
)

// walk (2021) registers its NotifyIcon with a Vista-era struct size, no
// icon, and NIS_HIDDEN, then flips state and icon afterwards. Current
// Windows 11 shells report success for that sequence but never persist or
// show the icon. This file owns the shell registration instead: the same
// (window, id 0) identity walk uses, a full modern NOTIFYICONDATA, and the
// icon, tooltip, and visible state all present at NIM_ADD. walk's window
// proc keeps driving menu and click events because the callback message and
// version below match walk's own.

// walkTrayCallbackMessage is walk's private notifyIconMessageId (WM_APP+0).
const walkTrayCallbackMessage = win.WM_APP

// registerTrayIcon replaces whatever registration exists for the window with
// a complete, visible one. Call once at startup after walk's setup.
func registerTrayIcon(hwnd win.HWND, lightTaskbar bool, tip string) error {
	nid, hIcon, err := trayIconData(hwnd, lightTaskbar)
	if err != nil {
		return err
	}
	defer win.DestroyIcon(hIcon)

	if tip16, err := syscall.UTF16FromString(tip); err == nil {
		copy(nid.SzTip[:], tip16)
		nid.UFlags |= win.NIF_TIP
	}

	// Drop walk's half-registration first; failure means it never stuck,
	// which is exactly the case being repaired.
	win.Shell_NotifyIcon(win.NIM_DELETE, nid)
	if !win.Shell_NotifyIcon(win.NIM_ADD, nid) {
		return errors.New("Shell_NotifyIcon: add tray icon failed")
	}
	nid.UVersion = win.NOTIFYICON_VERSION
	if !win.Shell_NotifyIcon(win.NIM_SETVERSION, nid) {
		return errors.New("Shell_NotifyIcon: set tray icon version failed")
	}
	return nil
}

// updateTrayIcon swaps the glyph on the existing registration, for taskbar
// theme changes.
func updateTrayIcon(hwnd win.HWND, lightTaskbar bool) error {
	nid, hIcon, err := trayIconData(hwnd, lightTaskbar)
	if err != nil {
		return err
	}
	defer win.DestroyIcon(hIcon)

	if !win.Shell_NotifyIcon(win.NIM_MODIFY, nid) {
		return errors.New("Shell_NotifyIcon: update tray icon failed")
	}
	return nil
}

func trayIconData(hwnd win.HWND, lightTaskbar bool) (*win.NOTIFYICONDATA, win.HICON, error) {
	glyph := logo.White
	if lightTaskbar {
		glyph = logo.Dark
	}
	size := int(win.GetSystemMetrics(win.SM_CXSMICON))
	if size <= 0 {
		size = 16
	}
	hIcon, err := iconHandleFromImage(logo.Mark(size, glyph))
	if err != nil {
		return nil, 0, err
	}

	nid := &win.NOTIFYICONDATA{
		HWnd:             hwnd,
		UFlags:           win.NIF_MESSAGE | win.NIF_ICON,
		UCallbackMessage: walkTrayCallbackMessage,
		HIcon:            hIcon,
	}
	nid.CbSize = uint32(unsafe.Sizeof(*nid))
	return nid, hIcon, nil
}

// iconHandleFromImage builds an HICON with straight-alpha 32-bit color, the
// format DrawIconEx blends natively. The caller owns the returned handle;
// the shell copies it, so destroying after Shell_NotifyIcon is safe.
func iconHandleFromImage(img *image.NRGBA) (win.HICON, error) {
	width := img.Rect.Dx()
	height := img.Rect.Dy()

	info := win.BITMAPINFO{BmiHeader: win.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})),
		BiWidth:       int32(width),
		BiHeight:      -int32(height), // top-down, matching image rows
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: win.BI_RGB,
	}}

	hdc := win.GetDC(0)
	defer win.ReleaseDC(0, hdc)

	var bits unsafe.Pointer
	hColor := win.CreateDIBSection(hdc, &info.BmiHeader, win.DIB_RGB_COLORS, &bits, 0, 0)
	if hColor == 0 || bits == nil {
		return 0, errors.New("CreateDIBSection failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(hColor))

	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	for y := 0; y < height; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < width; x++ {
			pixels[(y*width+x)*4+0] = row[x*4+2] // B
			pixels[(y*width+x)*4+1] = row[x*4+1] // G
			pixels[(y*width+x)*4+2] = row[x*4+0] // R
			pixels[(y*width+x)*4+3] = row[x*4+3] // A
		}
	}

	hMask := win.CreateBitmap(int32(width), int32(height), 1, 1, nil)
	if hMask == 0 {
		return 0, errors.New("CreateBitmap failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(hMask))

	iconInfo := win.ICONINFO{FIcon: win.TRUE, HbmMask: hMask, HbmColor: hColor}
	hIcon := win.CreateIconIndirect(&iconInfo)
	if hIcon == 0 {
		return 0, errors.New("CreateIconIndirect failed")
	}
	return hIcon, nil
}
