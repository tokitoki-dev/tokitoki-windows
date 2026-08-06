//! Launch-at-login via the per-user Run registry key
//! (mirrors Go `internal/launch`).
//!
//! The stored value is the exe path wrapped in literal double quotes only —
//! deliberately NOT escaped, since backslashes must survive verbatim.

use std::{io, path::Path};

/// `HKCU` subkey holding per-user autostart entries.
pub const RUN_KEY: &str = r"Software\Microsoft\Windows\CurrentVersion\Run";
/// Our value name under [`RUN_KEY`].
pub const VALUE_NAME: &str = "Tokitoki";

/// Whether the autostart entry exists (its path is not inspected).
#[must_use]
pub fn is_enabled() -> bool {
    imp::current_value().is_some()
}

/// Adds or removes the autostart entry for the current executable.
///
/// # Errors
/// Returns an [`io::Error`] on registry failure.
pub fn set_enabled(enabled: bool) -> io::Result<()> {
    if enabled {
        let exe = std::env::current_exe()?;
        imp::write_value(&quoted(&exe))
    } else {
        imp::delete_value()
    }
}

/// Repoints a stale autostart entry at the current executable, so a moved app
/// doesn't silently break autostart. A missing entry is a no-op.
///
/// # Errors
/// Returns an [`io::Error`] on registry failure.
pub fn reconcile() -> io::Result<()> {
    let Some(existing) = imp::current_value() else {
        return Ok(());
    };
    let exe = std::env::current_exe()?;
    let current = exe.to_string_lossy();
    if !dequote(&existing).eq_ignore_ascii_case(&current) {
        return imp::write_value(&quoted(&exe));
    }
    Ok(())
}

fn quoted(path: &Path) -> String {
    format!("\"{}\"", path.display())
}

fn dequote(value: &str) -> &str {
    value.trim_matches('"')
}

#[cfg(windows)]
mod imp {
    use std::io;

    use winreg::{enums::HKEY_CURRENT_USER, RegKey};

    use super::{RUN_KEY, VALUE_NAME};

    pub fn current_value() -> Option<String> {
        RegKey::predef(HKEY_CURRENT_USER)
            .open_subkey(RUN_KEY)
            .ok()?
            .get_value(VALUE_NAME)
            .ok()
    }

    pub fn write_value(value: &str) -> io::Result<()> {
        let (key, _) = RegKey::predef(HKEY_CURRENT_USER).create_subkey(RUN_KEY)?;
        key.set_value(VALUE_NAME, &value)
    }

    pub fn delete_value() -> io::Result<()> {
        let key = match RegKey::predef(HKEY_CURRENT_USER)
            .open_subkey_with_flags(RUN_KEY, winreg::enums::KEY_SET_VALUE)
        {
            Ok(key) => key,
            Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(()),
            Err(err) => return Err(err),
        };
        match key.delete_value(VALUE_NAME) {
            Err(err) if err.kind() == io::ErrorKind::NotFound => Ok(()),
            other => other,
        }
    }
}

#[cfg(not(windows))]
mod imp {
    use std::io;

    pub fn current_value() -> Option<String> {
        None
    }

    pub fn write_value(_value: &str) -> io::Result<()> {
        Ok(())
    }

    pub fn delete_value() -> io::Result<()> {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    mod quoted {
        use super::*;

        #[test]
        fn wraps_in_literal_quotes_without_escaping() {
            let path = Path::new(r"C:\Program Files\app.exe");
            assert_eq!(quoted(path), r#""C:\Program Files\app.exe""#);
        }
    }

    mod dequote {
        use super::*;

        #[test]
        fn strips_surrounding_quotes() {
            assert_eq!(dequote(r#""C:\x.exe""#), r"C:\x.exe");
        }

        #[test]
        fn leaves_unquoted_values_untouched() {
            assert_eq!(dequote(r"C:\x.exe"), r"C:\x.exe");
        }
    }
}
