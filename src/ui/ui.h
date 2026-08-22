/* The Windows front end: hidden window, tray icon, context menu, dialogs,
 * theme handling, update announcements. One UI thread owns the message
 * loop; workers post WM_APP-range messages back.
 *
 * Ports the Rust UI including its post-review fixes, plus three repairs the
 * Rust review surfaced: TaskbarCreated re-adds the tray icon after an
 * Explorer restart, NIM_SETVERSION makes balloon clicks deliverable, and a
 * manual update check feeds the same pending-update slot the menu installs
 * from. */
#ifndef TOKITOKI_UI_H
#define TOKITOKI_UI_H

#include "app_update.h"

#include <windows.h>

#include <stdbool.h>

#define WM_TRAY_CALLBACK (WM_APP + 1)
#define WM_UPDATE_AVAILABLE (WM_APP + 2)
#define WM_SETUP_REQUIRED (WM_APP + 3)
#define WM_SETUP_WARN (WM_APP + 4)

#define NIN_BALLOON_USER_CLICK 0x0405

typedef struct UiState {
    HWND hwnd;
    UINT taskbar_created_msg; /* RegisterWindowMessage("TaskbarCreated") */
    bool taskbar_light;

    SRWLOCK lock; /* guards the fields below */
    UpdateInfo pending_update;
    bool has_pending_update;
    char offered_version[64];
    wchar_t startup_warning[512];
    wchar_t restart_target[MAX_PATH];
    bool restart_requested;

    volatile LONG installing;    /* interlocked re-entry guards */
    volatile LONG settings_open;
} UiState;

extern UiState g_ui;

/* Runs the tray UI until quit. Returns false when setup failed. */
bool ui_run(void);
/* After ui_run: the exe to relaunch when an update restart was accepted. */
bool ui_pending_restart(wchar_t *out, size_t cap);

/* tray.c */
bool tray_add(HWND hwnd, bool light_taskbar);
void tray_refresh_icon(HWND hwnd, bool light_taskbar);
void tray_balloon(HWND hwnd, const wchar_t *title, const wchar_t *text, bool warning);
void tray_remove(HWND hwnd);

/* theme.c */
bool theme_taskbar_light(void);
bool theme_apps_light(void);
void theme_enable_dark_menus(void);
void theme_flush_menus(void);
void theme_apply_window_chrome(HWND hwnd, bool dark, COLORREF caption);

/* task_dialog.c — window title is always "Tokitoki". */
void task_dialog_error(const wchar_t *instruction, const wchar_t *content);
/* Command-link chooser: 0-based index or -1 on cancel. */
int task_dialog_choice(const wchar_t *instruction, const wchar_t *content,
                       const wchar_t *const *links, int link_count);

/* settings_dialog.c — blocks its calling thread with its own pump. */
void settings_dialog_show(void);
void ui_open_settings(void); /* guarded thread spawn */

/* updater.c */
void updater_spawn(void);
void updater_offer_install(void);
/* Stores an update in the shared pending slot (announce elsewhere). */
void updater_set_pending(const UpdateInfo *update);

#endif
