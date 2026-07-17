// Package settings persists Windows client preferences.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib"
)

const fileName = "windows-settings.json"

var knownProviders = []string{
	"claude",
	"codex",
	"copilot",
	"gemini",
	"kimi",
	"qwen",
	"openclaw",
	"pi",
	"amp",
	"droid",
	"kilo",
	"hermes",
	"codebuff",
	"opencode",
	"goose",
}

// Settings contains Windows client preferences.
type Settings struct {
	EnabledProviders []string `json:"enabled_providers"`
}

// Store reads and writes settings under the shared TokiToki data directory.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore creates a settings store inside dataDir.
func NewStore(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, fileName)}
}

// Load returns persisted settings, falling back to defaults when no settings
// file exists.
func (s *Store) Load() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Settings{}, err
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	settings.EnabledProviders = NormalizeProviders(settings.EnabledProviders)
	if len(settings.EnabledProviders) == 0 {
		settings.EnabledProviders = Default().EnabledProviders
	}
	return settings, nil
}

// Save atomically persists settings.
func (s *Store) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings.EnabledProviders = NormalizeProviders(settings.EnabledProviders)
	if len(settings.EnabledProviders) == 0 {
		return errors.New("at least one provider must be enabled")
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".windows-settings.tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// Default returns the default Windows client settings.
func Default() Settings {
	return Settings{EnabledProviders: KnownProviders()}
}

// KnownProviders returns the canonical provider order.
func KnownProviders() []string {
	return append([]string{}, knownProviders...)
}

// NormalizeProviders removes unknown and duplicate providers while preserving
// the canonical provider order.
func NormalizeProviders(providers []string) []string {
	enabled := make(map[string]bool, len(providers))
	for _, provider := range providers {
		enabled[provider] = true
	}

	var normalized []string
	for _, provider := range knownProviders {
		if enabled[provider] {
			normalized = append(normalized, provider)
		}
	}
	return normalized
}

// DataDir returns the default shared TokiToki data directory.
func DataDir() (string, error) {
	return agentlib.DefaultDataDir()
}
