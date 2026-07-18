package version

import "testing"

func TestSummary(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
	})

	tests := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{
			name:    "release omits commit",
			version: "1.0.0",
			commit:  "abc1234",
			want:    "Version 1.0.0",
		},
		{
			name:    "development version",
			version: "dev",
			commit:  "local",
			want:    "Version dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			Commit = tt.commit

			if got := Summary(); got != tt.want {
				t.Fatalf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}
