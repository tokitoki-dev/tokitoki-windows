//! Single-instance guard via a session-local named mutex
//! (mirrors Go `internal/instance`).

use std::io;

/// The session-local mutex name; `Local\` scopes it to the login session.
pub const MUTEX_NAME: &str = r"Local\TokitokiWindowsTray";

/// Holds the instance mutex; dropping it releases the claim.
///
/// The updater relaunch flow MUST drop this before spawning the new process,
/// or the child sees a live instance and exits silently.
pub struct Lock {
    /// Held only so Drop closes the mutex handle.
    _handle: imp::Handle,
}

impl Lock {
    /// Releases the mutex now (drop also releases it).
    pub fn release(self) {}
}

/// Claims the single-instance mutex.
///
/// Returns `Ok(None)` when another instance already holds it — the caller
/// should exit silently.
///
/// # Errors
/// Returns an [`io::Error`] when the mutex cannot be created at all.
pub fn acquire() -> io::Result<Option<Lock>> {
    imp::acquire().map(|handle| handle.map(|handle| Lock { _handle: handle }))
}

#[cfg(windows)]
mod imp {
    use std::io;

    use windows::{
        core::w,
        Win32::{
            Foundation::{CloseHandle, GetLastError, ERROR_ALREADY_EXISTS, HANDLE},
            System::Threading::CreateMutexW,
        },
    };

    pub struct Handle(HANDLE);

    // SAFETY: the mutex handle is only closed once, on drop.
    unsafe impl Send for Handle {}

    impl Drop for Handle {
        fn drop(&mut self) {
            // SAFETY: `self.0` is a live mutex handle owned by this struct.
            unsafe {
                let _ = CloseHandle(self.0);
            }
        }
    }

    pub fn acquire() -> io::Result<Option<Handle>> {
        // SAFETY: plain Win32 call; the name is a static wide string.
        let handle = unsafe { CreateMutexW(None, true, w!(r"Local\TokitokiWindowsTray")) }
            .map_err(|err| io::Error::other(err.message()))?;
        // SAFETY: reads this thread's last-error slot set by CreateMutexW.
        if unsafe { GetLastError() } == ERROR_ALREADY_EXISTS {
            drop(Handle(handle));
            return Ok(None);
        }
        Ok(Some(Handle(handle)))
    }
}

#[cfg(not(windows))]
mod imp {
    use std::io;

    pub struct Handle;

    pub fn acquire() -> io::Result<Option<Handle>> {
        Ok(Some(Handle))
    }
}
