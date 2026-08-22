#include "data_dirs.h"

#include "util/wstr.h"

#include <windows.h>

#include <stdlib.h>
#include <string.h>

typedef struct Provider {
    const wchar_t *name;
    const wchar_t *env;
    /* Appended to every env-provided root (Goose's sessions.db path). */
    const wchar_t *env_suffix;
    /* Rewrite a trailing `projects` segment to its parent (Claude quirk). */
    bool trim_projects;
    /* Defaults: `~/` is home, `$XDG_CONFIG_HOME/` falls back to ~/.config. */
    const wchar_t *defaults[4];
} Provider;

static const Provider PROVIDERS[] = {
    {L"claude", L"CLAUDE_CONFIG_DIR", NULL, true,
     {L"~/.claude", L"$XDG_CONFIG_HOME/claude", NULL, NULL}},
    {L"codex", L"CODEX_CONFIG_DIR", NULL, false, {L"~/.codex", NULL, NULL, NULL}},
    {L"copilot", L"COPILOT_OTEL_FILE_EXPORTER_PATH", NULL, false,
     {L"~/.copilot/otel", NULL, NULL, NULL}},
    {L"gemini", L"GEMINI_DATA_DIR", NULL, false, {L"~/.gemini/tmp", NULL, NULL, NULL}},
    {L"kimi", L"KIMI_DATA_DIR", NULL, false, {L"~/.kimi", NULL, NULL, NULL}},
    {L"qwen", L"QWEN_DATA_DIR", NULL, false, {L"~/.qwen", NULL, NULL, NULL}},
    {L"openclaw", L"OPENCLAW_DIR", NULL, false,
     {L"~/.openclaw", L"~/.clawdbot", L"~/.moltbot", L"~/.moldbot"}},
    {L"pi", L"PI_AGENT_DIR", NULL, false, {L"~/.pi/agent/sessions", NULL, NULL, NULL}},
    {L"amp", L"AMP_DATA_DIR", NULL, false, {L"~/.local/share/amp", NULL, NULL, NULL}},
    {L"droid", L"DROID_SESSIONS_DIR", NULL, false,
     {L"~/.factory/sessions", NULL, NULL, NULL}},
    {L"kilo", L"KILO_DATA_DIR", NULL, false, {L"~/.local/share/kilo", NULL, NULL, NULL}},
    {L"hermes", L"HERMES_HOME", NULL, false, {L"~/.hermes", NULL, NULL, NULL}},
    {L"codebuff", L"CODEBUFF_DATA_DIR", NULL, false,
     {L"~/.config/manicode", L"~/.config/manicode-dev", L"~/.config/manicode-staging",
      NULL}},
    {L"opencode", L"OPENCODE_DATA_DIR", NULL, false,
     {L"~/.local/share/opencode", NULL, NULL, NULL}},
    {L"goose", L"GOOSE_PATH_ROOT", L"data/sessions/sessions.db", false,
     {L"~/.local/share/goose/sessions/sessions.db",
      L"~/Library/Application Support/goose/sessions/sessions.db",
      L"~/.local/share/Block/goose/sessions/sessions.db", NULL}},
};

#define MAX_CANDIDATES 16

typedef struct Candidates {
    wchar_t *paths[MAX_CANDIDATES]; /* malloc'd */
    size_t count;
} Candidates;

static bool os_env(const wchar_t *name, wchar_t *out, size_t cap) {
    DWORD len = GetEnvironmentVariableW(name, out, (DWORD)cap);
    return len > 0 && len < cap;
}

/* Converts forward slashes from table templates into backslashes. */
static wchar_t *normalize_separators(wchar_t *path) {
    for (wchar_t *p = path; *p; p++) {
        if (*p == L'/') {
            *p = L'\\';
        }
    }
    return path;
}

static wchar_t *join_path(const wchar_t *base, const wchar_t *rest) {
    size_t cap = wcslen(base) + wcslen(rest) + 2;
    wchar_t *joined = malloc(cap * sizeof(wchar_t));
    if (joined) {
        _snwprintf_s(joined, cap, _TRUNCATE, L"%s\\%s", base, rest);
        normalize_separators(joined);
    }
    return joined;
}

static wchar_t *expand_template(const wchar_t *tmpl, EnvLookupFn env, const wchar_t *home) {
    if (wcsncmp(tmpl, L"~/", 2) == 0) {
        return join_path(home, tmpl + 2);
    }
    if (wcsncmp(tmpl, L"$XDG_CONFIG_HOME/", 17) == 0) {
        wchar_t config[MAX_PATH];
        if (env(L"XDG_CONFIG_HOME", config, MAX_PATH)) {
            return join_path(config, tmpl + 17);
        }
        wchar_t *fallback = join_path(home, L".config");
        if (!fallback) {
            return NULL;
        }
        wchar_t *joined = join_path(fallback, tmpl + 17);
        free(fallback);
        return joined;
    }
    wchar_t *copy = wstr_dup(tmpl);
    return copy ? normalize_separators(copy) : NULL;
}

/* Rewrites a trailing `projects` segment to its parent, in place. */
static void trim_projects_segment(wchar_t *path) {
    size_t len = wcslen(path);
    while (len > 0 && (path[len - 1] == L'\\' || path[len - 1] == L'/')) {
        path[--len] = L'\0';
    }
    const wchar_t *last = path + len;
    while (last > path && last[-1] != L'\\' && last[-1] != L'/') {
        last--;
    }
    if (wcscmp(last, L"projects") == 0 && last > path) {
        path[(last - path) - 1] = L'\0';
    }
}

/* Splits a comma-separated env value into candidate paths. */
static void split_configured(const wchar_t *value, const Provider *p, Candidates *out) {
    const wchar_t *cursor = value;
    while (*cursor && out->count < MAX_CANDIDATES) {
        const wchar_t *comma = wcschr(cursor, L',');
        size_t len = comma ? (size_t)(comma - cursor) : wcslen(cursor);

        /* Trim whitespace. */
        while (len > 0 && (*cursor == L' ' || *cursor == L'\t')) {
            cursor++;
            len--;
        }
        while (len > 0 && (cursor[len - 1] == L' ' || cursor[len - 1] == L'\t')) {
            len--;
        }
        if (len > 0) {
            wchar_t *entry = malloc((len + 1) * sizeof(wchar_t));
            if (entry) {
                wmemcpy(entry, cursor, len);
                entry[len] = L'\0';
                normalize_separators(entry);
                if (p->trim_projects) {
                    trim_projects_segment(entry);
                }
                if (p->env_suffix) {
                    wchar_t *with_suffix = join_path(entry, p->env_suffix);
                    free(entry);
                    entry = with_suffix;
                }
                if (entry) {
                    out->paths[out->count++] = entry;
                }
            }
        }
        cursor = comma ? comma + 1 : cursor + len;
        if (comma) {
            continue;
        }
        break;
    }
}

static void candidates_for(const Provider *p, EnvLookupFn env, const wchar_t *home,
                           Candidates *out) {
    out->count = 0;
    wchar_t value[4096];
    if (env(p->env, value, 4096)) {
        split_configured(value, p, out);
        return;
    }
    for (size_t i = 0; i < 4 && p->defaults[i]; i++) {
        wchar_t *path = expand_template(p->defaults[i], env, home);
        if (path && out->count < MAX_CANDIDATES) {
            out->paths[out->count++] = path;
        } else {
            free(path);
        }
    }
}

static void candidates_free(Candidates *c) {
    for (size_t i = 0; i < c->count; i++) {
        free(c->paths[i]);
    }
    c->count = 0;
}

static bool path_exists(const wchar_t *path, bool *is_dir) {
    DWORD attrs = GetFileAttributesW(path);
    if (attrs == INVALID_FILE_ATTRIBUTES) {
        return false;
    }
    *is_dir = (attrs & FILE_ATTRIBUTE_DIRECTORY) != 0;
    return true;
}

static void default_home(wchar_t *out, size_t cap) {
    if (!os_env(L"USERPROFILE", out, cap) && !os_env(L"HOME", out, cap)) {
        wcscpy_s(out, cap, L".");
    }
}

void data_dirs_resolve_with(EnvLookupFn env, const wchar_t *home, ProviderDirs *out) {
    size_t provider_count = ARRAYSIZE(PROVIDERS);
    out->items = calloc(provider_count, sizeof(ProviderDir));
    out->count = 0;
    if (!out->items) {
        return;
    }
    for (size_t i = 0; i < provider_count; i++) {
        Candidates c;
        candidates_for(&PROVIDERS[i], env, home, &c);
        for (size_t j = 0; j < c.count; j++) {
            bool is_dir;
            if (path_exists(c.paths[j], &is_dir)) {
                out->items[out->count].provider = PROVIDERS[i].name;
                out->items[out->count].dir = c.paths[j];
                c.paths[j] = NULL; /* ownership moved */
                out->count++;
                break;
            }
        }
        candidates_free(&c);
    }
    /* Providers sorted by name, like the Rust BTreeMap ordering. */
    for (size_t i = 1; i < out->count; i++) {
        ProviderDir key = out->items[i];
        size_t j = i;
        while (j > 0 && wcscmp(out->items[j - 1].provider, key.provider) > 0) {
            out->items[j] = out->items[j - 1];
            j--;
        }
        out->items[j] = key;
    }
}

void data_dirs_resolve(ProviderDirs *out) {
    wchar_t home[MAX_PATH];
    default_home(home, MAX_PATH);
    data_dirs_resolve_with(os_env, home, out);
}

/* Existing dir -> itself; existing file -> its parent (if a directory). */
static wchar_t *watchable_dir(const wchar_t *path) {
    bool is_dir;
    if (!path_exists(path, &is_dir)) {
        return NULL;
    }
    if (is_dir) {
        return wstr_dup(path);
    }
    const wchar_t *sep = wcsrchr(path, L'\\');
    if (!sep || sep == path) {
        return NULL;
    }
    size_t len = (size_t)(sep - path);
    wchar_t *parent = malloc((len + 1) * sizeof(wchar_t));
    if (!parent) {
        return NULL;
    }
    wmemcpy(parent, path, len);
    parent[len] = L'\0';
    if (!path_exists(parent, &is_dir) || !is_dir) {
        free(parent);
        return NULL;
    }
    return parent;
}

static int compare_wide(const void *a, const void *b) {
    return wcscmp(*(wchar_t *const *)a, *(wchar_t *const *)b);
}

void data_dirs_watch_paths_with(EnvLookupFn env, const wchar_t *home, WatchPaths *out) {
    size_t provider_count = ARRAYSIZE(PROVIDERS);
    size_t cap = provider_count * MAX_CANDIDATES;
    out->items = calloc(cap, sizeof(wchar_t *));
    out->count = 0;
    if (!out->items) {
        return;
    }
    for (size_t i = 0; i < provider_count; i++) {
        Candidates c;
        candidates_for(&PROVIDERS[i], env, home, &c);
        for (size_t j = 0; j < c.count; j++) {
            wchar_t *dir = watchable_dir(c.paths[j]);
            if (dir && out->count < cap) {
                out->items[out->count++] = dir;
            } else {
                free(dir);
            }
        }
        candidates_free(&c);
    }
    if (out->count > 1) {
        qsort(out->items, out->count, sizeof(wchar_t *), compare_wide);
        size_t unique = 1;
        for (size_t i = 1; i < out->count; i++) {
            if (wcscmp(out->items[i], out->items[unique - 1]) == 0) {
                free(out->items[i]);
            } else {
                out->items[unique++] = out->items[i];
            }
        }
        out->count = unique;
    }
}

void data_dirs_watch_paths(WatchPaths *out) {
    wchar_t home[MAX_PATH];
    default_home(home, MAX_PATH);
    data_dirs_watch_paths_with(os_env, home, out);
}

void provider_dirs_free(ProviderDirs *dirs) {
    for (size_t i = 0; i < dirs->count; i++) {
        free(dirs->items[i].dir);
    }
    free(dirs->items);
    dirs->items = NULL;
    dirs->count = 0;
}

void watch_paths_free(WatchPaths *paths) {
    for (size_t i = 0; i < paths->count; i++) {
        free(paths->items[i]);
    }
    free(paths->items);
    paths->items = NULL;
    paths->count = 0;
}
