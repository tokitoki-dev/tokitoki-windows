// Package agentcli resolves and invokes the shared Tokitoki CLI.
//
// Every Tokitoki front-end and editor plugin on a machine drives one shared
// copy of the `tokitoki` CLI; its location is a contract documented in
// tokitoki-cli/README.md, and AgentProcess.swift in the macOS app is the
// reference implementation of the seeding rules this package follows. The
// Windows app links none of the CLI's Go packages: each operation invokes the
// binary once and parses what it writes to standard output, so both apps and
// every plugin run exactly the same logic.
package agentcli

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// SyncTimeout bounds one sync invocation end to end — the CLI's own
	// upload timeout, mirrored here so a wedged child cannot outlive its
	// purpose.
	SyncTimeout = 2 * time.Minute

	// opTimeout bounds the short operations: reading or writing the key,
	// asking a version. Local file work behind one process spawn.
	opTimeout = 15 * time.Second

	// updateTimeout bounds `tokitoki update`, which may download a binary.
	updateTimeout = 5 * time.Minute
)

// ErrMissingAPIKey reports that no API key is configured. The CLI signals it
// with exit code 3, the one failure a front-end acts on.
var ErrMissingAPIKey = errors.New("API key is not configured")

// exitNoAPIKey is the CLI's documented exit code for a missing API key.
const exitNoAPIKey = 3

// BaseURL returns the Tokitoki server every subsystem talks to, honoring the
// same TOKITOKI_BASE_URL override the CLI reads — the app and the CLI it
// spawns must always agree on the server.
func BaseURL() string {
	value := strings.TrimRight(strings.TrimSpace(os.Getenv("TOKITOKI_BASE_URL")), "/")
	if value == "" {
		return "https://tokitoki.dev"
	}
	return value
}

// DataDir returns the shared Tokitoki data directory, creating it if needed.
// The CLI keeps the API key, database, and locks here; the app adds only its
// own settings file.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".tokitoki")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SharedBinary returns the fleet-wide CLI path: %USERPROFILE%\.tokitoki\bin\
// tokitoki.exe. The bin\ segment keeps executables apart from the data files
// in the directory above it.
func SharedBinary() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin", binaryName), nil
}

// SyncArgs renders provider directories as the CLI's sync invocation:
// repeated --provider-dir provider=dir flags in stable order. Nothing to
// scan renders as nil, which callers treat as "do not invoke".
func SyncArgs(providerDirs map[string][]string) []string {
	providers := make([]string, 0, len(providerDirs))
	for provider := range providerDirs {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	var args []string
	for _, provider := range providers {
		dirs := append([]string(nil), providerDirs[provider]...)
		sort.Strings(dirs)
		for _, dir := range dirs {
			if dir != "" {
				args = append(args, "--provider-dir", provider+"="+dir)
			}
		}
	}
	return args
}
