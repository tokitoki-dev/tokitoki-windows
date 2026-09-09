/* Domain-layer tests: version parsing, settings persistence, provider
 * discovery, sync command-line building, update helpers, logo rendering. */
#include "test.h"

#include "agent_cli.h"
#include "app_update.h"
#include "data_dirs.h"
#include "logo.h"
#include "settings.h"
#include "version.h"

#include <windows.h>

#include <shlobj.h> /* SHCreateDirectoryExW */

#include <stdlib.h>
#include <string.h>

/* --- helpers ---------------------------------------------------------- */

static void temp_dir(wchar_t *out, size_t cap) {
    wchar_t base[MAX_PATH];
    GetTempPathW(MAX_PATH, base);
    static unsigned counter;
    _snwprintf_s(out, cap, _TRUNCATE, L"%stokitoki-test-%lu-%u", base,
                 GetCurrentProcessId(), ++counter);
    CreateDirectoryW(out, NULL);
}

static void write_file_w(const wchar_t *dir, const wchar_t *name, const char *body) {
    wchar_t path[MAX_PATH];
    _snwprintf_s(path, MAX_PATH, _TRUNCATE, L"%s\\%s", dir, name);
    HANDLE f = CreateFileW(path, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, 0, NULL);
    DWORD written;
    WriteFile(f, body, (DWORD)strlen(body), &written, NULL);
    CloseHandle(f);
}

/* --- version ---------------------------------------------------------- */

void test_version(void) {
    unsigned v[3];
    TEST_CHECK(parse_version3("1.2.3", v) && v[0] == 1 && v[1] == 2 && v[2] == 3);
    TEST_CHECK(parse_version3("v0.1.6", v) && v[2] == 6);
    TEST_CHECK(parse_version3("  1.2.3-rc1  ", v) && v[2] == 3);
    TEST_CHECK(!parse_version3("dev", v));
    TEST_CHECK(!parse_version3("1.2", v));
    TEST_CHECK(!parse_version3("1.2.3.4", v));
    TEST_CHECK(strcmp(version_utf8(), "dev") == 0); /* test build stays dev */

    TEST_CHECK(update_semverish("0.1.0"));
    TEST_CHECK(update_semverish("v1.2.3"));
    TEST_CHECK(update_semverish("1.2.3-beta+build"));
    TEST_CHECK(!update_semverish("dev"));
}

/* --- settings --------------------------------------------------------- */

void test_settings(void) {
    wchar_t dir[MAX_PATH];
    temp_dir(dir, MAX_PATH);
    Settings s;

    /* Missing file yields defaults. */
    TEST_CHECK(settings_load(dir, &s));
    TEST_CHECK(!s.tracking_disabled && !s.automatic_updates_disabled);

    /* Round-trip both flags. */
    s.tracking_disabled = true;
    s.automatic_updates_disabled = true;
    TEST_CHECK(settings_save(dir, s));
    Settings loaded;
    TEST_CHECK(settings_load(dir, &loaded));
    TEST_CHECK(loaded.tracking_disabled && loaded.automatic_updates_disabled);

    /* Defaults serialize to the empty object. */
    Settings defaults = {false, false};
    TEST_CHECK(settings_save(dir, defaults));
    wchar_t path[MAX_PATH];
    _snwprintf_s(path, MAX_PATH, _TRUNCATE, L"%s\\windows-settings.json", dir);
    HANDLE f = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING, 0, NULL);
    char body[64] = {0};
    DWORD read = 0;
    ReadFile(f, body, sizeof(body) - 1, &read, NULL);
    CloseHandle(f);
    TEST_CHECK_STR_EQ(body, "{}\n");

    /* Legacy Go-era fields are tolerated. */
    write_file_w(dir, L"windows-settings.json",
                 "{\"tracking_disabled\":true,\"enabled_providers\":[\"claude\"]}");
    TEST_CHECK(settings_load(dir, &loaded));
    TEST_CHECK(loaded.tracking_disabled && !loaded.automatic_updates_disabled);
}

/* --- data_dirs -------------------------------------------------------- */

static wchar_t g_fake_home[MAX_PATH];
static wchar_t g_env_name[64];
static wchar_t g_env_value[512];

static bool fake_env(const wchar_t *name, wchar_t *out, size_t cap) {
    if (g_env_name[0] && wcscmp(name, g_env_name) == 0) {
        wcscpy_s(out, cap, g_env_value);
        return true;
    }
    return false;
}

void test_data_dirs(void) {
    temp_dir(g_fake_home, MAX_PATH);
    g_env_name[0] = L'\0';

    /* Nothing exists: empty resolution. */
    ProviderDirs dirs;
    data_dirs_resolve_with(fake_env, g_fake_home, &dirs);
    TEST_CHECK(dirs.count == 0);
    provider_dirs_free(&dirs);

    /* First existing candidate wins; result is sorted by provider. */
    wchar_t sub[MAX_PATH];
    _snwprintf_s(sub, MAX_PATH, _TRUNCATE, L"%s\\.claude", g_fake_home);
    CreateDirectoryW(sub, NULL);
    _snwprintf_s(sub, MAX_PATH, _TRUNCATE, L"%s\\.codex", g_fake_home);
    CreateDirectoryW(sub, NULL);
    data_dirs_resolve_with(fake_env, g_fake_home, &dirs);
    TEST_CHECK(dirs.count == 2);
    TEST_CHECK(dirs.count == 2 && wcscmp(dirs.items[0].provider, L"claude") == 0);
    TEST_CHECK(dirs.count == 2 && wcscmp(dirs.items[1].provider, L"codex") == 0);
    provider_dirs_free(&dirs);

    /* Env override replaces defaults; trailing `projects` segment trimmed. */
    wcscpy_s(g_env_name, 64, L"CLAUDE_CONFIG_DIR");
    _snwprintf_s(g_env_value, 512, _TRUNCATE, L"%s\\custom\\projects", g_fake_home);
    _snwprintf_s(sub, MAX_PATH, _TRUNCATE, L"%s\\custom", g_fake_home);
    CreateDirectoryW(sub, NULL);
    data_dirs_resolve_with(fake_env, g_fake_home, &dirs);
    bool found_custom = false;
    for (size_t i = 0; i < dirs.count; i++) {
        if (wcscmp(dirs.items[i].provider, L"claude") == 0) {
            found_custom = wcsstr(dirs.items[i].dir, L"custom") != NULL &&
                           wcsstr(dirs.items[i].dir, L"projects") == NULL;
        }
    }
    TEST_CHECK(found_custom);
    g_env_name[0] = L'\0';
    provider_dirs_free(&dirs);

    /* Watch paths: an existing FILE maps to its parent directory. */
    _snwprintf_s(sub, MAX_PATH, _TRUNCATE,
                 L"%s\\.local\\share\\goose\\sessions", g_fake_home);
    SHCreateDirectoryExW(NULL, sub, NULL);
    wchar_t db[MAX_PATH];
    _snwprintf_s(db, MAX_PATH, _TRUNCATE, L"%s\\sessions.db", sub);
    HANDLE f = CreateFileW(db, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, 0, NULL);
    CloseHandle(f);
    WatchPaths paths;
    data_dirs_watch_paths_with(fake_env, g_fake_home, &paths);
    bool found_parent = false;
    for (size_t i = 0; i < paths.count; i++) {
        if (wcscmp(paths.items[i], sub) == 0) {
            found_parent = true;
        }
    }
    TEST_CHECK(found_parent);
    /* Deduped and sorted. */
    for (size_t i = 1; i < paths.count; i++) {
        TEST_CHECK(wcscmp(paths.items[i - 1], paths.items[i]) < 0);
    }
    watch_paths_free(&paths);
}

/* --- agent_cli -------------------------------------------------------- */

void test_agent_cli(void) {
    /* Command-line quoting: plain, spaced, quoted, trailing backslash. */
    wchar_t cmdline[512] = L"";
    TEST_CHECK(cmdline_append_arg(cmdline, 512, L"C:\\bin\\tokitoki.exe"));
    TEST_CHECK(cmdline_append_arg(cmdline, 512, L"get"));
    TEST_CHECK(wcscmp(cmdline, L"C:\\bin\\tokitoki.exe get") == 0);

    cmdline[0] = L'\0';
    TEST_CHECK(cmdline_append_arg(cmdline, 512, L"C:\\Program Files\\app.exe"));
    TEST_CHECK(wcscmp(cmdline, L"\"C:\\Program Files\\app.exe\"") == 0);

    cmdline[0] = L'\0';
    TEST_CHECK(cmdline_append_arg(cmdline, 512, L"say \"hi\""));
    TEST_CHECK(wcscmp(cmdline, L"\"say \\\"hi\\\"\"") == 0);

    cmdline[0] = L'\0';
    TEST_CHECK(cmdline_append_arg(cmdline, 512, L"C:\\dir with space\\"));
    TEST_CHECK(wcscmp(cmdline, L"\"C:\\dir with space\\\\\"") == 0);

    /* Sync command line: sorted pairs, empty dirs skipped, NULL when empty. */
    ProviderDir items[2];
    items[0].provider = L"claude";
    items[0].dir = L"C:\\data\\claude";
    items[1].provider = L"codex";
    items[1].dir = L"C:\\data x\\codex";
    ProviderDirs dirs = {items, 2};
    wchar_t *sync = agent_build_sync_cmdline(L"C:\\bin\\tokitoki.exe", &dirs);
    TEST_CHECK(sync != NULL);
    if (sync) {
        TEST_CHECK(wcscmp(sync,
                          L"C:\\bin\\tokitoki.exe sync"
                          L" --provider-dir claude=C:\\data\\claude"
                          L" --provider-dir \"codex=C:\\data x\\codex\"") == 0);
        free(sync);
    }
    ProviderDirs empty = {NULL, 0};
    TEST_CHECK(agent_build_sync_cmdline(L"C:\\bin\\tokitoki.exe", &empty) == NULL);

    /* Base URL default has no trailing slash. */
    wchar_t base[1024];
    agent_base_url(base, 1024);
    TEST_CHECK(wcslen(base) > 0 && base[wcslen(base) - 1] != L'/');
}

/* --- app_update ------------------------------------------------------- */

void test_app_update(void) {
    char digest[80];
    update_normalize_digest("SHA256:ABCDEF", digest);
    TEST_CHECK_STR_EQ(digest, "abcdef");
    update_normalize_digest("AbCd01", digest);
    TEST_CHECK_STR_EQ(digest, "abcd01");

    TEST_CHECK(update_trusted_transport(L"https://tokitoki.dev/x"));
    TEST_CHECK(update_trusted_transport(L"http://localhost:3000/x"));
    TEST_CHECK(update_trusted_transport(L"http://127.0.0.1/x"));
    TEST_CHECK(!update_trusted_transport(L"http://evil.example/x"));
    TEST_CHECK(!update_trusted_transport(L"ftp://tokitoki.dev/x"));
    TEST_CHECK(!update_trusted_transport(L"http://localhost.evil.example/x"));

    /* Swap: replaces the exe and keeps a backup; rollback restores. */
    wchar_t dir[MAX_PATH];
    temp_dir(dir, MAX_PATH);
    wchar_t exe[MAX_PATH], old_path[MAX_PATH], staged[MAX_PATH];
    _snwprintf_s(exe, MAX_PATH, _TRUNCATE, L"%s\\app.exe", dir);
    _snwprintf_s(old_path, MAX_PATH, _TRUNCATE, L"%s\\app.exe.old", dir);
    _snwprintf_s(staged, MAX_PATH, _TRUNCATE, L"%s\\staged", dir);
    write_file_w(dir, L"app.exe", "v1");
    write_file_w(dir, L"staged", "v2");
    TEST_CHECK(update_swap_files(exe, old_path, staged));
    char body[8] = {0};
    HANDLE f = CreateFileW(exe, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING, 0, NULL);
    DWORD read = 0;
    ReadFile(f, body, 7, &read, NULL);
    CloseHandle(f);
    TEST_CHECK_STR_EQ(body, "v2");

    /* Missing staged file rolls the original back. */
    TEST_CHECK(!update_swap_files(exe, old_path, staged));
    f = CreateFileW(exe, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING, 0, NULL);
    TEST_CHECK(f != INVALID_HANDLE_VALUE);
    if (f != INVALID_HANDLE_VALUE) {
        CloseHandle(f);
    }
}

/* --- logo ------------------------------------------------------------- */

void test_logo(void) {
    enum { SIZE = 16 };
    static uint8_t rgba[SIZE * SIZE * 4];
    logo_mark(SIZE, true, rgba);

    /* Corner transparent, center partially covered by the hand. */
    TEST_CHECK(rgba[3] == 0);
    size_t center = ((size_t)8 * SIZE + 8) * 4 + 3;
    TEST_CHECK(rgba[center] > 100);

    /* The 16 px glyph stays visible. */
    int opaque = 0;
    for (size_t i = 3; i < sizeof(rgba); i += 4) {
        if (rgba[i] > 128) {
            opaque++;
        }
    }
    TEST_CHECK(opaque > 20);

    /* Dark variant uses the #18181B ink. */
    logo_mark(SIZE, false, rgba);
    TEST_CHECK(rgba[(8 * SIZE + 8) * 4] == 0x18 && rgba[(8 * SIZE + 8) * 4 + 2] == 0x1B);
}
