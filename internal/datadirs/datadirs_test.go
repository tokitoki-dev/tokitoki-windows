package datadirs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveUsesExistingProviderDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claude := filepath.Join(home, ".claude")
	if err := mkdir(claude); err != nil {
		t.Fatal(err)
	}

	got := Resolve([]string{"claude", "codex"})
	if got.ClaudeDir != claude {
		t.Fatalf("ClaudeDir = %q, want %q", got.ClaudeDir, claude)
	}
	if got.CodexDir != "" {
		t.Fatalf("CodexDir = %q, want empty missing dir", got.CodexDir)
	}
}

func TestWatchPathsDedupesAndSorts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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

func mkdir(path string) error {
	return os.MkdirAll(path, 0o700)
}
