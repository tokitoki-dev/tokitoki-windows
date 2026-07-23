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

// Settings contains Windows client preferences. Provider selection does not
// exist — as on macOS, every known provider is always tracked — so the file
// carries only the tracking switch. Legacy files with an enabled_providers
// list still parse; the list is ignored.
type Settings struct {
	// TrackingDisabled pauses all monitoring and syncing. Stored inverted so
	// the zero value — and every settings file written before the field
	// existed — means "tracking on", the default.
	TrackingDisabled bool `json:"tracking_disabled,omitempty"`
}

// Store reads and writes settings under the shared Tokitoki data directory.
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
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// Save atomically persists settings.
func (s *Store) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// DataDir returns the default shared Tokitoki data directory.
func DataDir() (string, error) {
	return agentlib.DefaultDataDir()
}
