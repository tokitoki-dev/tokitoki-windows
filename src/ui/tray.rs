//! Tray icon registration, theme-aware glyph, and balloon notifications.
//!
//! The glyph is rendered at runtime by [`crate::logo`] at `16 * dpi / 96` px:
//! white on the default dark taskbar, dark on a light one.

use std::sync::atomic::{AtomicIsize, Ordering};

use windows::Win32::{
    Foundation::HWND,
    Graphics::Gdi::{CreateBitmap, DeleteObject},
    UI::{
        HiDpi::GetDpiForWindow,
        Shell::{
            Shell_NotifyIconW, NIF_ICON, NIF_INFO, NIF_MESSAGE, NIF_TIP, NIIF_INFO, NIIF_WARNING,
            NIM_ADD, NIM_DELETE, NIM_MODIFY, NIM_SETVERSION, NOTIFYICONDATAW, NOTIFYICONDATAW_0,
        },
        WindowsAndMessaging::{CreateIconIndirect, DestroyIcon, HICON, ICONINFO},
    },
};

use super::imp::WM_TRAY_CALLBACK;
use crate::logo;

const TRAY_ID: u32 = 1;
/// `NOTIFYICON_VERSION` (3): keeps the classic `WM_LBUTTONUP`/`WM_RBUTTONUP`
/// callback semantics AND makes the shell deliver balloon events
/// (`NIN_BALLOONUSERCLICK`). Without `NIM_SETVERSION` the shell stays on
/// version 0, which never sends balloon clicks — the update balloon would
/// look clickable but do nothing.
const NOTIFYICON_VERSION_3: u32 = 3;
/// The current glyph handle, so a theme flip can destroy the old one.
static CURRENT_ICON: AtomicIsize = AtomicIsize::new(0);

/// Registers the tray icon.
///
/// # Errors
/// Returns a message when the glyph or the shell registration fails.
pub(super) fn add(hwnd: HWND, light_taskbar: bool) -> Result<(), String> {
    let icon = glyph(hwnd, light_taskbar).ok_or("render tray glyph")?;
    let mut data = base_data(hwnd);
    data.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
    data.uCallbackMessage = WM_TRAY_CALLBACK;
    data.hIcon = icon;
    copy_wide(&mut data.szTip, "Tokitoki");
    // SAFETY: fully initialized NOTIFYICONDATAW for our own window.
    if !unsafe { Shell_NotifyIconW(NIM_ADD, &raw const data) }.as_bool() {
        return Err("Shell_NotifyIconW(NIM_ADD) failed".to_owned());
    }
    let mut version_data = base_data(hwnd);
    version_data.Anonymous = NOTIFYICONDATAW_0 {
        uVersion: NOTIFYICON_VERSION_3,
    };
    // SAFETY: opting the freshly added icon into version-3 callbacks.
    unsafe {
        let _ = Shell_NotifyIconW(NIM_SETVERSION, &raw const version_data);
    }
    swap_current(icon);
    Ok(())
}

/// Swaps in a re-rendered glyph after a taskbar theme change.
pub(super) fn refresh_icon(hwnd: HWND, light_taskbar: bool) {
    let Some(icon) = glyph(hwnd, light_taskbar) else {
        return;
    };
    let mut data = base_data(hwnd);
    data.uFlags = NIF_ICON;
    data.hIcon = icon;
    // SAFETY: modifying our registered tray icon.
    if unsafe { Shell_NotifyIconW(NIM_MODIFY, &raw const data) }.as_bool() {
        swap_current(icon);
    } else {
        destroy(icon);
    }
}

/// Shows a balloon notification.
pub(super) fn balloon(hwnd: HWND, title: &str, text: &str, warning: bool) {
    let mut data = base_data(hwnd);
    data.uFlags = NIF_INFO;
    data.dwInfoFlags = if warning { NIIF_WARNING } else { NIIF_INFO };
    copy_wide(&mut data.szInfoTitle, title);
    copy_wide(&mut data.szInfo, text);
    // SAFETY: modifying our registered tray icon.
    unsafe {
        let _ = Shell_NotifyIconW(NIM_MODIFY, &raw const data);
    }
}

/// Removes the tray icon and frees the glyph.
pub(super) fn remove(hwnd: HWND) {
    let data = base_data(hwnd);
    // SAFETY: deleting our registered tray icon.
    unsafe {
        let _ = Shell_NotifyIconW(NIM_DELETE, &raw const data);
    }
    swap_current(HICON(std::ptr::null_mut()));
}

fn base_data(hwnd: HWND) -> NOTIFYICONDATAW {
    NOTIFYICONDATAW {
        cbSize: u32::try_from(std::mem::size_of::<NOTIFYICONDATAW>()).unwrap_or(0),
        hWnd: hwnd,
        uID: TRAY_ID,
        ..Default::default()
    }
}

fn glyph(hwnd: HWND, light_taskbar: bool) -> Option<HICON> {
    // SAFETY: querying DPI of our own window.
    let dpi = unsafe { GetDpiForWindow(hwnd) }.max(96);
    let size = 16 * dpi / 96;
    let color = if light_taskbar {
        logo::DARK
    } else {
        logo::WHITE
    };
    hicon(&logo::mark(size, color))
}

/// Builds an `HICON` from a straight-alpha RGBA image via a 32bpp BGRA
/// bitmap plus an empty monochrome mask.
fn hicon(image: &logo::Image) -> Option<HICON> {
    let bgra: Vec<u8> = image
        .rgba
        .chunks_exact(4)
        .flat_map(|px| [px[2], px[1], px[0], px[3]])
        .collect();
    let side = i32::try_from(image.size).ok()?;
    // SAFETY: the pixel buffer outlives both CreateBitmap calls; GDI objects
    // are released on every path.
    unsafe {
        let color = CreateBitmap(side, side, 1, 32, Some(bgra.as_ptr().cast()));
        let mask = CreateBitmap(side, side, 1, 1, None);
        let info = ICONINFO {
            fIcon: true.into(),
            xHotspot: 0,
            yHotspot: 0,
            hbmMask: mask,
            hbmColor: color,
        };
        let icon = CreateIconIndirect(&raw const info).ok();
        let _ = DeleteObject(color.into());
        let _ = DeleteObject(mask.into());
        icon
    }
}

fn swap_current(new: HICON) {
    let old = CURRENT_ICON.swap(new.0 as isize, Ordering::AcqRel);
    destroy(HICON(old as *mut core::ffi::c_void));
}

fn destroy(icon: HICON) {
    if !icon.0.is_null() {
        // SAFETY: destroying an icon we created and no longer use.
        unsafe {
            let _ = DestroyIcon(icon);
        }
    }
}

fn copy_wide(dest: &mut [u16], text: &str) {
    let encoded: Vec<u16> = text.encode_utf16().take(dest.len() - 1).collect();
    dest[..encoded.len()].copy_from_slice(&encoded);
    dest[encoded.len()] = 0;
}
