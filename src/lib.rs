//! Tokitoki Windows tray agent — a Rust port of `tokitoki-windows` (Go).
//!
//! The binary in `main.rs` stays thin; all behavior lives here so unit and
//! integration tests can drive the same code paths. Module layout mirrors the
//! Go project's `internal/` packages one-to-one:
//!
//! | Rust module   | Go package             | Responsibility                                  |
//! |---------------|------------------------|-------------------------------------------------|
//! | [`agent_cli`] | `internal/agentcli`    | Locate, seed, and invoke the shared CLI          |
//! | [`api_key`]   | `internal/apikey`      | Direct server-side API-key verification          |
//! | [`app`]       | `internal/app`         | Platform-neutral service coordinator             |
//! | [`app_update`]| `internal/appupdate`   | Self-update check, download, and binary swap     |
//! | [`data_dirs`] | `internal/datadirs`    | Provider data-directory discovery                |
//! | [`instance`]  | `internal/instance`    | Single-instance named-mutex guard                |
//! | [`launch`]    | `internal/launch`      | Launch-at-login registry entry                   |
//! | [`logo`]      | `internal/logo`        | Procedural clock-mark renderer (no bitmaps)      |
//! | [`settings`]  | `internal/settings`    | Preferences persistence (atomic JSON)            |
//! | [`syncer`]    | `internal/syncer`      | Coalescing sync worker                           |
//! | [`ui`]        | `internal/ui`          | Tray icon, menu, dialogs, theme, updater UI      |
//! | [`version`]   | `internal/version`     | Build metadata injected at compile time          |
//! | [`watcher`]   | `internal/watcher`     | Recursive debounced filesystem watcher           |

pub mod agent_cli;
pub mod api_key;
pub mod app;
pub mod app_update;
pub mod data_dirs;
pub mod instance;
pub mod launch;
pub mod logo;
pub mod settings;
pub mod syncer;
pub mod ui;
pub mod version;
pub mod watcher;
