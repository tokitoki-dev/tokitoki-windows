#include "version.h"

#ifndef TOKITOKI_VERSION
#define TOKITOKI_VERSION "dev"
#endif

#define WIDEN2(x) L##x
#define WIDEN(x) WIDEN2(x)

const char *version_utf8(void) {
    return TOKITOKI_VERSION;
}

const wchar_t *version_wide(void) {
    return WIDEN(TOKITOKI_VERSION);
}

const wchar_t *version_summary(void) {
    return L"Version " WIDEN(TOKITOKI_VERSION);
}
