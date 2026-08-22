#include "app_update.h"

#include "agent_cli.h" /* parse_version3 */
#include "util/http.h"
#include "util/json.h"
#include "util/sha256.h"
#include "util/wstr.h"

#include <windows.h>

#include <winhttp.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#pragma comment(lib, "winhttp.lib")

#define CHECK_TIMEOUT_MS (15u * 1000u)
#define DOWNLOAD_TIMEOUT_MS (10u * 60u * 1000u)
#define USER_AGENT L"tokitoki-windows"
#define OLD_SUFFIX L".old"

bool update_semverish(const char *version) {
    unsigned parts[3];
    /* Cut build/pre-release decoration before the strict x.y.z parse. */
    char head[64];
    strncpy_s(head, sizeof(head), version, _TRUNCATE);
    char *dash = strpbrk(head, "-+");
    if (dash) {
        *dash = '\0';
    }
    return parse_version3(head, parts);
}

void update_normalize_digest(const char *digest, char out[80]) {
    while (*digest == ' ' || *digest == '\t') {
        digest++;
    }
    if (_strnicmp(digest, "sha256:", 7) == 0) {
        digest += 7;
    }
    size_t i = 0;
    for (; digest[i] && i < 79; i++) {
        char ch = digest[i];
        out[i] = (ch >= 'A' && ch <= 'Z') ? (char)(ch - 'A' + 'a') : ch;
    }
    out[i] = '\0';
}

bool update_trusted_transport(const wchar_t *url) {
    /* Parse the URL properly rather than prefix-matching: a string like
     * "https://tokitoki.dev@evil.example/x" begins with "https://" yet
     * connects to evil.example, and "http://127.evil.example/" begins with
     * "127." yet is not loopback. Decide on the real cracked host. */
    URL_COMPONENTS parts;
    memset(&parts, 0, sizeof(parts));
    parts.dwStructSize = sizeof(parts);
    wchar_t host[256];
    parts.lpszHostName = host;
    parts.dwHostNameLength = ARRAYSIZE(host);
    if (!WinHttpCrackUrl(url, 0, 0, &parts)) {
        return false;
    }
    if (parts.nScheme == INTERNET_SCHEME_HTTPS) {
        return true;
    }
    if (parts.nScheme != INTERNET_SCHEME_HTTP) {
        return false;
    }
    /* Cleartext downloads only ever leave loopback. */
    if (_wcsicmp(host, L"localhost") == 0 || _wcsicmp(host, L"::1") == 0) {
        return true;
    }
    /* 127.0.0.0/8 */
    if (wcsncmp(host, L"127.", 4) == 0) {
        return true;
    }
    return false;
}

static const char *arch_token(void) {
#if defined(_M_ARM64)
    return "arm64";
#else
    return "amd64";
#endif
}

UpdateCheck app_update_check(const wchar_t *base_url, const char *current,
                             UpdateInfo *out) {
    memset(out, 0, sizeof(*out));
    if (!update_semverish(current)) {
        return UPDATE_DEV_BUILD;
    }

    wchar_t url[2048];
    /* `current` is x.y.z-shaped (checked above): URL-safe as-is. */
    _snwprintf_s(url, ARRAYSIZE(url), _TRUNCATE,
                 L"%s/api/updates/check?channel=windows&platform=windows"
                 L"&arch=%hs&version=%hs",
                 base_url, arch_token(), current);

    HttpResponse response;
    if (!http_request(L"GET", url, NULL, 0, CHECK_TIMEOUT_MS, USER_AGENT,
                      &response) ||
        response.status != 200) {
        http_response_free(&response);
        return UPDATE_CHECK_FAILED;
    }
    const char *body = (const char *)response.body.data;

    UpdateCheck result = UPDATE_CHECK_FAILED;
    if (!json_bool_is_true(body, "available")) {
        /* Either explicitly unavailable or undecodable; only the former is
         * a clean "up to date". */
        JsonSlice value;
        bool available = true;
        if (json_obj_get(body, "available", &value) &&
            json_as_bool(value, &available) && !available) {
            result = UPDATE_UP_TO_DATE;
        }
        http_response_free(&response);
        return result;
    }

    JsonSlice value;
    char *version = json_obj_get(body, "version", &value) ? json_as_string(value) : NULL;
    char *path = json_obj_get(body, "url", &value) ? json_as_string(value) : NULL;
    if (version && path && path[0] != '\0') { /* Go guards the empty URL too */
        strncpy_s(out->version, sizeof(out->version), version, _TRUNCATE);
        _snwprintf_s(out->download_url, ARRAYSIZE(out->download_url), _TRUNCATE,
                     L"%s%hs", base_url, path);
        if (json_obj_get(body, "size", &value)) {
            json_as_u64(value, &out->size);
        }
        if (json_obj_get(body, "sha256", &value)) {
            char *digest = json_as_string(value);
            if (digest) {
                strncpy_s(out->sha256, sizeof(out->sha256), digest, _TRUNCATE);
                free(digest);
            }
        }
        result = UPDATE_AVAILABLE;
    }
    free(version);
    free(path);
    http_response_free(&response);
    return result;
}

bool update_swap_files(const wchar_t *exe, const wchar_t *old_path,
                       const wchar_t *staged) {
    if (!MoveFileExW(exe, old_path, MOVEFILE_REPLACE_EXISTING)) {
        return false;
    }
    if (!MoveFileExW(staged, exe, 0)) {
        /* Roll back so the executable path never ends up empty. */
        MoveFileExW(old_path, exe, 0);
        DeleteFileW(staged);
        return false;
    }
    return true;
}

typedef struct DownloadSink {
    HANDLE file;
    Sha256 digest;
    unsigned long long written;
    bool ok;
} DownloadSink;

static bool download_sink(void *ctx, const uint8_t *chunk, size_t len) {
    DownloadSink *sink = ctx;
    DWORD written = 0;
    if (!WriteFile(sink->file, chunk, (DWORD)len, &written, NULL) ||
        written != len) {
        sink->ok = false;
        return false;
    }
    if (!sha256_update(&sink->digest, chunk, len)) {
        sink->ok = false;
        return false;
    }
    sink->written += len;
    return true;
}

bool app_update_install(const UpdateInfo *update) {
    if (!update_trusted_transport(update->download_url)) {
        return false;
    }
    wchar_t exe[MAX_PATH];
    if (!GetModuleFileNameW(NULL, exe, MAX_PATH)) {
        return false;
    }
    wchar_t old_path[MAX_PATH + 8];
    _snwprintf_s(old_path, ARRAYSIZE(old_path), _TRUNCATE, L"%s%s", exe, OLD_SUFFIX);
    DeleteFileW(old_path); /* leftover from a previous swap */

    /* Stage the download next to the exe so the rename never crosses
     * volumes. */
    wchar_t staged[MAX_PATH + 32];
    wcscpy_s(staged, ARRAYSIZE(staged), exe);
    wchar_t *sep = wcsrchr(staged, L'\\');
    if (!sep) {
        return false;
    }
    _snwprintf_s(sep, ARRAYSIZE(staged) - (size_t)(sep - staged), _TRUNCATE,
                 L"\\.tokitoki-update-%lu", GetCurrentProcessId());

    DownloadSink sink;
    sink.file = CreateFileW(staged, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS,
                            FILE_ATTRIBUTE_NORMAL, NULL);
    if (sink.file == INVALID_HANDLE_VALUE) {
        return false;
    }
    sink.written = 0;
    sink.ok = sha256_init(&sink.digest);

    unsigned status = 0;
    bool ok = sink.ok &&
              http_download(update->download_url, DOWNLOAD_TIMEOUT_MS, USER_AGENT,
                            download_sink, &sink, &status) &&
              sink.ok;
    CloseHandle(sink.file);

    if (ok && update->size > 0 && sink.written != update->size) {
        ok = false;
    }
    char expected[80];
    update_normalize_digest(update->sha256, expected);
    if (ok && expected[0] != '\0') {
        char got[65];
        ok = sha256_final_hex(&sink.digest, got) && strcmp(got, expected) == 0;
    } else {
        sha256_destroy(&sink.digest);
    }

    if (!ok) {
        DeleteFileW(staged);
        return false;
    }
    return update_swap_files(exe, old_path, staged);
}

void app_update_cleanup_leftovers(void) {
    wchar_t exe[MAX_PATH];
    if (GetModuleFileNameW(NULL, exe, MAX_PATH)) {
        wchar_t old_path[MAX_PATH + 8];
        _snwprintf_s(old_path, ARRAYSIZE(old_path), _TRUNCATE, L"%s%s", exe,
                     OLD_SUFFIX);
        DeleteFileW(old_path);
    }
}

bool app_update_relaunch(const wchar_t *exe) {
    wchar_t cmdline[MAX_PATH + 4];
    _snwprintf_s(cmdline, ARRAYSIZE(cmdline), _TRUNCATE, L"\"%s\"", exe);
    STARTUPINFOW si;
    memset(&si, 0, sizeof(si));
    si.cb = sizeof(si);
    PROCESS_INFORMATION pi;
    memset(&pi, 0, sizeof(pi));
    if (!CreateProcessW(exe, cmdline, NULL, NULL, FALSE, 0, NULL, NULL, &si, &pi)) {
        return false;
    }
    CloseHandle(pi.hThread);
    CloseHandle(pi.hProcess);
    return true;
}
