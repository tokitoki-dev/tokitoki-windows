#include "syncer.h"

#include <windows.h>

#include <stdlib.h>

typedef struct SyncerSync {
    SRWLOCK lock;
    CONDITION_VARIABLE cond;
} SyncerSync;

static SyncerSync *sync_of(Syncer *s) {
    return (SyncerSync *)s->lock_storage;
}

static DWORD WINAPI worker_main(LPVOID param) {
    Syncer *s = param;
    SyncerSync *sy = sync_of(s);
    AcquireSRWLockExclusive(&sy->lock);
    for (;;) {
        while (!s->pending && !s->stopping) {
            SleepConditionVariableSRW(&sy->cond, &sy->lock, INFINITE, 0);
        }
        if (s->stopping) {
            break;
        }
        s->pending = false;
        ReleaseSRWLockExclusive(&sy->lock);
        s->run(s->run_ctx);
        AcquireSRWLockExclusive(&sy->lock);
    }
    ReleaseSRWLockExclusive(&sy->lock);
    return 0;
}

static DWORD WINAPI ticker_main(LPVOID param) {
    Syncer *s = param;
    /* interval is stashed in the event's ctx via the struct; see start. */
    for (;;) {
        DWORD wait = WaitForSingleObject((HANDLE)s->ticker_stop, s->ticker_interval_ms);
        if (wait != WAIT_TIMEOUT) {
            return 0;
        }
        syncer_trigger(s);
    }
}

bool syncer_start(Syncer *s, SyncRunFn run, void *ctx) {
    memset(s, 0, sizeof(*s));
    s->run = run;
    s->run_ctx = ctx;
    SyncerSync *sy = malloc(sizeof(SyncerSync));
    if (!sy) {
        return false;
    }
    InitializeSRWLock(&sy->lock);
    InitializeConditionVariable(&sy->cond);
    s->lock_storage = sy;
    s->cond_storage = &sy->cond;

    s->worker_thread = CreateThread(NULL, 0, worker_main, s, 0, NULL);
    if (!s->worker_thread) {
        free(sy);
        s->lock_storage = NULL;
        return false;
    }
    return true;
}

void syncer_trigger(Syncer *s) {
    SyncerSync *sy = sync_of(s);
    if (!sy) {
        return;
    }
    AcquireSRWLockExclusive(&sy->lock);
    s->pending = true;
    ReleaseSRWLockExclusive(&sy->lock);
    WakeConditionVariable(&sy->cond);
}

bool syncer_start_ticker(Syncer *s, unsigned interval_ms) {
    if (s->ticker_thread) {
        return true;
    }
    s->ticker_interval_ms = interval_ms;
    s->ticker_stop = CreateEventW(NULL, TRUE, FALSE, NULL);
    if (!s->ticker_stop) {
        return false;
    }
    s->ticker_thread = CreateThread(NULL, 0, ticker_main, s, 0, NULL);
    return s->ticker_thread != NULL;
}

void syncer_stop(Syncer *s) {
    SyncerSync *sy = sync_of(s);
    if (!sy) {
        return;
    }
    if (s->ticker_thread) {
        SetEvent((HANDLE)s->ticker_stop);
        WaitForSingleObject((HANDLE)s->ticker_thread, INFINITE);
        CloseHandle((HANDLE)s->ticker_thread);
        CloseHandle((HANDLE)s->ticker_stop);
        s->ticker_thread = NULL;
        s->ticker_stop = NULL;
    }
    AcquireSRWLockExclusive(&sy->lock);
    s->stopping = true;
    ReleaseSRWLockExclusive(&sy->lock);
    WakeConditionVariable(&sy->cond);
    WaitForSingleObject((HANDLE)s->worker_thread, INFINITE);
    CloseHandle((HANDLE)s->worker_thread);
    s->worker_thread = NULL;
    /* The SyncerSync storage is deliberately NOT freed: a worker thread that
     * outlives the main loop (e.g. the Settings dialog's save-key path) can
     * still call syncer_trigger, which reads lock_storage and acquires the
     * lock. Since the syncer is a process-lifetime singleton stopped only at
     * exit, keeping the tiny struct alive trades a harmless one-time leak for
     * the absence of a shutdown use-after-free. A post-stop trigger just sets
     * `pending` on a worker that will never run — a no-op. */
}
