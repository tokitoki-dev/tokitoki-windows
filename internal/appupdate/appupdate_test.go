package appupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReturnsUpdateWithAbsoluteDownloadURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"available": true,
			"version":   "1.2.0",
			"url":       "/api/updates/download/windows/1.2.0/windows/amd64",
		})
	}))
	defer server.Close()

	update, err := Check(context.Background(), server.URL, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if update == nil {
		t.Fatal("Check() = nil, want update")
	}
	if update.Version != "1.2.0" {
		t.Fatalf("version = %q, want 1.2.0", update.Version)
	}
	want := server.URL + "/api/updates/download/windows/1.2.0/windows/amd64"
	if update.DownloadURL != want {
		t.Fatalf("download URL = %q, want %q", update.DownloadURL, want)
	}
	if gotPath == "" || !containsAll(gotPath, "channel=windows", "platform=windows", "version=1.0.0") {
		t.Fatalf("request = %q, want channel/platform/version params", gotPath)
	}
}

func TestCheckReturnsNilWhenCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"available": false})
	}))
	defer server.Close()

	update, err := Check(context.Background(), server.URL, "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if update != nil {
		t.Fatalf("Check() = %+v, want nil", update)
	}
}

func TestCheckRejectsDevBuild(t *testing.T) {
	_, err := Check(context.Background(), "http://localhost:0", "dev")
	if !errors.Is(err, ErrDevBuild) {
		t.Fatalf("Check() error = %v, want ErrDevBuild", err)
	}
}

func TestCheckDecodesDigestAndSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"available": true,
			"version":   "1.2.0",
			"url":       "/dl",
			"size":      42,
			"sha256":    "abc123",
		})
	}))
	defer server.Close()

	update, err := Check(context.Background(), server.URL, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if update.Size != 42 || update.SHA256 != "abc123" {
		t.Fatalf("update = %+v, want size 42 and sha256 abc123", update)
	}
}

// serveBinary returns a server that hands out payload at /dl and an Update
// describing it truthfully.
func serveBinary(t *testing.T, payload []byte) (*httptest.Server, *Update) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dl" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256(payload)
	return server, &Update{
		Version:     "1.2.0",
		DownloadURL: server.URL + "/dl",
		Size:        int64(len(payload)),
		SHA256:      hex.EncodeToString(digest[:]),
	}
}

// scratchExecutable creates a stand-in for the running binary and returns
// its path.
func scratchExecutable(t *testing.T) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "app.exe")
	if err := os.WriteFile(executable, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	return executable
}

func TestInstallSwapsExecutable(t *testing.T) {
	payload := []byte("new build")
	_, update := serveBinary(t, payload)
	executable := scratchExecutable(t)

	if err := install(context.Background(), update, executable); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("executable holds %q, want %q", got, payload)
	}
	old, err := os.ReadFile(executable + oldSuffix)
	if err != nil {
		t.Fatalf("previous binary not kept aside: %v", err)
	}
	if string(old) != "old build" {
		t.Fatalf("%s holds %q, want the previous build", oldSuffix, old)
	}
}

func TestInstallRejectsDigestMismatch(t *testing.T) {
	_, update := serveBinary(t, []byte("new build"))
	update.SHA256 = strings.Repeat("0", 64)
	executable := scratchExecutable(t)

	err := install(context.Background(), update, executable)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("install() error = %v, want sha256 mismatch", err)
	}
	assertUntouched(t, executable)
}

func TestInstallRejectsTruncatedDownload(t *testing.T) {
	_, update := serveBinary(t, []byte("new build"))
	update.Size += 5
	update.SHA256 = ""
	executable := scratchExecutable(t)

	err := install(context.Background(), update, executable)
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("install() error = %v, want a size complaint", err)
	}
	assertUntouched(t, executable)
}

func TestInstallRefusesInsecureTransport(t *testing.T) {
	executable := scratchExecutable(t)
	update := &Update{Version: "1.2.0", DownloadURL: "http://updates.example.com/dl"}

	err := install(context.Background(), update, executable)
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("install() error = %v, want an https refusal", err)
	}
	assertUntouched(t, executable)
}

// assertUntouched verifies a failed install left the executable's directory
// exactly as it was: the old build in place, no temp files, no .old.
func assertUntouched(t *testing.T, executable string) {
	t.Helper()
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old build" {
		t.Fatalf("executable holds %q, want the old build untouched", got)
	}
	entries, err := os.ReadDir(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory has %d entries, want just the executable: %v", len(entries), entries)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
