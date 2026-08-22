#include "agent_cli.h"

#include "resource.h"
#include "util/buf.h"
#include "util/inflate.h"
#include "util/json.h"
#include "util/wstr.h"

#include <windows.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define DEFAULT_BASE_URL L"https://tokitoki.dev"
#define EXIT_NO_API_KEY 3u

void agent_base_url(wchar_t *out, size_t cap) {
    wchar_t value[1024];
    DWORD len = GetEnvironmentVariableW(L"TOKITOKI_BASE_URL", value, ARRAYSIZE(value));
    if (len == 0 || len >= ARRAYSIZE(value)) {
        wcscpy_s(out, cap, DEFAULT_BASE_URL);
        return;
    }
    /* Trim whitespace, then trailing slashes. */
    wchar_t *start = value;
    while (*start == L' ' || *start == L'\t') {
        start++;
    }
    size_t end = wcslen(start);
    while (end > 0 && (start[end - 1] == L' ' || start[end - 1] == L'\t')) {
        end--;
    }
    while (end > 0 && start[end - 1] == L'/') {
        end--;
    }
    start[end] = L'\0';
    if (end == 0) {
        wcscpy_s(out, cap, DEFAULT_BASE_URL);
    } else {
        wcscpy_s(out, cap, start);
    }
}

bool agent_data_dir(wchar_t *out, size_t cap) {
    wchar_t home[MAX_PATH];
    DWORD len = GetEnvironmentVariableW(L"USERPROFILE", home, MAX_PATH);
    if (len == 0 || len >= MAX_PATH) {
        len = GetEnvironmentVariableW(L"HOME", home, MAX_PATH);
        if (len == 0 || len >= MAX_PATH) {
            return false;
        }
    }
    _snwprintf_s(out, cap, _TRUNCATE, L"%s\\.tokitoki", home);
    CreateDirectoryW(out, NULL);
    DWORD attrs = GetFileAttributesW(out);
    return attrs != INVALID_FILE_ATTRIBUTES && (attrs & FILE_ATTRIBUTE_DIRECTORY);
}

bool agent_shared_binary(wchar_t *out, size_t cap) {
    wchar_t dir[MAX_PATH];
    if (!agent_data_dir(dir, MAX_PATH)) {
        return false;
    }
    _snwprintf_s(out, cap, _TRUNCATE, L"%s\\bin\\tokitoki.exe", dir);
    return true;
}

bool parse_version3(const char *text, unsigned out[3]) {
    while (*text == ' ' || *text == '\t' || *text == '\r' || *text == '\n') {
        text++;
    }
    if (*text == 'v') {
        text++;
    }
    for (int part = 0; part < 3; part++) {
        if (*text < '0' || *text > '9') {
            return false;
        }
        unsigned value = 0;
        while (*text >= '0' && *text <= '9') {
            value = value * 10 + (unsigned)(*text - '0');
            text++;
        }
        out[part] = value;
        if (part == 2) {
            break;
        }
        /* Skip any non-dot decoration (e.g. "-rc1") up to the next dot. */
        const char *dot = strchr(text, '.');
        if (!dot) {
            return false;
        }
        text = dot + 1;
    }
    /* A fourth dotted part means this is not x.y.z. */
    return strchr(text, '.') == NULL;
}

bool cmdline_append_arg(wchar_t *cmdline, size_t cap, const wchar_t *arg) {
    size_t len = wcslen(cmdline);
    if (len > 0) {
        if (len + 2 > cap) {
            return false;
        }
        cmdline[len++] = L' ';
        cmdline[len] = L'\0';
    }
    bool need_quotes = *arg == L'\0' || wcspbrk(arg, L" \t\"") != NULL;
    if (!need_quotes) {
        size_t arg_len = wcslen(arg);
        if (len + arg_len + 1 > cap) {
            return false; /* graceful failure, not a wcscat_s abort */
        }
        wmemcpy(cmdline + len, arg, arg_len + 1);
        return true;
    }
    /* MSVCRT rules: backslashes are literal unless they precede a quote, in
     * which case they (and the quote) must be escaped. */
    if (len + 1 >= cap) {
        return false;
    }
    cmdline[len++] = L'"';
    for (const wchar_t *p = arg; *p; p++) {
        size_t backslashes = 0;
        while (*p == L'\\') {
            backslashes++;
            p++;
        }
        if (*p == L'"' || *p == L'\0') {
            backslashes *= 2; /* escape them so the closing quote survives */
        }
        for (size_t i = 0; i < backslashes; i++) {
            if (len + 1 >= cap) {
                return false;
            }
            cmdline[len++] = L'\\';
        }
        if (*p == L'\0') {
            p--; /* let the outer loop terminate */
            continue;
        }
        if (*p == L'"') {
            if (len + 2 >= cap) {
                return false;
            }
            cmdline[len++] = L'\\';
        }
        if (len + 1 >= cap) {
            return false;
        }
        cmdline[len++] = *p;
    }
    if (len + 2 > cap) {
        return false;
    }
    cmdline[len++] = L'"';
    cmdline[len] = L'\0';
    return true;
}

wchar_t *agent_build_sync_cmdline(const wchar_t *exe, const ProviderDirs *dirs) {
    if (dirs->count == 0) {
        return NULL;
    }
    size_t cap = 4096;
    for (size_t i = 0; i < dirs->count; i++) {
        cap += wcslen(dirs->items[i].dir) * 2 + 64;
    }
    wchar_t *cmdline = calloc(cap, sizeof(wchar_t));
    if (!cmdline) {
        return NULL;
    }
    bool ok = cmdline_append_arg(cmdline, cap, exe) &&
              cmdline_append_arg(cmdline, cap, L"sync");
    size_t pairs = 0;
    for (size_t i = 0; ok && i < dirs->count; i++) {
        if (dirs->items[i].dir[0] == L'\0') {
            continue; /* empty entries are skipped, like Go/Rust */
        }
        /* Size the pair to the actual path so a long provider dir is never
         * silently truncated into a wrong --provider-dir argument. */
        size_t pair_cap = wcslen(dirs->items[i].provider) +
                          wcslen(dirs->items[i].dir) + 2;
        wchar_t *pair = malloc(pair_cap * sizeof(wchar_t));
        if (!pair) {
            ok = false;
            break;
        }
        _snwprintf_s(pair, pair_cap, _TRUNCATE, L"%s=%s",
                     dirs->items[i].provider, dirs->items[i].dir);
        ok = cmdline_append_arg(cmdline, cap, L"--provider-dir") &&
             cmdline_append_arg(cmdline, cap, pair);
        free(pair);
        pairs++;
    }
    if (!ok || pairs == 0) {
        free(cmdline);
        return NULL;
    }
    return cmdline;
}

/* ---- hidden-console process runner ---------------------------------- */

typedef struct PipeReader {
    HANDLE pipe;
    Buf data;
} PipeReader;

static DWORD WINAPI pipe_reader_thread(LPVOID param) {
    PipeReader *reader = param;
    uint8_t chunk[4096];
    DWORD read = 0;
    while (ReadFile(reader->pipe, chunk, sizeof(chunk), &read, NULL) && read > 0) {
        if (!buf_push(&reader->data, chunk, read)) {
            break;
        }
    }
    return 0;
}

static bool make_pipe(HANDLE *read_end, HANDLE *write_end) {
    SECURITY_ATTRIBUTES sa = {sizeof(sa), NULL, TRUE};
    if (!CreatePipe(read_end, write_end, &sa, 0)) {
        return false;
    }
    /* Only the child inherits the write end. */
    SetHandleInformation(*read_end, HANDLE_FLAG_INHERIT, 0);
    return true;
}

/* Runs `cmdline` (first token must be the exe path) with a hidden console.
 * Returns stdout as malloc'd UTF-8; stderr (char-truncated) lands in
 * `detail` on failure. `detail` may be NULL. */
static AgentErr run_cmdline(wchar_t *cmdline, unsigned timeout_ms,
                            char **out_stdout, char *detail) {
    if (out_stdout) {
        *out_stdout = NULL;
    }
    if (detail) {
        detail[0] = '\0';
    }

    HANDLE out_read, out_write, err_read, err_write;
    if (!make_pipe(&out_read, &out_write)) {
        return AGENT_ERR_LAUNCH;
    }
    if (!make_pipe(&err_read, &err_write)) {
        CloseHandle(out_read);
        CloseHandle(out_write);
        return AGENT_ERR_LAUNCH;
    }

    STARTUPINFOW si;
    memset(&si, 0, sizeof(si));
    si.cb = sizeof(si);
    si.dwFlags = STARTF_USESTDHANDLES;
    si.hStdOutput = out_write;
    si.hStdError = err_write;
    PROCESS_INFORMATION pi;
    memset(&pi, 0, sizeof(pi));

    BOOL spawned = CreateProcessW(NULL, cmdline, NULL, NULL, TRUE,
                                  CREATE_NO_WINDOW, NULL, NULL, &si, &pi);
    /* Parent must drop the write ends or the readers never see EOF. */
    CloseHandle(out_write);
    CloseHandle(err_write);
    if (!spawned) {
        CloseHandle(out_read);
        CloseHandle(err_read);
        return AGENT_ERR_LAUNCH;
    }
    CloseHandle(pi.hThread);

    PipeReader readers[2];
    readers[0].pipe = out_read;
    readers[1].pipe = err_read;
    buf_init(&readers[0].data);
    buf_init(&readers[1].data);
    HANDLE threads[2] = {
        CreateThread(NULL, 0, pipe_reader_thread, &readers[0], 0, NULL),
        CreateThread(NULL, 0, pipe_reader_thread, &readers[1], 0, NULL),
    };

    AgentErr result = AGENT_OK;
    if (WaitForSingleObject(pi.hProcess, timeout_ms) == WAIT_TIMEOUT) {
        TerminateProcess(pi.hProcess, 1);
        WaitForSingleObject(pi.hProcess, 5000);
        result = AGENT_ERR_TIMEOUT;
    }
    for (int i = 0; i < 2; i++) {
        if (threads[i]) {
            WaitForSingleObject(threads[i], 5000);
            CloseHandle(threads[i]);
        }
    }
    CloseHandle(out_read);
    CloseHandle(err_read);

    DWORD exit_code = 1;
    GetExitCodeProcess(pi.hProcess, &exit_code);
    CloseHandle(pi.hProcess);

    if (result == AGENT_OK && exit_code == EXIT_NO_API_KEY) {
        result = AGENT_ERR_MISSING_KEY;
    } else if (result == AGENT_OK && exit_code != 0) {
        result = AGENT_ERR_FAILED;
        if (detail) {
            size_t n = readers[1].data.len;
            if (n > AGENT_DETAIL_CHARS * 4 - 1) {
                n = AGENT_DETAIL_CHARS * 4 - 1;
            }
            memcpy(detail, readers[1].data.data, n);
            detail[n] = '\0';
            utf8_truncate_chars(detail, AGENT_DETAIL_CHARS);
        }
    }
    if (result == AGENT_OK && out_stdout) {
        if (buf_push_byte(&readers[0].data, 0)) {
            *out_stdout = (char *)readers[0].data.data;
            buf_init(&readers[0].data); /* ownership moved */
        } else {
            result = AGENT_ERR_LAUNCH;
        }
    }
    buf_free(&readers[0].data);
    buf_free(&readers[1].data);
    return result;
}

/* Builds `"<shared>" <args...>` and runs it. */
static AgentErr run_shared(const wchar_t *const *args, size_t arg_count,
                           unsigned timeout_ms, char **out_stdout, char *detail) {
    if (detail) {
        detail[0] = '\0'; /* every exit path must leave a valid C string */
    }
    wchar_t exe[MAX_PATH];
    if (!agent_shared_binary(exe, MAX_PATH)) {
        return AGENT_ERR_LOCATION;
    }
    wchar_t cmdline[8192] = L"";
    if (!cmdline_append_arg(cmdline, ARRAYSIZE(cmdline), exe)) {
        return AGENT_ERR_LOCATION;
    }
    for (size_t i = 0; i < arg_count; i++) {
        if (!cmdline_append_arg(cmdline, ARRAYSIZE(cmdline), args[i])) {
            return AGENT_ERR_LOCATION;
        }
    }
    return run_cmdline(cmdline, timeout_ms, out_stdout, detail);
}

/* Requires {"ok":true} on stdout. */
static AgentErr run_shared_ok(const wchar_t *const *args, size_t arg_count,
                              unsigned timeout_ms) {
    char *output = NULL;
    AgentErr err = run_shared(args, arg_count, timeout_ms, &output, NULL);
    if (err != AGENT_OK) {
        free(output);
        return err;
    }
    bool ok = output && json_bool_is_true(utf8_trim(output), "ok");
    free(output);
    return ok ? AGENT_OK : AGENT_ERR_OUTPUT;
}

AgentErr agent_sync(const ProviderDirs *dirs) {
    wchar_t exe[MAX_PATH];
    if (!agent_shared_binary(exe, MAX_PATH)) {
        return AGENT_ERR_LOCATION;
    }
    wchar_t *cmdline = agent_build_sync_cmdline(exe, dirs);
    if (!cmdline) {
        return AGENT_OK; /* nothing to scan is a successful no-op */
    }
    char *output = NULL;
    AgentErr err = run_cmdline(cmdline, AGENT_SYNC_TIMEOUT_MS, &output, NULL);
    free(cmdline);
    if (err != AGENT_OK) {
        free(output);
        return err;
    }
    bool ok = output && json_bool_is_true(utf8_trim(output), "ok");
    free(output);
    return ok ? AGENT_OK : AGENT_ERR_OUTPUT;
}

AgentErr agent_get_api_key(char **out_key, char *detail) {
    static const wchar_t *args[] = {L"get", L"key"};
    char *output = NULL;
    AgentErr err = run_shared(args, 2, AGENT_OP_TIMEOUT_MS, &output, detail);
    if (err != AGENT_OK) {
        free(output);
        return err;
    }
    *out_key = output ? utf8_trim(output) : NULL;
    return *out_key ? AGENT_OK : AGENT_ERR_OUTPUT;
}

AgentErr agent_set_api_key(const char *key) {
    wchar_t *wide_key = wstr_from_utf8(key, (size_t)-1);
    if (!wide_key) {
        return AGENT_ERR_LOCATION;
    }
    const wchar_t *args[] = {L"set", L"key", wide_key};
    AgentErr err = run_shared_ok(args, 3, AGENT_OP_TIMEOUT_MS);
    free(wide_key);
    return err;
}

VerifyResult agent_verify_key(const char *key) {
    wchar_t *wide_key = wstr_from_utf8(key, (size_t)-1);
    if (!wide_key) {
        return VERIFY_UNAVAILABLE;
    }
    const wchar_t *args[] = {L"verify", L"key", wide_key};
    char *output = NULL;
    AgentErr err = run_shared(args, 3, AGENT_OP_TIMEOUT_MS, &output, NULL);
    free(wide_key);
    if (err != AGENT_OK || !output) {
        free(output);
        return VERIFY_UNAVAILABLE; /* "could not check", not "invalid" */
    }
    /* The CLI exits 0 with {"ok":true,"valid":bool} for a definite answer;
     * a present, decodable `valid` is the only thing that distinguishes
     * valid/invalid from unavailable. */
    JsonSlice slice;
    bool valid = false;
    VerifyResult result = VERIFY_UNAVAILABLE;
    if (json_bool_is_true(utf8_trim(output), "ok") &&
        json_obj_get(output, "valid", &slice) && json_as_bool(slice, &valid)) {
        result = valid ? VERIFY_VALID : VERIFY_INVALID;
    }
    free(output);
    return result;
}

AgentErr agent_get_dashboard_url(wchar_t **out_url) {
    static const wchar_t *args[] = {L"get", L"dashboard-url"};
    char *output = NULL;
    AgentErr err = run_shared(args, 2, AGENT_OP_TIMEOUT_MS, &output, NULL);
    if (err != AGENT_OK) {
        free(output);
        return err;
    }
    if (!output) {
        return AGENT_ERR_OUTPUT;
    }
    utf8_trim(output);
    bool plausible = strncmp(output, "https://", 8) == 0 ||
                     strncmp(output, "http://", 7) == 0;
    if (!plausible) {
        free(output);
        return AGENT_ERR_OUTPUT;
    }
    *out_url = wstr_from_utf8(output, (size_t)-1);
    free(output);
    return *out_url ? AGENT_OK : AGENT_ERR_OUTPUT;
}

AgentErr agent_update(void) {
    static const wchar_t *args[] = {L"update"};
    return run_shared_ok(args, 1, AGENT_UPDATE_TIMEOUT_MS);
}

/* ---- first-launch seeding from the embedded resource ------------------ */

static bool load_resource_bytes(int id, const uint8_t **out_data, size_t *out_len) {
    HRSRC found = FindResourceW(NULL, MAKEINTRESOURCEW(id), (LPCWSTR)RT_RCDATA);
    if (!found) {
        return false;
    }
    HGLOBAL loaded = LoadResource(NULL, found);
    if (!loaded) {
        return false;
    }
    *out_data = LockResource(loaded);
    *out_len = SizeofResource(NULL, found);
    return *out_data != NULL && *out_len > 0;
}

/* Installed CLI version via `tokitoki version`; false when unknown. */
static bool installed_cli_version(const wchar_t *shared, unsigned out[3]) {
    wchar_t cmdline[MAX_PATH * 2] = L"";
    if (!cmdline_append_arg(cmdline, ARRAYSIZE(cmdline), shared) ||
        !cmdline_append_arg(cmdline, ARRAYSIZE(cmdline), L"version")) {
        return false;
    }
    char *output = NULL;
    if (run_cmdline(cmdline, AGENT_OP_TIMEOUT_MS, &output, NULL) != AGENT_OK) {
        free(output);
        return false;
    }
    bool ok = output && parse_version3(utf8_trim(output), out);
    free(output);
    return ok;
}

static bool version_less(const unsigned a[3], const unsigned b[3]) {
    for (int i = 0; i < 3; i++) {
        if (a[i] != b[i]) {
            return a[i] < b[i];
        }
    }
    return false;
}

void agent_bootstrap(void) {
    const uint8_t *gz_data;
    size_t gz_len;
    if (!load_resource_bytes(IDR_CLI_GZ, &gz_data, &gz_len)) {
        return; /* dev build: no payload, no seeding, no downloads, ever */
    }
    const uint8_t *ver_data;
    size_t ver_len;
    unsigned bundled[3];
    char ver_text[64] = {0};
    if (!load_resource_bytes(IDR_CLI_VERSION, &ver_data, &ver_len) ||
        ver_len >= sizeof(ver_text)) {
        return;
    }
    memcpy(ver_text, ver_data, ver_len);
    if (!parse_version3(ver_text, bundled)) {
        return; /* an unknown bundled version must never replace a live CLI */
    }

    wchar_t shared[MAX_PATH];
    if (!agent_shared_binary(shared, MAX_PATH)) {
        return;
    }
    DWORD attrs = GetFileAttributesW(shared);
    if (attrs != INVALID_FILE_ATTRIBUTES && !(attrs & FILE_ATTRIBUTE_DIRECTORY)) {
        unsigned current[3];
        if (installed_cli_version(shared, current) && !version_less(current, bundled)) {
            return; /* never downgrade */
        }
    }

    Buf binary;
    buf_init(&binary);
    if (!gzip_inflate(gz_data, gz_len, &binary)) {
        buf_free(&binary);
        return;
    }

    /* Atomic seed: write .tokitoki.exe.seed, then rename into place. */
    wchar_t bin_dir[MAX_PATH];
    wcscpy_s(bin_dir, MAX_PATH, shared);
    wchar_t *sep = wcsrchr(bin_dir, L'\\');
    if (sep) {
        *sep = L'\0';
    }
    /* Create <data>\bin (parents already exist via agent_data_dir). */
    CreateDirectoryW(bin_dir, NULL);

    wchar_t staging[MAX_PATH];
    _snwprintf_s(staging, MAX_PATH, _TRUNCATE, L"%s\\.tokitoki.exe.seed", bin_dir);
    HANDLE file = CreateFileW(staging, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS,
                              FILE_ATTRIBUTE_NORMAL, NULL);
    bool ok = file != INVALID_HANDLE_VALUE;
    if (ok) {
        DWORD written = 0;
        ok = WriteFile(file, binary.data, (DWORD)binary.len, &written, NULL) &&
             written == binary.len;
        CloseHandle(file);
    }
    buf_free(&binary);
    if (ok) {
        MoveFileExW(staging, shared, MOVEFILE_REPLACE_EXISTING);
    } else {
        DeleteFileW(staging);
    }
}
