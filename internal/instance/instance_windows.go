//go:build windows

// Package instance prevents duplicate tray app instances.
package instance

import "golang.org/x/sys/windows"

const mutexName = `Local\TokitokiWindowsTray`

// Lock owns the Windows named mutex that marks the running tray instance.
type Lock struct {
	handle windows.Handle
}

// Acquire returns acquired=false when another instance is already running.
func Acquire() (*Lock, bool, error) {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false, err
	}

	handle, err := windows.CreateMutex(nil, true, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(handle)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &Lock{handle: handle}, true, nil
}

// Close releases the instance lock.
func (l *Lock) Close() error {
	if l == nil || l.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(l.handle)
	l.handle = 0
	return err
}
