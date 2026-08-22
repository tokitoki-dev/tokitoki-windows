/* Hidden window, message loop, tray menu dispatch. */
#include "ui/ui.h"

#include "agent_cli.h"
#include "app.h"
#include "util/wstr.h"

#include <shellapi.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

UiState g_ui;

#define CMD_TRACKING 100
#define CMD_DASHBOARD 101
#define CMD_SETTINGS 102
#define CMD_INSTALL_UPDATE 103
#define CMD_QUIT 104

static DWORD WINAPI settings_thread(LPVOID param) {
    (void)param;
    settings_dialog_show();
    InterlockedExchange(&g_ui.settings_open, 0);
    return 0;
}

void ui_open_settings(void) {
    if (InterlockedCompareExchange(&g_ui.settings_open, 1, 0) != 0) {
        return;
    }
    HANDLE thread = CreateThread(NULL, 0, settings_thread, NULL, 0, NULL);
    if (thread) {
        CloseHandle(thread);
    } else {
        InterlockedExchange(&g_ui.settings_open, 0);
    }
}

static DWORD WINAPI dashboard_thread(LPVOID param) {
    (void)param;
    wchar_t *target = app_dashboard_target();
    if (target) {
        ShellExecuteW(NULL, L"open", target, NULL, NULL, SW_SHOWNORMAL);
        free(target);
    }
    return 0;
}

static DWORD WINAPI startup_check_thread(LPVOID param) {
    (void)param;
    char detail[AGENT_DETAIL_CHARS * 4];
    char *key = NULL;
    AgentErr err = app_get_api_key(&key, detail);
    free(key);
    if (err == AGENT_OK) {
        return 0;
    }
    if (err == AGENT_ERR_MISSING_KEY) {
        PostMessageW(g_ui.hwnd, WM_SETUP_REQUIRED, 0, 0);
        return 0;
    }
    utf8_truncate_chars(detail, 160);
    wchar_t *wide = wstr_from_utf8(detail, (size_t)-1);
    AcquireSRWLockExclusive(&g_ui.lock);
    g_ui.startup_warning[0] = L'\0';
    if (wide) {
        wcscpy_s(g_ui.startup_warning, ARRAYSIZE(g_ui.startup_warning), wide);
    }
    ReleaseSRWLockExclusive(&g_ui.lock);
    free(wide);
    PostMessageW(g_ui.hwnd, WM_SETUP_WARN, 0, 0);
    return 0;
}

static void show_menu(HWND hwnd) {
    AcquireSRWLockShared(&g_ui.lock);
    bool has_update = g_ui.has_pending_update;
    char pending_version[64];
    strcpy_s(pending_version, sizeof(pending_version), g_ui.pending_update.version);
    ReleaseSRWLockShared(&g_ui.lock);

    HMENU menu = CreatePopupMenu();
    if (!menu) {
        return;
    }
    UINT tracking_flags = MF_STRING | (app_tracking_enabled() ? MF_CHECKED : 0u);
    AppendMenuW(menu, tracking_flags, CMD_TRACKING, L"Tracking enabled");
    AppendMenuW(menu, MF_STRING, CMD_DASHBOARD, L"Dashboard");
    AppendMenuW(menu, MF_STRING, CMD_SETTINGS, L"Settings…");
    if (has_update) {
        wchar_t label[96];
        _snwprintf_s(label, ARRAYSIZE(label), _TRUNCATE, L"Install update %hs…",
                     pending_version);
        AppendMenuW(menu, MF_STRING, CMD_INSTALL_UPDATE, label);
    }
    AppendMenuW(menu, MF_SEPARATOR, 0, NULL);
    AppendMenuW(menu, MF_STRING, CMD_QUIT, L"Quit Tokitoki");

    /* Required so the menu closes when the user clicks elsewhere. */
    SetForegroundWindow(hwnd);
    POINT point;
    GetCursorPos(&point);
    int cmd = TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_RETURNCMD | TPM_NONOTIFY,
                             point.x, point.y, 0, hwnd, NULL);
    DestroyMenu(menu);

    switch (cmd) {
    case CMD_TRACKING:
        app_set_tracking_enabled(!app_tracking_enabled());
        break;
    case CMD_DASHBOARD: {
        HANDLE thread = CreateThread(NULL, 0, dashboard_thread, NULL, 0, NULL);
        if (thread) {
            CloseHandle(thread);
        }
        break;
    }
    case CMD_SETTINGS:
        ui_open_settings();
        break;
    case CMD_INSTALL_UPDATE:
        updater_offer_install();
        break;
    case CMD_QUIT:
        PostQuitMessage(0);
        break;
    default:
        break;
    }
}

static LRESULT CALLBACK wndproc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam) {
    if (msg == WM_TRAY_CALLBACK) {
        switch ((UINT)(lparam & 0xFFFF)) {
        case WM_LBUTTONUP:
            ui_open_settings();
            break;
        case WM_RBUTTONUP:
            show_menu(hwnd);
            break;
        case NIN_BALLOON_USER_CLICK:
            updater_offer_install();
            break;
        default:
            break;
        }
        return 0;
    }
    if (g_ui.taskbar_created_msg != 0 && msg == g_ui.taskbar_created_msg) {
        /* Explorer restarted: the new taskbar has no record of our icon. */
        g_ui.taskbar_light = theme_taskbar_light();
        tray_add(hwnd, g_ui.taskbar_light);
        return 0;
    }
    switch (msg) {
    case WM_UPDATE_AVAILABLE: {
        AcquireSRWLockShared(&g_ui.lock);
        wchar_t text[128];
        _snwprintf_s(text, ARRAYSIZE(text), _TRUNCATE,
                     L"Version %hs is ready to install.",
                     g_ui.pending_update.version);
        ReleaseSRWLockShared(&g_ui.lock);
        tray_balloon(hwnd, L"Tokitoki update available", text, false);
        return 0;
    }
    case WM_SETUP_REQUIRED:
        tray_balloon(hwnd, L"Tokitoki setup required",
                     L"Paste your API key to start syncing.", false);
        ui_open_settings();
        return 0;
    case WM_SETUP_WARN: {
        wchar_t detail[512];
        AcquireSRWLockShared(&g_ui.lock);
        wcscpy_s(detail, ARRAYSIZE(detail), g_ui.startup_warning);
        ReleaseSRWLockShared(&g_ui.lock);
        tray_balloon(hwnd, L"Tokitoki setup check failed", detail, true);
        return 0;
    }
    case WM_SETTINGCHANGE: {
        theme_flush_menus();
        bool light = theme_taskbar_light();
        if (light != g_ui.taskbar_light) {
            g_ui.taskbar_light = light;
            tray_refresh_icon(hwnd, light);
        }
        return 0;
    }
    case WM_DESTROY:
        PostQuitMessage(0);
        return 0;
    default:
        return DefWindowProcW(hwnd, msg, wparam, lparam);
    }
}

bool ui_run(void) {
    InitializeSRWLock(&g_ui.lock);
    theme_enable_dark_menus();
    g_ui.taskbar_created_msg = RegisterWindowMessageW(L"TaskbarCreated");
    g_ui.taskbar_light = theme_taskbar_light();

    HINSTANCE instance = GetModuleHandleW(NULL);
    WNDCLASSW cls;
    memset(&cls, 0, sizeof(cls));
    cls.lpfnWndProc = wndproc;
    cls.hInstance = instance;
    cls.lpszClassName = L"TokitokiTrayWindow";
    if (!RegisterClassW(&cls)) {
        return false;
    }
    HWND hwnd = CreateWindowExW(0, L"TokitokiTrayWindow", L"Tokitoki",
                                WS_OVERLAPPED, 0, 0, 0, 0, NULL, NULL, instance,
                                NULL);
    if (!hwnd) {
        return false;
    }
    g_ui.hwnd = hwnd;

    if (!tray_add(hwnd, g_ui.taskbar_light)) {
        DestroyWindow(hwnd);
        return false;
    }

    HANDLE startup = CreateThread(NULL, 0, startup_check_thread, NULL, 0, NULL);
    if (startup) {
        CloseHandle(startup);
    }
    updater_spawn();

    MSG msg;
    while (GetMessageW(&msg, NULL, 0, 0) > 0) {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
    tray_remove(hwnd);
    return true;
}

bool ui_pending_restart(wchar_t *out, size_t cap) {
    AcquireSRWLockShared(&g_ui.lock);
    bool requested = g_ui.restart_requested;
    if (requested) {
        wcscpy_s(out, cap, g_ui.restart_target);
    }
    ReleaseSRWLockShared(&g_ui.lock);
    return requested;
}
