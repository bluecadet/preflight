package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bluecadet/preflight/internal/runner"
	"github.com/bluecadet/preflight/internal/target"
)

func TestCommandTUISnapshots(t *testing.T) {
	tests := []struct {
		name      string
		run       func(*testing.T) string
		normalize func(string) string
	}{
		{
			name: "plan",
			run: func(t *testing.T) string {
				playbookPath := writeTestPlaybook(t)
				restore := chdirForTest(t, filepath.Dir(playbookPath))
				defer restore()

				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", "tui"); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runPlan(cmd, []string{filepath.Base(playbookPath)})
				})
				if err != nil {
					t.Fatalf("runPlan: %v", err)
				}
				return out
			},
			normalize: normalizeCommandSnapshot,
		},
		{
			name: "state-show",
			run: func(t *testing.T) string {
				dir := t.TempDir()
				restore := chdirForTest(t, dir)
				defer restore()

				statePath := filepath.Join(dir, "state.json")
				writeCommandStateFile(t, statePath)
				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", "tui"); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				if err := cmd.Flags().Set("state-file", filepath.Base(statePath)); err != nil {
					t.Fatalf("Set state-file: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runStateShow(cmd, nil)
				})
				if err != nil {
					t.Fatalf("runStateShow: %v", err)
				}
				return out
			},
			normalize: normalizeCommandSnapshot,
		},
		{
			name: "validate",
			run: func(t *testing.T) string {
				dir := t.TempDir()
				restore := chdirForTest(t, dir)
				defer restore()

				playbookPath := filepath.Join(dir, "playbook.yml")
				if err := os.WriteFile(playbookPath, []byte(`
name: lobby
tasks:
  - name: configure
    uses: preflight/windows-machine
  - name: quiet mode
    uses: preflight/windows-quiet-mode
`), 0o644); err != nil {
					t.Fatalf("WriteFile(%q): %v", playbookPath, err)
				}

				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", "tui"); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runValidate(cmd, []string{"playbook.yml"})
				})
				if err != nil {
					t.Fatalf("runValidate: %v", err)
				}
				return out
			},
			normalize: normalizeCommandSnapshot,
		},
		{
			name: "action-list",
			run: func(t *testing.T) string {
				dir := t.TempDir()
				restore := chdirForTest(t, dir)
				defer restore()

				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", "tui"); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runActionList(cmd, nil)
				})
				if err != nil {
					t.Fatalf("runActionList: %v", err)
				}
				return normalizePath(out, dir)
			},
			normalize: normalizeCommandSnapshot,
		},
		{
			name: "action-info",
			run: func(t *testing.T) string {
				cmd := newTestCommand()
				if err := cmd.Flags().Set("output", "tui"); err != nil {
					t.Fatalf("Set output: %v", err)
				}
				out, err := captureStdout(t, func() error {
					return runActionInfo(cmd, []string{"preflight/autologin"})
				})
				if err != nil {
					t.Fatalf("runActionInfo: %v", err)
				}
				return out
			},
			normalize: normalizeCommandSnapshot,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.run(t)
			assertCommandSnapshot(t, filepath.Join("testdata", "tui-"+tc.name+".golden"), tc.normalize(got))
		})
	}
}

func assertCommandSnapshot(t *testing.T, path, got string) {
	t.Helper()

	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("snapshot mismatch for %s:\nwant:\n%s\n\ngot:\n%s", path, string(want), got)
	}
}

func normalizeCommandSnapshot(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = cmdDurationRE.ReplaceAllString(s, "<elapsed>")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		line = cmdTopBorderRE.ReplaceAllString(line, "${1}────────────────────────${2}")
		line = cmdInnerRuleRE.ReplaceAllString(line, "│ ──────────────────────── │")
		if strings.HasPrefix(line, "│") && strings.HasSuffix(line, "│") {
			line = strings.TrimRight(strings.TrimSuffix(line, "│"), " ") + " │"
		}
		lines[i] = line
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

func normalizePath(s, path string) string {
	if path == "" {
		return s
	}
	s = strings.ReplaceAll(s, path, "<cwd>")
	return strings.ReplaceAll(s, "/private"+path, "<cwd>")
}

var (
	cmdDurationRE  = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ms|s|m|h)\b`)
	cmdTopBorderRE = regexp.MustCompile(`^([╭╰])─+([╮╯])$`)
	cmdInnerRuleRE = regexp.MustCompile(`^│\s*─{10,}\s*│$`)
)

func writeCommandStateFile(t *testing.T, path string) {
	t.Helper()

	state := &runner.State{
		LastApplied: time.Date(2026, 4, 9, 17, 0, 0, 0, time.UTC),
		Tasks: map[string]runner.TaskSnapshot{
			"echo": {
				TaskKey:   "echo",
				TaskName:  "echo",
				Module:    "shell",
				TaskHash:  "task-hash",
				ParamHash: "param-hash",
				Status:    target.StatusOK,
			},
		},
	}
	if err := state.Save(path); err != nil {
		t.Fatalf("Save state: %v", err)
	}
}
