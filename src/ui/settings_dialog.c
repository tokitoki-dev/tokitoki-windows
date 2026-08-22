/* The Settings window: raw Win32, DPI-scaled, fixed-size, themed light/dark
 * from AppsUseLightTheme.
 *
 * - API Key: edit box with cue banner, saved on focus-out only when
 *   non-empty and changed; Verify Key button with inline status.
 * - Launch at login / Automatic updates rows: bold title, muted description,
 *   right-pinned checkbox applied instantly. Checkboxes have NO caption
 *   (caption pixels would leak next to the glyph); their screen-reader name
 *   is attached via IAccPropServices instead.
 * - Check for Updates button with inline status; a found update feeds the
 *   shared pending slot so the tray menu can install it.
 * - Muted version footer.
 *
 * The dialog runs on its own thread with its own pump; blocking CLI/HTTP
 * work runs on workers that post WM_APP results back. */
#include "ui/ui.h"

#include "agent_cli.h"
#include "app.h"
#include "launch.h"
#include "resource.h"
#include "util/wstr.h"
#include "version.h"

#include <commctrl.h>
#include <initguid.h>
#include <oleacc.h>
#include <uxtheme.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#pragma comment(lib, "uxtheme.lib")
#pragma comment(lib, "ole32.lib")
#pragma comment(lib, "oleacc.lib")

#define WM_VERIFY_RESULT (WM_APP + 10)
#define WM_UPDATE_RESULT (WM_APP + 11)

#define ID_KEY_EDIT 1001
#define ID_VERIFY_BTN 1002
#define ID_LAUNCH_CHK 1003
#define ID_UPDATES_CHK 1004
#define ID_CHECK_BTN 1005

/* Layout in 96-dpi px (mirrors the Rust dialog). */
#define CLIENT_WIDTH 460
#define MARGIN_X 24
#define MARGIN_Y 20
#define SECTION_GAP 18
#define BUTTON_WIDTH 170
#define EDIT_HEIGHT 28
#define BUTTON_HEIGHT 30
#define CHECKBOX_SIZE 18

enum {
    VERIFY_OUTCOME_VALID = 0,
    VERIFY_OUTCOME_INVALID = 1,
    VERIFY_OUTCOME_UNAVAILABLE = 2,
};
enum {
    UPDATE_OUTCOME_UP_TO_DATE = 0,
    UPDATE_OUTCOME_AVAILABLE = 1,
    UPDATE_OUTCOME_DEV = 2,
    UPDATE_OUTCOME_FAILED = 3,
};

typedef struct Palette {
    COLORREF text, muted, background, edit_background, separator;
} Palette;

#define MAX_TRACKED 8

typedef struct DialogState {
    bool dark;
    UINT dpi;
    Palette palette;
    HFONT font, bold_font;
    HBRUSH background_brush, edit_brush, separator_brush;
    HWND key_edit, verify_btn, verify_status, check_btn, update_status;
    HWND muted[MAX_TRACKED];
    int muted_count;
    HWND separators[MAX_TRACKED];
    int separator_count;
    char last_saved_key[512];
} DialogState;

static Palette palette_for(bool dark) {
    Palette p;
    if (dark) {
        p.text = RGB(0xF4, 0xF4, 0xF5);
        p.muted = RGB(0xA1, 0xA1, 0xAA);
        p.background = RGB(0x20, 0x20, 0x20);
        p.edit_background = RGB(0x2D, 0x2D, 0x2D);
        p.separator = RGB(0x3F, 0x3F, 0x46);
    } else {
        p.text = RGB(0x1B, 0x1B, 0x1B);
        p.muted = RGB(0x6B, 0x72, 0x80);
        p.background = RGB(0xF3, 0xF3, 0xF3);
        p.edit_background = RGB(0xFF, 0xFF, 0xFF);
        p.separator = RGB(0xE5, 0xE7, 0xEB);
    }
    return p;
}

static int scaled(const DialogState *s, int value) {
    return value * (int)s->dpi / 96;
}

/* The user's message font (bold variant for titles) at the dialog's DPI. */
static void message_fonts(UINT dpi, HFONT *font, HFONT *bold) {
    NONCLIENTMETRICSW metrics;
    memset(&metrics, 0, sizeof(metrics));
    metrics.cbSize = sizeof(metrics);
    LOGFONTW logfont;
    if (SystemParametersInfoForDpi(SPI_GETNONCLIENTMETRICS, sizeof(metrics),
                                   &metrics, 0, dpi)) {
        logfont = metrics.lfMessageFont;
    } else {
        memset(&logfont, 0, sizeof(logfont));
        wcscpy_s(logfont.lfFaceName, LF_FACESIZE, L"Segoe UI");
        logfont.lfHeight = -(9 * (int)dpi / 72);
    }
    *font = CreateFontIndirectW(&logfont);
    logfont.lfWeight = FW_BOLD;
    *bold = CreateFontIndirectW(&logfont);
}

/* Attaches a UIA/MSAA name to a caption-less control via the accessibility
 * property store: announced by screen readers, never painted. */
static void set_accessible_name(HWND control, const wchar_t *name) {
    IAccPropServices *services = NULL;
    if (FAILED(CoCreateInstance(&CLSID_AccPropServices, NULL, CLSCTX_INPROC_SERVER,
                                &IID_IAccPropServices, (void **)&services))) {
        return;
    }
    services->lpVtbl->SetHwndPropStr(services, control, (DWORD)OBJID_CLIENT,
                                     CHILDID_SELF, PROPID_ACC_NAME, name);
    services->lpVtbl->Release(services);
}

static HWND make_control(HWND parent, DialogState *s, const wchar_t *cls,
                         const wchar_t *text, DWORD style, bool bold, int id,
                         int x, int y, int w, int h) {
    HWND control = CreateWindowExW(
        0, cls, text, WS_CHILD | WS_VISIBLE | style, scaled(s, x), scaled(s, y),
        scaled(s, w), scaled(s, h), parent, (HMENU)(INT_PTR)id,
        GetModuleHandleW(NULL), NULL);
    if (!control) {
        return NULL;
    }
    SendMessageW(control, WM_SETFONT, (WPARAM)(bold ? s->bold_font : s->font), TRUE);
    SetWindowTheme(control, s->dark ? L"DarkMode_Explorer" : L"Explorer", NULL);
    return control;
}

static void track_muted(DialogState *s, HWND control) {
    if (s->muted_count < MAX_TRACKED) {
        s->muted[s->muted_count++] = control;
    }
}

static void toggle_row(HWND hwnd, DialogState *s, int *y, const wchar_t *title,
                       const wchar_t *description, int id, bool checked) {
    make_control(hwnd, s, L"STATIC", title, 0, true, 0, MARGIN_X, *y,
                 CLIENT_WIDTH - 2 * MARGIN_X - CHECKBOX_SIZE - 24, 18);
    HWND desc = make_control(hwnd, s, L"STATIC", description, 0, false, 0,
                             MARGIN_X, *y + 20,
                             CLIENT_WIDTH - 2 * MARGIN_X - CHECKBOX_SIZE - 24, 16);
    track_muted(s, desc);
    HWND checkbox = make_control(hwnd, s, L"BUTTON", L"",
                                 WS_TABSTOP | BS_AUTOCHECKBOX, false, id,
                                 CLIENT_WIDTH - MARGIN_X - CHECKBOX_SIZE,
                                 *y + (36 - CHECKBOX_SIZE) / 2, CHECKBOX_SIZE,
                                 CHECKBOX_SIZE);
    SendMessageW(checkbox, BM_SETCHECK, checked ? BST_CHECKED : BST_UNCHECKED, 0);
    set_accessible_name(checkbox, title);
    *y += 36 + 16;
}

static void build_contents(HWND hwnd, DialogState *s, const char *initial_key) {
    int y = MARGIN_Y;
    int content_width = CLIENT_WIDTH - 2 * MARGIN_X;

    make_control(hwnd, s, L"STATIC", L"API Key", 0, true, 0, MARGIN_X, y,
                 content_width, 18);
    y += 18 + 6;

    wchar_t *wide_key = wstr_from_utf8(initial_key, (size_t)-1);
    s->key_edit = make_control(hwnd, s, L"EDIT", wide_key ? wide_key : L"",
                               WS_TABSTOP | WS_BORDER | ES_AUTOHSCROLL, false,
                               ID_KEY_EDIT, MARGIN_X, y, content_width,
                               EDIT_HEIGHT);
    free(wide_key);
    SendMessageW(s->key_edit, EM_SETCUEBANNER, TRUE,
                 (LPARAM)L"Paste your API key");
    LPARAM margin = MAKELPARAM(scaled(s, 6), scaled(s, 6));
    SendMessageW(s->key_edit, EM_SETMARGINS, EC_LEFTMARGIN | EC_RIGHTMARGIN, margin);
    y += EDIT_HEIGHT + 10;

    s->verify_btn = make_control(hwnd, s, L"BUTTON", L"Verify Key", WS_TABSTOP,
                                 false, ID_VERIFY_BTN, MARGIN_X, y, BUTTON_WIDTH,
                                 BUTTON_HEIGHT);
    s->verify_status = make_control(hwnd, s, L"STATIC", L"", 0, false, 0,
                                    MARGIN_X + BUTTON_WIDTH + 10,
                                    y + (BUTTON_HEIGHT - 16) / 2,
                                    content_width - BUTTON_WIDTH - 10, 16);
    track_muted(s, s->verify_status);
    y += BUTTON_HEIGHT + SECTION_GAP;

    HWND sep = make_control(hwnd, s, L"STATIC", L"", 0, false, 0, MARGIN_X, y,
                            content_width, 1);
    s->separators[s->separator_count++] = sep;
    y += 1 + SECTION_GAP;

    toggle_row(hwnd, s, &y, L"Launch at login",
               L"Start Tokitoki automatically when you sign in.", ID_LAUNCH_CHK,
               launch_is_enabled());
    toggle_row(hwnd, s, &y, L"Automatic updates",
               L"Download and offer updates when they are ready.",
               ID_UPDATES_CHK, app_automatic_updates_enabled());

    s->check_btn = make_control(hwnd, s, L"BUTTON", L"Check for Updates",
                                WS_TABSTOP, false, ID_CHECK_BTN, MARGIN_X, y,
                                BUTTON_WIDTH, BUTTON_HEIGHT);
    s->update_status = make_control(hwnd, s, L"STATIC", L"", 0, false, 0,
                                    MARGIN_X + BUTTON_WIDTH + 10,
                                    y + (BUTTON_HEIGHT - 16) / 2,
                                    content_width - BUTTON_WIDTH - 10, 16);
    track_muted(s, s->update_status);
    y += BUTTON_HEIGHT + SECTION_GAP;

    sep = make_control(hwnd, s, L"STATIC", L"", 0, false, 0, MARGIN_X, y,
                       content_width, 1);
    s->separators[s->separator_count++] = sep;
    y += 1 + SECTION_GAP;
    HWND footer = make_control(hwnd, s, L"STATIC", version_summary(), 0, false, 0,
                               MARGIN_X, y, content_width, 16);
    track_muted(s, footer);
    y += 16 + MARGIN_Y;

    /* Size the window around the content and center it on screen. */
    RECT rect = {0, 0, scaled(s, CLIENT_WIDTH), scaled(s, y)};
    AdjustWindowRectExForDpi(&rect, WS_CAPTION | WS_SYSMENU, FALSE, 0, s->dpi);
    int width = rect.right - rect.left;
    int height = rect.bottom - rect.top;
    int screen_x = (GetSystemMetrics(SM_CXSCREEN) - width) / 2;
    int screen_y = (GetSystemMetrics(SM_CYSCREEN) - height) / 2;
    SetWindowPos(hwnd, HWND_TOP, screen_x > 0 ? screen_x : 0,
                 screen_y > 0 ? screen_y : 0, width, height,
                 SWP_NOACTIVATE | SWP_NOZORDER);
}

static void set_text(HWND control, const wchar_t *text) {
    SetWindowTextW(control, text);
}

static void window_text_utf8(HWND control, char *out, size_t cap) {
    wchar_t wide[512];
    GetWindowTextW(control, wide, ARRAYSIZE(wide));
    char *utf8 = utf8_from_wstr(wide);
    if (utf8) {
        strncpy_s(out, cap, utf8, _TRUNCATE);
        free(utf8);
    } else {
        out[0] = '\0';
    }
    utf8_trim(out);
}

/* Saves the key only when non-empty and actually changed. */
static void save_key_if_changed(DialogState *s) {
    char key[512];
    window_text_utf8(s->key_edit, key, sizeof(key));
    if (key[0] == '\0' || strcmp(key, s->last_saved_key) == 0) {
        return;
    }
    strcpy_s(s->last_saved_key, sizeof(s->last_saved_key), key);
    /* Blocking CLI call, but this thread owns only this dialog and the save
     * happens on focus-out/close — an acceptable pause, and ordering stays
     * strictly sequential (the Rust port's racy fire-and-forget saves were
     * a review finding). */
    app_set_api_key(key);
}

typedef struct VerifyCtx {
    HWND hwnd;
    char key[512];
} VerifyCtx;

static DWORD WINAPI verify_main(LPVOID param) {
    VerifyCtx *ctx = param;
    WPARAM outcome;
    switch (agent_verify_key(ctx->key)) {
    case VERIFY_VALID:
        outcome = VERIFY_OUTCOME_VALID;
        break;
    case VERIFY_INVALID:
        outcome = VERIFY_OUTCOME_INVALID;
        break;
    default:
        outcome = VERIFY_OUTCOME_UNAVAILABLE;
        break;
    }
    PostMessageW(ctx->hwnd, WM_VERIFY_RESULT, outcome, 0);
    free(ctx);
    return 0;
}

static void start_verify(HWND hwnd, DialogState *s) {
    VerifyCtx *ctx = calloc(1, sizeof(VerifyCtx));
    if (!ctx) {
        return;
    }
    window_text_utf8(s->key_edit, ctx->key, sizeof(ctx->key));
    if (ctx->key[0] == '\0') {
        set_text(s->verify_status, L"Paste your API key.");
        free(ctx);
        return;
    }
    ctx->hwnd = hwnd;
    set_text(s->verify_status, L"Verifying…");
    EnableWindow(s->verify_btn, FALSE);
    HANDLE thread = CreateThread(NULL, 0, verify_main, ctx, 0, NULL);
    if (thread) {
        CloseHandle(thread);
    } else {
        free(ctx);
        EnableWindow(s->verify_btn, TRUE);
    }
}

typedef struct CheckCtx {
    HWND hwnd;
    /* Deliberately NO DialogState pointer: the dialog can be destroyed while
     * this worker is in flight (up to 15 s); results travel only through the
     * always-alive g_ui pending slot and a posted message. */
} CheckCtx;

static DWORD WINAPI check_main(LPVOID param) {
    CheckCtx *ctx = param;
    wchar_t base[1024];
    agent_base_url(base, 1024);
    UpdateInfo update;
    WPARAM outcome;
    switch (app_update_check(base, version_utf8(), &update)) {
    case UPDATE_AVAILABLE:
        /* Feed the shared slot so the tray menu gains "Install update…" —
         * without this, a manual check with auto-updates off announces an
         * update that has no install path. */
        updater_set_pending(&update);
        outcome = UPDATE_OUTCOME_AVAILABLE;
        break;
    case UPDATE_UP_TO_DATE:
        outcome = UPDATE_OUTCOME_UP_TO_DATE;
        break;
    case UPDATE_DEV_BUILD:
        outcome = UPDATE_OUTCOME_DEV;
        break;
    default:
        outcome = UPDATE_OUTCOME_FAILED;
        break;
    }
    PostMessageW(ctx->hwnd, WM_UPDATE_RESULT, outcome, 0);
    free(ctx);
    return 0;
}

static void start_update_check(HWND hwnd, DialogState *s) {
    CheckCtx *ctx = calloc(1, sizeof(CheckCtx));
    if (!ctx) {
        return;
    }
    ctx->hwnd = hwnd;
    set_text(s->update_status, L"Checking…");
    EnableWindow(s->check_btn, FALSE);
    HANDLE thread = CreateThread(NULL, 0, check_main, ctx, 0, NULL);
    if (thread) {
        CloseHandle(thread);
    } else {
        free(ctx);
        EnableWindow(s->check_btn, TRUE);
    }
}

static void on_command(HWND hwnd, DialogState *s, int id, int code) {
    switch (id) {
    case IDCANCEL: /* Esc via IsDialogMessage */
        save_key_if_changed(s);
        DestroyWindow(hwnd);
        break;
    case ID_KEY_EDIT:
        if (code == EN_KILLFOCUS) {
            save_key_if_changed(s);
        }
        break;
    case ID_VERIFY_BTN:
        if (code == BN_CLICKED) {
            start_verify(hwnd, s);
        }
        break;
    case ID_CHECK_BTN:
        if (code == BN_CLICKED) {
            start_update_check(hwnd, s);
        }
        break;
    case ID_LAUNCH_CHK:
        if (code == BN_CLICKED) {
            bool checked = SendMessageW((HWND)GetDlgItem(hwnd, ID_LAUNCH_CHK),
                                        BM_GETCHECK, 0, 0) == BST_CHECKED;
            if (!launch_set_enabled(checked)) {
                task_dialog_error(L"Couldn't update launch at login",
                                  L"The registry entry could not be changed.");
            }
        }
        break;
    case ID_UPDATES_CHK:
        if (code == BN_CLICKED) {
            bool checked = SendMessageW((HWND)GetDlgItem(hwnd, ID_UPDATES_CHK),
                                        BM_GETCHECK, 0, 0) == BST_CHECKED;
            app_set_automatic_updates_enabled(checked);
        }
        break;
    default:
        break;
    }
}

static LRESULT CALLBACK dialog_proc(HWND hwnd, UINT msg, WPARAM wparam,
                                    LPARAM lparam) {
    DialogState *s = (DialogState *)GetWindowLongPtrW(hwnd, GWLP_USERDATA);
    if (!s) {
        return DefWindowProcW(hwnd, msg, wparam, lparam);
    }
    switch (msg) {
    case WM_COMMAND:
        on_command(hwnd, s, LOWORD(wparam), HIWORD(wparam));
        return 0;
    case WM_VERIFY_RESULT: {
        const wchar_t *text =
            wparam == VERIFY_OUTCOME_VALID     ? L"\x2713 Key is valid."
            : wparam == VERIFY_OUTCOME_INVALID ? L"\x2717 Key is invalid or has been revoked."
                                               : L"\x26A0 Couldn't verify the key. Try again.";
        set_text(s->verify_status, text);
        EnableWindow(s->verify_btn, TRUE);
        return 0;
    }
    case WM_UPDATE_RESULT: {
        wchar_t text[128];
        if (wparam == UPDATE_OUTCOME_AVAILABLE) {
            AcquireSRWLockShared(&g_ui.lock);
            _snwprintf_s(text, ARRAYSIZE(text), _TRUNCATE,
                         L"Version %hs is available.",
                         g_ui.pending_update.version);
            ReleaseSRWLockShared(&g_ui.lock);
        } else if (wparam == UPDATE_OUTCOME_UP_TO_DATE) {
            wcscpy_s(text, ARRAYSIZE(text), L"You're up to date.");
        } else if (wparam == UPDATE_OUTCOME_DEV) {
            wcscpy_s(text, ARRAYSIZE(text),
                     L"Development build; updates are disabled.");
        } else {
            wcscpy_s(text, ARRAYSIZE(text),
                     L"\x26A0 Couldn't check for updates. Try again.");
        }
        set_text(s->update_status, text);
        EnableWindow(s->check_btn, TRUE);
        return 0;
    }
    case WM_CTLCOLORSTATIC: {
        HDC hdc = (HDC)wparam;
        HWND control = (HWND)lparam;
        for (int i = 0; i < s->separator_count; i++) {
            if (s->separators[i] == control) {
                return (LRESULT)s->separator_brush;
            }
        }
        COLORREF color = s->palette.text;
        for (int i = 0; i < s->muted_count; i++) {
            if (s->muted[i] == control) {
                color = s->palette.muted;
            }
        }
        SetTextColor(hdc, color);
        SetBkMode(hdc, TRANSPARENT);
        return (LRESULT)s->background_brush;
    }
    case WM_CTLCOLOREDIT: {
        HDC hdc = (HDC)wparam;
        SetTextColor(hdc, s->palette.text);
        SetBkColor(hdc, s->palette.edit_background);
        return (LRESULT)s->edit_brush;
    }
    case WM_CTLCOLORBTN:
        return (LRESULT)s->background_brush;
    case WM_ERASEBKGND: {
        RECT client;
        GetClientRect(hwnd, &client);
        FillRect((HDC)wparam, &client, s->background_brush);
        return 1;
    }
    case WM_CLOSE:
        save_key_if_changed(s);
        DestroyWindow(hwnd);
        return 0;
    case WM_DESTROY:
        PostQuitMessage(0);
        return 0;
    case WM_NCDESTROY:
        SetWindowLongPtrW(hwnd, GWLP_USERDATA, 0);
        DeleteObject(s->font);
        DeleteObject(s->bold_font);
        DeleteObject(s->background_brush);
        DeleteObject(s->edit_brush);
        DeleteObject(s->separator_brush);
        free(s);
        return DefWindowProcW(hwnd, msg, wparam, lparam);
    default:
        return DefWindowProcW(hwnd, msg, wparam, lparam);
    }
}

void settings_dialog_show(void) {
    /* Blocking CLI read before any window exists — this thread owns no UI
     * yet, so the pause cannot freeze anything. */
    char *initial_key = NULL;
    app_get_api_key(&initial_key, NULL);

    /* For IAccPropServices; tolerant of already-initialized. */
    CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);

    HINSTANCE instance = GetModuleHandleW(NULL);
    WNDCLASSW cls;
    memset(&cls, 0, sizeof(cls));
    cls.lpfnWndProc = dialog_proc;
    cls.hInstance = instance;
    cls.lpszClassName = L"TokitokiSettingsWindow";
    /* Without a class cursor, WM_SETCURSOR keeps whatever was active — the
     * app-starting spinner from the blocking `get key` call above. */
    cls.hCursor = LoadCursorW(NULL, IDC_ARROW);
    RegisterClassW(&cls); /* re-registration on later opens fails: fine */

    HWND hwnd = CreateWindowExW(0, L"TokitokiSettingsWindow", L"Settings",
                                /* WS_CLIPCHILDREN: background erase must not
                                 * repaint over children (flicker). */
                                WS_CAPTION | WS_SYSMENU | WS_CLIPCHILDREN, 0, 0,
                                10, 10, NULL, NULL, instance, NULL);
    if (!hwnd) {
        free(initial_key);
        return;
    }

    DialogState *s = calloc(1, sizeof(DialogState));
    if (!s) {
        DestroyWindow(hwnd);
        free(initial_key);
        return;
    }
    s->dark = !theme_apps_light();
    s->dpi = GetDpiForWindow(hwnd);
    if (s->dpi < 96) {
        s->dpi = 96;
    }
    s->palette = palette_for(s->dark);
    message_fonts(s->dpi, &s->font, &s->bold_font);
    s->background_brush = CreateSolidBrush(s->palette.background);
    s->edit_brush = CreateSolidBrush(s->palette.edit_background);
    s->separator_brush = CreateSolidBrush(s->palette.separator);
    if (initial_key) {
        /* Truncate: a CLI that prints an over-long key (or a stray line)
         * must not abort the process via strcpy_s bounds-check. */
        strncpy_s(s->last_saved_key, sizeof(s->last_saved_key), initial_key,
                  _TRUNCATE);
    }

    /* Caption bar tinted to the body background: one surface, not
     * "system chrome + gray box". */
    theme_apply_window_chrome(hwnd, s->dark, s->palette.background);
    HICON icon = LoadIconW(instance, MAKEINTRESOURCEW(IDI_APP));
    if (icon) {
        SendMessageW(hwnd, WM_SETICON, ICON_BIG, (LPARAM)icon);
    }

    build_contents(hwnd, s, initial_key ? initial_key : "");
    free(initial_key);
    SetWindowLongPtrW(hwnd, GWLP_USERDATA, (LONG_PTR)s);

    ShowWindow(hwnd, SW_SHOW);
    /* The one text input is why people open this dialog. */
    SetFocus(s->key_edit);

    MSG msg;
    while (GetMessageW(&msg, NULL, 0, 0) > 0) {
        if (IsDialogMessageW(hwnd, &msg)) {
            continue;
        }
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
}
