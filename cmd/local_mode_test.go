package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunPlaybookRejectsRemoteTargets(t *testing.T) {
	playbookPath := writeTestPlaybook(t)

	tests := []struct {
		name string
		run  func(*cobra.Command, []string) error
	}{
		{name: "apply", run: runApply},
		{name: "check", run: runCheck},
		{name: "plan", run: runPlan},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newTestCommand()
			if err := cmd.Flags().Set("target", "remote-host"); err != nil {
				t.Fatalf("Set target: %v", err)
			}

			err := tc.run(cmd, []string{playbookPath})
			if err == nil {
				t.Fatal("expected remote target error, got nil")
			}
			if !strings.Contains(err.Error(), "local-only mode") {
				t.Fatalf("expected local-only error, got %v", err)
			}
		})
	}
}

func TestRunFactsRejectsRemoteTargetArgument(t *testing.T) {
	err := runFacts(newTestCommand(), []string{"remote-host"})
	if err == nil {
		t.Fatal("expected remote facts error, got nil")
	}
	if !strings.Contains(err.Error(), "local-only mode") {
		t.Fatalf("expected local-only facts error, got %v", err)
	}
}

func TestRunPlanRejectsUnsupportedConcurrency(t *testing.T) {
	cmd := newTestCommand()
	if err := cmd.Flags().Set("concurrency", "2"); err != nil {
		t.Fatalf("Set concurrency: %v", err)
	}

	err := runPlan(cmd, []string{writeTestPlaybook(t)})
	if err == nil {
		t.Fatal("expected concurrency error, got nil")
	}
	if !strings.Contains(err.Error(), "not supported in local-only mode") {
		t.Fatalf("expected unsupported concurrency error, got %v", err)
	}
}

func TestRunPlanTimeoutReturnsDeadlineExceeded(t *testing.T) {
	cmd := newTestCommand()
	if err := cmd.Flags().Set("timeout", "0s"); err != nil {
		t.Fatalf("Set timeout: %v", err)
	}

	err := runPlan(cmd, []string{writeTestPlaybook(t)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestRunPlaybookFetchPhaseReturnsNil(t *testing.T) {
	playbookPath := writeTestPlaybook(t)
	cmd := newTestCommand()
	if err := cmd.Flags().Set("phase", "fetch"); err != nil {
		t.Fatalf("Set phase: %v", err)
	}

	if err := runApply(cmd, []string{playbookPath}); err != nil {
		t.Fatalf("expected nil for fetch phase, got %v", err)
	}
}

func TestRunPlaybookStagePhaseReturnsNotImplemented(t *testing.T) {
	playbookPath := writeTestPlaybook(t)
	cmd := newTestCommand()
	if err := cmd.Flags().Set("phase", "stage"); err != nil {
		t.Fatalf("Set phase: %v", err)
	}

	err := runApply(cmd, []string{playbookPath})
	if err == nil {
		t.Fatal("expected stage error, got nil")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected not implemented error, got %v", err)
	}
}

func newTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringSliceP("target", "t", nil, "")
	cmd.Flags().StringArrayP("var", "e", nil, "")
	cmd.Flags().StringSlice("tags", nil, "")
	cmd.Flags().StringSlice("skip-tags", nil, "")
	cmd.Flags().Bool("check", false, "")
	cmd.Flags().Bool("diff", false, "")
	cmd.Flags().BoolP("verbose", "v", false, "")
	cmd.Flags().String("output", "text", "")
	cmd.Flags().Int("concurrency", 0, "")
	cmd.Flags().String("timeout", "", "")
	cmd.Flags().String("phase", "", "")
	cmd.Flags().String("state-file", "", "")
	cmd.SetContext(context.Background())
	return cmd
}

func writeTestPlaybook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(path, []byte(`
name: test
tasks:
  - name: echo
    shell:
      cmd: echo
      args: ["hello"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}
