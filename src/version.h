/* Build metadata injected at compile time via /DTOKITOKI_VERSION="x.y.z";
 * local builds default to "dev" (self-update stays disabled). */
#ifndef TOKITOKI_VERSION_H
#define TOKITOKI_VERSION_H

#include <wchar.h>

const char *version_utf8(void);
const wchar_t *version_wide(void);
/* "Version <x>" for the Settings footer. */
const wchar_t *version_summary(void);

#endif
