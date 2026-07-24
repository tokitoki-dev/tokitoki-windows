//go:build windows

package launch

import (
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// preserveRunValue saves the real "Tokitoki" Run entry and restores it when
// the test ends, so exercising the actual registry key leaves it untouched.
func preserveRunValue(t *testing.T) {
	t.Helper()
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open run key: %v", err)
	}
	saved, _, readErr := key.GetStringValue(valueName)
	key.Close()

	t.Cleanup(func() {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			t.Fatalf("restore run key: %v", err)
		}
		defer key.Close()
		if errors.Is(readErr, registry.ErrNotExist) {
			_ = key.DeleteValue(valueName)
			return
		}
		_ = key.SetStringValue(valueName, saved)
	})
}

func setRunValue(t *testing.T, value string) {
	t.Helper()
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if err := key.SetStringValue(valueName, value); err != nil {
		t.Fatal(err)
	}
}

func runValue(t *testing.T) (string, bool) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(valueName)
	if err != nil {
		return "", false
	}
	return value, true
}

func deleteRunValue(t *testing.T) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer key.Close()
	_ = key.DeleteValue(valueName)
}

func TestReconcileRepointsMovedEntry(t *testing.T) {
	preserveRunValue(t)
	setRunValue(t, quotePath(`C:\old\location\tokitoki-windows.exe`))

	if err := Reconcile(); err != nil {
		t.Fatal(err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := runValue(t)
	if !ok {
		t.Fatal("entry disappeared after reconcile")
	}
	if !strings.EqualFold(strings.Trim(got, `"`), exe) {
		t.Fatalf("entry = %q, want current exe %q", got, exe)
	}
	if !IsEnabled() {
		t.Fatal("IsEnabled() = false after reconcile, want true")
	}
}

func TestReconcileLeavesCurrentEntryAlone(t *testing.T) {
	preserveRunValue(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	setRunValue(t, quotePath(exe))

	if err := Reconcile(); err != nil {
		t.Fatal(err)
	}
	got, _ := runValue(t)
	if !strings.EqualFold(strings.Trim(got, `"`), exe) {
		t.Fatalf("entry = %q, want %q", got, exe)
	}
}

func TestSetEnabledStoresUnescapedPath(t *testing.T) {
	preserveRunValue(t)

	if err := SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := runValue(t)
	if !ok {
		t.Fatal("no entry after SetEnabled(true)")
	}
	if want := quotePath(exe); got != want {
		t.Fatalf("stored %q, want %q", got, want)
	}
	if strings.Contains(got, `\\`) {
		t.Fatalf("stored path has doubled backslashes: %q", got)
	}

	if err := SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, ok := runValue(t); ok {
		t.Fatal("entry remained after SetEnabled(false)")
	}
}

func TestReconcileIgnoresDisabledAutostart(t *testing.T) {
	preserveRunValue(t)
	deleteRunValue(t)

	if err := Reconcile(); err != nil {
		t.Fatal(err)
	}
	if _, ok := runValue(t); ok {
		t.Fatal("reconcile created an entry when autostart was off")
	}
	if IsEnabled() {
		t.Fatal("IsEnabled() = true with no entry")
	}
}
