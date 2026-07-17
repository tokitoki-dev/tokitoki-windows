package datadirs

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib"
)

// setHome fakes the home directory for os.UserHomeDir, which reads
// USERPROFILE on Windows and HOME everywhere else. Setting only HOME makes
// these tests pass on macOS and silently probe the real home on Windows.
func setHome(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	} else {
		t.Setenv("HOME", dir)
	}
}

func TestResolveUsesExistingProviderDirs(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	claude := filepath.Join(home, ".claude")
	if err := mkdir(claude); err != nil {
		t.Fatal(err)
	}

	got := Resolve([]string{"claude", "codex"})
	if dirs := got.ProviderDirs[agentlib.ProviderClaude]; len(dirs) != 1 || dirs[0] != claude {
		t.Fatalf("claude dirs = %v, want %q", dirs, claude)
	}
	if dirs := got.ProviderDirs[agentlib.ProviderCodex]; len(dirs) != 0 {
		t.Fatalf("codex dirs = %v, want empty missing dir", dirs)
	}
}

func TestWatchPathsDedupesAndSorts(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	claude := filepath.Join(home, ".claude")
	codex := filepath.Join(home, ".codex")
	if err := mkdir(codex); err != nil {
		t.Fatal(err)
	}
	if err := mkdir(claude); err != nil {
		t.Fatal(err)
	}

	got := WatchPaths([]string{"codex", "claude", "codex"})
	want := []string{claude, codex}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchPaths() = %v, want %v", got, want)
	}
}

func TestClaudeConfigDirTrimsProjects(t *testing.T) {
	root := t.TempDir()
	configured := filepath.Join(root, "projects")
	if err := mkdir(configured); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", configured)

	got := WatchPaths([]string{"claude"})
	if !reflect.DeepEqual(got, []string{root}) {
		t.Fatalf("WatchPaths() = %v, want root without projects", got)
	}
}

func TestCopilotExporterFilePathIsResolvedAndWatchedByParent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "copilot.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COPILOT_OTEL_FILE_EXPORTER_PATH", file)

	resolved := Resolve([]string{"copilot"})
	if dirs := resolved.ProviderDirs[agentlib.ProviderCopilot]; len(dirs) != 1 || dirs[0] != file {
		t.Fatalf("copilot dirs = %v, want exporter file", dirs)
	}
	if got := WatchPaths([]string{"copilot"}); !reflect.DeepEqual(got, []string{dir}) {
		t.Fatalf("WatchPaths() = %v, want exporter parent dir", got)
	}
}

func mkdir(path string) error {
	return os.MkdirAll(path, 0o700)
}
