package cmd

import (
	"io"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/output"
)

func TestGetOutputFormatHonorsExplicitText(t *testing.T) {
	oldDetect := detectOutputFormat
	detectOutputFormat = func(_ io.Writer) output.Format { return output.FormatTUI }
	defer func() { detectOutputFormat = oldDetect }()

	cmd := newTestCommand()
	if err := cmd.Flags().Set("output", "text"); err != nil {
		t.Fatalf("Set output: %v", err)
	}

	if got := getOutputFormat(cmd); got != output.FormatText {
		t.Fatalf("expected explicit text output, got %q", got)
	}
}

func TestGetOutputFormatUsesAutoDetectWhenUnset(t *testing.T) {
	oldDetect := detectOutputFormat
	detectOutputFormat = func(_ io.Writer) output.Format { return output.FormatTUI }
	defer func() { detectOutputFormat = oldDetect }()

	cmd := newTestCommand()
	if got := getOutputFormat(cmd); got != output.FormatTUI {
		t.Fatalf("expected autodetected tui output, got %q", got)
	}
}

func TestRendererBackedCommandsRenderTextAndTUI(t *testing.T) {
	requireWindowsLocal(t)
	playbookPath := writeTestPlaybook(t)

	tests := []struct {
		name        string
		run         func(*testing.T, string) string
		expectInOut string
	}{
		{
			name: "apply",
			run: func(t *testing.T, outFmt string) string {
				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", outFmt); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runApply(cmd, []string{playbookPath})
				})
				if err != nil {
					t.Fatalf("runApply: %v", err)
				}
				return out
			},
			expectInOut: "echo",
		},
		{
			name: "check",
			run: func(t *testing.T, outFmt string) string {
				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", outFmt); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runCheck(cmd, []string{playbookPath})
				})
				if err != nil {
					t.Fatalf("runCheck: %v", err)
				}
				return out
			},
			expectInOut: "echo",
		},
		{
			name: "plan",
			run: func(t *testing.T, outFmt string) string {
				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", outFmt); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runPlan(cmd, []string{playbookPath})
				})
				if err != nil {
					t.Fatalf("runPlan: %v", err)
				}
				return out
			},
			expectInOut: "echo",
		},
		{
			name: "facts",
			run: func(t *testing.T, outFmt string) string {
				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", outFmt); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runFacts(cmd, nil)
				})
				if err != nil {
					t.Fatalf("runFacts: %v", err)
				}
				return out
			},
			expectInOut: "localhost",
		},
	}

	for _, tc := range tests {
		for _, outFmt := range []string{"text", "tui"} {
			t.Run(tc.name+"_"+outFmt, func(t *testing.T) {
				out := tc.run(t, outFmt)
				if !strings.Contains(out, tc.expectInOut) {
					t.Fatalf("expected %q in output, got %q", tc.expectInOut, out)
				}
			})
		}
	}
}
