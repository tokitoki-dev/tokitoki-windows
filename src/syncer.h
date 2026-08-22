/* Coalescing sync worker (mirrors Go internal/syncer): any number of
 * triggers that arrive while a sync runs collapse into exactly one
 * follow-up run. A single pending flag under an SRWLOCK is the whole
 * data structure — no queue to mismanage. */
#ifndef TOKITOKI_SYNCER_H
#define TOKITOKI_SYNCER_H

#include <stdbool.h>

typedef void (*SyncRunFn)(void *ctx);

typedef struct Syncer {
    void *worker_thread; /* HANDLE */
    void *ticker_thread; /* HANDLE */
    void *ticker_stop;   /* HANDLE (event) */
    void *lock_storage;  /* SRWLOCK + CONDITION_VARIABLE, malloc'd */
    void *cond_storage;  /* points into lock_storage */
    unsigned ticker_interval_ms;
    bool pending;
    bool stopping;
    SyncRunFn run;
    void *run_ctx;
} Syncer;

bool syncer_start(Syncer *s, SyncRunFn run, void *ctx);
/* Never blocks; extra triggers while busy coalesce into one follow-up. */
void syncer_trigger(Syncer *s);
/* Triggers every `interval_ms` until stop. */
bool syncer_start_ticker(Syncer *s, unsigned interval_ms);
/* Stops both threads and joins them. */
void syncer_stop(Syncer *s);

#endif
