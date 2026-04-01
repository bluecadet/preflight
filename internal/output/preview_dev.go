//go:build devtools

package output

import (
	"io"
	"slices"

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
		{
			Name:        "run-single-loading",
			Title:       "Run: single host loading",
			Description: "One host mid-run with an active task and inline logs.",
			run: func(w io.Writer, input io.Reader) error {
				return runPreviewTUI(w, input, "preview", false, []Event{
					{Type: EventPlayStart, PlayName: "kiosk-baseline", Target: "local"},
					{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
					{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
					{Type: EventPhaseStart, Target: "local", Phase: "apply"},
					{Type: EventPhaseEnd, Target: "local", Phase: "apply", Status: "ok", TaskTotal: 4},
					{Type: EventTaskStart, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", TaskTotal: 4},
					{Type: EventTaskLog, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", Stream: "stdout", Line: "Downloading package metadata..."},
					{Type: EventTaskLog, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", Stream: "stdout", Line: "Verifying installer hash..."},
					{Type: EventTaskLog, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", Stream: "stdout", Line: "Starting install..."},
					{Type: EventTaskStart, Target: "local", TaskID: "set-timezone", TaskName: "Set timezone", Module: "powershell", TaskTotal: 4},
					{Type: EventTaskResult, Target: "local", TaskID: "set-timezone", TaskName: "Set timezone", Module: "powershell", Status: "ok", Message: "already configured"},
					{Type: EventTaskStart, Target: "local", TaskID: "autologin", TaskName: "Configure autologin", Module: "registry", TaskTotal: 4},
					{Type: EventTaskResult, Target: "local", TaskID: "autologin", TaskName: "Configure autologin", Module: "registry", Status: "changed", Message: "change applied"},
				})
			},
		},
		{
			Name:        "run-single-failed",
			Title:       "Run: failed task",
			Description: "Single host with a failed task expanded and related logs.",
			run: func(w io.Writer, input io.Reader) error {
				return runPreviewTUI(w, input, "preview", true, []Event{
					{Type: EventPlayStart, PlayName: "kiosk-baseline", Target: "local"},
					{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
					{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
					{Type: EventPhaseEnd, Target: "local", Phase: "apply", Status: "failed", TaskTotal: 3},
					{Type: EventTaskStart, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", TaskTotal: 3},
					{Type: EventTaskLog, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", Stream: "stdout", Line: "Found package Google.Chrome"},
					{Type: EventTaskLog, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", Stream: "stderr", Line: "Another installation is already in progress."},
					{Type: EventTaskLog, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", Stream: "stderr", Line: "HRESULT: 0x80070652"},
					{Type: EventTaskResult, Target: "local", TaskID: "install-chrome", TaskName: "Install Chrome", Module: "shell", Status: "failed", Message: "exit code 1"},
					{Type: EventTaskStart, Target: "local", TaskID: "set-timezone", TaskName: "Set timezone", Module: "powershell", TaskTotal: 3},
					{Type: EventTaskResult, Target: "local", TaskID: "set-timezone", TaskName: "Set timezone", Module: "powershell", Status: "ok", Message: "already configured"},
					{Type: EventTaskStart, Target: "local", TaskID: "restart-shell", TaskName: "Restart shell", Module: "shell", TaskTotal: 3},
					{Type: EventTaskResult, Target: "local", TaskID: "restart-shell", TaskName: "Restart shell", Module: "shell", Status: "skipped", Message: "dependency failed"},
					{Type: EventPlayEnd, Target: "local", OKCount: 1, FailedCount: 1, SkippedCount: 1},
				})
			},
		},
		{
			Name:        "run-multi-host",
			Title:       "Run: multi-host tabs",
			Description: "Two hosts with mixed progress and host tab overflow behavior.",
			run: func(w io.Writer, input io.Reader) error {
				return runPreviewTUI(w, input, "preview", true, []Event{
					{Type: EventPlayStart, PlayName: "exhibit-rollout", Target: "kiosk-a"},
					{Type: EventPlayStart, PlayName: "exhibit-rollout", Target: "kiosk-b"},
					{Type: EventPhaseEnd, Phase: "fetch", Status: "ok"},
					{Type: EventPhaseEnd, Phase: "plan", Status: "ok"},
					{Type: EventPhaseEnd, Target: "kiosk-a", Phase: "apply", Status: "ok", TaskTotal: 3},
					{Type: EventPhaseEnd, Target: "kiosk-b", Phase: "apply", Status: "ok", TaskTotal: 3},
					{Type: EventTaskStart, Target: "kiosk-a", TaskID: "copy-bundle", TaskName: "Copy content bundle", Module: "file", TaskTotal: 3},
					{Type: EventTaskResult, Target: "kiosk-a", TaskID: "copy-bundle", TaskName: "Copy content bundle", Module: "file", Status: "changed", Message: "uploaded archive"},
					{Type: EventTaskStart, Target: "kiosk-a", TaskID: "power-plan", TaskName: "Set power plan", Module: "power_plan", TaskTotal: 3},
					{Type: EventTaskResult, Target: "kiosk-a", TaskID: "power-plan", TaskName: "Set power plan", Module: "power_plan", Status: "ok", Message: "already configured"},
					{Type: EventTaskStart, Target: "kiosk-a", TaskID: "disable-update", TaskName: "Disable Windows Update", Module: "service", TaskTotal: 3},
					{Type: EventTaskResult, Target: "kiosk-a", TaskID: "disable-update", TaskName: "Disable Windows Update", Module: "service", Status: "changed", Message: "change applied"},
					{Type: EventPlayEnd, Target: "kiosk-a", OKCount: 1, ChangedCount: 2},
					{Type: EventTaskStart, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "Copy content bundle", Module: "file", TaskTotal: 3},
					{Type: EventTaskLog, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "Copy content bundle", Module: "file", Stream: "stdout", Line: "Uploading archive..."},
					{Type: EventTaskLog, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "Copy content bundle", Module: "file", Stream: "stdout", Line: "Extracting to C:\\Exhibit\\Content"},
					{Type: EventTaskResult, Target: "kiosk-b", TaskID: "copy-bundle", TaskName: "Copy content bundle", Module: "file", Status: "changed", Message: "bundle copied"},
					{Type: EventTaskStart, Target: "kiosk-b", TaskID: "chrome", TaskName: "Install Chrome", Module: "winget_package", TaskTotal: 3},
					{Type: EventTaskLog, Target: "kiosk-b", TaskID: "chrome", TaskName: "Install Chrome", Module: "winget_package", Stream: "stderr", Line: "Installer returned 1603"},
					{Type: EventTaskResult, Target: "kiosk-b", TaskID: "chrome", TaskName: "Install Chrome", Module: "winget_package", Status: "failed", Message: "exit code 1"},
					{Type: EventTaskStart, Target: "kiosk-b", TaskID: "restart-shell", TaskName: "Restart shell", Module: "shell", TaskTotal: 3},
					{Type: EventTaskResult, Target: "kiosk-b", TaskID: "restart-shell", TaskName: "Restart shell", Module: "shell", Status: "skipped", Message: "dependency failed"},
					{Type: EventPlayEnd, Target: "kiosk-b", ChangedCount: 1, FailedCount: 1, SkippedCount: 1},
				})
			},
		},
		{
			Name:        "screen-plan",
			Title:       "Screen: plan list",
			Description: "Static list preview for plan output.",
			run: func(w io.Writer, input io.Reader) error {
				return runScreenPreview(w, input, Screen{
					Command: "plan",
					Subject: "play: kiosk-baseline",
					Summary: []ScreenStat{
						{Label: "tasks", Value: "12", Tone: "info"},
						{Label: "target", Value: "local", Tone: "ok"},
					},
					Content: ScreenContent{
						Kind: ScreenKindList,
						Items: []ScreenItem{
							{Title: "Set timezone", Status: "ok", Subtitle: "powershell", Summary: "already configured", Meta: []string{"tags: baseline"}},
							{Title: "Configure autologin", Status: "changed", Subtitle: "registry", Summary: "would change", Meta: []string{"tags: baseline, login"}},
							{Title: "Disable lock screen", Status: "ok", Subtitle: "registry", Summary: "already configured"},
							{Title: "Reboot if needed", Status: "skipped", Subtitle: "reboot", Summary: "when-condition-false", DetailTitle: "When", Detail: []ScreenLine{{Text: "facts.os.build > 0", Tone: "info"}}, AutoExpand: true},
						},
					},
				})
			},
		},
		{
			Name:        "screen-diff",
			Title:       "Screen: state diff",
			Description: "Comparison-style screen with changed, new, and removed items.",
			run: func(w io.Writer, input io.Reader) error {
				return runScreenPreview(w, input, Screen{
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
								Title:       "Configure autologin",
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
							{Title: "Install Chrome", Status: "new", Subtitle: "winget_package", Summary: "new task"},
							{Title: "Legacy kiosk app", Status: "removed", Subtitle: "package", Summary: "removed from plan"},
						},
					},
				})
			},
		},
		{
			Name:        "screen-inventory",
			Title:       "Screen: inventory list",
			Description: "Inventory list with multiple rows and expandable detail content.",
			run: func(w io.Writer, input io.Reader) error {
				return runScreenPreview(w, input, Screen{
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
								Detail:      []ScreenLine{{Text: "Port: 5985", Tone: "info"}, {Text: "Transport: winrm", Tone: "info"}},
								AutoExpand:  true,
							},
							{Title: "lab-b", Status: "ok", Subtitle: "winrm", Summary: "10.0.0.12", Meta: []string{"groups: lab, east"}},
							{Title: "lobby-a", Status: "ok", Subtitle: "local", Summary: "localhost", Meta: []string{"groups: lobby"}},
						},
					},
				})
			},
		},
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

func runPreviewTUI(w io.Writer, input io.Reader, command string, done bool, events []Event) error {
	model := newTUIModel(make(chan Event), Options{Input: input, Command: command})
	for _, event := range events {
		model = model.applyEvent(event)
	}
	model.done = done
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
