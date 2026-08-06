//! First-launch seeding of the shared CLI from the embedded payload.
//!
//! Release builds embed a pinned, gzipped CLI (`embedded/tokitoki.exe.gz` +
//! `embedded/VERSION`, fetched at build time). Dev builds embed nothing and
//! this module is a no-op: no seeding, no downloads, ever.
//!
//! Seeding rules (mirror Go `agentcli.Bootstrap`):
//! - Never replace a live CLI with an unknown bundled version.
//! - Never downgrade: an installed CLI at or above the bundled version stays.
//! - Writes are atomic: gunzip to `.tokitoki.exe.seed`, then rename into
//!   place, so a crash never leaves a half-written CLI.

use super::Error;

/// Seeds `%USERPROFILE%\.tokitoki\bin\tokitoki.exe` from the embedded payload
/// when the installed CLI is absent or older. No-op in dev builds.
///
/// # Errors
/// Returns [`Error`] when the payload cannot be decompressed or written.
pub fn bootstrap() -> Result<(), Error> {
    imp::bootstrap()
}

#[cfg(not(embedded_cli))]
mod imp {
    use super::Error;

    #[expect(
        clippy::unnecessary_wraps,
        reason = "signature shared with embedded_cli builds"
    )]
    pub fn bootstrap() -> Result<(), Error> {
        tracing::debug!("no embedded CLI payload; skipping bootstrap");
        Ok(())
    }
}

#[cfg(embedded_cli)]
mod imp {
    use std::{fs, io::Read, time::Duration};

    use super::Error;
    use crate::agent_cli::{client::run_binary, parse_version, shared_binary};

    const EMBEDDED_GZ: &[u8] = include_bytes!(concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/embedded/tokitoki.exe.gz"
    ));
    const EMBEDDED_VERSION: &str =
        include_str!(concat!(env!("CARGO_MANIFEST_DIR"), "/embedded/VERSION"));

    const SEED_NAME: &str = ".tokitoki.exe.seed";
    const VERSION_TIMEOUT: Duration = Duration::from_secs(15);

    pub fn bootstrap() -> Result<(), Error> {
        // An unknown bundled version must never replace a live CLI.
        let Some(bundled) = parse_version(EMBEDDED_VERSION) else {
            tracing::warn!("embedded CLI version is unparsable; skipping bootstrap");
            return Ok(());
        };

        let shared = shared_binary()?;
        if fs::metadata(&shared).is_ok_and(|meta| meta.is_file()) {
            let current = run_binary(&shared, "version", &["version".to_owned()], VERSION_TIMEOUT)
                .ok()
                .and_then(|output| parse_version(&output));
            // Never downgrade. A CLI that can't report a version is replaced.
            if current.is_some_and(|current| current >= bundled) {
                return Ok(());
            }
        }
        seed(&shared)
    }

    fn seed(shared: &std::path::Path) -> Result<(), Error> {
        let mut binary = Vec::new();
        flate2::read::GzDecoder::new(EMBEDDED_GZ)
            .read_to_end(&mut binary)
            .map_err(Error::Location)?;

        let bin_dir = shared.parent().ok_or_else(|| {
            Error::Location(std::io::Error::new(
                std::io::ErrorKind::NotFound,
                "shared binary has no parent directory",
            ))
        })?;
        fs::create_dir_all(bin_dir).map_err(Error::Location)?;

        let staging = bin_dir.join(SEED_NAME);
        fs::write(&staging, &binary).map_err(Error::Location)?;
        fs::rename(&staging, shared).map_err(Error::Location)?;
        tracing::info!("seeded shared CLI {EMBEDDED_VERSION}");
        Ok(())
    }
}
