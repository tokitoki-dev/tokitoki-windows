package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreDefaultsWhenFileMissing(t *testing.T) {
	store := NewStore(t.TempDir())

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackingDisabled {
		t.Fatal("TrackingDisabled = true, want default false")
	}
}

func TestStoreRoundTripsTrackingDisabled(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	if err := store.Save(Settings{TrackingDisabled: true}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.TrackingDisabled {
		t.Fatal("TrackingDisabled = false, want persisted true")
	}
	if filepath.Dir(store.path) != dir {
		t.Fatalf("store path = %q, want inside data dir", store.path)
	}

	if err := store.Save(Settings{}); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackingDisabled {
		t.Fatal("TrackingDisabled = true, want default false")
	}
}

func TestStoreIgnoresLegacyProviderList(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	legacy := []byte(`{"enabled_providers":["codex"],"tracking_disabled":true}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, fileName), legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.TrackingDisabled {
		t.Fatal("TrackingDisabled = false, want value from legacy file")
	}
}
