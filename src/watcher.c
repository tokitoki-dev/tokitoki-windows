#include "watcher.h"

#include <windows.h>

#include <stdlib.h>
#include <string.h>

#define MAX_ROOTS 32
#define BUFFER_BYTES (8 * 1024)

typedef struct Root {
    HANDLE dir;
    HANDLE event; /* signaled by the overlapped RDCW completion */
    OVERLAPPED overlapped;
    /* DWORD-aligned as ReadDirectoryChangesW requires. */
    __declspec(align(4)) unsigned char buffer[BUFFER_BYTES];
} Root;

struct Watcher {
    unsigned debounce_ms;
    WatchCallback on_change;
    void *ctx;

    Root roots[MAX_ROOTS];
    size_t root_count;
    HANDLE stop_event;
    HANDLE changed_event; /* watch thread -> debounce thread */
    HANDLE watch_thread;
    HANDLE debounce_thread;
};

static bool issue_read(Root *root) {
    return ReadDirectoryChangesW(
               root->dir, root->buffer, BUFFER_BYTES, TRUE,
               FILE_NOTIFY_CHANGE_FILE_NAME | FILE_NOTIFY_CHANGE_DIR_NAME |
                   FILE_NOTIFY_CHANGE_SIZE | FILE_NOTIFY_CHANGE_LAST_WRITE,
               NULL, &root->overlapped, NULL) != FALSE;
}

static DWORD WINAPI watch_main(LPVOID param) {
    Watcher *w = param;
    HANDLE waits[MAX_ROOTS + 1];
    waits[0] = w->stop_event;
    for (size_t i = 0; i < w->root_count; i++) {
        waits[i + 1] = w->roots[i].event;
    }
    DWORD count = (DWORD)(w->root_count + 1);
    for (;;) {
        DWORD which = WaitForMultipleObjects(count, waits, FALSE, INFINITE);
        if (which == WAIT_OBJECT_0 || which == WAIT_FAILED) {
            return 0; /* stop requested */
        }
        Root *root = &w->roots[which - WAIT_OBJECT_0 - 1];
        DWORD bytes = 0;
        GetOverlappedResult(root->dir, &root->overlapped, &bytes, FALSE);
        /* Even a zero-byte completion (buffer overflow) means "something
         * changed"; the sync rescans everything anyway. */
        SetEvent(w->changed_event);
        ResetEvent(root->event);
        if (!issue_read(root)) {
            /* Root vanished (deleted/unmounted): stop watching it. */
            ResetEvent(root->event);
        }
    }
}

static DWORD WINAPI debounce_main(LPVOID param) {
    Watcher *w = param;
    HANDLE waits[2] = {w->stop_event, w->changed_event};
    for (;;) {
        DWORD which = WaitForMultipleObjects(2, waits, FALSE, INFINITE);
        if (which != WAIT_OBJECT_0 + 1) {
            return 0;
        }
        /* Burst window: keep absorbing events until things go quiet. */
        for (;;) {
            which = WaitForMultipleObjects(2, waits, FALSE, w->debounce_ms);
            if (which == WAIT_OBJECT_0) {
                return 0;
            }
            if (which == WAIT_TIMEOUT) {
                w->on_change(w->ctx);
                break;
            }
        }
    }
}

Watcher *watcher_create(unsigned debounce_ms, WatchCallback on_change, void *ctx) {
    Watcher *w = calloc(1, sizeof(Watcher));
    if (!w) {
        return NULL;
    }
    w->debounce_ms = debounce_ms ? debounce_ms : 2000;
    w->on_change = on_change;
    w->ctx = ctx;
    return w;
}

bool watcher_start(Watcher *w, const WatchPaths *paths) {
    watcher_stop(w);
    if (!paths || paths->count == 0) {
        return true;
    }

    w->stop_event = CreateEventW(NULL, TRUE, FALSE, NULL);
    w->changed_event = CreateEventW(NULL, FALSE, FALSE, NULL); /* auto-reset */
    if (!w->stop_event || !w->changed_event) {
        watcher_stop(w);
        return false;
    }

    for (size_t i = 0; i < paths->count && w->root_count < MAX_ROOTS; i++) {
        Root *root = &w->roots[w->root_count];
        memset(root, 0, offsetof(Root, buffer));
        root->dir = CreateFileW(paths->items[i], FILE_LIST_DIRECTORY,
                                FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
                                NULL, OPEN_EXISTING,
                                FILE_FLAG_BACKUP_SEMANTICS | FILE_FLAG_OVERLAPPED,
                                NULL);
        if (root->dir == INVALID_HANDLE_VALUE) {
            continue; /* skipped, like the Go original logs-and-continues */
        }
        root->event = CreateEventW(NULL, TRUE, FALSE, NULL);
        root->overlapped.hEvent = root->event;
        if (!root->event || !issue_read(root)) {
            if (root->event) {
                CloseHandle(root->event);
            }
            CloseHandle(root->dir);
            continue;
        }
        w->root_count++;
    }
    if (w->root_count == 0) {
        watcher_stop(w);
        return true; /* nothing watchable is not an error */
    }

    w->watch_thread = CreateThread(NULL, 0, watch_main, w, 0, NULL);
    w->debounce_thread = CreateThread(NULL, 0, debounce_main, w, 0, NULL);
    if (!w->watch_thread || !w->debounce_thread) {
        watcher_stop(w);
        return false;
    }
    return true;
}

void watcher_stop(Watcher *w) {
    if (w->stop_event) {
        SetEvent(w->stop_event);
    }
    if (w->watch_thread) {
        WaitForSingleObject(w->watch_thread, INFINITE);
        CloseHandle(w->watch_thread);
        w->watch_thread = NULL;
    }
    if (w->debounce_thread) {
        WaitForSingleObject(w->debounce_thread, INFINITE);
        CloseHandle(w->debounce_thread);
        w->debounce_thread = NULL;
    }
    for (size_t i = 0; i < w->root_count; i++) {
        Root *root = &w->roots[i];
        /* Wait for the canceled I/O to actually complete: the kernel writes
         * into the OVERLAPPED/buffer on completion, and this memory is
         * reused by the next watcher_start. */
        if (CancelIoEx(root->dir, &root->overlapped) ||
            GetLastError() != ERROR_NOT_FOUND) {
            DWORD bytes = 0;
            GetOverlappedResult(root->dir, &root->overlapped, &bytes, TRUE);
        }
        CloseHandle(root->dir);
        CloseHandle(root->event);
    }
    w->root_count = 0;
    if (w->changed_event) {
        CloseHandle(w->changed_event);
        w->changed_event = NULL;
    }
    if (w->stop_event) {
        CloseHandle(w->stop_event);
        w->stop_event = NULL;
    }
}

void watcher_destroy(Watcher *w) {
    if (w) {
        watcher_stop(w);
        free(w);
    }
}
