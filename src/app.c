#include "app.h"

#include "data_dirs.h"
#include "settings.h"
#include "syncer.h"
#include "watcher.h"

#include <windows.h>

#include <stdlib.h>

#define CLI_UPDATE_INTERVAL_MS (24u * 60u * 60u * 1000u)

static struct {
    SRWLOCK lock; /* guards prefs + settings file writes */
    bool tracking;
    bool auto_updates;
    wchar_t data_dir[MAX_PATH];

    Syncer syncer;
    Watcher *watcher;
    HANDLE cli_update_thread;
    HANDLE cli_update_stop;
} g_app;

static void sync_run(void *ctx) {
    (void)ctx;
    AcquireSRWLockShared(&g_app.lock);
    bool tracking = g_app.tracking;
    ReleaseSRWLockShared(&g_app.lock);
    if (!tracking) {
        return;
    }
    ProviderDirs dirs;
    data_dirs_resolve(&dirs);
    if (dirs.count > 0) {
        agent_sync(&dirs);
    }
    provider_dirs_free(&dirs);
}

static void watch_changed(void *ctx) {
    (void)ctx;
    syncer_trigger(&g_app.syncer);
}

static DWORD WINAPI cli_update_main(LPVOID param) {
    (void)param;
    for (;;) {
        agent_update(); /* failures are routine (offline, dev CLI) */
        if (WaitForSingleObject(g_app.cli_update_stop, CLI_UPDATE_INTERVAL_MS) !=
            WAIT_TIMEOUT) {
            return 0;
        }
    }
}

bool app_init(void) {
    InitializeSRWLock(&g_app.lock);
    g_app.tracking = true;
    g_app.auto_updates = true;
    if (!agent_data_dir(g_app.data_dir, MAX_PATH)) {
        g_app.data_dir[0] = L'\0';
    }
    g_app.watcher = watcher_create(APP_WATCH_DEBOUNCE_MS, watch_changed, NULL);
    return g_app.watcher && syncer_start(&g_app.syncer, sync_run, NULL);
}

void app_start(void) {
    Settings settings;
    if (settings_load(g_app.data_dir, &settings)) {
        AcquireSRWLockExclusive(&g_app.lock);
        g_app.tracking = !settings.tracking_disabled;
        g_app.auto_updates = !settings.automatic_updates_disabled;
        ReleaseSRWLockExclusive(&g_app.lock);
    }

    syncer_start_ticker(&g_app.syncer, APP_SYNC_INTERVAL_MS);
    app_restart_monitoring();

    /* Synchronous: the startup key probe runs right after this returns. */
    agent_bootstrap();
    syncer_trigger(&g_app.syncer);

    g_app.cli_update_stop = CreateEventW(NULL, TRUE, FALSE, NULL);
    if (g_app.cli_update_stop) {
        g_app.cli_update_thread =
            CreateThread(NULL, 0, cli_update_main, NULL, 0, NULL);
    }
}

void app_stop(void) {
    if (g_app.cli_update_thread) {
        SetEvent(g_app.cli_update_stop);
        /* Join fully: the thread may be inside a 5-minute `tokitoki update`
         * and still uses cli_update_stop afterward, so closing the handle on
         * a timeout would leave it waiting on a freed/recycled handle. */
        WaitForSingleObject(g_app.cli_update_thread, INFINITE);
        CloseHandle(g_app.cli_update_thread);
        CloseHandle(g_app.cli_update_stop);
        g_app.cli_update_thread = NULL;
        g_app.cli_update_stop = NULL;
    }
    if (g_app.watcher) {
        watcher_stop(g_app.watcher);
    }
    /* Stop the syncer last: worker threads (e.g. the Settings dialog's
     * save_key path) can still call syncer_trigger until everything joins. */
    syncer_stop(&g_app.syncer);
}

bool app_tracking_enabled(void) {
    AcquireSRWLockShared(&g_app.lock);
    bool enabled = g_app.tracking;
    ReleaseSRWLockShared(&g_app.lock);
    return enabled;
}

/* Persists the in-memory flags, which are authoritative: re-loading the file
 * first would let a transient read failure (AV lock, corruption) feed
 * defaults into the save and clobber the flag not being toggled. Caller
 * holds the exclusive lock, so flag update and file write are one atomic
 * step and the file never transiently disagrees with memory. */
static void save_prefs_locked(void) {
    Settings settings;
    settings.tracking_disabled = !g_app.tracking;
    settings.automatic_updates_disabled = !g_app.auto_updates;
    settings_save(g_app.data_dir, settings);
}

void app_set_tracking_enabled(bool enabled) {
    AcquireSRWLockExclusive(&g_app.lock);
    g_app.tracking = enabled;
    save_prefs_locked();
    ReleaseSRWLockExclusive(&g_app.lock);
    if (enabled) {
        app_restart_monitoring();
        syncer_trigger(&g_app.syncer);
    } else {
        watcher_stop(g_app.watcher);
    }
}

bool app_automatic_updates_enabled(void) {
    AcquireSRWLockShared(&g_app.lock);
    bool enabled = g_app.auto_updates;
    ReleaseSRWLockShared(&g_app.lock);
    return enabled;
}

void app_set_automatic_updates_enabled(bool enabled) {
    AcquireSRWLockExclusive(&g_app.lock);
    g_app.auto_updates = enabled;
    save_prefs_locked();
    ReleaseSRWLockExclusive(&g_app.lock);
}

void app_sync_now(void) {
    syncer_trigger(&g_app.syncer);
}

void app_restart_monitoring(void) {
    /* With tracking off there is nothing to watch (the guard the Rust
     * port's review found missing). */
    if (!app_tracking_enabled()) {
        watcher_stop(g_app.watcher);
        return;
    }
    WatchPaths paths;
    data_dirs_watch_paths(&paths);
    watcher_start(g_app.watcher, &paths);
    watch_paths_free(&paths);
}

wchar_t *app_dashboard_target(void) {
    wchar_t *url = NULL;
    if (agent_get_dashboard_url(&url) == AGENT_OK) {
        return url;
    }
    wchar_t base[1024];
    agent_base_url(base, 1024);
    size_t cap = wcslen(base) + 1;
    wchar_t *fallback = malloc(cap * sizeof(wchar_t));
    if (fallback) {
        wcscpy_s(fallback, cap, base);
    }
    return fallback;
}

AgentErr app_get_api_key(char **out_key, char *detail) {
    return agent_get_api_key(out_key, detail);
}

AgentErr app_set_api_key(const char *key) {
    AgentErr err = agent_set_api_key(key);
    if (err == AGENT_OK) {
        app_sync_now();
    }
    return err;
}
