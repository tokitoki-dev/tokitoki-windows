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
	ClaudeDir string
	CodexDir  string
}

// SyncOptions converts Directories into agentlib sync options.
func (d Directories) SyncOptions() agentlib.SyncOptions {
	return agentlib.SyncOptions{
		ClaudeDir: d.ClaudeDir,
		CodexDir:  d.CodexDir,
	}
}

// Resolve returns existing provider directories for providers.
func Resolve(providers []string) Directories {
	var dirs Directories
	if contains(providers, "claude") {
		dirs.ClaudeDir = firstExistingDir(claudePaths())
	}
	if contains(providers, "codex") {
		dirs.CodexDir = firstExistingDir(codexPaths())
	}
	return dirs
}

// WatchPaths returns existing directories to watch for providers.
func WatchPaths(providers []string) []string {
	seen := map[string]bool{}
	for _, provider := range providers {
		for _, path := range paths(provider) {
			if isExistingDir(path) {
				seen[path] = true
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

func firstExistingDir(paths []string) string {
	for _, path := range paths {
		if isExistingDir(path) {
			return path
		}
	}
	return ""
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
