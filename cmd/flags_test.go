package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// runCommandNames lists the run commands (apply, check, stage).
var runCommandNames = []string{"apply", "check", "stage"}

// reportSubCommandNames maps parent → subcommands that are report-type commands
// (render directly, no run dir, honor --output json).
var reportSubCommands = map[string][]string{
	"action":    {"list"},
	"plugin":    {"list"},
	"inventory": {"list"},
	"secret":    {"list"},
	"state":     {"show", "diff"},
}

// reportTopLevelNames are standalone report commands.
var reportTopLevelNames = []string{"validate", "plan", "facts"}

// expectedRunFlags are the output-controlling flags that should be present
// on every run command (apply/check/stage).
var expectedRunFlags = []struct {
	name  string
	short string
}{
	{name: "verbose", short: "v"},
	{name: "output", short: ""},
	{name: "max-fail-lines", short: ""},
	{name: "color", short: ""},
	{name: "no-color", short: ""},
	{name: "concurrency", short: ""},
	{name: "fail-fast", short: ""},
	{name: "run-id", short: ""},
	{name: "keep-runs", short: ""},
}

// expectedReportFlags are the output-controlling flags that should be present
// on report commands that don't need --concurrency or run-specific flags.
var expectedReportFlags = []struct {
	name  string
	short string
}{
	{name: "verbose", short: "v"},
	{name: "output", short: ""},
	{name: "color", short: ""},
	{name: "no-color", short: ""},
}

func TestRunCommandsHaveConsistentOutputFlags(t *testing.T) {
	for _, name := range runCommandNames {
		t.Run(name, func(t *testing.T) {
			cmd := findCommand(rootCmd, name)
			if cmd == nil {
				t.Fatalf("command %q not found", name)
			}
			for _, flag := range expectedRunFlags {
				f := cmd.Flags().Lookup(flag.name)
				if f == nil {
					t.Errorf("run command %q missing flag --%s", name, flag.name)
				}
			}
		})
	}
}

func TestReportTopLevelCommandsHaveOutputFlags(t *testing.T) {
	for _, name := range reportTopLevelNames {
		t.Run(name, func(t *testing.T) {
			cmd := findCommand(rootCmd, name)
			if cmd == nil {
				t.Fatalf("command %q not found", name)
			}
			for _, flag := range expectedReportFlags {
				f := cmd.Flags().Lookup(flag.name)
				if f == nil {
					t.Errorf("report command %q missing flag --%s", name, flag.name)
				}
			}
		})
	}
}

func TestReportSubCommandsHaveOutputFlags(t *testing.T) {
	for parentName, subNames := range reportSubCommands {
		parent := findCommand(rootCmd, parentName)
		if parent == nil {
			t.Fatalf("parent command %q not found", parentName)
		}
		for _, subName := range subNames {
			t.Run(parentName+"_"+subName, func(t *testing.T) {
				sub := findSubCommand(parent, subName)
				if sub == nil {
					t.Fatalf("subcommand %s %s not found", parentName, subName)
				}
				for _, flag := range expectedReportFlags {
					f := sub.Flags().Lookup(flag.name)
					if f == nil {
						t.Errorf("command %s %s missing flag --%s", parentName, subName, flag.name)
					}
				}
			})
		}
	}
}

func TestReportCommandCreatesNoRunDir(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	playbookPath := filepath.Join(dir, "playbook.yml")
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

	cmd := newTestCommand()
	if _, err := captureStdout(t, func() error {
		return runValidate(cmd, []string{playbookPath})
	}); err != nil {
		t.Fatalf("runValidate: %v", err)
	}

	runsDir := filepath.Join(dir, ".preflight", "runs")
	if entries, err := os.ReadDir(runsDir); err == nil {
		t.Fatalf("expected no .preflight/runs/ directory, found %d entries: %v", len(entries), entries)
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error reading .preflight/runs/: %v", err)
	}
}

func TestRunCommandCreatesRunDir(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	playbookPath := filepath.Join(dir, "playbook.yml")
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

	cmd := newTestCommand()
	if _, err := captureStdout(t, func() error {
		return runApply(cmd, []string{playbookPath})
	}); err != nil {
		t.Fatalf("runApply: %v", err)
	}

	runsDir := filepath.Join(dir, ".preflight", "runs")
	if _, err := os.Stat(runsDir); os.IsNotExist(err) {
		t.Fatal("expected .preflight/runs/ directory to exist after apply")
	} else if err != nil {
		t.Fatalf("stat .preflight/runs/: %v", err)
	}
}

func TestReportJSONOutput(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	playbookPath := filepath.Join(dir, "playbook.yml")
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

	cmd := newTestCommand()
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("Set output: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runValidate(cmd, []string{playbookPath})
	})
	if err != nil {
		t.Fatalf("runValidate with --output json: %v", err)
	}

	if len(out) == 0 {
		t.Fatal("expected JSON output, got empty string")
	}
	if out[0] != '{' {
		t.Fatalf("expected JSON object starting with '{', got %q", out[:1])
	}
}

func TestReportJSONOutputNoRunDir(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	playbookPath := filepath.Join(dir, "playbook.yml")
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

	cmd := newTestCommand()
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("Set output: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runValidate(cmd, []string{playbookPath})
	}); err != nil {
		t.Fatalf("runValidate: %v", err)
	}

	runsDir := filepath.Join(dir, ".preflight", "runs")
	if entries, err := os.ReadDir(runsDir); err == nil {
		t.Fatalf("expected no .preflight/runs/, found %d entries", len(entries))
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommandWithRunID(t *testing.T) {
	// Test that --run-id overrides the auto-generated run ID.
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	playbookPath := filepath.Join(dir, "playbook.yml")
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

	cmd := newTestCommand()
	if err := cmd.Flags().Set("run-id", "my-custom-run"); err != nil {
		t.Fatalf("Set run-id: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runApply(cmd, []string{playbookPath})
	}); err != nil {
		t.Fatalf("runApply: %v", err)
	}

	// Check that run dir uses the custom ID.
	runDir := filepath.Join(dir, ".preflight", "runs", "my-custom-run")
	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		t.Fatalf("expected run dir %q to exist", runDir)
	}
}

// findCommand performs a breadth-first search for a command by name.
func findCommand(root *cobra.Command, name string) *cobra.Command {
	queue := []*cobra.Command{root}
	for len(queue) > 0 {
		cmd := queue[0]
		queue = queue[1:]
		if cmd.Name() == name {
			return cmd
		}
		queue = append(queue, cmd.Commands()...)
	}
	return nil
}

// findSubCommand searches immediate subcommands for one matching name.
func findSubCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}
