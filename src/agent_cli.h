/* Shared-CLI resolution and invocation (mirrors Go internal/agentcli).
 *
 * The tray app contains no sync/upload logic of its own: every operation
 * spawns the shared `tokitoki` CLI at %USERPROFILE%\.tokitoki\bin\
 * tokitoki.exe once per call, console hidden — the same contract the macOS
 * app and editor plugins follow.
 *
 * Error details deliberately never contain argv: `set key <key>` would leak
 * the API key into logs. */
#ifndef TOKITOKI_AGENT_CLI_H
#define TOKITOKI_AGENT_CLI_H

#include "data_dirs.h"

#include <stdbool.h>
#include <stddef.h>
#include <wchar.h>

#define AGENT_SYNC_TIMEOUT_MS (2u * 60u * 1000u)
#define AGENT_OP_TIMEOUT_MS (15u * 1000u)
#define AGENT_UPDATE_TIMEOUT_MS (5u * 60u * 1000u)

/* Stderr detail cap, in characters (Go: msg[:197] + "..."). */
#define AGENT_DETAIL_CHARS 200

typedef enum AgentErr {
    AGENT_OK = 0,
    AGENT_ERR_MISSING_KEY, /* CLI exit code 3 */
    AGENT_ERR_LAUNCH,
    AGENT_ERR_TIMEOUT,
    AGENT_ERR_FAILED,
    AGENT_ERR_OUTPUT,
    AGENT_ERR_LOCATION,
} AgentErr;

/* Three-state key-verification answer (mirrors the CLI's {ok,valid} JSON). */
typedef enum VerifyResult {
    VERIFY_VALID,
    VERIFY_INVALID,     /* the server gave a definite "not valid" */
    VERIFY_UNAVAILABLE, /* could not check: CLI/transport/parse trouble */
} VerifyResult;

/* TOKITOKI_BASE_URL (trimmed of whitespace/trailing '/') or the default
 * https://tokitoki.dev. */
void agent_base_url(wchar_t *out, size_t cap);

/* %USERPROFILE%\.tokitoki, created if absent. */
bool agent_data_dir(wchar_t *out, size_t cap);
bool agent_shared_binary(wchar_t *out, size_t cap);

/* Operations — one short-lived hidden CLI process each. */
AgentErr agent_sync(const ProviderDirs *dirs);
/* Trimmed key, malloc'd UTF-8; caller frees. */
AgentErr agent_get_api_key(char **out_key, char detail[AGENT_DETAIL_CHARS * 4]);
AgentErr agent_set_api_key(const char *key);
/* Verifies a candidate key against the server via `tokitoki verify key <key>`
 * (the CLI owns all server contact; the Windows app no longer speaks HTTP). */
VerifyResult agent_verify_key(const char *key);
/* Malloc'd wide URL (http/https only); caller frees. */
AgentErr agent_get_dashboard_url(wchar_t **out_url);
AgentErr agent_update(void);

/* Seeds the shared CLI from the embedded resource payload; a dev build
 * (no payload resource) is a no-op. Never downgrades. */
void agent_bootstrap(void);

/* --- exposed for tests --- */

/* Parses "1.2.3"/"v1.2.3" (leading digits per part, exactly 3 parts). */
bool parse_version3(const char *text, unsigned out[3]);

/* Appends one argument to a command line with MSVCRT quoting rules. */
bool cmdline_append_arg(wchar_t *cmdline, size_t cap, const wchar_t *arg);

/* Builds `"<exe>" sync --provider-dir p=d ...`; NULL when nothing to scan.
 * Caller frees. */
wchar_t *agent_build_sync_cmdline(const wchar_t *exe, const ProviderDirs *dirs);

#endif
