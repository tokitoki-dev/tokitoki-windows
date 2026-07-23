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
	// The machine running the tests may point these anywhere; the fake home
	// must fully control what exists.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
}

func TestResolveUsesExistingProviderDirs(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	claude := filepath.Join(home, ".claude")
	if err := mkdir(claude); err != nil {
		t.Fatal(err)
	}

	got := Resolve()
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

	got := WatchPaths()
	want := []string{claude, codex}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchPaths() = %v, want %v", got, want)
	}
}

func TestClaudePathsIncludeXDGConfigDir(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	xdgClaude := filepath.Join(home, ".config", "claude")
	if err := mkdir(xdgClaude); err != nil {
		t.Fatal(err)
	}

	got := WatchPaths()
	if !reflect.DeepEqual(got, []string{xdgClaude}) {
		t.Fatalf("WatchPaths() = %v, want XDG claude dir", got)
	}
}

func TestClaudeConfigDirTrimsProjects(t *testing.T) {
	setHome(t, t.TempDir())
	root := t.TempDir()
	configured := filepath.Join(root, "projects")
	if err := mkdir(configured); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", configured)

	got := WatchPaths()
	if !reflect.DeepEqual(got, []string{root}) {
		t.Fatalf("WatchPaths() = %v, want root without projects", got)
	}
}

func TestCopilotExporterFilePathIsResolvedAndWatchedByParent(t *testing.T) {
	setHome(t, t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "copilot.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COPILOT_OTEL_FILE_EXPORTER_PATH", file)

	resolved := Resolve()
	if dirs := resolved.ProviderDirs[agentlib.ProviderCopilot]; len(dirs) != 1 || dirs[0] != file {
		t.Fatalf("copilot dirs = %v, want exporter file", dirs)
	}
	if got := WatchPaths(); !reflect.DeepEqual(got, []string{dir}) {
		t.Fatalf("WatchPaths() = %v, want exporter parent dir", got)
	}
}

func mkdir(path string) error {
	return os.MkdirAll(path, 0o700)
}
