#include "settings.h"

#include "util/json.h"

#include <windows.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define FILE_NAME L"windows-settings.json"

static void settings_path(const wchar_t *data_dir, wchar_t *out, size_t cap) {
    _snwprintf_s(out, cap, _TRUNCATE, L"%s\\%s", data_dir, FILE_NAME);
}

bool settings_load(const wchar_t *data_dir, Settings *out) {
    out->tracking_disabled = false;
    out->automatic_updates_disabled = false;

    wchar_t path[MAX_PATH];
    settings_path(data_dir, path, MAX_PATH);

    HANDLE file = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL,
                              OPEN_EXISTING, 0, NULL);
    if (file == INVALID_HANDLE_VALUE) {
        /* Missing file is the defaults, not an error. */
        return GetLastError() == ERROR_FILE_NOT_FOUND ||
               GetLastError() == ERROR_PATH_NOT_FOUND;
    }
    LARGE_INTEGER size;
    if (!GetFileSizeEx(file, &size) || size.QuadPart > 1 << 20) {
        CloseHandle(file);
        return false;
    }
    char *body = malloc((size_t)size.QuadPart + 1);
    DWORD read = 0;
    bool ok = body && ReadFile(file, body, (DWORD)size.QuadPart, &read, NULL) &&
              read == (DWORD)size.QuadPart;
    CloseHandle(file);
    if (!ok) {
        free(body);
        return false;
    }
    body[read] = '\0';

    /* A JSON object is required; each known flag defaults to false. */
    JsonSlice probe;
    bool is_object = json_obj_get(body, "tracking_disabled", &probe) ||
                     json_obj_get(body, "automatic_updates_disabled", &probe) ||
                     strchr(body, '{') != NULL;
    if (is_object) {
        out->tracking_disabled = json_bool_is_true(body, "tracking_disabled");
        out->automatic_updates_disabled =
            json_bool_is_true(body, "automatic_updates_disabled");
    }
    free(body);
    return is_object;
}

bool settings_save(const wchar_t *data_dir, Settings settings) {
    CreateDirectoryW(data_dir, NULL); /* best effort; open failure reports */

    /* Serialize exactly like the Rust build (pretty, trailing newline) so
     * the two implementations remain byte-compatible. */
    char body[256];
    if (!settings.tracking_disabled && !settings.automatic_updates_disabled) {
        strcpy_s(body, sizeof(body), "{}\n");
    } else if (settings.tracking_disabled && settings.automatic_updates_disabled) {
        strcpy_s(body, sizeof(body),
                 "{\n  \"tracking_disabled\": true,\n"
                 "  \"automatic_updates_disabled\": true\n}\n");
    } else if (settings.tracking_disabled) {
        strcpy_s(body, sizeof(body), "{\n  \"tracking_disabled\": true\n}\n");
    } else {
        strcpy_s(body, sizeof(body),
                 "{\n  \"automatic_updates_disabled\": true\n}\n");
    }

    wchar_t tmp[MAX_PATH];
    _snwprintf_s(tmp, MAX_PATH, _TRUNCATE, L"%s\\.windows-settings.tmp.%lu",
                 data_dir, GetCurrentProcessId());
    HANDLE file = CreateFileW(tmp, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS,
                              FILE_ATTRIBUTE_NORMAL, NULL);
    if (file == INVALID_HANDLE_VALUE) {
        return false;
    }
    DWORD written = 0;
    DWORD len = (DWORD)strlen(body);
    bool ok = WriteFile(file, body, len, &written, NULL) && written == len;
    CloseHandle(file);
    if (!ok) {
        DeleteFileW(tmp);
        return false;
    }

    wchar_t path[MAX_PATH];
    settings_path(data_dir, path, MAX_PATH);
    if (!MoveFileExW(tmp, path, MOVEFILE_REPLACE_EXISTING)) {
        DeleteFileW(tmp);
        return false;
    }
    return true;
}
