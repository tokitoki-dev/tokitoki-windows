/* Tray icon registration, theme-aware glyph, balloon notifications.
 * The glyph is rendered at runtime at 16 * dpi / 96 px: white on the default
 * dark taskbar, dark on a light one.
 *
 * NIM_SETVERSION(NOTIFYICON_VERSION) after every NIM_ADD is load-bearing:
 * without it the shell uses version-0 semantics and never delivers
 * NIN_BALLOONUSERCLICK, so update balloons would be unclickable. */
#include "ui/ui.h"

#include "logo.h"

#include <shellapi.h>

#include <stdlib.h>
#include <string.h>

#pragma comment(lib, "shell32.lib")

#define TRAY_ID 1

static HICON g_current_icon;

static HICON glyph_icon(HWND hwnd, bool light_taskbar) {
    UINT dpi = GetDpiForWindow(hwnd);
    if (dpi < 96) {
        dpi = 96;
    }
    UINT size = 16 * dpi / 96;
    uint8_t *rgba = malloc((size_t)size * size * 4);
    if (!rgba) {
        return NULL;
    }
    logo_mark(size, !light_taskbar, rgba);

    /* RGBA (straight alpha) -> BGRA for the 32bpp color bitmap. */
    for (size_t i = 0; i < (size_t)size * size * 4; i += 4) {
        uint8_t r = rgba[i];
        rgba[i] = rgba[i + 2];
        rgba[i + 2] = r;
    }
    HBITMAP color = CreateBitmap((int)size, (int)size, 1, 32, rgba);
    HBITMAP mask = CreateBitmap((int)size, (int)size, 1, 1, NULL);
    free(rgba);
    if (!color || !mask) {
        if (color) {
            DeleteObject(color);
        }
        if (mask) {
            DeleteObject(mask);
        }
        return NULL;
    }
    ICONINFO info = {TRUE, 0, 0, mask, color};
    HICON icon = CreateIconIndirect(&info);
    DeleteObject(color);
    DeleteObject(mask);
    return icon;
}

static void base_data(HWND hwnd, NOTIFYICONDATAW *data) {
    memset(data, 0, sizeof(*data));
    data->cbSize = sizeof(*data);
    data->hWnd = hwnd;
    data->uID = TRAY_ID;
}

static void swap_current(HICON icon) {
    if (g_current_icon) {
        DestroyIcon(g_current_icon);
    }
    g_current_icon = icon;
}

bool tray_add(HWND hwnd, bool light_taskbar) {
    HICON icon = glyph_icon(hwnd, light_taskbar);
    if (!icon) {
        return false;
    }
    NOTIFYICONDATAW data;
    base_data(hwnd, &data);
    data.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
    data.uCallbackMessage = WM_TRAY_CALLBACK;
    data.hIcon = icon;
    wcscpy_s(data.szTip, ARRAYSIZE(data.szTip), L"Tokitoki");
    if (!Shell_NotifyIconW(NIM_ADD, &data)) {
        DestroyIcon(icon);
        return false;
    }
    swap_current(icon);

    data.uFlags = 0;
    data.uVersion = NOTIFYICON_VERSION;
    Shell_NotifyIconW(NIM_SETVERSION, &data);
    return true;
}

void tray_refresh_icon(HWND hwnd, bool light_taskbar) {
    HICON icon = glyph_icon(hwnd, light_taskbar);
    if (!icon) {
        return;
    }
    NOTIFYICONDATAW data;
    base_data(hwnd, &data);
    data.uFlags = NIF_ICON;
    data.hIcon = icon;
    if (Shell_NotifyIconW(NIM_MODIFY, &data)) {
        swap_current(icon);
    } else {
        DestroyIcon(icon);
    }
}

void tray_balloon(HWND hwnd, const wchar_t *title, const wchar_t *text, bool warning) {
    NOTIFYICONDATAW data;
    base_data(hwnd, &data);
    data.uFlags = NIF_INFO;
    data.dwInfoFlags = warning ? NIIF_WARNING : NIIF_INFO;
    /* Truncate, never abort: caller text (localized CLI stderr) can exceed
     * these fixed fields, and wcscpy_s would trip the CRT invalid-parameter
     * handler and terminate the process. */
    wcsncpy_s(data.szInfoTitle, ARRAYSIZE(data.szInfoTitle), title, _TRUNCATE);
    wcsncpy_s(data.szInfo, ARRAYSIZE(data.szInfo), text, _TRUNCATE);
    Shell_NotifyIconW(NIM_MODIFY, &data);
}

void tray_remove(HWND hwnd) {
    NOTIFYICONDATAW data;
    base_data(hwnd, &data);
    Shell_NotifyIconW(NIM_DELETE, &data);
    swap_current(NULL);
}
