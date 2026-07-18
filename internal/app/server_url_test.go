package app

import (
	"testing"

	"github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib"
)

func TestBaseURLConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name: "defaults to shared server",
			want: "https://tokitoki.dev",
		},
		{
			name:  "uses environment override",
			value: "http://localhost:9093",
			want:  "http://localhost:9093",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TOKITOKI_BASE_URL", tt.value)

			if got := agentlib.BaseURL(); got != tt.want {
				t.Fatalf("BaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
