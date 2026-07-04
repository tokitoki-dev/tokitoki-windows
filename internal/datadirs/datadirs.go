// Package datadirs resolves local AI client data directories.
package datadirs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/labx/tokitoki-agent/pkg/agentlib"
)

// Directories contains selected provider directories.
type Directories struct {
	ProviderDirs map[agentlib.Provider][]string
}

// SyncOptions converts Directories into agentlib sync options.
func (d Directories) SyncOptions() agentlib.SyncOptions {
	return agentlib.SyncOptions{ProviderDirs: d.ProviderDirs}
}

// Resolve returns existing provider directories for providers.
func Resolve(providers []string) Directories {
	dirs := Directories{ProviderDirs: make(map[agentlib.Provider][]string)}
	for _, provider := range providers {
		if path := firstExistingPath(paths(provider)); path != "" {
			dirs.ProviderDirs[agentlib.Provider(provider)] = []string{path}
		}
	}
	return dirs
}

// WatchPaths returns existing directories to watch for providers.
func WatchPaths(providers []string) []string {
	seen := map[string]bool{}
	for _, provider := range providers {
		for _, path := range paths(provider) {
			if dir := existingWatchDir(path); dir != "" {
				seen[dir] = true
			}
		}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func paths(provider string) []string {
	switch provider {
	case "claude":
		return claudePaths()
	case "codex":
		return codexPaths()
	case "copilot":
		return copilotPaths()
	case "gemini":
		return geminiPaths()
	case "kimi":
		return kimiPaths()
	case "qwen":
		return qwenPaths()
	case "openclaw":
		return openClawPaths()
	case "pi":
		return piPaths()
	case "amp":
		return ampPaths()
	case "droid":
		return droidPaths()
	case "kilo":
		return kiloPaths()
	case "hermes":
		return hermesPaths()
	case "codebuff":
		return codebuffPaths()
	case "opencode":
		return openCodePaths()
	case "goose":
		return goosePaths()
	default:
		return nil
	}
}

func claudePaths() []string {
	if configured := os.Getenv("CLAUDE_CONFIG_DIR"); configured != "" {
		return splitConfiguredDirs(configured, true)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".claude")}
}

func codexPaths() []string {
	if configured := os.Getenv("CODEX_CONFIG_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".codex")}
}

func copilotPaths() []string {
	if configured := os.Getenv("COPILOT_OTEL_FILE_EXPORTER_PATH"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".copilot", "otel")}
}

func geminiPaths() []string {
	if configured := os.Getenv("GEMINI_DATA_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".gemini", "tmp")}
}

func kimiPaths() []string {
	if configured := os.Getenv("KIMI_DATA_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".kimi")}
}

func qwenPaths() []string {
	if configured := os.Getenv("QWEN_DATA_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".qwen")}
}

func openClawPaths() []string {
	if configured := os.Getenv("OPENCLAW_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".openclaw"),
		filepath.Join(home, ".clawdbot"),
		filepath.Join(home, ".moltbot"),
		filepath.Join(home, ".moldbot"),
	}
}

func piPaths() []string {
	if configured := os.Getenv("PI_AGENT_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".pi", "agent", "sessions")}
}

func ampPaths() []string {
	if configured := os.Getenv("AMP_DATA_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "share", "amp")}
}

func droidPaths() []string {
	if configured := os.Getenv("DROID_SESSIONS_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".factory", "sessions")}
}

func kiloPaths() []string {
	if configured := os.Getenv("KILO_DATA_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "share", "kilo")}
}

func hermesPaths() []string {
	if configured := os.Getenv("HERMES_HOME"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".hermes")}
}

func codebuffPaths() []string {
	if configured := os.Getenv("CODEBUFF_DATA_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".config", "manicode"),
		filepath.Join(home, ".config", "manicode-dev"),
		filepath.Join(home, ".config", "manicode-staging"),
	}
}

func openCodePaths() []string {
	if configured := os.Getenv("OPENCODE_DATA_DIR"); configured != "" {
		return splitConfiguredDirs(configured, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "share", "opencode")}
}

func goosePaths() []string {
	if configured := os.Getenv("GOOSE_PATH_ROOT"); strings.TrimSpace(configured) != "" {
		paths := splitConfiguredDirs(configured, false)
		for index, path := range paths {
			paths[index] = filepath.Join(path, "data", "sessions", "sessions.db")
		}
		return paths
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "share", "goose", "sessions", "sessions.db"),
		filepath.Join(home, "Library", "Application Support", "goose", "sessions", "sessions.db"),
		filepath.Join(home, ".local", "share", "Block", "goose", "sessions", "sessions.db"),
	}
}

func splitConfiguredDirs(value string, trimProjects bool) []string {
	rawParts := strings.Split(value, ",")
	paths := make([]string, 0, len(rawParts))
	for _, raw := range rawParts {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if trimProjects && filepath.Base(path) == "projects" {
			path = filepath.Dir(path)
		}
		paths = append(paths, path)
	}
	return paths
}

func firstExistingPath(paths []string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func existingWatchDir(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return path
	}
	dir := filepath.Dir(path)
	if dirInfo, err := os.Stat(dir); err == nil && dirInfo.IsDir() {
		return dir
	}
	return ""
}
