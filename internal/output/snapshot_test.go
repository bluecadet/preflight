package output

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTextRendererNewEventTypes_Snapshots(t *testing.T) {
	t.Parallel()

	for _, tc := range newEventSnapshotCases() {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := NewTextRendererWithOptions(&buf, tc.opts)
			for _, event := range tc.events {
				r.Emit(event)
			}
			r.Close()

			assertSnapshot(t, snapshotPath("text", tc.name), normalizeSnapshot(buf.String()))
		})
	}
}

type snapshotCase struct {
	name   string
	opts   Options
	events []Event
}

func newEventSnapshotCases() []snapshotCase {
	return []snapshotCase{
		{
			name: "run-with-one-ok-task",
			opts: Options{Mode: "apply", RunDir: "/var/log/preflight/run-20250301-120000"},
			events: []Event{
				RunStartEvent{
					Mode:         "apply",
					PlaybookPath: "kiosk-provision.yml",
					PlaybookName: "kiosk-provision",
					Targets:      []string{"kiosk-01"},
					DryRun:       false,
				},
				TargetStartEvent{
					Target:    "kiosk-01",
					Transport: "local",
				},
				TaskStartedEvent{
					Target:   "kiosk-01",
					TaskID:   "install-drivers",
					TaskName: "install display drivers",
					Module:   "command",
				},
				TaskOutputEvent{
					Target:   "kiosk-01",
					TaskID:   "install-drivers",
					TaskName: "install display drivers",
					Lines:    []string{"Installing driver version 2.14"},
				},
				TaskOKEvent{
					Target:    "kiosk-01",
					TaskID:    "install-drivers",
					TaskName:  "install display drivers",
					ElapsedMs: 500,
				},
				TargetCompleteEvent{
					Target:       "kiosk-01",
					Outcome:      "ok",
					OKCount:      1,
					ChangedCount: 0,
					FailedCount:  0,
					SkippedCount: 0,
					ElapsedMs:    5000,
				},
				RunSummaryEvent{
					Status:        "success",
					OKCount:       1,
					ElapsedMs:     5000,
					TargetTallies: TargetCounts{OK: 1},
				},
			},
		},
		{
			name: "run-with-failed-task",
			opts: Options{Mode: "apply", RunDir: "/var/log/preflight/run-20250301-120001"},
			events: []Event{
				RunStartEvent{
					Mode:         "apply",
					PlaybookPath: "app-deploy.yml",
					PlaybookName: "app-deploy",
					Targets:      []string{"app-01", "app-02"},
					DryRun:       false,
				},
				TargetStartEvent{
					Target:    "app-01",
					Transport: "winrm",
					Address:   "192.168.1.10",
				},
				TargetStartEvent{
					Target:    "app-02",
					Transport: "ssh",
					Address:   "192.168.1.20",
				},
				TaskStartedEvent{
					Target:     "app-01",
					TaskID:     "install-runtime",
					TaskName:   "install .NET runtime",
					Module:     "command",
					ActionPath: "apps/deploy",
				},
				TaskFailedEvent{
					Target:      "app-01",
					TaskID:      "install-runtime",
					TaskName:    "install .NET runtime",
					ActionPath:  "apps/deploy",
					ExitCode:    1,
					ElapsedMs:   15000,
					FailMessage: "DISM failure: 0x800f0954",
					Output:      []string{"DISM.exe /Online /Add-Capability", "Error: source files not found"},
				},
				TargetCompleteEvent{
					Target:       "app-01",
					Outcome:      "failed",
					OKCount:      0,
					ChangedCount: 0,
					FailedCount:  1,
					SkippedCount: 0,
					ElapsedMs:    16000,
				},
				RunSummaryEvent{
					Status:        "failed",
					OKCount:       0,
					ChangedCount:  0,
					FailedCount:   1,
					SkippedCount:  0,
					ElapsedMs:     16000,
					TargetTallies: TargetCounts{Failed: 1},
				},
			},
		},
		{
			name: "verbose-run",
			opts: Options{Verbose: true, Mode: "apply"},
			events: []Event{
				RunStartEvent{
					Mode:         "apply",
					PlaybookPath: "kiosk-provision.yml",
					PlaybookName: "kiosk-provision",
					Targets:      []string{"kiosk-01"},
				},
				TargetStartEvent{
					Target:    "kiosk-01",
					Transport: "local",
				},
				TaskStartedEvent{
					Target:   "kiosk-01",
					TaskID:   "configure",
					TaskName: "configure autologin",
					Module:   "registry",
				},
				TaskOutputEvent{
					Target:   "kiosk-01",
					TaskID:   "configure",
					TaskName: "configure autologin",
					Lines:    []string{"Setting HKLM\\\\SOFTWARE\\\\Microsoft\\\\Windows NT\\\\CurrentVersion\\\\Winlogon\\\\AutoAdminLogon"},
				},
				TaskChangedEvent{
					Target:    "kiosk-01",
					TaskID:    "configure",
					TaskName:  "configure autologin",
					ElapsedMs: 200,
				},
				TargetCompleteEvent{
					Target:       "kiosk-01",
					Outcome:      "ok",
					OKCount:      1,
					ChangedCount: 1,
					FailedCount:  0,
					SkippedCount: 0,
					ElapsedMs:    3000,
				},
				RunSummaryEvent{
					Status:        "success",
					OKCount:       1,
					ChangedCount:  1,
					ElapsedMs:     3000,
					TargetTallies: TargetCounts{OK: 1},
				},
			},
		},
		{
			name: "two-targets-mixed-results",
			opts: Options{Mode: "apply"},
			events: []Event{
				RunStartEvent{
					Mode:         "apply",
					PlaybookPath: "deploy.yml",
					PlaybookName: "deploy",
					Targets:      []string{"host-a", "host-b"},
				},
				TargetStartEvent{
					Target:    "host-a",
					Transport: "ssh",
					Address:   "10.0.1.1",
				},
				TargetStartEvent{
					Target:    "host-b",
					Transport: "winrm",
					Address:   "10.0.1.2",
				},
				TaskStartedEvent{
					Target:   "host-a",
					TaskID:   "sync",
					TaskName: "sync artifacts",
				},
				TaskOKEvent{
					Target:    "host-a",
					TaskID:    "sync",
					TaskName:  "sync artifacts",
					ElapsedMs: 800,
				},
				TaskStartedEvent{
					Target:   "host-b",
					TaskID:   "sync",
					TaskName: "sync artifacts",
				},
				TaskSkippedEvent{
					Target:   "host-b",
					TaskID:   "sync",
					TaskName: "sync artifacts",
					Reason:   "when-condition-false",
				},
				TargetCompleteEvent{
					Target:       "host-a",
					Outcome:      "ok",
					OKCount:      1,
					ChangedCount: 0,
					FailedCount:  0,
					SkippedCount: 0,
					ElapsedMs:    1000,
				},
				TargetCompleteEvent{
					Target:       "host-b",
					Outcome:      "ok",
					OKCount:      0,
					ChangedCount: 0,
					FailedCount:  0,
					SkippedCount: 1,
					ElapsedMs:    100,
				},
				RunSummaryEvent{
					Status:        "success",
					OKCount:       1,
					ChangedCount:  0,
					FailedCount:   0,
					SkippedCount:  1,
					ElapsedMs:     1100,
					TargetTallies: TargetCounts{OK: 2},
				},
			},
		},
	}
}

func snapshotPath(prefix, name string) string {
	return filepath.Join("testdata", prefix+"-"+name+".golden")
}

func assertSnapshot(t *testing.T, path, got string) {
	t.Helper()

	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", path, err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if diff := compareSnapshot(string(want), got); diff != "" {
		t.Fatalf("snapshot mismatch for %s:\n%s", path, diff)
	}
}

func compareSnapshot(want, got string) string {
	if want == got {
		return ""
	}
	return "want:\n" + want + "\n\ngot:\n" + got
}

func normalizeSnapshot(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

func collectPrintedBlocks(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	return collectPrintedMessages(cmd())
}

func collectPrintedMessages(msg tea.Msg) []string {
	if msg == nil {
		return nil
	}

	if m, ok := msg.(tea.BatchMsg); ok {
		var blocks []string
		for _, cmd := range m {
			blocks = append(blocks, collectPrintedBlocks(cmd)...)
		}
		return blocks
	}

	rv := reflect.ValueOf(msg)
	if !rv.IsValid() {
		return nil
	}

	rt := rv.Type()
	if rt.PkgPath() == "github.com/charmbracelet/bubbletea" && rt.Name() == "printLineMessage" {
		field := rv.FieldByName("messageBody")
		if field.IsValid() && field.Kind() == reflect.String {
			return []string{field.String()}
		}
		return nil
	}

	if rt.PkgPath() == "github.com/charmbracelet/bubbletea" && rt.Name() == "sequenceMsg" && rv.Kind() == reflect.Slice {
		var blocks []string
		for i := 0; i < rv.Len(); i++ {
			if cmd, ok := rv.Index(i).Interface().(tea.Cmd); ok {
				blocks = append(blocks, collectPrintedBlocks(cmd)...)
			}
		}
		return blocks
	}

	return nil
}
