package output

import (
	"reflect"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// snapshotCase holds a named event sequence and renderer options for
// golden-snapshot tests. Both the text and TUI renderer tests consume
// the same fixtures from newEventSnapshotCases().
type snapshotCase struct {
	name   string
	opts   Options
	events []Event
}

// newEventSnapshotCases returns the shared set of event fixtures used
// by both the text and TUI snapshot tests. Every fixture is exercised
// against both surfaces.
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
					Address:   "[IP_ADDRESS]",
				},
				TargetStartEvent{
					Target:    "app-02",
					Transport: "ssh",
					Address:   "[IP_ADDRESS]",
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
					Address:   "[IP_ADDRESS]",
				},
				TargetStartEvent{
					Target:    "host-b",
					Transport: "winrm",
					Address:   "[IP_ADDRESS]",
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
		{
			// TUI-specific fixture: multi-target task-finished output
			// (stops before TargetComplete / RunSummary, matching the
			// original TestTUIModel_MultiTargetTaskFinished test).
			name: "two-targets-task-finished",
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
					Address:   "[IP_ADDRESS]",
				},
				TargetStartEvent{
					Target:    "host-b",
					Transport: "winrm",
					Address:   "[IP_ADDRESS]",
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
			},
		},
	}
}

// tuiRenderSnapshot runs a sequence of events through a TUI model and
// returns the concatenated scroll-region blocks (printed via tea.Println)
// as a normalized snapshot string.
func tuiRenderSnapshot(events []Event) string {
	savedS := S
	S = NewTUIStyles(DefaultPalette(), true)
	defer func() { S = savedS }()

	m := newTUIModelWithOptions(make(chan Event, len(events)), Options{Color: ColorAlways})
	m.width = 80

	var allBlocks []string
	for _, evt := range events {
		_, cmd := m.applyEvent(evt)
		allBlocks = append(allBlocks, collectPrintedBlocks(cmd)...)
	}
	return normalizeSnapshot(strings.Join(allBlocks, "\n"))
}

// collectPrintedBlocks extracts the message bodies from tea.Println commands.
// It handles BatchMsg, sequenceMsg, and printLineMessage reflection types.
func collectPrintedBlocks(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	return collectPrintedMessages(cmd())
}

// collectPrintedMessages recursively extracts message bodies from tea.Msg values.
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
