package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunPlaybookUsesInventoryTargets(t *testing.T) {
	playbookPath, inventoryPath := writeTestPlaybookWithInventory(t)

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
			if err := cmd.Flags().Set("target", "lab"); err != nil {
				t.Fatalf("Set target: %v", err)
			}
			if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
				t.Fatalf("Set inventory: %v", err)
			}

			var stdout bytes.Buffer
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("Pipe: %v", err)
			}
			os.Stdout = w
			defer func() { os.Stdout = oldStdout }()

			done := make(chan struct{})
			go func() {
				_, _ = stdout.ReadFrom(r)
				close(done)
			}()

			err = tc.run(cmd, []string{playbookPath})
			_ = w.Close()
			<-done
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.name == "plan" {
				out := stdout.String()
				if !strings.Contains(out, "Target: kiosk-a") || !strings.Contains(out, "Target: kiosk-b") {
					t.Fatalf("expected per-target plan output, got %q", out)
				}
			}
		})
	}
}

func TestRunFactsWithInventoryMultipleHostsReturnsMap(t *testing.T) {
	_, inventoryPath := writeTestPlaybookWithInventory(t)
	cmd := newTestCommand()
	if err := cmd.Flags().Set("target", "lab"); err != nil {
		t.Fatalf("Set target: %v", err)
	}
	if err := cmd.Flags().Set("inventory", inventoryPath); err != nil {
		t.Fatalf("Set inventory: %v", err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	var stdout bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = stdout.ReadFrom(r)
		close(done)
	}()

	if err := runFacts(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = w.Close()
	<-done

	out := stdout.String()
	if !strings.Contains(out, "\"kiosk-a\"") || !strings.Contains(out, "\"kiosk-b\"") {
		t.Fatalf("expected multi-host facts map, got %q", out)
	}
}

func TestRunPlanAllowsConfiguredConcurrency(t *testing.T) {
	cmd := newTestCommand()
	if err := cmd.Flags().Set("concurrency", "2"); err != nil {
		t.Fatalf("Set concurrency: %v", err)
	}

	err := runPlan(cmd, []string{writeTestPlaybook(t)})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
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
	cmd.Flags().String("inventory", "", "")
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

func writeTestPlaybookWithInventory(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	playbookPath := filepath.Join(dir, "playbook.yml")
	inventoryPath := filepath.Join(dir, "inventory.yml")

	if err := os.WriteFile(playbookPath, []byte(`
name: test
tasks:
  - name: echo
    shell:
      cmd: echo
      args: ["hello"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", playbookPath, err)
	}

	if err := os.WriteFile(inventoryPath, []byte(`
groups:
  lab:
    hosts:
      - name: kiosk-a
        transport: local
      - name: kiosk-b
        transport: local
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", inventoryPath, err)
	}

	return playbookPath, inventoryPath
}
