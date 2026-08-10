//! Windows theme detection and dark-menu enabling.
//!
//! Theme state lives in the registry under
//! `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`:
//! `SystemUsesLightTheme` (taskbar/tray, default dark) and
//! `AppsUseLightTheme` (window content, default light).
//!
//! Dark context menus need the undocumented `uxtheme.dll` ordinals
//! 135 (`SetPreferredAppMode`) and 136 (`FlushMenuThemes`), available since
//! Windows 10 1903. Every lookup degrades silently on older builds.

use windows::{
    core::{w, PCSTR},
    Win32::System::LibraryLoader::{GetProcAddress, LoadLibraryExW, LOAD_LIBRARY_SEARCH_SYSTEM32},
};

const PERSONALIZE_KEY: &str = r"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize";
/// `SetPreferredAppMode` argument: follow the system dark/light preference.
const ALLOW_DARK: i32 = 1;

/// Whether the taskbar (and thus the tray) uses the light theme.
/// Missing value → `false` (the default taskbar is dark).
pub(super) fn taskbar_uses_light_theme() -> bool {
    read_personalize("SystemUsesLightTheme").unwrap_or(0) != 0
}

/// Whether app windows use the light theme. Missing value → `true`.
///
/// Not consumed yet — the placeholder Settings dialog has no chrome of its
/// own. The Settings dialog picks its chrome and palette from this.
pub(super) fn apps_use_light_theme() -> bool {
    read_personalize("AppsUseLightTheme").unwrap_or(1) != 0
}

/// Applies dark/light window chrome: immersive dark title bar, a Mica
/// (`DWMSBT_MAINWINDOW`) backdrop, and a caption bar tinted to `caption` so
/// the title bar blends into the window body instead of the stock
/// white/black strip. Fails soft on older Windows builds.
pub(super) fn apply_window_chrome(
    hwnd: windows::Win32::Foundation::HWND,
    dark: bool,
    caption: windows::Win32::Foundation::COLORREF,
) {
    use windows::Win32::Graphics::Dwm::{
        DwmSetWindowAttribute, DWMWA_CAPTION_COLOR, DWMWA_SYSTEMBACKDROP_TYPE,
        DWMWA_USE_IMMERSIVE_DARK_MODE,
    };

    let dark_flag: i32 = dark.into();
    let backdrop: i32 = 2; // DWMSBT_MAINWINDOW (Mica)
    let caption_value = caption.0;
    let size = u32::try_from(std::mem::size_of::<i32>()).unwrap_or(4);
    // SAFETY: all three attributes take a 4-byte value; failures are ignored
    // on purpose (attributes unsupported before Win10 1903 / Win11).
    unsafe {
        let _ = DwmSetWindowAttribute(
            hwnd,
            DWMWA_USE_IMMERSIVE_DARK_MODE,
            (&raw const dark_flag).cast(),
            size,
        );
        let _ = DwmSetWindowAttribute(
            hwnd,
            DWMWA_SYSTEMBACKDROP_TYPE,
            (&raw const backdrop).cast(),
            size,
        );
        let _ = DwmSetWindowAttribute(
            hwnd,
            DWMWA_CAPTION_COLOR,
            (&raw const caption_value).cast(),
            size,
        );
    }
}

fn read_personalize(value: &str) -> Option<u32> {
    winreg::RegKey::predef(winreg::enums::HKEY_CURRENT_USER)
        .open_subkey(PERSONALIZE_KEY)
        .ok()?
        .get_value::<u32, _>(value)
        .ok()
}

/// Opts this process into dark context menus. Call before any menu exists.
pub(super) fn enable_dark_menus() {
    if let Some(set_preferred_app_mode) = uxtheme_ordinal(135) {
        // SAFETY: SetPreferredAppMode(ordinal 135) takes a preferred-mode int
        // and returns the previous mode; calling convention is stdcall.
        let set_mode: extern "system" fn(i32) -> i32 =
            unsafe { std::mem::transmute(set_preferred_app_mode) };
        set_mode(ALLOW_DARK);
    }
    flush_menu_themes();
}

/// Re-applies menu theming after a system theme change.
pub(super) fn flush_menu_themes() {
    if let Some(flush) = uxtheme_ordinal(136) {
        // SAFETY: FlushMenuThemes(ordinal 136) takes no arguments.
        let flush_fn: extern "system" fn() = unsafe { std::mem::transmute(flush) };
        flush_fn();
    }
}

fn uxtheme_ordinal(ordinal: u16) -> Option<unsafe extern "system" fn() -> isize> {
    // SAFETY: system32-restricted load of a system DLL; ordinal lookup via
    // the low word of the name pointer, per Win32 convention.
    unsafe {
        let module = LoadLibraryExW(w!("uxtheme.dll"), None, LOAD_LIBRARY_SEARCH_SYSTEM32).ok()?;
        GetProcAddress(module, PCSTR(ordinal as usize as *const u8))
    }
}
