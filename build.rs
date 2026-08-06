//! Build script: injects version metadata, detects the optional embedded CLI
//! payload, and compiles the Windows resources (app icon + manifest).

use std::{env, error::Error, path::Path};

fn main() -> Result<(), Box<dyn Error>> {
    println!("cargo::rustc-check-cfg=cfg(embedded_cli)");

    // Release builds pass these; local builds fall back to the same defaults
    // the Go original uses ("dev" / "local" / "unknown").
    for (env_name, default) in [
        ("TOKITOKI_VERSION", "dev"),
        ("TOKITOKI_COMMIT", "local"),
        ("TOKITOKI_BUILD_DATE", "unknown"),
    ] {
        println!("cargo:rerun-if-env-changed={env_name}");
        let value = env::var(env_name).unwrap_or_else(|_| default.to_owned());
        println!("cargo:rustc-env={env_name}={value}");
    }

    // Dev builds ship no CLI payload; `agent_cli::bootstrap` becomes a no-op.
    println!("cargo:rerun-if-changed=embedded/tokitoki.exe.gz");
    println!("cargo:rerun-if-changed=embedded/VERSION");
    if Path::new("embedded/tokitoki.exe.gz").exists() && Path::new("embedded/VERSION").exists() {
        println!("cargo::rustc-cfg=embedded_cli");
    }

    if env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("windows") {
        println!("cargo:rerun-if-changed=assets/app-icon.ico");
        println!("cargo:rerun-if-changed=tokitoki-windows.exe.manifest");
        winresource::WindowsResource::new()
            // Resource ID 2 for parity with the Go build (rsrc assigned 2).
            .set_icon_with_id("assets/app-icon.ico", "2")
            .set_manifest_file("tokitoki-windows.exe.manifest")
            .compile()?;
    }
    Ok(())
}
