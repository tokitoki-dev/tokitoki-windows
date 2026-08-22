#include "launch.h"

#include <windows.h>

#include <string.h>

#define RUN_KEY L"Software\\Microsoft\\Windows\\CurrentVersion\\Run"
#define VALUE_NAME L"Tokitoki"

void launch_quote_path(const wchar_t *path, wchar_t *out, size_t cap) {
    _snwprintf_s(out, cap, _TRUNCATE, L"\"%s\"", path);
}

const wchar_t *launch_dequote(wchar_t *value) {
    size_t len = wcslen(value);
    if (len >= 2 && value[0] == L'"' && value[len - 1] == L'"') {
        value[len - 1] = L'\0';
        return value + 1;
    }
    return value;
}

static bool read_value(wchar_t *out, DWORD cap_bytes) {
    DWORD size = cap_bytes;
    LSTATUS status = RegGetValueW(HKEY_CURRENT_USER, RUN_KEY, VALUE_NAME,
                                  RRF_RT_REG_SZ, NULL, out, &size);
    return status == ERROR_SUCCESS;
}

bool launch_is_enabled(void) {
    wchar_t value[MAX_PATH * 2];
    return read_value(value, sizeof(value));
}

bool launch_set_enabled(bool enabled) {
    if (!enabled) {
        LSTATUS status = RegDeleteKeyValueW(HKEY_CURRENT_USER, RUN_KEY, VALUE_NAME);
        return status == ERROR_SUCCESS || status == ERROR_FILE_NOT_FOUND;
    }
    wchar_t exe[MAX_PATH];
    if (!GetModuleFileNameW(NULL, exe, MAX_PATH)) {
        return false;
    }
    wchar_t quoted[MAX_PATH + 2];
    launch_quote_path(exe, quoted, MAX_PATH + 2);
    DWORD bytes = (DWORD)((wcslen(quoted) + 1) * sizeof(wchar_t));
    return RegSetKeyValueW(HKEY_CURRENT_USER, RUN_KEY, VALUE_NAME, REG_SZ,
                           quoted, bytes) == ERROR_SUCCESS;
}

bool launch_reconcile(void) {
    wchar_t value[MAX_PATH * 2];
    if (!read_value(value, sizeof(value))) {
        return true; /* not enabled: nothing to fix */
    }
    wchar_t exe[MAX_PATH];
    if (!GetModuleFileNameW(NULL, exe, MAX_PATH)) {
        return false;
    }
    if (_wcsicmp(launch_dequote(value), exe) == 0) {
        return true;
    }
    return launch_set_enabled(true);
}
