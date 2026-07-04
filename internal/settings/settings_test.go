package settings

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoreDefaultsWhenFileMissing(t *testing.T) {
	store := NewStore(t.TempDir())

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.EnabledProviders, []string{"claude", "codex"}) {
		t.Fatalf("EnabledProviders = %v, want defaults", got.EnabledProviders)
	}
}

func TestStoreSavesNormalizedProviders(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	err := store.Save(Settings{EnabledProviders: []string{"unknown", "codex", "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.EnabledProviders, []string{"codex"}) {
		t.Fatalf("EnabledProviders = %v, want normalized codex", got.EnabledProviders)
	}
	if filepath.Dir(store.path) != dir {
		t.Fatalf("store path = %q, want inside data dir", store.path)
	}
}

func TestStoreRejectsEmptyProviders(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.Save(Settings{}); err == nil {
		t.Fatal("Save() error = nil, want empty provider rejection")
	}
}
