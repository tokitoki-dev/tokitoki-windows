package agentcli

import (
	"reflect"
	"testing"
)

func TestBaseURLConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "defaults to shared server", want: "https://tokitoki.dev"},
		{name: "uses environment override", value: "http://localhost:9093", want: "http://localhost:9093"},
		{name: "trims trailing slash", value: "http://localhost:9093/", want: "http://localhost:9093"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TOKITOKI_BASE_URL", tt.value)
			if got := BaseURL(); got != tt.want {
				t.Fatalf("BaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSyncArgsRendersSortedProviderFlags(t *testing.T) {
	args := SyncArgs(map[string][]string{
		"codex":  {`C:\data\codex`},
		"claude": {`C:\data\claude`},
	})
	want := []string{
		"--provider-dir", `claude=C:\data\claude`,
		"--provider-dir", `codex=C:\data\codex`,
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("SyncArgs() = %v, want %v", args, want)
	}
}

func TestSyncArgsEmptyMeansNothingToScan(t *testing.T) {
	if args := SyncArgs(nil); args != nil {
		t.Fatalf("SyncArgs(nil) = %v, want nil", args)
	}
	if args := SyncArgs(map[string][]string{"claude": {""}}); args != nil {
		t.Fatalf("SyncArgs(empty dir) = %v, want nil", args)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		raw  string
		want []int
	}{
		{raw: "0.1.6\n", want: []int{0, 1, 6}},
		{raw: "v1.2.3", want: []int{1, 2, 3}},
		{raw: "1.2.3-rc1", want: []int{1, 2, 3}},
		{raw: "dev", want: nil},
		{raw: "", want: nil},
		{raw: "1.2", want: nil},
	}
	for _, tt := range tests {
		if got := parseVersion(tt.raw); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("parseVersion(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

func TestVersionLess(t *testing.T) {
	if !versionLess([]int{0, 1, 5}, []int{0, 1, 6}) {
		t.Fatal("0.1.5 should precede 0.1.6")
	}
	if versionLess([]int{0, 1, 6}, []int{0, 1, 6}) {
		t.Fatal("equal versions should not precede each other")
	}
	if versionLess([]int{1, 0, 0}, []int{0, 9, 9}) {
		t.Fatal("1.0.0 should not precede 0.9.9")
	}
}

func TestRequireOK(t *testing.T) {
	if err := requireOK([]byte(`{"ok":true}` + "\n")); err != nil {
		t.Fatalf("requireOK(ok) = %v", err)
	}
	if err := requireOK([]byte(`{"ok":false}`)); err == nil {
		t.Fatal("requireOK(ok=false) should fail")
	}
	if err := requireOK([]byte("not json")); err == nil {
		t.Fatal("requireOK(garbage) should fail")
	}
}
