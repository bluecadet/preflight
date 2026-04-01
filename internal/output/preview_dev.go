//go:build devtools

package output

import (
	"bytes"
	"io"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type PreviewScenario struct {
	Name        string
	Title       string
	Description string
	run         func(io.Writer, io.Reader) error
}

func PreviewScenarios() []PreviewScenario {
	return []PreviewScenario{
		newScreenPreviewScenario("run-single-loading", "Run: single host loading", "Transcript preview for one host mid-run with an active task and logs.", previewRunScreen("preview", "single host loading", previewRunSingleLoadingEvents())),
		newScreenPreviewScenario("run-single-failed", "Run: failed task", "Transcript preview for a failed task and related logs.", previewRunScreen("preview", "failed task", previewRunSingleFailedEvents())),
		newScreenPreviewScenario("run-multi-host", "Run: multi-host transcript", "Transcript preview for two hosts with mixed progress.", previewRunScreen("preview", "multi-host transcript", previewRunMultiHostEvents())),
		newScreenPreviewScenario("run-deep-playbook", "Run: deep playbook", "Transcript preview with many tasks and nested action-style names.", previewRunScreen("preview", "deep playbook transcript", previewRunDeepPlaybookEvents())),
		newScreenPreviewScenario("run-host-overflow", "Run: host overflow", "Transcript preview with many hosts and varied outcomes.", previewRunScreen("preview", "host overflow transcript", previewRunHostOverflowEvents())),
		newScreenPreviewScenario("screen-plan", "Screen: plan list", "Static list preview for plan output.", previewPlanScreen()),
		newScreenPreviewScenario("screen-plan-tabs", "Screen: plan tabs", "Multi-target plan view with host tabs and nested action-style task names.", previewPlanTabbedScreen()),
		newScreenPreviewScenario("screen-diff", "Screen: state diff", "Comparison-style screen with changed, new, and removed items.", previewDiffScreen()),
		newScreenPreviewScenario("screen-inventory", "Screen: inventory list", "Inventory list with multiple rows and expandable detail content.", previewInventoryScreen()),
		newScreenPreviewScenario("screen-facts-tabs", "Screen: facts tabs", "Document-style preview with multiple host tabs and structured fact output.", previewFactsTabbedScreen()),
		newScreenPreviewScenario("screen-action-info", "Screen: action info", "Document preview for a complex action with nested inputs and tasks.", previewActionInfoScreen()),
	}
}

func PreviewScenarioByName(name string) (PreviewScenario, bool) {
	for _, scenario := range PreviewScenarios() {
		if scenario.Name == name {
			return scenario, true
		}
	}
	return PreviewScenario{}, false
}

func PreviewScenarioNames() []string {
	scenarios := PreviewScenarios()
	names := make([]string, 0, len(scenarios))
	for _, scenario := range scenarios {
		names = append(names, scenario.Name)
	}
	slices.Sort(names)
	return names
}

func RunPreviewScenario(w io.Writer, input io.Reader, name string) error {
	scenario, ok := PreviewScenarioByName(name)
	if !ok {
		return ErrUnknownPreviewScenario{name: name}
	}
	return scenario.run(w, input)
}

type ErrUnknownPreviewScenario struct {
	name string
}

func (e ErrUnknownPreviewScenario) Error() string {
	return "unknown preview scenario: " + e.name
}

func runScreenPreview(w io.Writer, input io.Reader, screen Screen) error {
	model := newStaticScreenModel(screen)
	programOptions := []tea.ProgramOption{
		tea.WithOutput(w),
		tea.WithoutSignalHandler(),
	}
	if input != nil {
		programOptions = append(programOptions, tea.WithInput(input))
		programOptions = append(programOptions, tea.WithAltScreen())
	}
	_, err := tea.NewProgram(model, programOptions...).Run()
	return err
}

func newScreenPreviewScenario(name, title, description string, screen Screen) PreviewScenario {
	return PreviewScenario{
		Name:        name,
		Title:       title,
		Description: description,
		run: func(w io.Writer, input io.Reader) error {
			return runScreenPreview(w, input, screen)
		},
	}
}

func previewRunScreen(command, subject string, events []Event) Screen {
	var buf bytes.Buffer
	renderer := NewTextRendererWithOptions(&buf, Options{
		Command: command,
		Verbose: true,
	})
	for _, event := range events {
		renderer.Emit(event)
	}
	return Screen{
		Command: command,
		Subject: subject,
		Content: ScreenContent{
			Kind:     ScreenKindDocument,
			Document: strings.TrimRight(buf.String(), "\n"),
		},
	}
}

func previewRunSingleLoadingEvents() []Event {
	return []Event{
		{Type: EventPlayStart, PlayName: "kiosk-baseline", Target: "local"},
		{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
		{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
		{Type: EventPhaseStart, Target: "local", Phase: "apply"},
		{Type: EventPhaseEnd, Target: "local", Phase: "apply", Status: "ok", TaskTotal: 7},
		{Type: EventTaskStart, Target: "local", TaskID: "action-bootstrap", TaskName: "windows-machine > Prepare bootstrap directory", Module: "file", TaskTotal: 7},
		{Type: EventTaskResult, Target: "local", TaskID: "action-bootstrap", TaskName: "windows-machine > Prepare bootstrap directory", Module: "file", Status: "changed", Message: "directory created"},
		{Type: EventTaskStart, Target: "local", TaskID: "timezone", TaskName: "windows-machine > Set timezone", Module: "powershell", TaskTotal: 7},
		{Type: EventTaskResult, Target: "local", TaskID: "timezone", TaskName: "windows-machine > Set timezone", Module: "powershell", Status: "ok", Message: "already configured"},
		{Type: EventTaskStart, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", TaskTotal: 7},
		{Type: EventTaskLog, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", Stream: "stdout", Line: "Downloading package metadata..."},
		{Type: EventTaskLog, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", Stream: "stdout", Line: "Verifying installer hash..."},
		{Type: EventTaskLog, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", Stream: "stdout", Line: "Starting install..."},
		{Type: EventTaskStart, Target: "local", TaskID: "autologin", TaskName: "windows-machine > kiosk-session > Configure autologin", Module: "registry", TaskTotal: 7},
		{Type: EventTaskResult, Target: "local", TaskID: "autologin", TaskName: "windows-machine > kiosk-session > Configure autologin", Module: "registry", Status: "changed", Message: "change applied"},
	}
}

func previewRunSingleFailedEvents() []Event {
	return []Event{
		{Type: EventPlayStart, PlayName: "kiosk-baseline", Target: "local"},
		{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
		{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
		{Type: EventPhaseEnd, Target: "local", Phase: "apply", Status: "failed", TaskTotal: 5},
		{Type: EventTaskStart, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", TaskTotal: 5},
		{Type: EventTaskLog, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", Stream: "stdout", Line: "Found package Google.Chrome"},
		{Type: EventTaskLog, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", Stream: "stderr", Line: "Another installation is already in progress."},
		{Type: EventTaskLog, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", Stream: "stderr", Line: "HRESULT: 0x80070652"},
		{Type: EventTaskResult, Target: "local", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "shell", Status: "failed", Message: "exit code 1"},
		{Type: EventTaskStart, Target: "local", TaskID: "timezone", TaskName: "windows-machine > Set timezone", Module: "powershell", TaskTotal: 5},
		{Type: EventTaskResult, Target: "local", TaskID: "timezone", TaskName: "windows-machine > Set timezone", Module: "powershell", Status: "ok", Message: "already configured"},
		{Type: EventTaskStart, Target: "local", TaskID: "restart-shell", TaskName: "windows-machine > kiosk-session > Restart shell", Module: "shell", TaskTotal: 5},
		{Type: EventTaskResult, Target: "local", TaskID: "restart-shell", TaskName: "windows-machine > kiosk-session > Restart shell", Module: "shell", Status: "skipped", Message: "dependency failed"},
		{Type: EventPlayEnd, Target: "local", OKCount: 1, FailedCount: 1, SkippedCount: 1},
	}
}

func previewRunMultiHostEvents() []Event {
	return []Event{
		{Type: EventPlayStart, PlayName: "exhibit-rollout", Target: "kiosk-a"},
		{Type: EventPlayStart, PlayName: "exhibit-rollout", Target: "kiosk-b"},
		{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
		{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
		{Type: EventPhaseEnd, Target: "kiosk-a", Phase: "apply", Status: "ok", TaskTotal: 4},
		{Type: EventPhaseEnd, Target: "kiosk-b", Phase: "apply", Status: "ok", TaskTotal: 4},
		{Type: EventTaskStart, Target: "kiosk-a", TaskID: "copy-bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", TaskTotal: 4},
		{Type: EventTaskResult, Target: "kiosk-a", TaskID: "copy-bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", Status: "changed", Message: "uploaded archive"},
		{Type: EventTaskStart, Target: "kiosk-a", TaskID: "power-plan", TaskName: "windows-machine > Set power plan", Module: "power_plan", TaskTotal: 4},
		{Type: EventTaskResult, Target: "kiosk-a", TaskID: "power-plan", TaskName: "windows-machine > Set power plan", Module: "power_plan", Status: "ok", Message: "already configured"},
		{Type: EventTaskStart, Target: "kiosk-a", TaskID: "disable-update", TaskName: "windows-machine > Disable Windows Update", Module: "service", TaskTotal: 4},
		{Type: EventTaskResult, Target: "kiosk-a", TaskID: "disable-update", TaskName: "windows-machine > Disable Windows Update", Module: "service", Status: "changed", Message: "change applied"},
		{Type: EventPlayEnd, Target: "kiosk-a", OKCount: 1, ChangedCount: 2},
		{Type: EventTaskStart, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", TaskTotal: 4},
		{Type: EventTaskLog, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", Stream: "stdout", Line: "Uploading archive..."},
		{Type: EventTaskLog, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", Stream: "stdout", Line: "Extracting to C:\\Exhibit\\Content"},
		{Type: EventTaskResult, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", Status: "changed", Message: "bundle copied"},
		{Type: EventTaskStart, Target: "kiosk-b", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "winget_package", TaskTotal: 4},
		{Type: EventTaskLog, Target: "kiosk-b", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "winget_package", Stream: "stderr", Line: "Installer returned 1603"},
		{Type: EventTaskResult, Target: "kiosk-b", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "winget_package", Status: "failed", Message: "exit code 1"},
		{Type: EventTaskStart, Target: "kiosk-b", TaskID: "restart-shell", TaskName: "windows-machine > kiosk-session > Restart shell", Module: "shell", TaskTotal: 4},
		{Type: EventTaskResult, Target: "kiosk-b", TaskID: "restart-shell", TaskName: "windows-machine > kiosk-session > Restart shell", Module: "shell", Status: "skipped", Message: "dependency failed"},
		{Type: EventPlayEnd, Target: "kiosk-b", ChangedCount: 1, FailedCount: 1, SkippedCount: 1},
	}
}

func previewRunDeepPlaybookEvents() []Event {
	return []Event{
		{Type: EventPlayStart, PlayName: "museum-floor-kiosk", Target: "gallery-kiosk-01"},
		{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
		{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
		{Type: EventPhaseEnd, Target: "gallery-kiosk-01", Phase: "apply", Status: "ok", TaskTotal: 11},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "facts", TaskName: "windows-machine > gather > Collect machine facts", Module: "facts", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "facts", TaskName: "windows-machine > gather > Collect machine facts", Module: "facts", Status: "ok", Message: "cached"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "hostname", TaskName: "windows-machine > identity > Set computer name", Module: "hostname", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "hostname", TaskName: "windows-machine > identity > Set computer name", Module: "hostname", Status: "changed", Message: "restart required"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "timezone", TaskName: "windows-machine > baseline > Set timezone", Module: "powershell", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "timezone", TaskName: "windows-machine > baseline > Set timezone", Module: "powershell", Status: "ok", Message: "already configured"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "winget_package", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "chrome", TaskName: "windows-machine > browser > Install Chrome", Module: "winget_package", Status: "changed", Message: "installed 135.0"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "content-sync", TaskName: "signage-rollout > content > Expand exhibit bundle", Module: "archive", TaskTotal: 11},
		{Type: EventTaskLog, Target: "gallery-kiosk-01", TaskID: "content-sync", TaskName: "signage-rollout > content > Expand exhibit bundle", Module: "archive", Stream: "stdout", Line: "Expanding exhibit-bundle-2026-03-28.zip"},
		{Type: EventTaskLog, Target: "gallery-kiosk-01", TaskID: "content-sync", TaskName: "signage-rollout > content > Expand exhibit bundle", Module: "archive", Stream: "stdout", Line: "Writing files to C:\\Exhibit\\Content"},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "content-sync", TaskName: "signage-rollout > content > Expand exhibit bundle", Module: "archive", Status: "changed", Message: "49 files updated"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "lockscreen", TaskName: "windows-machine > kiosk-session > Disable lock screen", Module: "registry", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "lockscreen", TaskName: "windows-machine > kiosk-session > Disable lock screen", Module: "registry", Status: "ok", Message: "already configured"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", Status: "changed", Message: "shell set to signage-launcher.exe"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "telemetry", TaskName: "observability > telemetry > Upload baseline markers", Module: "shell", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "telemetry", TaskName: "observability > telemetry > Upload baseline markers", Module: "shell", Status: "skipped", Message: "offline mode"},
		{Type: EventTaskStart, Target: "gallery-kiosk-01", TaskID: "reboot", TaskName: "windows-machine > restart > Reboot if needed", Module: "reboot", TaskTotal: 11},
		{Type: EventTaskResult, Target: "gallery-kiosk-01", TaskID: "reboot", TaskName: "windows-machine > restart > Reboot if needed", Module: "reboot", Status: "changed", Message: "reboot scheduled"},
		{Type: EventPlayEnd, Target: "gallery-kiosk-01", OKCount: 3, ChangedCount: 5, SkippedCount: 1},
	}
}

func previewRunHostOverflowEvents() []Event {
	events := []Event{
		{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
		{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
	}
	hosts := []string{"east-a", "east-b", "east-c", "west-a", "west-b", "west-c"}
	for idx, host := range hosts {
		events = append(events, Event{Type: EventPlayStart, PlayName: "floor-rollout", Target: host})
		events = append(events, Event{Type: EventPhaseEnd, Target: host, Phase: "apply", Status: "ok", TaskTotal: 3})
		events = append(events, Event{Type: EventTaskStart, Target: host, TaskID: "bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", TaskTotal: 3})
		events = append(events, Event{Type: EventTaskResult, Target: host, TaskID: "bundle", TaskName: "signage-rollout > Copy content bundle", Module: "file", Status: "changed", Message: "archive copied"})
		events = append(events, Event{Type: EventTaskStart, Target: host, TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", TaskTotal: 3})
		if idx%3 == 0 {
			events = append(events, Event{Type: EventTaskResult, Target: host, TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", Status: "ok", Message: "already configured"})
			events = append(events, Event{Type: EventPlayEnd, Target: host, OKCount: 1, ChangedCount: 1})
			continue
		}
		if idx%3 == 1 {
			events = append(events, Event{Type: EventTaskLog, Target: host, TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", Stream: "stdout", Line: "Writing shell value under HKLM\\Software\\Microsoft\\Windows NT\\CurrentVersion\\Winlogon"})
			events = append(events, Event{Type: EventTaskResult, Target: host, TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", Status: "changed", Message: "shell updated"})
			events = append(events, Event{Type: EventPlayEnd, Target: host, ChangedCount: 2})
			continue
		}
		events = append(events, Event{Type: EventTaskLog, Target: host, TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", Stream: "stderr", Line: "Access denied while updating kiosk shell"})
		events = append(events, Event{Type: EventTaskResult, Target: host, TaskID: "shell", TaskName: "windows-machine > kiosk-session > Configure custom shell", Module: "registry", Status: "failed", Message: "permission error"})
		events = append(events, Event{Type: EventPlayEnd, Target: host, ChangedCount: 1, FailedCount: 1})
	}
	return events
}

func previewPlanScreen() Screen {
	return Screen{
		Command: "plan",
		Subject: "play: kiosk-baseline",
		Summary: []ScreenStat{
			{Label: "tasks", Value: "12", Tone: "info"},
			{Label: "target", Value: "local", Tone: "ok"},
		},
		Content: ScreenContent{
			Kind: ScreenKindList,
			Items: []ScreenItem{
				{Title: "windows-machine > baseline > Set timezone", Status: "ok", Subtitle: "powershell", Summary: "already configured", Meta: []string{"tags: baseline"}},
				{Title: "windows-machine > kiosk-session > Configure autologin", Status: "changed", Subtitle: "registry", Summary: "would change", Meta: []string{"tags: baseline, login"}},
				{Title: "windows-machine > kiosk-session > Disable lock screen", Status: "ok", Subtitle: "registry", Summary: "already configured"},
				{Title: "signage-rollout > content > Expand exhibit bundle", Status: "changed", Subtitle: "archive", Summary: "49 files would update"},
				{Title: "windows-machine > restart > Reboot if needed", Status: "skipped", Subtitle: "reboot", Summary: "when-condition-false", DetailTitle: "When", Detail: []ScreenLine{{Text: "facts.os.build > 0", Tone: "info"}}, AutoExpand: true},
			},
		},
	}
}

func previewPlanTabbedScreen() Screen {
	return Screen{
		Command: "plan",
		Subject: "play: exhibit-rollout",
		Summary: []ScreenStat{
			{Label: "targets", Value: "2", Tone: "info"},
			{Label: "phase", Value: "plan ready", Tone: "ok"},
		},
		Tabs: []ScreenTab{
			{
				Label:  "kiosk-a",
				Status: "complete",
				Meta:   "8 tasks",
				Content: ScreenContent{
					Kind: ScreenKindList,
					Summary: []ScreenStat{
						{Label: "changes", Value: "3", Tone: "changed"},
					},
					Items: []ScreenItem{
						{Title: "windows-machine > identity > Set computer name", Status: "changed", Subtitle: "hostname", Summary: "restart required"},
						{Title: "windows-machine > browser > Install Chrome", Status: "ok", Subtitle: "winget_package", Summary: "already installed"},
						{Title: "signage-rollout > content > Expand exhibit bundle", Status: "changed", Subtitle: "archive", Summary: "49 files would update", AutoExpand: true, DetailTitle: "Source action", Detail: []ScreenLine{{Text: "action ref: github.com/bluecadet/signage-rollout@v3", Tone: "info"}}},
					},
				},
			},
			{
				Label:  "kiosk-b",
				Status: "failed",
				Meta:   "8 tasks",
				Content: ScreenContent{
					Kind: ScreenKindList,
					Summary: []ScreenStat{
						{Label: "failed", Value: "1", Tone: "failed"},
					},
					Items: []ScreenItem{
						{Title: "windows-machine > browser > Install Chrome", Status: "failed", Subtitle: "winget_package", Summary: "exit code 1", DetailTitle: "Recent logs", Detail: []ScreenLine{{Prefix: "err", Text: "Installer returned 1603", Tone: "failed"}}, AutoExpand: true},
						{Title: "windows-machine > kiosk-session > Restart shell", Status: "skipped", Subtitle: "shell", Summary: "dependency failed"},
					},
				},
			},
		},
	}
}

func previewDiffScreen() Screen {
	return Screen{
		Command: "state diff",
		Subject: "play: kiosk-baseline",
		Summary: []ScreenStat{
			{Label: "changed", Value: "3", Tone: "changed"},
			{Label: "target", Value: "kiosk-a", Tone: "info"},
		},
		Content: ScreenContent{
			Kind: ScreenKindList,
			Items: []ScreenItem{
				{
					Title:       "windows-machine > kiosk-session > Configure autologin",
					Status:      "changed",
					Subtitle:    "registry",
					Summary:     "recorded: ok",
					DetailTitle: "Planned",
					Detail: []ScreenLine{
						{Prefix: "inf", Text: "recorded: ok", Tone: "info"},
						{Prefix: "inf", Text: "planned: username=kiosk, enabled=true", Tone: "changed"},
					},
					AutoExpand: true,
				},
				{Title: "windows-machine > browser > Install Chrome", Status: "new", Subtitle: "winget_package", Summary: "new task"},
				{Title: "legacy-kiosk > package > Remove old exhibit app", Status: "removed", Subtitle: "package", Summary: "removed from plan"},
			},
		},
	}
}

func previewInventoryScreen() Screen {
	return Screen{
		Command: "inventory list",
		Subject: "6 hosts, 3 groups",
		Content: ScreenContent{
			Kind: ScreenKindList,
			Items: []ScreenItem{
				{
					Title:       "lab-a",
					Status:      "ok",
					Subtitle:    "winrm",
					Summary:     "10.0.0.11",
					Meta:        []string{"groups: lab, east"},
					DetailTitle: "Selected",
					Detail: []ScreenLine{
						{Text: "Port: 5985", Tone: "info"},
						{Text: "Transport: winrm", Tone: "info"},
						{Text: "Vars: timezone=America/New_York", Tone: "info"},
					},
					AutoExpand: true,
				},
				{Title: "lab-b", Status: "ok", Subtitle: "winrm", Summary: "10.0.0.12", Meta: []string{"groups: lab, east"}},
				{Title: "lobby-a", Status: "ok", Subtitle: "local", Summary: "localhost", Meta: []string{"groups: lobby"}},
			},
		},
	}
}

func previewFactsTabbedScreen() Screen {
	return Screen{
		Command: "facts",
		Subject: "play: gather-facts",
		Summary: []ScreenStat{
			{Label: "hosts", Value: "2", Tone: "info"},
		},
		Tabs: []ScreenTab{
			{
				Label:  "kiosk-a",
				Status: "complete",
				Meta:   "Windows 11",
				Content: ScreenContent{
					Kind: ScreenKindDocument,
					Summary: []ScreenStat{
						{Label: "hostname", Value: "kiosk-a", Tone: "ok"},
						{Label: "arch", Value: "amd64", Tone: "info"},
					},
					Document: "facts\n  os\n    name: Windows 11\n    version: 10.0.22631\n  disks[0]\n    path: C:\\\n    free_gb: 112.4\n  kiosk\n    shell: signage-launcher.exe\n    autologin: true",
				},
			},
			{
				Label:  "kiosk-b",
				Status: "complete",
				Meta:   "Windows 10",
				Content: ScreenContent{
					Kind: ScreenKindDocument,
					Summary: []ScreenStat{
						{Label: "hostname", Value: "kiosk-b", Tone: "ok"},
						{Label: "arch", Value: "amd64", Tone: "info"},
					},
					Document: "facts\n  os\n    name: Windows 10\n    version: 10.0.19045\n  disks[0]\n    path: C:\\\n    free_gb: 88.1\n  kiosk\n    shell: explorer.exe\n    autologin: false",
				},
			},
		},
	}
}

func previewActionInfoScreen() Screen {
	return Screen{
		Command: "action info",
		Subject: "github.com/bluecadet/signage-rollout@v3",
		Content: ScreenContent{
			Kind:     ScreenKindDocument,
			Document: "Name        github.com/bluecadet/signage-rollout@v3\nVersion     v3.2.1\nDescription Stage and activate exhibit content for kiosk hosts.\n\nInputs\ncontent_bundle    required\ncontent_root      optional\nlaunch_shell      optional\n\nTasks\n1. windows-machine > baseline > Ensure content root exists\n2. signage-rollout > content > Copy content bundle\n3. signage-rollout > content > Expand exhibit bundle\n4. windows-machine > kiosk-session > Configure custom shell\n5. windows-machine > restart > Reboot if needed",
		},
	}
}
