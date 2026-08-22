/* Provider data-directory discovery (mirrors Go internal/datadirs and the
 * Rust data_dirs module). Every provider is one row in a table; the
 * per-provider quirks (comma-separated env overrides, Claude's trailing
 * `projects` segment, Goose's session-db suffix) are data, not code paths.
 * There is no provider selection — all providers, always. */
#ifndef TOKITOKI_DATA_DIRS_H
#define TOKITOKI_DATA_DIRS_H

#include <stdbool.h>
#include <stddef.h>
#include <wchar.h>

typedef struct ProviderDir {
    const wchar_t *provider; /* static table name, not owned */
    wchar_t *dir;            /* malloc'd */
} ProviderDir;

typedef struct ProviderDirs {
    ProviderDir *items; /* sorted by provider name */
    size_t count;
} ProviderDirs;

typedef struct WatchPaths {
    wchar_t **items; /* malloc'd paths, deduped + sorted */
    size_t count;
} WatchPaths;

/* Environment lookup, injectable for tests. Returns false when unset. */
typedef bool (*EnvLookupFn)(const wchar_t *name, wchar_t *out, size_t cap);

/* For each provider, the first existing candidate path. */
void data_dirs_resolve(ProviderDirs *out);
void data_dirs_resolve_with(EnvLookupFn env, const wchar_t *home, ProviderDirs *out);

/* Union of watchable directories over all candidates: an existing directory
 * watches itself, an existing file watches its parent. */
void data_dirs_watch_paths(WatchPaths *out);
void data_dirs_watch_paths_with(EnvLookupFn env, const wchar_t *home, WatchPaths *out);

void provider_dirs_free(ProviderDirs *dirs);
void watch_paths_free(WatchPaths *paths);

#endif
