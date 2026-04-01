package cmd

import "testing"

func TestShouldAutoUseTUI(t *testing.T) {
	tests := []struct {
		commandPath string
		want        bool
	}{
		{commandPath: "preflight apply", want: true},
		{commandPath: "preflight state diff", want: true},
		{commandPath: "preflight inventory list", want: true},
		{commandPath: "preflight validate", want: false},
		{commandPath: "preflight facts", want: false},
		{commandPath: "preflight state show", want: false},
		{commandPath: "preflight action fetch", want: false},
	}

	for _, tt := range tests {
		if got := shouldAutoUseTUI(tt.commandPath); got != tt.want {
			t.Fatalf("shouldAutoUseTUI(%q) = %t, want %t", tt.commandPath, got, tt.want)
		}
	}
}
