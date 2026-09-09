/* Launch-at-login via HKCU\Software\Microsoft\Windows\CurrentVersion\Run,
 * value "Tokitoki". The stored value is the exe path wrapped in literal
 * double quotes only — backslashes must survive verbatim. */
#ifndef TOKITOKI_LAUNCH_H
#define TOKITOKI_LAUNCH_H

#include <stdbool.h>
#include <stddef.h>
#include <wchar.h>

bool launch_is_enabled(void);
bool launch_set_enabled(bool enabled);
/* Repoints a stale entry at the current exe; missing entry is a no-op. */
bool launch_reconcile(void);

/* Exposed for tests. */
void launch_quote_path(const wchar_t *path, wchar_t *out, size_t cap);
const wchar_t *launch_dequote(wchar_t *value);

#endif
