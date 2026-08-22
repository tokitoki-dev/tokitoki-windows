/* Entry point (mirrors the Go/Rust lifecycle): cleanup leftovers ->
 * single-instance guard -> autostart reconcile -> app start -> tray UI
 * loop -> optional update relaunch (after releasing the instance lock, or
 * the child sees a live instance and quits). */
#include "app.h"
#include "app_update.h"
#include "instance.h"
#include "launch.h"
#include "ui/ui.h"

#include <windows.h>

#include <commctrl.h>

int WINAPI wWinMain(HINSTANCE instance, HINSTANCE prev, PWSTR cmdline, int show) {
    (void)instance;
    (void)prev;
    (void)cmdline;
    (void)show;

    app_update_cleanup_leftovers();

    void *lock = NULL;
    switch (instance_acquire(&lock)) {
    case INSTANCE_ALREADY_RUNNING:
        return 0; /* silent, like the Go original */
    case INSTANCE_ERROR:
        return 1;
    case INSTANCE_ACQUIRED:
        break;
    }

    launch_reconcile(); /* a moved exe must not break autostart */

    INITCOMMONCONTROLSEX controls = {sizeof(controls), ICC_STANDARD_CLASSES};
    InitCommonControlsEx(&controls);

    if (!app_init()) {
        instance_release(lock);
        return 1;
    }
    app_start();

    bool ui_ok = ui_run();
    app_stop();

    wchar_t restart[MAX_PATH];
    if (ui_pending_restart(restart, MAX_PATH)) {
        instance_release(lock); /* before the relaunch, or the child quits */
        app_update_relaunch(restart);
    } else {
        instance_release(lock);
    }
    return ui_ok ? 0 : 1;
}
