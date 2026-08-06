//! The Settings window (mirrors the walk-based dialog in Go `internal/ui`).
//!
//! Raw Win32, DPI-scaled, fixed-size, themed light or dark from
//! `AppsUseLightTheme`:
//!
//! - **API Key**: edit box with the "Paste your API key" cue banner, saved on
//!   focus-out only when non-empty and changed; "Verify Key" button with an
//!   inline status label.
//! - **Launch at login** and **Automatic updates** rows: bold title, muted
//!   description, right-pinned checkbox applied instantly.
//! - **Check for Updates** button with inline status.
//! - Muted version footer.
//!
//! The dialog runs on its own thread with its own message loop; blocking CLI
//! and HTTP calls run on worker threads that post results back via
//! `WM_APP`-range messages.

use std::{
    cell::RefCell,
    sync::{Arc, Mutex},
    thread,
};

use windows::{
    core::{w, PCWSTR},
    Win32::{
        Foundation::{COLORREF, HWND, LPARAM, LRESULT, RECT, WPARAM},
        Graphics::Gdi::{
            CreateFontIndirectW, CreateSolidBrush, DeleteObject, FillRect, SetBkColor, SetBkMode,
            SetTextColor, FW_BOLD, HBRUSH, HDC, HFONT, TRANSPARENT,
        },
        System::LibraryLoader::GetModuleHandleW,
        UI::{
            Controls::{SetWindowTheme, BST_CHECKED, BST_UNCHECKED},
            HiDpi::{AdjustWindowRectExForDpi, GetDpiForWindow, SystemParametersInfoForDpi},
            Input::KeyboardAndMouse::EnableWindow,
            WindowsAndMessaging::{
                CreateWindowExW, DefWindowProcW, DestroyWindow, DispatchMessageW, GetClientRect,
                GetDlgItem, GetMessageW, GetSystemMetrics, GetWindowLongPtrW, GetWindowTextW,
                IDC_ARROW, IsDialogMessageW, LoadCursorW, LoadIconW, PostMessageW,
                PostQuitMessage, RegisterClassW,
                SendMessageW, SetWindowLongPtrW, SetWindowPos, SetWindowTextW, ShowWindow,
                TranslateMessage, BM_GETCHECK, BM_SETCHECK, BS_AUTOCHECKBOX, ES_AUTOHSCROLL,
                GWLP_USERDATA, HMENU, HWND_TOP, ICON_BIG, MSG, NONCLIENTMETRICSW, SM_CXSCREEN,
                SM_CYSCREEN, SPI_GETNONCLIENTMETRICS, SWP_NOACTIVATE, SWP_NOZORDER, SW_SHOW,
                WINDOW_EX_STYLE, WINDOW_STYLE, WM_APP, WM_CLOSE, WM_COMMAND, WM_CTLCOLORBTN,
                WM_CTLCOLOREDIT, WM_CTLCOLORSTATIC, WM_DESTROY, WM_ERASEBKGND, WM_NCDESTROY,
                WM_SETFONT, WM_SETICON, WNDCLASSW, WS_BORDER, WS_CAPTION, WS_CHILD, WS_SYSMENU,
                WS_TABSTOP, WS_VISIBLE,
            },
        },
    },
};

use super::{
    imp::{wide, Ui},
    task_dialog, theme,
};
use crate::{agent_cli, api_key::Verifier, app_update, launch, version};

/// Posted by the verify worker; `wparam` is a [`VerifyOutcome`] code.
const WM_VERIFY_RESULT: u32 = WM_APP + 10;
/// Posted by the update-check worker; `wparam` is an [`UpdateOutcome`] code.
const WM_UPDATE_RESULT: u32 = WM_APP + 11;

const ID_CANCEL: u16 = 2; // IDCANCEL, sent by IsDialogMessage on Esc
const ID_KEY_EDIT: u16 = 1001;
const ID_VERIFY_BTN: u16 = 1002;
const ID_LAUNCH_CHK: u16 = 1003;
const ID_UPDATES_CHK: u16 = 1004;
const ID_CHECK_BTN: u16 = 1005;

const EN_KILLFOCUS: u16 = 0x0200;
const BN_CLICKED: u16 = 0;
/// `EM_SETCUEBANNER` (`ECM_FIRST + 1`).
const EM_SETCUEBANNER: u32 = 0x1501;

/// Layout constants in 96-dpi pixels (mirroring the Go dialog's metrics).
const CLIENT_WIDTH: i32 = 460;
const MARGIN_X: i32 = 20;
const MARGIN_Y: i32 = 18;
const SPACING: i32 = 12;
const BUTTON_WIDTH: i32 = 170;

#[derive(Clone, Copy)]
enum VerifyOutcome {
    Valid = 0,
    Invalid = 1,
    Unavailable = 2,
}

#[derive(Clone, Copy)]
enum UpdateOutcome {
    UpToDate = 0,
    Available = 1,
    DevBuild = 2,
    Failed = 3,
}

struct Palette {
    text: COLORREF,
    muted: COLORREF,
    background: COLORREF,
    edit_background: COLORREF,
    separator: COLORREF,
}

const fn rgb(r: u32, g: u32, b: u32) -> COLORREF {
    COLORREF(r | (g << 8) | (b << 16))
}

impl Palette {
    fn for_theme(dark: bool) -> Self {
        if dark {
            Self {
                text: rgb(0xF4, 0xF4, 0xF5),
                muted: rgb(0xA1, 0xA1, 0xAA),
                background: rgb(0x20, 0x20, 0x20),
                edit_background: rgb(0x2D, 0x2D, 0x2D),
                separator: rgb(0x3F, 0x3F, 0x46),
            }
        } else {
            Self {
                text: rgb(0x1B, 0x1B, 0x1B),
                muted: rgb(0x6B, 0x72, 0x80),
                background: rgb(0xF3, 0xF3, 0xF3),
                edit_background: rgb(0xFF, 0xFF, 0xFF),
                separator: rgb(0xE5, 0xE7, 0xEB),
            }
        }
    }
}

/// Per-dialog state, owned by the window via `GWLP_USERDATA`; touched only on
/// the dialog thread.
struct State {
    ui: &'static Ui,
    palette: Palette,
    font: HFONT,
    bold_font: HFONT,
    background_brush: HBRUSH,
    edit_brush: HBRUSH,
    separator_brush: HBRUSH,
    key_edit: HWND,
    verify_btn: HWND,
    verify_status: HWND,
    check_btn: HWND,
    update_status: HWND,
    muted_labels: Vec<HWND>,
    separators: Vec<HWND>,
    /// Last key handed to `set key`, so focus-out saves only real changes.
    last_saved_key: RefCell<String>,
    /// Version reported by the last update check (worker → UI thread).
    available_version: Arc<Mutex<String>>,
}

/// Opens the Settings window and blocks this thread until it closes.
pub(super) fn show(ui: &'static Ui) {
    // Blocking CLI read before any window exists — this thread owns no UI yet.
    let initial_key = ui.app.api_key().unwrap_or_default();

    // SAFETY: window class registration, window/control creation, and the
    // message pump all stay on this thread; State is freed in WM_NCDESTROY.
    unsafe {
        let Ok(instance) = GetModuleHandleW(None) else {
            return;
        };
        let class = WNDCLASSW {
            lpfnWndProc: Some(wndproc),
            hInstance: instance.into(),
            lpszClassName: w!("TokitokiSettingsWindow"),
            // Without a class cursor WM_SETCURSOR keeps whatever cursor was
            // last active — the app-starting spinner, after the blocking
            // `get key` call above.
            hCursor: LoadCursorW(None, IDC_ARROW).unwrap_or_default(),
            ..Default::default()
        };
        // Re-registration on later opens fails; that is fine.
        let _ = RegisterClassW(&raw const class);

        let Ok(hwnd) = CreateWindowExW(
            WINDOW_EX_STYLE(0),
            w!("TokitokiSettingsWindow"),
            w!("Settings"),
            WS_CAPTION | WS_SYSMENU,
            0,
            0,
            10,
            10,
            None,
            None,
            Some(instance.into()),
            None,
        ) else {
            return;
        };

        let dark = !theme::apps_use_light_theme();
        theme::apply_window_chrome(hwnd, dark);
        // MAKEINTRESOURCE(2): the app icon compiled in by build.rs.
        if let Ok(icon) = LoadIconW(
            Some(instance.into()),
            PCWSTR(std::ptr::without_provenance(2)),
        ) {
            SendMessageW(
                hwnd,
                WM_SETICON,
                Some(WPARAM(ICON_BIG as usize)),
                Some(LPARAM(icon.0 as isize)),
            );
        }

        let state = build_contents(ui, hwnd, dark, &initial_key);
        SetWindowLongPtrW(hwnd, GWLP_USERDATA, Box::into_raw(Box::new(state)) as isize);

        let _ = ShowWindow(hwnd, SW_SHOW);

        let mut msg = MSG::default();
        while GetMessageW(&raw mut msg, None, 0, 0).as_bool() {
            if IsDialogMessageW(hwnd, &raw const msg).as_bool() {
                continue;
            }
            let _ = TranslateMessage(&raw const msg);
            DispatchMessageW(&raw const msg);
        }
    }
}

/// Creates all child controls, sizes and centers the window. Returns the
/// dialog state. Must run on the dialog thread.
#[expect(
    clippy::too_many_lines,
    reason = "linear top-to-bottom control layout; splitting it hides the visual order"
)]
unsafe fn build_contents(ui: &'static Ui, hwnd: HWND, dark: bool, initial_key: &str) -> State {
    // SAFETY: forwarded from `show`; all handles belong to this thread.
    unsafe {
        let dpi = GetDpiForWindow(hwnd).max(96);
        let scale = |value: i32| value * i32::try_from(dpi).unwrap_or(96) / 96;
        let (font, bold_font) = message_fonts(dpi);
        let palette = Palette::for_theme(dark);

        let content_width = CLIENT_WIDTH - 2 * MARGIN_X;
        let mut muted_labels = Vec::new();
        let mut separators = Vec::new();
        let mut y = MARGIN_Y;

        let control_theme = if dark {
            w!("DarkMode_Explorer")
        } else {
            w!("Explorer")
        };
        let make =
            |class: PCWSTR, text: &str, style: u32, id: u16, x: i32, y: i32, w_: i32, h: i32| {
                let text_wide = wide(text);
                let control = CreateWindowExW(
                    WINDOW_EX_STYLE(0),
                    class,
                    PCWSTR(text_wide.as_ptr()),
                    WS_CHILD | WS_VISIBLE | WINDOW_STYLE(style),
                    scale(x),
                    scale(y),
                    scale(w_),
                    scale(h),
                    Some(hwnd),
                    Some(HMENU(id as usize as *mut core::ffi::c_void)),
                    None,
                    None,
                )
                .unwrap_or_default();
                let bold = style & STYLE_BOLD_MARKER != 0;
                let font_handle = if bold { bold_font } else { font };
                SendMessageW(
                    control,
                    WM_SETFONT,
                    Some(WPARAM(font_handle.0 as usize)),
                    Some(LPARAM(1)),
                );
                let _ = SetWindowTheme(control, control_theme, PCWSTR::null());
                control
            };

        // API Key section.
        make(
            w!("STATIC"),
            "API Key",
            STYLE_BOLD_MARKER,
            0,
            MARGIN_X,
            y,
            content_width,
            16,
        );
        y += 22;
        let key_edit = make(
            w!("EDIT"),
            initial_key,
            WS_TABSTOP.0 | WS_BORDER.0 | ES_AUTOHSCROLL as u32,
            ID_KEY_EDIT,
            MARGIN_X,
            y,
            content_width,
            24,
        );
        let cue = wide("Paste your API key");
        SendMessageW(
            key_edit,
            EM_SETCUEBANNER,
            Some(WPARAM(1)),
            Some(LPARAM(cue.as_ptr() as isize)),
        );
        y += 24 + 8;
        let verify_btn = make(
            w!("BUTTON"),
            "Verify Key",
            WS_TABSTOP.0,
            ID_VERIFY_BTN,
            MARGIN_X,
            y,
            BUTTON_WIDTH,
            26,
        );
        let verify_status = make(
            w!("STATIC"),
            "",
            0,
            0,
            MARGIN_X + BUTTON_WIDTH + 8,
            y + 5,
            content_width - BUTTON_WIDTH - 8,
            16,
        );
        muted_labels.push(verify_status);
        y += 26 + SPACING;

        separators.push(make(w!("STATIC"), "", 0, 0, MARGIN_X, y, content_width, 1));
        y += 1 + SPACING;

        // Toggle rows: bold title, muted description, right-pinned checkbox.
        let mut toggle_row = |title: &str, description: &str, id: u16, checked: bool| {
            make(
                w!("STATIC"),
                title,
                STYLE_BOLD_MARKER,
                0,
                MARGIN_X,
                y,
                content_width - 40,
                16,
            );
            let description_label = make(
                w!("STATIC"),
                description,
                0,
                0,
                MARGIN_X,
                y + 18,
                content_width - 40,
                16,
            );
            muted_labels.push(description_label);
            let checkbox = make(
                w!("BUTTON"),
                "",
                WS_TABSTOP.0 | BS_AUTOCHECKBOX as u32,
                id,
                CLIENT_WIDTH - MARGIN_X - 16,
                y + 8,
                16,
                16,
            );
            SendMessageW(
                checkbox,
                BM_SETCHECK,
                Some(WPARAM(if checked {
                    BST_CHECKED.0
                } else {
                    BST_UNCHECKED.0
                } as usize)),
                None,
            );
            y += 34 + SPACING;
        };
        toggle_row(
            "Launch at login",
            "Start Tokitoki automatically when you sign in.",
            ID_LAUNCH_CHK,
            launch::is_enabled(),
        );
        toggle_row(
            "Automatic updates",
            "Download and offer updates when they are ready.",
            ID_UPDATES_CHK,
            ui.app.automatic_updates_enabled(),
        );

        let check_btn = make(
            w!("BUTTON"),
            "Check for Updates",
            WS_TABSTOP.0,
            ID_CHECK_BTN,
            MARGIN_X,
            y,
            BUTTON_WIDTH,
            26,
        );
        let update_status = make(
            w!("STATIC"),
            "",
            0,
            0,
            MARGIN_X + BUTTON_WIDTH + 8,
            y + 5,
            content_width - BUTTON_WIDTH - 8,
            16,
        );
        muted_labels.push(update_status);
        y += 26 + SPACING;

        separators.push(make(w!("STATIC"), "", 0, 0, MARGIN_X, y, content_width, 1));
        y += 1 + SPACING;
        let version_label = make(
            w!("STATIC"),
            &version::summary(),
            0,
            0,
            MARGIN_X,
            y,
            content_width,
            16,
        );
        muted_labels.push(version_label);
        y += 16 + MARGIN_Y;

        size_and_center(hwnd, dpi, scale(CLIENT_WIDTH), scale(y));

        let background_brush = CreateSolidBrush(palette.background);
        let edit_brush = CreateSolidBrush(palette.edit_background);
        let separator_brush = CreateSolidBrush(palette.separator);
        State {
            ui,
            palette,
            font,
            bold_font,
            background_brush,
            edit_brush,
            separator_brush,
            key_edit,
            verify_btn,
            verify_status,
            check_btn,
            update_status,
            muted_labels,
            separators,
            last_saved_key: RefCell::new(initial_key.to_owned()),
            available_version: Arc::new(Mutex::new(String::new())),
        }
    }
}

/// Marker OR-ed into the style argument of `make` to request the bold font;
/// stripped before it reaches `CreateWindowExW` (bit 31 is unused by the
/// static/button styles we create).
const STYLE_BOLD_MARKER: u32 = 0x8000_0000;

unsafe fn size_and_center(hwnd: HWND, dpi: u32, client_width: i32, client_height: i32) {
    // SAFETY: pure Win32 geometry calls on our own window.
    unsafe {
        let mut rect = RECT {
            left: 0,
            top: 0,
            right: client_width,
            bottom: client_height,
        };
        let _ = AdjustWindowRectExForDpi(
            &raw mut rect,
            WS_CAPTION | WS_SYSMENU,
            false,
            WINDOW_EX_STYLE(0),
            dpi,
        );
        let width = rect.right - rect.left;
        let height = rect.bottom - rect.top;
        let x = (GetSystemMetrics(SM_CXSCREEN) - width) / 2;
        let y = (GetSystemMetrics(SM_CYSCREEN) - height) / 2;
        let _ = SetWindowPos(
            hwnd,
            Some(HWND_TOP),
            x.max(0),
            y.max(0),
            width,
            height,
            SWP_NOACTIVATE | SWP_NOZORDER,
        );
    }
}

/// The user's message font (and a bold variant) at the given DPI, falling
/// back to Segoe UI 9 pt.
fn message_fonts(dpi: u32) -> (HFONT, HFONT) {
    let mut metrics = NONCLIENTMETRICSW {
        cbSize: u32::try_from(std::mem::size_of::<NONCLIENTMETRICSW>()).unwrap_or(0),
        ..Default::default()
    };
    // SAFETY: pvparam points at a properly sized NONCLIENTMETRICSW.
    let ok = unsafe {
        SystemParametersInfoForDpi(
            SPI_GETNONCLIENTMETRICS.0,
            metrics.cbSize,
            Some((&raw mut metrics).cast()),
            0,
            dpi,
        )
    }
    .is_ok();

    let mut logfont = if ok {
        metrics.lfMessageFont
    } else {
        let mut fallback = windows::Win32::Graphics::Gdi::LOGFONTW::default();
        let name: Vec<u16> = "Segoe UI".encode_utf16().collect();
        fallback.lfFaceName[..name.len()].copy_from_slice(&name);
        fallback.lfHeight = -(9 * i32::try_from(dpi).unwrap_or(96) / 72);
        fallback
    };
    // SAFETY: CreateFontIndirectW copies the LOGFONTW.
    let font = unsafe { CreateFontIndirectW(&raw const logfont) };
    logfont.lfWeight = i32::try_from(FW_BOLD.0).unwrap_or(700);
    // SAFETY: same as above.
    let bold = unsafe { CreateFontIndirectW(&raw const logfont) };
    (font, bold)
}

extern "system" fn wndproc(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    // SAFETY: GWLP_USERDATA is either null (during creation) or the Box we
    // planted in `show`; it is only freed in WM_NCDESTROY below.
    let state = unsafe {
        let ptr = GetWindowLongPtrW(hwnd, GWLP_USERDATA) as *mut State;
        ptr.as_ref()
    };
    let Some(state) = state else {
        // SAFETY: default handling before the state exists.
        return unsafe { DefWindowProcW(hwnd, msg, wparam, lparam) };
    };

    match msg {
        WM_COMMAND => {
            let id = u16::try_from(wparam.0 & 0xFFFF).unwrap_or(0);
            let code = u16::try_from((wparam.0 >> 16) & 0xFFFF).unwrap_or(0);
            on_command(hwnd, state, id, code);
            LRESULT(0)
        }
        WM_VERIFY_RESULT => {
            let text = match wparam.0 {
                v if v == VerifyOutcome::Valid as usize => "✓ Key is valid.",
                v if v == VerifyOutcome::Invalid as usize => {
                    "✗ Key is invalid or has been revoked."
                }
                _ => "⚠ Couldn't verify the key. Try again.",
            };
            set_text(state.verify_status, text);
            // SAFETY: re-enabling our own button on the UI thread.
            unsafe {
                let _ = EnableWindow(state.verify_btn, true);
            }
            LRESULT(0)
        }
        WM_UPDATE_RESULT => {
            let text = match wparam.0 {
                v if v == UpdateOutcome::UpToDate as usize => "You're up to date.".to_owned(),
                v if v == UpdateOutcome::Available as usize => {
                    let version = state
                        .available_version
                        .lock()
                        .unwrap_or_else(std::sync::PoisonError::into_inner)
                        .clone();
                    format!("Version {version} is available.")
                }
                v if v == UpdateOutcome::DevBuild as usize => {
                    "Development build; updates are disabled.".to_owned()
                }
                _ => "⚠ Couldn't check for updates. Try again.".to_owned(),
            };
            set_text(state.update_status, &text);
            // SAFETY: re-enabling our own button on the UI thread.
            unsafe {
                let _ = EnableWindow(state.check_btn, true);
            }
            LRESULT(0)
        }
        WM_CTLCOLORSTATIC => on_ctl_color_static(state, wparam, lparam),
        WM_CTLCOLOREDIT => {
            let hdc = HDC(wparam.0 as *mut core::ffi::c_void);
            // SAFETY: the DC is live for the duration of this message.
            unsafe {
                SetTextColor(hdc, state.palette.text);
                SetBkColor(hdc, state.palette.edit_background);
            }
            LRESULT(state.edit_brush.0 as isize)
        }
        WM_CTLCOLORBTN => LRESULT(state.background_brush.0 as isize),
        WM_ERASEBKGND => {
            let hdc = HDC(wparam.0 as *mut core::ffi::c_void);
            let mut client = RECT::default();
            // SAFETY: filling our own client rect with our own brush.
            unsafe {
                let _ = GetClientRect(hwnd, &raw mut client);
                FillRect(hdc, &raw const client, state.background_brush);
            }
            LRESULT(1)
        }
        WM_CLOSE => {
            // Save a still-focused edit before closing.
            save_key_if_changed(hwnd, state);
            // SAFETY: destroying our own window.
            unsafe {
                let _ = DestroyWindow(hwnd);
            }
            LRESULT(0)
        }
        WM_DESTROY => {
            // SAFETY: ends this thread's message loop.
            unsafe { PostQuitMessage(0) };
            LRESULT(0)
        }
        WM_NCDESTROY => {
            // SAFETY: reclaims the Box planted in `show` exactly once and
            // frees the GDI objects it owns.
            unsafe {
                let ptr = SetWindowLongPtrW(hwnd, GWLP_USERDATA, 0) as *mut State;
                if !ptr.is_null() {
                    let state = Box::from_raw(ptr);
                    let _ = DeleteObject(state.font.into());
                    let _ = DeleteObject(state.bold_font.into());
                    let _ = DeleteObject(state.background_brush.into());
                    let _ = DeleteObject(state.edit_brush.into());
                    let _ = DeleteObject(state.separator_brush.into());
                }
                DefWindowProcW(hwnd, msg, wparam, lparam)
            }
        }
        // SAFETY: everything else takes the default path.
        _ => unsafe { DefWindowProcW(hwnd, msg, wparam, lparam) },
    }
}

fn on_ctl_color_static(state: &State, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    let hdc = HDC(wparam.0 as *mut core::ffi::c_void);
    let control = HWND(lparam.0 as *mut core::ffi::c_void);
    if state.separators.contains(&control) {
        return LRESULT(state.separator_brush.0 as isize);
    }
    let color = if state.muted_labels.contains(&control) {
        state.palette.muted
    } else {
        state.palette.text
    };
    // SAFETY: the DC is live for the duration of this message.
    unsafe {
        SetTextColor(hdc, color);
        SetBkMode(hdc, TRANSPARENT);
    }
    LRESULT(state.background_brush.0 as isize)
}

fn on_command(hwnd: HWND, state: &State, id: u16, code: u16) {
    match (id, code) {
        (ID_CANCEL, _) => {
            save_key_if_changed(hwnd, state);
            // SAFETY: destroying our own window (Esc via IsDialogMessage).
            unsafe {
                let _ = DestroyWindow(hwnd);
            }
        }
        (ID_KEY_EDIT, EN_KILLFOCUS) => save_key_if_changed(hwnd, state),
        (ID_VERIFY_BTN, BN_CLICKED) => start_verify(hwnd, state),
        (ID_CHECK_BTN, BN_CLICKED) => start_update_check(hwnd, state),
        (ID_LAUNCH_CHK, BN_CLICKED) => {
            let enabled = is_checked(hwnd, ID_LAUNCH_CHK);
            if let Err(err) = launch::set_enabled(enabled) {
                tracing::warn!("launch at login: {err}");
                task_dialog::error("Couldn't update launch at login", &err.to_string());
            }
        }
        (ID_UPDATES_CHK, BN_CLICKED) => {
            state
                .ui
                .app
                .set_automatic_updates_enabled(is_checked(hwnd, ID_UPDATES_CHK));
        }
        _ => {}
    }
}

/// Saves the edit-box key when it is non-empty and actually changed — the
/// same rule the Go dialog applies on `OnEditingFinished`. The CLI call runs
/// on a worker thread.
fn save_key_if_changed(_hwnd: HWND, state: &State) {
    let key = window_text(state.key_edit).trim().to_owned();
    if key.is_empty() || *state.last_saved_key.borrow() == key {
        return;
    }
    state.last_saved_key.replace(key.clone());
    let app = Arc::clone(&state.ui.app);
    thread::spawn(move || {
        if let Err(err) = app.set_api_key(&key) {
            tracing::warn!("set api key: {err}");
        }
    });
}

fn start_verify(hwnd: HWND, state: &State) {
    let key = window_text(state.key_edit).trim().to_owned();
    if key.is_empty() {
        set_text(state.verify_status, "Paste your API key.");
        return;
    }
    set_text(state.verify_status, "Verifying…");
    // SAFETY: disabling our own button on the UI thread.
    unsafe {
        let _ = EnableWindow(state.verify_btn, false);
    }
    let hwnd_value = hwnd.0 as isize;
    thread::spawn(move || {
        let outcome = match Verifier::new(&agent_cli::base_url()).verify(&key) {
            Ok(true) => VerifyOutcome::Valid,
            Ok(false) => VerifyOutcome::Invalid,
            Err(_) => VerifyOutcome::Unavailable,
        };
        post(hwnd_value, WM_VERIFY_RESULT, outcome as usize);
    });
}

fn start_update_check(hwnd: HWND, state: &State) {
    set_text(state.update_status, "Checking…");
    // SAFETY: disabling our own button on the UI thread.
    unsafe {
        let _ = EnableWindow(state.check_btn, false);
    }
    let hwnd_value = hwnd.0 as isize;
    let available_version = Arc::clone(&state.available_version);
    thread::spawn(move || {
        let outcome = match app_update::check(&agent_cli::base_url(), version::VERSION) {
            Ok(None) => UpdateOutcome::UpToDate,
            Ok(Some(update)) => {
                *available_version
                    .lock()
                    .unwrap_or_else(std::sync::PoisonError::into_inner) = update.version;
                UpdateOutcome::Available
            }
            Err(app_update::Error::DevBuild) => UpdateOutcome::DevBuild,
            Err(err) => {
                tracing::debug!("manual update check: {err}");
                UpdateOutcome::Failed
            }
        };
        post(hwnd_value, WM_UPDATE_RESULT, outcome as usize);
    });
}

/// Posts a `WM_APP` result message from a worker thread.
fn post(hwnd_value: isize, msg: u32, code: usize) {
    let hwnd = HWND(hwnd_value as *mut core::ffi::c_void);
    // SAFETY: posting to a window that outlives its workers is safe; if it
    // was already destroyed the call just fails.
    unsafe {
        let _ = PostMessageW(Some(hwnd), msg, WPARAM(code), LPARAM(0));
    }
}

fn is_checked(hwnd: HWND, id: u16) -> bool {
    // SAFETY: querying a child control we created.
    unsafe {
        let control = GetDlgItem(Some(hwnd), i32::from(id)).unwrap_or_default();
        SendMessageW(control, BM_GETCHECK, None, None).0
            == isize::try_from(BST_CHECKED.0).unwrap_or(1)
    }
}

fn window_text(hwnd: HWND) -> String {
    let mut buffer = [0u16; 512];
    // SAFETY: reading text from a control we created into a local buffer.
    let len = unsafe { GetWindowTextW(hwnd, &mut buffer) };
    String::from_utf16_lossy(&buffer[..usize::try_from(len).unwrap_or(0)])
}

fn set_text(hwnd: HWND, text: &str) {
    let text_wide = wide(text);
    // SAFETY: setting text on a control we created.
    unsafe {
        let _ = SetWindowTextW(hwnd, PCWSTR(text_wide.as_ptr()));
    }
}
