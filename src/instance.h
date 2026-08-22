/* Single-instance guard via the session-local named mutex
 * `Local\TokitokiWindowsTray` — the SAME name the Go and Rust builds use, so
 * mixed-version handovers (updates) keep excluding each other correctly.
 *
 * The updater relaunch flow MUST release the lock before spawning the new
 * process, or the child sees a live instance and exits silently. */
#ifndef TOKITOKI_INSTANCE_H
#define TOKITOKI_INSTANCE_H

typedef enum InstanceResult {
    INSTANCE_ACQUIRED,
    INSTANCE_ALREADY_RUNNING,
    INSTANCE_ERROR,
} InstanceResult;

/* On INSTANCE_ACQUIRED, `*out_handle` holds the mutex (a HANDLE). */
InstanceResult instance_acquire(void **out_handle);
void instance_release(void *handle);

#endif
