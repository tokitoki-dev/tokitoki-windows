#include "instance.h"

#include <windows.h>

InstanceResult instance_acquire(void **out_handle) {
    *out_handle = NULL;
    HANDLE mutex = CreateMutexW(NULL, TRUE, L"Local\\TokitokiWindowsTray");
    if (!mutex) {
        return INSTANCE_ERROR;
    }
    if (GetLastError() == ERROR_ALREADY_EXISTS) {
        CloseHandle(mutex);
        return INSTANCE_ALREADY_RUNNING;
    }
    *out_handle = mutex;
    return INSTANCE_ACQUIRED;
}

void instance_release(void *handle) {
    if (handle) {
        CloseHandle((HANDLE)handle);
    }
}
