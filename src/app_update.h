/* App self-update: check, download, verify, atomic binary swap (mirrors Go
 * internal/appupdate). No external updater — the running exe renames itself
 * aside as ".old" and the new build takes its path. */
#ifndef TOKITOKI_APP_UPDATE_H
#define TOKITOKI_APP_UPDATE_H

#include <stdbool.h>
#include <wchar.h>

typedef struct UpdateInfo {
    char version[64];
    wchar_t download_url[2048]; /* absolute; server publishes a relative path */
    unsigned long long size;    /* 0 disables the size check */
    char sha256[80];            /* empty disables the digest check */
} UpdateInfo;

typedef enum UpdateCheck {
    UPDATE_AVAILABLE,
    UPDATE_UP_TO_DATE,
    UPDATE_DEV_BUILD, /* current version is not x.y.z: checks disabled */
    UPDATE_CHECK_FAILED,
} UpdateCheck;

UpdateCheck app_update_check(const wchar_t *base_url, const char *current,
                             UpdateInfo *out);

/* Downloads + verifies + swaps. On success the exe path holds the new build;
 * the running process keeps executing the old image. */
bool app_update_install(const UpdateInfo *update);

/* Removes the ".old" binary a previous update left behind. Best effort. */
void app_update_cleanup_leftovers(void);

/* Caller must release the instance lock first. */
bool app_update_relaunch(const wchar_t *exe);

/* --- exposed for tests --- */
bool update_semverish(const char *version);
/* Strips an optional "sha256:" prefix and lowercases into out[80]. */
void update_normalize_digest(const char *digest, char out[80]);
/* https always; plain http only for localhost/loopback. */
bool update_trusted_transport(const wchar_t *url);
/* Rename swap with rollback; exposed so tests can drive it on temp files. */
bool update_swap_files(const wchar_t *exe, const wchar_t *old_path,
                       const wchar_t *staged);

#endif
