/* Recursive debounced filesystem watcher (mirrors Go internal/watcher).
 * ReadDirectoryChangesW is natively recursive on Windows, so new session
 * subdirectories are picked up without re-walking. Any burst of change
 * events collapses into one callback after the quiet window.
 *
 * The per-root buffer is 8 KiB on purpose: the OS default sized against
 * thousands of watched subtrees would cost real memory (the Go port learned
 * this the hard way). */
#ifndef TOKITOKI_WATCHER_H
#define TOKITOKI_WATCHER_H

#include "data_dirs.h"

#include <stdbool.h>

typedef void (*WatchCallback)(void *ctx);

typedef struct Watcher Watcher; /* opaque */

Watcher *watcher_create(unsigned debounce_ms, WatchCallback on_change, void *ctx);
/* (Re)starts watching `paths` recursively; an empty list just stops. */
bool watcher_start(Watcher *w, const WatchPaths *paths);
void watcher_stop(Watcher *w);
void watcher_destroy(Watcher *w);

#endif
