/* Background app-update schedule and the install/restart flow. The loop
 * keeps ticking while automatic updates are off (re-enabling works without
 * a restart), exits permanently on a dev build, and announces any given
 * version at most once. */
#include "ui/ui.h"

#include "agent_cli.h"
#include "app.h"
#include "version.h"

#include <stdio.h>

#define FIRST_CHECK_DELAY_MS (5u * 1000u)
#define CHECK_INTERVAL_MS (24u * 60u * 60u * 1000u)

void updater_set_pending(const UpdateInfo *update) {
    AcquireSRWLockExclusive(&g_ui.lock);
    g_ui.pending_update = *update;
    g_ui.has_pending_update = true;
    ReleaseSRWLockExclusive(&g_ui.lock);
}

/* One check; returns false when the loop should stop (dev build). */
static bool check_once(void) {
    wchar_t base[1024];
    agent_base_url(base, 1024);
    UpdateInfo update;
    UpdateCheck result = app_update_check(base, version_utf8(), &update);
    if (result == UPDATE_DEV_BUILD) {
        return false;
    }
    if (result != UPDATE_AVAILABLE) {
        return true;
    }
    AcquireSRWLockExclusive(&g_ui.lock);
    bool already_offered =
        strcmp(g_ui.offered_version, update.version) == 0;
    if (!already_offered) {
        strcpy_s(g_ui.offered_version, sizeof(g_ui.offered_version), update.version);
        g_ui.pending_update = update;
        g_ui.has_pending_update = true;
    }
    ReleaseSRWLockExclusive(&g_ui.lock);
    if (!already_offered) {
        PostMessageW(g_ui.hwnd, WM_UPDATE_AVAILABLE, 0, 0);
    }
    return true;
}

static DWORD WINAPI updater_main(LPVOID param) {
    (void)param;
    Sleep(FIRST_CHECK_DELAY_MS);
    for (;;) {
        if (app_automatic_updates_enabled() && !check_once()) {
            return 0;
        }
        Sleep(CHECK_INTERVAL_MS);
    }
}

void updater_spawn(void) {
    HANDLE thread = CreateThread(NULL, 0, updater_main, NULL, 0, NULL);
    if (thread) {
        CloseHandle(thread);
    }
}

static DWORD WINAPI install_main(LPVOID param) {
    (void)param;
    AcquireSRWLockShared(&g_ui.lock);
    bool has_update = g_ui.has_pending_update;
    UpdateInfo update = g_ui.pending_update;
    ReleaseSRWLockShared(&g_ui.lock);
    if (!has_update) {
        InterlockedExchange(&g_ui.installing, 0);
        return 0;
    }

    wchar_t instruction[128];
    wchar_t content[256];
    _snwprintf_s(instruction, ARRAYSIZE(instruction), _TRUNCATE,
                 L"Version %hs is available", update.version);
    _snwprintf_s(content, ARRAYSIZE(content), _TRUNCATE,
                 L"You have %s. Tokitoki keeps running while the update "
                 L"downloads and installs.",
                 version_wide());
    static const wchar_t *offer_links[] = {L"Install now", L"Not now"};
    if (task_dialog_choice(instruction, content, offer_links, 2) != 0) {
        InterlockedExchange(&g_ui.installing, 0);
        return 0;
    }

    if (!app_update_install(&update)) {
        task_dialog_error(L"Update failed",
                          L"The update could not be downloaded and verified. "
                          L"Tokitoki keeps running on the current version.");
        InterlockedExchange(&g_ui.installing, 0);
        return 0;
    }

    AcquireSRWLockExclusive(&g_ui.lock);
    g_ui.has_pending_update = false;
    ReleaseSRWLockExclusive(&g_ui.lock);

    _snwprintf_s(instruction, ARRAYSIZE(instruction), _TRUNCATE,
                 L"Version %hs is installed", update.version);
    static const wchar_t *restart_links[] = {L"Restart now", L"Later"};
    if (task_dialog_choice(instruction, L"Restarting takes a few seconds…",
                           restart_links, 2) == 0) {
        wchar_t exe[MAX_PATH];
        if (GetModuleFileNameW(NULL, exe, MAX_PATH)) {
            AcquireSRWLockExclusive(&g_ui.lock);
            wcscpy_s(g_ui.restart_target, MAX_PATH, exe);
            g_ui.restart_requested = true;
            ReleaseSRWLockExclusive(&g_ui.lock);
            /* WM_CLOSE -> DefWindowProc destroys -> WM_DESTROY quits the
             * loop; main relaunches after releasing the instance mutex. */
            PostMessageW(g_ui.hwnd, WM_CLOSE, 0, 0);
        }
    }
    InterlockedExchange(&g_ui.installing, 0);
    return 0;
}

void updater_offer_install(void) {
    if (InterlockedCompareExchange(&g_ui.installing, 1, 0) != 0) {
        return;
    }
    HANDLE thread = CreateThread(NULL, 0, install_main, NULL, 0, NULL);
    if (thread) {
        CloseHandle(thread);
    } else {
        InterlockedExchange(&g_ui.installing, 0);
    }
}
