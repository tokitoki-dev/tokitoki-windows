//! Provider data-directory discovery (mirrors Go `internal/datadirs`).
//!
//! Every supported provider is one row in [`PROVIDERS`]; the per-provider
//! quirks (comma-separated env overrides, Claude's trailing `projects`
//! segment, Goose's session-db suffix) are data on the row, not code paths.
//! There is no provider selection UI — all providers, always.

use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    path::{Path, PathBuf},
};

/// Resolved provider directories: provider name → existing data directories.
pub type ProviderDirs = BTreeMap<String, Vec<PathBuf>>;

struct Provider {
    name: &'static str,
    env: &'static str,
    /// Appended to every env-provided root (Goose's `sessions.db` path).
    env_suffix: &'static str,
    /// Trim a trailing `projects` segment from env entries (Claude quirk).
    trim_projects: bool,
    /// Default candidates; `~/` is the home dir, `$XDG_CONFIG_HOME/` expands
    /// with a `~/.config` fallback.
    defaults: &'static [&'static str],
}

const PROVIDERS: &[Provider] = &[
    Provider {
        name: "claude",
        env: "CLAUDE_CONFIG_DIR",
        env_suffix: "",
        trim_projects: true,
        defaults: &["~/.claude", "$XDG_CONFIG_HOME/claude"],
    },
    Provider {
        name: "codex",
        env: "CODEX_CONFIG_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.codex"],
    },
    Provider {
        name: "copilot",
        env: "COPILOT_OTEL_FILE_EXPORTER_PATH",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.copilot/otel"],
    },
    Provider {
        name: "gemini",
        env: "GEMINI_DATA_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.gemini/tmp"],
    },
    Provider {
        name: "kimi",
        env: "KIMI_DATA_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.kimi"],
    },
    Provider {
        name: "qwen",
        env: "QWEN_DATA_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.qwen"],
    },
    Provider {
        name: "openclaw",
        env: "OPENCLAW_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.openclaw", "~/.clawdbot", "~/.moltbot", "~/.moldbot"],
    },
    Provider {
        name: "pi",
        env: "PI_AGENT_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.pi/agent/sessions"],
    },
    Provider {
        name: "amp",
        env: "AMP_DATA_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.local/share/amp"],
    },
    Provider {
        name: "droid",
        env: "DROID_SESSIONS_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.factory/sessions"],
    },
    Provider {
        name: "kilo",
        env: "KILO_DATA_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.local/share/kilo"],
    },
    Provider {
        name: "hermes",
        env: "HERMES_HOME",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.hermes"],
    },
    Provider {
        name: "codebuff",
        env: "CODEBUFF_DATA_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &[
            "~/.config/manicode",
            "~/.config/manicode-dev",
            "~/.config/manicode-staging",
        ],
    },
    Provider {
        name: "opencode",
        env: "OPENCODE_DATA_DIR",
        env_suffix: "",
        trim_projects: false,
        defaults: &["~/.local/share/opencode"],
    },
    Provider {
        name: "goose",
        env: "GOOSE_PATH_ROOT",
        env_suffix: "data/sessions/sessions.db",
        trim_projects: false,
        defaults: &[
            "~/.local/share/goose/sessions/sessions.db",
            "~/Library/Application Support/goose/sessions/sessions.db",
            "~/.local/share/Block/goose/sessions/sessions.db",
        ],
    },
];

/// Returns, for each provider, the first existing candidate path.
///
/// Providers with no existing candidate are omitted; an empty map means there
/// is nothing to sync.
#[must_use]
pub fn resolve() -> ProviderDirs {
    resolve_with(&|name| std::env::var(name).ok(), &home_dir())
}

/// Returns the union of watchable directories over all candidate paths of all
/// providers: an existing directory watches itself, an existing file watches
/// its parent. Deduplicated and sorted.
#[must_use]
pub fn watch_paths() -> Vec<PathBuf> {
    watch_paths_with(&|name| std::env::var(name).ok(), &home_dir())
}

fn home_dir() -> PathBuf {
    ["USERPROFILE", "HOME"]
        .iter()
        .find_map(std::env::var_os)
        .map_or_else(|| PathBuf::from("."), PathBuf::from)
}

fn resolve_with(env: &dyn Fn(&str) -> Option<String>, home: &Path) -> ProviderDirs {
    PROVIDERS
        .iter()
        .filter_map(|provider| {
            let first = candidates(provider, env, home)
                .into_iter()
                .find(|path| fs::metadata(path).is_ok())?;
            Some((provider.name.to_owned(), vec![first]))
        })
        .collect()
}

fn watch_paths_with(env: &dyn Fn(&str) -> Option<String>, home: &Path) -> Vec<PathBuf> {
    let unique: BTreeSet<PathBuf> = PROVIDERS
        .iter()
        .flat_map(|provider| candidates(provider, env, home))
        .filter_map(|path| existing_watch_dir(&path))
        .collect();
    unique.into_iter().collect()
}

fn candidates(
    provider: &Provider,
    env: &dyn Fn(&str) -> Option<String>,
    home: &Path,
) -> Vec<PathBuf> {
    if let Some(value) = env(provider.env) {
        return split_configured_dirs(&value, provider.trim_projects)
            .into_iter()
            .map(|root| {
                if provider.env_suffix.is_empty() {
                    root
                } else {
                    root.join(provider.env_suffix)
                }
            })
            .collect();
    }
    provider
        .defaults
        .iter()
        .map(|template| expand(template, env, home))
        .collect()
}

/// Splits a comma-separated directory list, trimming whitespace and dropping
/// empties. With `trim_projects`, a trailing `projects` segment is rewritten
/// to its parent (Claude stores sessions under `<config>/projects`).
fn split_configured_dirs(value: &str, trim_projects: bool) -> Vec<PathBuf> {
    value
        .split(',')
        .map(str::trim)
        .filter(|entry| !entry.is_empty())
        .map(|entry| {
            let path = PathBuf::from(entry);
            if trim_projects && path.file_name().is_some_and(|name| name == "projects") {
                path.parent().map_or(path.clone(), Path::to_path_buf)
            } else {
                path
            }
        })
        .collect()
}

fn expand(template: &str, env: &dyn Fn(&str) -> Option<String>, home: &Path) -> PathBuf {
    if let Some(rest) = template.strip_prefix("~/") {
        return home.join(rest);
    }
    if let Some(rest) = template.strip_prefix("$XDG_CONFIG_HOME/") {
        let config = env("XDG_CONFIG_HOME").map_or_else(|| home.join(".config"), PathBuf::from);
        return config.join(rest);
    }
    PathBuf::from(template)
}

fn existing_watch_dir(path: &Path) -> Option<PathBuf> {
    let meta = fs::metadata(path).ok()?;
    if meta.is_dir() {
        return Some(path.to_path_buf());
    }
    let parent = path.parent()?;
    fs::metadata(parent)
        .ok()?
        .is_dir()
        .then(|| parent.to_path_buf())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn no_env(_: &str) -> Option<String> {
        None
    }

    mod split_configured_dirs {
        use super::*;

        #[test]
        fn trims_whitespace_and_drops_empties() {
            let dirs = split_configured_dirs(" a , ,b,", false);
            assert_eq!(dirs, vec![PathBuf::from("a"), PathBuf::from("b")]);
        }

        #[test]
        fn rewrites_trailing_projects_segment_to_parent() {
            let dirs = split_configured_dirs("/home/u/.claude/projects", true);
            assert_eq!(dirs, vec![PathBuf::from("/home/u/.claude")]);
        }

        #[test]
        fn keeps_projects_segment_without_trim_flag() {
            let dirs = split_configured_dirs("/home/u/.claude/projects", false);
            assert_eq!(dirs, vec![PathBuf::from("/home/u/.claude/projects")]);
        }
    }

    mod resolve_with {
        use super::*;

        #[test]
        fn picks_first_existing_default_candidate() {
            let home = tempfile::tempdir().unwrap();
            fs::create_dir_all(home.path().join(".claude")).unwrap();

            let dirs = resolve_with(&no_env, home.path());

            assert_eq!(dirs.get("claude"), Some(&vec![home.path().join(".claude")]));
        }

        #[test]
        fn omits_providers_with_no_existing_candidate() {
            let home = tempfile::tempdir().unwrap();
            let dirs = resolve_with(&no_env, home.path());
            assert!(dirs.is_empty(), "unexpected: {dirs:?}");
        }

        #[test]
        fn env_override_replaces_defaults() {
            let home = tempfile::tempdir().unwrap();
            let custom = home.path().join("custom-codex");
            fs::create_dir_all(&custom).unwrap();
            fs::create_dir_all(home.path().join(".codex")).unwrap();
            let custom_str = custom.to_string_lossy().into_owned();
            let env = move |name: &str| (name == "CODEX_CONFIG_DIR").then(|| custom_str.clone());

            let dirs = resolve_with(&env, home.path());

            assert_eq!(dirs.get("codex"), Some(&vec![custom]));
        }

        #[test]
        fn goose_env_root_gets_sessions_db_suffix() {
            let home = tempfile::tempdir().unwrap();
            let root = home.path().join("goose-root");
            let db_dir = root.join("data").join("sessions");
            fs::create_dir_all(&db_dir).unwrap();
            fs::write(db_dir.join("sessions.db"), b"").unwrap();
            let root_str = root.to_string_lossy().into_owned();
            let env = move |name: &str| (name == "GOOSE_PATH_ROOT").then(|| root_str.clone());

            let dirs = resolve_with(&env, home.path());

            assert_eq!(dirs.get("goose"), Some(&vec![db_dir.join("sessions.db")]));
        }
    }

    mod watch_paths_with {
        use super::*;

        #[test]
        fn maps_existing_file_candidate_to_its_parent() {
            let home = tempfile::tempdir().unwrap();
            let db_dir = home.path().join(".local/share/goose/sessions");
            fs::create_dir_all(&db_dir).unwrap();
            fs::write(db_dir.join("sessions.db"), b"").unwrap();

            let paths = watch_paths_with(&no_env, home.path());

            assert_eq!(paths, vec![db_dir]);
        }

        #[test]
        fn dedupes_and_sorts_directories() {
            let home = tempfile::tempdir().unwrap();
            fs::create_dir_all(home.path().join(".claude")).unwrap();
            fs::create_dir_all(home.path().join(".codex")).unwrap();

            let paths = watch_paths_with(&no_env, home.path());

            assert_eq!(
                paths,
                vec![home.path().join(".claude"), home.path().join(".codex")]
            );
        }
    }
}
