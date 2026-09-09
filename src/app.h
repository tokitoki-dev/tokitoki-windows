/* Platform-neutral service coordinator (mirrors Go internal/app). Owns the
 * settings state, syncer, watcher, and the daily CLI self-update loop; the
 * UI layer talks only to this. All functions are thread-safe. */
#ifndef TOKITOKI_APP_H
#define TOKITOKI_APP_H

#include "agent_cli.h"

#include <stdbool.h>
#include <wchar.h>

#define APP_SYNC_INTERVAL_MS (30u * 60u * 1000u)
#define APP_WATCH_DEBOUNCE_MS (2u * 1000u)

bool app_init(void);
/* Loads settings, starts monitoring, seeds the shared CLI (synchronously —
 * the Settings dialog reads the key right after), triggers the first sync,
 * and starts the daily `tokitoki update` loop. */
void app_start(void);
void app_stop(void);

bool app_tracking_enabled(void);
void app_set_tracking_enabled(bool enabled);
bool app_automatic_updates_enabled(void);
void app_set_automatic_updates_enabled(bool enabled);

void app_sync_now(void);
void app_restart_monitoring(void);

/* Minted dashboard login URL, falling back to the plain base URL. Malloc'd;
 * caller frees. Blocking — never call on the UI thread. */
wchar_t *app_dashboard_target(void);

/* Key access via the shared CLI (blocking). */
AgentErr app_get_api_key(char **out_key, char detail[AGENT_DETAIL_CHARS * 4]);
AgentErr app_set_api_key(const char *key);

#endif
