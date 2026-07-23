// Package appupdate keeps the Windows app current against the Tokitoki
// server. Check asks whether a newer build exists; Install downloads it next
// to the running executable, verifies the published digest, and swaps the
// binary in place. Windows never lets a running executable be overwritten,
// but it does let it be renamed — so the old binary steps aside as ".old"
// and the new one takes its path, exactly as the CLI's self-update does.
package appupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Channel is the release channel the Windows app reads on the Tokitoki server.
const Channel = "windows"

const (
	checkTimeout    = 15 * time.Second
	downloadTimeout = 10 * time.Minute

	// oldSuffix marks the previous binary, which the running process cannot
	// delete but can rename aside. The leftover is removed on the next app
	// start or the next install, whichever comes first.
	oldSuffix = ".old"
)

// ErrDevBuild marks a local build, which has no meaningful version to compare.
var ErrDevBuild = errors.New("development build; update checks are disabled")

// semverRE accepts what the server's update check accepts.
var semverRE = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+`)

// Update describes an available newer build.
type Update struct {
	// Version is the offered release version.
	Version string
	// DownloadURL is the absolute URL the build can be fetched from.
	DownloadURL string
	// Size is the asset's byte count; 0 when the server did not publish one.
	Size int64
	// SHA256 is the asset's hex digest. Present whenever the release
	// published one; the download is rejected if it does not match.
	SHA256 string
}

// Check returns the available update, or nil when current is already the
// newest published build.
func Check(ctx context.Context, baseURL, current string) (*Update, error) {
	if !semverRE.MatchString(current) {
		return nil, ErrDevBuild
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	checkURL := fmt.Sprintf(
		"%s/api/updates/check?channel=%s&platform=windows&arch=%s&version=%s",
		baseURL, Channel, runtime.GOARCH, url.QueryEscape(current),
	)
	response, err := httpGet(ctx, checkURL)
	if err != nil {
		return nil, fmt.Errorf("check for update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check for update: server returned %s", response.Status)
	}

	var decoded struct {
		Available bool   `json:"available"`
		Version   string `json:"version"`
		URL       string `json:"url"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("check for update: %w", err)
	}
	if !decoded.Available {
		return nil, nil
	}
	if decoded.URL == "" {
		return nil, fmt.Errorf("check for update: update %s has no download URL", decoded.Version)
	}
	return &Update{
		Version:     decoded.Version,
		DownloadURL: baseURL + decoded.URL,
		Size:        decoded.Size,
		SHA256:      decoded.SHA256,
	}, nil
}

// Install downloads the update and replaces the running executable. On
// return the path holds the new build: the next launch runs it, whether the
// user restarts now or not. The running process keeps executing the old
// image either way.
func Install(ctx context.Context, update *Update) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	return install(ctx, update, executable)
}

// install is Install with the executable made explicit, so tests can point
// it at a scratch file.
func install(ctx context.Context, update *Update, executable string) error {
	// The downloaded bytes become the next thing this machine executes.
	// Fetching them over cleartext HTTP hands that to whoever sits on the
	// path — loopback is the only exception, because it never leaves the
	// machine.
	if err := requireTrustedTransport(update.DownloadURL); err != nil {
		return err
	}

	// A leftover from a previous swap; gone unless that process still runs.
	_ = os.Remove(executable + oldSuffix)

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	tmp, err := download(ctx, update, filepath.Dir(executable))
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	return swap(tmp, executable)
}

// download streams the new binary into a temp file inside dir — the
// executable's own directory, so the final rename never crosses volumes.
func download(ctx context.Context, update *Update, dir string) (string, error) {
	response, err := httpGet(ctx, update.DownloadURL)
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download update: server returned %s", response.Status)
	}

	file, err := os.CreateTemp(dir, ".tokitoki-update-*")
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), response.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil && update.Size > 0 && written != update.Size {
		err = fmt.Errorf("got %d bytes, expected %d", written, update.Size)
	}
	// The digest ties the bytes on disk to the bytes the release was
	// published with, before anything executes them. Releases without a
	// published digest fall back to the size check.
	if err == nil && update.SHA256 != "" {
		got := hex.EncodeToString(digest.Sum(nil))
		want := strings.ToLower(strings.TrimPrefix(update.SHA256, "sha256:"))
		if got != want {
			err = fmt.Errorf("sha256 mismatch: downloaded %s, server published %s", got, want)
		}
	}
	if err == nil {
		err = os.Chmod(file.Name(), 0o755)
	}
	if err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("download update: %w", err)
	}
	return file.Name(), nil
}

// swap installs the new binary over the old one: the old binary is renamed
// aside, the new one renamed onto its path. Both renames are atomic, and a
// failed second rename puts the old binary back — the executable path never
// ends up empty or torn.
func swap(newBinary, executable string) error {
	old := executable + oldSuffix
	if err := os.Rename(executable, old); err != nil {
		return fmt.Errorf("install update: %w", err)
	}
	if err := os.Rename(newBinary, executable); err != nil {
		_ = os.Rename(old, executable)
		return fmt.Errorf("install update: %w", err)
	}
	return nil
}

// requireTrustedTransport rejects download URLs an on-path attacker can
// rewrite.
func requireTrustedTransport(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("update download URL %q: %w", rawURL, err)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to download an update over %q: a non-loopback update server must use https", rawURL)
}

// CleanupLeftovers removes the ".old" binary a previous update left beside
// the executable. Failure is normal — the previous process may still be
// exiting — and the next install retries anyway.
func CleanupLeftovers() {
	if executable, err := os.Executable(); err == nil {
		_ = os.Remove(executable + oldSuffix)
	}
}

// Relaunch starts a fresh process from executable. The caller must release
// the single-instance lock first, or the new process sees a live instance
// and quits.
func Relaunch(executable string) error {
	return exec.Command(executable).Start()
}

func httpGet(ctx context.Context, target string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "tokitoki-windows")
	return http.DefaultClient.Do(request)
}
