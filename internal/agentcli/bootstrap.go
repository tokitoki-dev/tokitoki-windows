package agentcli

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// The release build embeds the pinned CLI here, gzip-compressed — it more
// than halves the shipped executable, and the bytes are only ever needed
// when a seed actually happens (fetch-cli-release.ps1 puts the files in
// place; they are gitignored). A dev build embeds only the README and
// Bootstrap does nothing — it uses whatever shared CLI the machine has.
//
//go:embed all:embedded
var embeddedFS embed.FS

const (
	embeddedBinary  = "embedded/tokitoki.exe.gz"
	embeddedVersion = "embedded/VERSION"
)

// Bootstrap seeds the shared CLI from the embedded copy, following the
// shared-CLI contract in tokitoki-cli/README.md: resolve shared first, seed
// when it is missing or older, never a downgrade, never a download. The
// embedded build's version comes from the pin baked in at build time, so the
// common case — shared already current — costs one `version` exec of the
// shared binary and no extraction.
func Bootstrap(ctx context.Context, logger *slog.Logger) {
	data, err := embeddedFS.ReadFile(embeddedBinary)
	if err != nil {
		return // dev build: nothing bundled
	}
	bundled := embeddedVersionComponents()

	shared, err := SharedBinary()
	if err != nil {
		logger.Warn("resolve shared CLI", "error", err)
		return
	}

	if info, statErr := os.Stat(shared); statErr == nil && info.Mode().IsRegular() {
		// A bundle that cannot state its version never replaces a live CLI.
		if bundled == nil {
			return
		}
		if current := binaryVersion(ctx, shared); current != nil && !versionLess(current, bundled) {
			return // never a downgrade
		}
		// Shared is older — or cannot even report a version, in which case a
		// binary that works replaces one that does not.
	}

	if err := seed(data, shared); err != nil {
		logger.Warn("seed shared CLI", "error", err)
	}
}

// seed decompresses the embedded bytes, stages them next to the destination
// and renames them into place, so no concurrent invocation ever sees a
// half-written CLI. Decompression happens only here — the common startup
// path never touches the compressed payload.
func seed(compressed []byte, shared string) error {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return err
	}
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(shared), 0o700); err != nil {
		return err
	}
	staging := filepath.Join(filepath.Dir(shared), "."+binaryName+".seed")
	if err := os.WriteFile(staging, data, 0o755); err != nil {
		return err
	}
	if err := os.Rename(staging, shared); err != nil {
		_ = os.Remove(staging)
		return err
	}
	return nil
}

// embeddedVersionComponents reads the pin the build embedded alongside the
// binary. nil means an unversioned bundle.
func embeddedVersionComponents() []int {
	raw, err := embeddedFS.ReadFile(embeddedVersion)
	if err != nil {
		return nil
	}
	return parseVersion(string(raw))
}

// binaryVersion asks a CLI binary its version. nil means the binary cannot
// say: a dev build, an old CLI, or a broken file.
func binaryVersion(ctx context.Context, binary string) []int {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "version")
	hideConsole(cmd)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}
	return parseVersion(stdout.String())
}

// parseVersion turns "v1.2.3" or "1.2.3" into comparable components, taking
// the leading digits of each part the way the macOS app does. Anything else
// — "dev", two parts, empty — is nil.
func parseVersion(raw string) []int {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil
	}
	components := make([]int, 0, len(parts))
	for _, part := range parts {
		digits := part
		if cut := strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }); cut >= 0 {
			digits = part[:cut]
		}
		value, err := strconv.Atoi(digits)
		if err != nil {
			return nil
		}
		components = append(components, value)
	}
	return components
}

// versionLess reports whether a precedes b, component by component.
func versionLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
