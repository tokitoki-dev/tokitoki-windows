/* Theme detection and dark-menu enabling.
 *
 * Theme state lives under HKCU\...\Themes\Personalize: SystemUsesLightTheme
 * (taskbar, default dark) and AppsUseLightTheme (windows, default light).
 * Dark context menus need the undocumented uxtheme ordinals 135
 * (SetPreferredAppMode) and 136 (FlushMenuThemes), Win10 1903+. The ordinal
 * addresses are resolved once and cached — re-resolving on every
 * WM_SETTINGCHANGE was a measurable waste the Rust review flagged. */
#include "ui/ui.h"

#include <dwmapi.h>

#pragma comment(lib, "dwmapi.lib")

#define PERSONALIZE_KEY L"Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize"

static bool read_personalize(const wchar_t *value, DWORD *out) {
    DWORD size = sizeof(*out);
    return RegGetValueW(HKEY_CURRENT_USER, PERSONALIZE_KEY, value,
                        RRF_RT_REG_DWORD, NULL, out, &size) == ERROR_SUCCESS;
}

bool theme_taskbar_light(void) {
    DWORD value = 0;
    return read_personalize(L"SystemUsesLightTheme", &value) && value != 0;
}

bool theme_apps_light(void) {
    DWORD value = 1;
    if (!read_personalize(L"AppsUseLightTheme", &value)) {
        return true;
    }
    return value != 0;
}

typedef int(WINAPI *SetPreferredAppModeFn)(int);
typedef void(WINAPI *FlushMenuThemesFn)(void);

static SetPreferredAppModeFn g_set_mode;
static FlushMenuThemesFn g_flush;
static bool g_resolved;

static void resolve_ordinals(void) {
    if (g_resolved) {
        return;
    }
    g_resolved = true;
    HMODULE uxtheme = LoadLibraryExW(L"uxtheme.dll", NULL,
                                     LOAD_LIBRARY_SEARCH_SYSTEM32);
    if (!uxtheme) {
        return;
    }
    g_set_mode = (SetPreferredAppModeFn)(void *)GetProcAddress(uxtheme,
                                                              MAKEINTRESOURCEA(135));
    g_flush = (FlushMenuThemesFn)(void *)GetProcAddress(uxtheme,
                                                        MAKEINTRESOURCEA(136));
}

void theme_enable_dark_menus(void) {
    resolve_ordinals();
    if (g_set_mode) {
        g_set_mode(1); /* AllowDark: follow the system preference */
    }
    theme_flush_menus();
}

void theme_flush_menus(void) {
    resolve_ordinals();
    if (g_flush) {
        g_flush();
    }
}

void theme_apply_window_chrome(HWND hwnd, bool dark, COLORREF caption) {
    /* All three fail soft on builds that predate the attribute. */
    BOOL dark_flag = dark ? TRUE : FALSE;
    DwmSetWindowAttribute(hwnd, DWMWA_USE_IMMERSIVE_DARK_MODE, &dark_flag,
                          sizeof(dark_flag));
    DWORD backdrop = 2; /* DWMSBT_MAINWINDOW (Mica) */
    DwmSetWindowAttribute(hwnd, DWMWA_SYSTEMBACKDROP_TYPE, &backdrop,
                          sizeof(backdrop));
    DwmSetWindowAttribute(hwnd, DWMWA_CAPTION_COLOR, &caption, sizeof(caption));
}
