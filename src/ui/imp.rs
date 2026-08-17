//! UI implementation: hidden window, message loop, tray menu dispatch.

use std::{
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, AtomicIsize, Ordering},
        Arc, Mutex, OnceLock, PoisonError,
    },
    thread,
};

use windows::{
    core::{w, PCWSTR},
    Win32::{
        Foundation::{HWND, LPARAM, LRESULT, POINT, WPARAM},
        System::LibraryLoader::GetModuleHandleW,
        UI::{
            Shell::ShellExecuteW,
            WindowsAndMessaging::{
                AppendMenuW, CreatePopupMenu, CreateWindowExW, DefWindowProcW, DestroyMenu,
                DispatchMessageW, GetCursorPos, GetMessageW, PostQuitMessage, RegisterClassW,
                RegisterWindowMessageW, SetForegroundWindow, TrackPopupMenu, TranslateMessage,
                MF_CHECKED, MF_SEPARATOR,
                MF_STRING, MSG, SW_SHOWNORMAL, TPM_NONOTIFY, TPM_RETURNCMD, TPM_RIGHTBUTTON,
                WINDOW_EX_STYLE, WM_APP, WM_DESTROY, WM_LBUTTONUP, WM_RBUTTONUP, WM_SETTINGCHANGE,
                WNDCLASSW, WS_OVERLAPPED,
            },
        },
    },
};

use super::{settings_dialog, theme, tray, updater, Error};
use crate::{app::App, app_update::Update};

/// Tray callback message.
pub(super) const WM_TRAY_CALLBACK: u32 = WM_APP + 1;
/// Posted by the updater thread when a new version is ready to announce.
pub(super) const WM_UPDATE_AVAILABLE: u32 = WM_APP + 2;
/// Posted by the startup check when no API key is configured.
pub(super) const WM_SETUP_REQUIRED: u32 = WM_APP + 3;
/// Posted by the startup check when the key probe failed outright.
pub(super) const WM_SETUP_WARN: u32 = WM_APP + 4;

/// Balloon-clicked notification (`NIN_BALLOONUSERCLICK`).
const NIN_BALLOON_USER_CLICK: u32 = 0x0405;

const CMD_TRACKING: usize = 100;
const CMD_DASHBOARD: usize = 101;
const CMD_SETTINGS: usize = 102;
const CMD_INSTALL_UPDATE: usize = 103;
const CMD_QUIT: usize = 104;

/// Shared UI state reachable from the window procedure and worker threads.
pub(super) struct Ui {
    pub(super) app: Arc<App>,
    hwnd: AtomicIsize,
    pub(super) pending_update: Mutex<Option<Update>>,
    pub(super) installing: AtomicBool,
    settings_open: AtomicBool,
    taskbar_light: AtomicBool,
    startup_warning: Mutex<String>,
    restart_target: Mutex<Option<PathBuf>>,
}

static STATE: OnceLock<Ui> = OnceLock::new();

/// The shell broadcasts `TaskbarCreated` when Explorer (re)starts; the new
/// taskbar has no record of previously added icons, so we must `NIM_ADD`
/// again or the app keeps running headless with no way to reach the menu.
static TASKBAR_CREATED: OnceLock<u32> = OnceLock::new();

fn taskbar_created_message() -> u32 {
    // SAFETY: registering a well-known broadcast message name; the same name
    // always maps to the same id within a session.
    *TASKBAR_CREATED.get_or_init(|| unsafe { RegisterWindowMessageW(w!("TaskbarCreated")) })
}

impl Ui {
    pub(super) fn hwnd(&self) -> HWND {
        HWND(self.hwnd.load(Ordering::Acquire) as *mut core::ffi::c_void)
    }

    /// Stores the restart target; `main` relaunches after the loop exits.
    pub(super) fn request_restart(&self, target: PathBuf) {
        *lock(&self.restart_target) = Some(target);
    }
}

pub(super) fn state() -> Option<&'static Ui> {
    STATE.get()
}

pub fn pending_restart() -> Option<PathBuf> {
    state().and_then(|ui| lock(&ui.restart_target).take())
}

pub fn run(app: Arc<App>) -> Result<(), Error> {
    theme::enable_dark_menus();
    // Register the broadcast id before the first message can arrive.
    let _ = taskbar_created_message();

    let ui = STATE.get_or_init(|| Ui {
        app,
        hwnd: AtomicIsize::new(0),
        pending_update: Mutex::new(None),
        installing: AtomicBool::new(false),
        settings_open: AtomicBool::new(false),
        taskbar_light: AtomicBool::new(theme::taskbar_uses_light_theme()),
        startup_warning: Mutex::new(String::new()),
        restart_target: Mutex::new(None),
    });

    let hwnd = create_hidden_window()?;
    ui.hwnd.store(hwnd.0 as isize, Ordering::Release);

    tray::add(hwnd, ui.taskbar_light.load(Ordering::Relaxed))
        .map_err(|err| Error(format!("tray icon: {err}")))?;

    spawn_startup_check(ui);
    updater::spawn(ui);

    // The message loop: blocks until Quit posts WM_QUIT.
    let mut msg = MSG::default();
    // SAFETY: plain message pump on the thread that owns the window.
    unsafe {
        while GetMessageW(&raw mut msg, None, 0, 0).as_bool() {
            let _ = TranslateMessage(&raw const msg);
            DispatchMessageW(&raw const msg);
        }
    }

    tray::remove(hwnd);
    Ok(())
}

fn create_hidden_window() -> Result<HWND, Error> {
    // SAFETY: standard window-class registration + creation; the class name
    // outlives the process and the wndproc is a static fn.
    unsafe {
        let instance = GetModuleHandleW(None).map_err(|err| Error(err.to_string()))?;
        let class = WNDCLASSW {
            lpfnWndProc: Some(wndproc),
            hInstance: instance.into(),
            lpszClassName: w!("TokitokiTrayWindow"),
            ..Default::default()
        };
        if RegisterClassW(&raw const class) == 0 {
            return Err(Error("RegisterClassW failed".to_owned()));
        }
        CreateWindowExW(
            WINDOW_EX_STYLE(0),
            w!("TokitokiTrayWindow"),
            w!("Tokitoki"),
            WS_OVERLAPPED,
            0,
            0,
            0,
            0,
            None,
            None,
            Some(instance.into()),
            None,
        )
        .map_err(|err| Error(format!("CreateWindowExW: {err}")))
    }
}

extern "system" fn wndproc(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    let Some(ui) = state() else {
        // SAFETY: default handling for messages arriving before init.
        return unsafe { DefWindowProcW(hwnd, msg, wparam, lparam) };
    };
    // Explorer restarted: re-register the tray icon with the new taskbar.
    // (Guard against a failed registration returning 0 == WM_NULL.)
    if msg != 0 && msg == taskbar_created_message() {
        let light = theme::taskbar_uses_light_theme();
        ui.taskbar_light.store(light, Ordering::Relaxed);
        if let Err(err) = tray::add(hwnd, light) {
            tracing::warn!("re-add tray icon after Explorer restart: {err}");
        }
        return LRESULT(0);
    }
    match msg {
        WM_TRAY_CALLBACK => {
            // The tray callback packs the mouse message into the low word.
            match u16::try_from(lparam.0 & 0xFFFF).map_or(0, u32::from) {
                WM_LBUTTONUP => open_settings(ui),
                WM_RBUTTONUP => show_menu(ui, hwnd),
                NIN_BALLOON_USER_CLICK => updater::offer_install(ui),
                _ => {}
            }
            LRESULT(0)
        }
        WM_UPDATE_AVAILABLE => {
            let version = lock(&ui.pending_update)
                .as_ref()
                .map(|update| update.version.clone());
            if let Some(version) = version {
                tray::balloon(
                    hwnd,
                    "Tokitoki update available",
                    &format!("Version {version} is ready to install."),
                    false,
                );
            }
            LRESULT(0)
        }
        WM_SETUP_REQUIRED => {
            tray::balloon(
                hwnd,
                "Tokitoki setup required",
                "Paste your API key to start syncing.",
                false,
            );
            open_settings(ui);
            LRESULT(0)
        }
        WM_SETUP_WARN => {
            let detail = lock(&ui.startup_warning).clone();
            tray::balloon(hwnd, "Tokitoki setup check failed", &detail, true);
            LRESULT(0)
        }
        WM_SETTINGCHANGE => {
            theme::flush_menu_themes();
            let light = theme::taskbar_uses_light_theme();
            if ui.taskbar_light.swap(light, Ordering::Relaxed) != light {
                tray::refresh_icon(hwnd, light);
            }
            LRESULT(0)
        }
        WM_DESTROY => {
            // SAFETY: ends the message loop for this thread.
            unsafe { PostQuitMessage(0) };
            LRESULT(0)
        }
        // SAFETY: everything else takes the default path.
        _ => unsafe { DefWindowProcW(hwnd, msg, wparam, lparam) },
    }
}

fn show_menu(ui: &'static Ui, hwnd: HWND) {
    let pending_version = lock(&ui.pending_update)
        .as_ref()
        .map(|update| update.version.clone());

    // SAFETY: menu handles are created and destroyed within this function on
    // the UI thread.
    unsafe {
        let Ok(menu) = CreatePopupMenu() else {
            return;
        };
        let tracking_flags = if ui.app.tracking_enabled() {
            MF_STRING | MF_CHECKED
        } else {
            MF_STRING
        };
        let _ = AppendMenuW(menu, tracking_flags, CMD_TRACKING, w!("Tracking enabled"));
        let _ = AppendMenuW(menu, MF_STRING, CMD_DASHBOARD, w!("Dashboard"));
        let _ = AppendMenuW(menu, MF_STRING, CMD_SETTINGS, w!("Settings…"));
        if let Some(version) = &pending_version {
            let label = wide(&format!("Install update {version}…"));
            let _ = AppendMenuW(menu, MF_STRING, CMD_INSTALL_UPDATE, PCWSTR(label.as_ptr()));
        }
        let _ = AppendMenuW(menu, MF_SEPARATOR, 0, PCWSTR::null());
        let _ = AppendMenuW(menu, MF_STRING, CMD_QUIT, w!("Quit Tokitoki"));

        // Required so the menu closes when the user clicks elsewhere.
        let _ = SetForegroundWindow(hwnd);
        let mut point = POINT::default();
        let _ = GetCursorPos(&raw mut point);
        let cmd = TrackPopupMenu(
            menu,
            TPM_RIGHTBUTTON | TPM_RETURNCMD | TPM_NONOTIFY,
            point.x,
            point.y,
            None,
            hwnd,
            None,
        );
        let _ = DestroyMenu(menu);
        dispatch(ui, usize::try_from(cmd.0).unwrap_or(0));
    }
}

fn dispatch(ui: &'static Ui, cmd: usize) {
    match cmd {
        CMD_TRACKING => ui.app.set_tracking_enabled(!ui.app.tracking_enabled()),
        CMD_DASHBOARD => open_dashboard(ui),
        CMD_SETTINGS => open_settings(ui),
        CMD_INSTALL_UPDATE => updater::offer_install(ui),
        CMD_QUIT => {
            // SAFETY: quits the message loop; cleanup runs after it returns.
            unsafe { PostQuitMessage(0) };
        }
        _ => {}
    }
}

/// Resolves the dashboard login URL off the UI thread, then opens it.
fn open_dashboard(ui: &'static Ui) {
    thread::spawn(move || {
        let target = ui.app.dashboard_target();
        let target_wide = wide(&target);
        // SAFETY: ShellExecuteW with owned wide strings kept alive across
        // the call.
        unsafe {
            ShellExecuteW(
                None,
                w!("open"),
                PCWSTR(target_wide.as_ptr()),
                None,
                None,
                SW_SHOWNORMAL,
            );
        }
    });
}

/// Opens the Settings dialog on its own thread (`TaskDialog` pumps its own
/// loop); a guard prevents stacking.
fn open_settings(ui: &'static Ui) {
    if ui.settings_open.swap(true, Ordering::SeqCst) {
        return;
    }
    thread::spawn(move || {
        settings_dialog::show(ui);
        ui.settings_open.store(false, Ordering::SeqCst);
    });
}

/// Startup key probe: missing key → setup balloon + Settings; other errors →
/// warning balloon with truncated detail.
fn spawn_startup_check(ui: &'static Ui) {
    thread::spawn(move || {
        use windows::Win32::UI::WindowsAndMessaging::PostMessageW;
        let message = match ui.app.api_key() {
            Ok(_) => return,
            Err(crate::agent_cli::Error::MissingApiKey) => WM_SETUP_REQUIRED,
            Err(err) => {
                let mut detail = err.to_string();
                detail.truncate(160);
                *lock(&ui.startup_warning) = detail;
                WM_SETUP_WARN
            }
        };
        // SAFETY: posting to our own window from a worker thread.
        unsafe {
            let _ = PostMessageW(Some(ui.hwnd()), message, WPARAM(0), LPARAM(0));
        }
    });
}

pub(super) fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

/// NUL-terminated UTF-16 for PCWSTR arguments.
pub(super) fn wide(text: &str) -> Vec<u16> {
    text.encode_utf16().chain(std::iter::once(0)).collect()
}
