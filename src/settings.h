/* Preferences at <data dir>\windows-settings.json (mirrors the Go/Rust
 * schema). Both flags are inverted so an absent file/field means the feature
 * is ON. Unknown fields (legacy `enabled_providers`) still parse.
 *
 * Not thread-safe on its own — app.c serializes access. Writes are atomic:
 * temp file in the same directory, then MoveFileEx(REPLACE_EXISTING). */
#ifndef TOKITOKI_SETTINGS_H
#define TOKITOKI_SETTINGS_H

#include <stdbool.h>
#include <wchar.h>

typedef struct Settings {
    bool tracking_disabled;
    bool automatic_updates_disabled;
} Settings;

/* Missing file yields defaults and succeeds; unreadable/invalid JSON fails
 * (with `out` reset to defaults). */
bool settings_load(const wchar_t *data_dir, Settings *out);
bool settings_save(const wchar_t *data_dir, Settings settings);

#endif
