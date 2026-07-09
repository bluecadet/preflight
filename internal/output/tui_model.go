package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tuiModel is the Bubble Tea model for the TUI renderer.
// It delegates event folding to RunProjection and renders commit
// descriptors to the scroll region.
type tuiModel struct {
	projection   *RunProjection
	spinner      spinner.Model
	width        int
	events       chan Event
	verbose      bool
	maxFailLines int
	done         bool
}

type tuiEventMsg struct{ event Event }
type tuiDoneMsg struct{}

func newTUIModel(events chan Event) tuiModel {
	return newTUIModelWithOptions(events, Options{})
}

func newTUIModelWithOptions(events chan Event, opts Options) tuiModel {
	colorMode := opts.Color
	if colorMode == ColorAuto {
		colorMode = DetectColor("", false, os.Stdout)
	}
	if !colorMode.UseColor() {
		S = NewTUIStyles(DefaultPalette(), false)
	}

	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(S.Spin),
	)
	maxFailLines := opts.MaxFailLines
	if maxFailLines <= 0 {
		maxFailLines = defaultFailureOutputLimit
	}
	return tuiModel{
		projection:   NewRunProjectionWithOptions(opts),
		spinner:      s,
		events:       events,
		verbose:      opts.Verbose,
		maxFailLines: maxFailLines,
		width:        80,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForEvent())
}

func (m tuiModel) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		e, ok := <-m.events
		if !ok {
			return tuiDoneMsg{}
		}
		return tuiEventMsg{event: e}
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tuiEventMsg:
		next, cmd := m.applyEvent(msg.event)
		if cmd == nil {
			return next, next.waitForEvent()
		}
		return next, tea.Sequence(cmd, next.waitForEvent())
	case tuiDoneMsg:
		m.done = true
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m tuiModel) applyEvent(event Event) (tuiModel, tea.Cmd) {
	descriptors := m.projection.Apply(event)

	if len(descriptors) == 0 {
		return m, nil
	}

	var cmds []tea.Cmd
	for _, d := range descriptors {
		switch desc := d.(type) {
		case *RunStartDescriptor:
			cmds = append(cmds, m.renderRunStart(*desc))
		case TargetRosterDescriptor:
			cmds = append(cmds, m.renderTargetRoster(desc))
		case TaskFinishedDescriptor:
			cmds = append(cmds, m.renderTaskFinished(desc))
		case CardDescriptor:
			cmds = append(cmds, m.renderCard(desc))
		case WarningDescriptor:
			cmds = append(cmds, tea.Println(tsRenderNotice("!", S.Changed, "warning: "+desc.Message, m.width)))
		}
	}
	return m, tea.Sequence(cmds...)
}

// targetBadge returns a transport-colored [target] badge string.
// Returns empty string when host labels should not be shown.
func (m tuiModel) targetBadge(target string) string {
	if target == "" {
		return ""
	}
	displayName := m.projection.DisplayTarget(target)
	transport := m.projection.TargetTransport(target)
	style := m.transportStyle(transport)
	return style.Render("[" + displayName + "]")
}

// transportStyle returns the lipgloss.Style for a given transport type.
func (m tuiModel) transportStyle(transport string) lipgloss.Style {
	switch transport {
	case "ssh":
		return S.TransportSSH
	case "winrm":
		return S.TransportWinRM
	default:
		return S.TransportLocal
	}
}

func (m tuiModel) renderRunStart(d RunStartDescriptor) tea.Cmd {
	var lines []string

	if d.SingleTarget != nil {
		// Single-target: folded header line.
		playbook := d.PlaybookPath
		if playbook == "" {
			playbook = d.PlaybookName
		}
		target := d.SingleTarget.Name + " (" + d.SingleTarget.Transport
		if d.SingleTarget.Address != "" {
			target += " • " + d.SingleTarget.Address
		}
		target += ")"
		elapsed := formatElapsed(m.projection.Elapsed())
		line := padLine("RUN  "+playbook+" → "+target, elapsed, m.width)
		lines = append(lines, S.Bold.Render(line))
	} else {
		// Multi-target: current multi-line block.
		lines = append(lines, S.Bold.Render(titleRunMode(d.Mode)))
		if d.PlaybookPath != "" {
			lines = append(lines, "playbook: "+d.PlaybookPath)
		} else if d.PlaybookName != "" {
			lines = append(lines, "playbook: "+d.PlaybookName)
		}
		if d.PlaybookPath != "" && d.PlaybookName != "" {
			lines = append(lines, "name: "+d.PlaybookName)
		}
		if len(d.Targets) > 1 {
			lines = append(lines, fmt.Sprintf("targets: %d", len(d.Targets)))
			if len(d.Targets) <= 5 {
				lines = append(lines, "  "+strings.Join(d.Targets, ", "))
			}
		}
	}
	lines = append(lines, m.renderDivider())
	return tea.Println(strings.Join(lines, "\n") + "\n")
}

func (m tuiModel) renderTargetRoster(d TargetRosterDescriptor) tea.Cmd {
	maxName := 0
	for _, ti := range d.Targets {
		maxName = max(maxName, len(ti.Name))
	}
	var lines []string
	lines = append(lines, "Targets:")
	for _, ti := range d.Targets {
		name := AlignLeft(ti.Name, maxName)
		s := "  " + name + " (" + ti.Transport
		if ti.Address != "" {
			s += " • " + ti.Address
		}
		s += ")"
		lines = append(lines, s)
	}
	return tea.Println(strings.Join(lines, "\n"))
}

func (m tuiModel) renderTaskFinished(d TaskFinishedDescriptor) tea.Cmd {
	left := statusStyle(d.Status).Render(statusGlyph(d.Status, m.projection.IsCheckMode())) + " " + S.TaskName.Render(d.TaskName)
	if m.projection.ShouldShowHostLabels() && d.Target != "" {
		left = m.targetBadge(d.Target) + " " + left
	}
	right := ""
	if d.Elapsed > 0 && d.Status != "skipped" {
		right = formatElapsed(d.Elapsed)
	}
	line := tsRow(padLine(left, right, m.width))

	var detailLines []string
	switch d.Status {
	case "failed":
		msg := strings.TrimSpace(d.Message)
		if msg == "" {
			msg = "task failed"
		}
		detailLines = append(detailLines, tsOutputLines(2, "ERROR: "+msg, m.width)...)
		if len(d.Output) > 0 {
			detailLines = append(detailLines, tsOutputLine(2, "output:"))
			for _, l := range limitFailureOutput(m.maxFailLines, d.Output) {
				detailLines = append(detailLines, tsOutputLines(4, l, m.width)...)
			}
			if len(d.Output) > m.maxFailLines {
				detailLines = append(detailLines, tsOutputLines(2, fmt.Sprintf("output truncated: showing last %d of %d lines", m.maxFailLines, len(d.Output)), m.width)...)
			}
		}
		detailLines = append(detailLines, tsOutputLines(2, "target stopped: remaining tasks were not run", m.width)...)
	case "skipped":
		if d.Message != "" {
			detailLines = tsOutputLines(2, "reason: "+d.Message, m.width)
		}
	case "changed":
		if detail := changedDetail(d.Message, m.projection.IsCheckMode()); detail != "" {
			detailLines = tsOutputLines(2, detail, m.width)
		}
	case "ok":
		if detail := okDetail(d.Message); detail != "" {
			detailLines = tsOutputLines(2, detail, m.width)
		}
	}

	var b strings.Builder
	b.WriteString(line)
	for _, dl := range detailLines {
		b.WriteByte('\n')
		b.WriteString(dl)
	}
	return tea.Println(b.String())
}

func (m tuiModel) renderCard(d CardDescriptor) tea.Cmd {
	var block string
	switch d.Kind {
	case "facts":
		if e, ok := d.Event.(FactsEvent); ok {
			block = renderFactsCard(e, m.width)
		}
	case "plan":
		if e, ok := d.Event.(PlanEvent); ok {
			block = renderPlanCard(e, m.width)
		}
	case "state":
		if e, ok := d.Event.(StateEvent); ok {
			block = renderStateCard(e, m.width)
		}
	case "validate":
		if e, ok := d.Event.(ValidationEvent); ok {
			block = renderValidationCard(e, m.width)
		}
	case "action_catalog":
		if e, ok := d.Event.(ActionCatalogEvent); ok {
			block = renderActionCatalogCard(e, m.width)
		}
	case "action_info":
		if e, ok := d.Event.(ActionInfoEvent); ok {
			block = renderActionInfoCard(e, m.width)
		}
	case "action_fetch":
		if e, ok := d.Event.(ActionFetchEvent); ok {
			block = renderActionFetchCard(e, m.width)
		}
	}
	if block == "" {
		return nil
	}
	return tea.Println("\n" + block)
}

// View renders only the live zone (running tasks + footer).
func (m tuiModel) View() string {
	if m.done {
		return m.renderFinalSummary()
	}

	activities := m.projection.OrderedActivities()
	running := m.projection.OrderedRunningTasks()
	if len(activities) == 0 && len(running) == 0 && m.projection.Total() == 0 {
		return ""
	}

	if len(activities) > 0 && len(running) == 0 && m.projection.Total() == 0 {
		var b strings.Builder
		b.WriteString("In Progress\n")
		for _, activity := range activities {
			b.WriteString(m.renderActivity(activity))
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n") + "\n"
	}

	runningTargets := m.projection.ActiveTargetCount()
	dense := len(running)+len(activities) > maxLiveLines
	visibleActivities, visibleRunning, hiddenCount := visibleLiveEntries(activities, running, maxLiveLines)
	var b strings.Builder
	if len(running)+len(activities) > 0 {
		b.WriteString("In Progress\n")
	}
	for _, activity := range visibleActivities {
		b.WriteString(m.renderActivity(activity))
		b.WriteByte('\n')
	}
	for _, task := range visibleRunning {
		b.WriteString(m.renderRunning(task, dense))
		b.WriteByte('\n')
	}
	if hiddenCount > 0 {
		fmt.Fprintf(&b, "+ %d more running tasks\n", hiddenCount)
	}
	b.WriteString(m.renderDivider())
	b.WriteByte('\n')
	b.WriteString(m.renderFooter(runningTargets))
	b.WriteByte('\n')
	return b.String()
}

func (m tuiModel) renderActivity(activity *activeActivity) string {
	spin := S.Spin.Render(strings.TrimRight(m.spinner.View(), " "))
	timer := S.Elapsed.Render(formatElapsed(time.Since(activity.startAt)))
	message := strings.TrimSpace(activity.message)
	if m.projection.ShouldShowHostLabels() && activity.target != "" {
		message = "[" + m.projection.DisplayTarget(activity.target) + "] " + message
	}
	return tsRow(spin, S.Muted.Render(message), timer)
}

func (m tuiModel) renderRunning(task *activeTask, dense bool) string {
	spin := S.Spin.Render(strings.TrimRight(m.spinner.View(), " "))
	timer := S.Elapsed.Render(formatElapsed(time.Since(task.startAt)))

	var b strings.Builder
	if task.actionPath != "" {
		header := renderDisplayPath(task.actionPath)
		if m.projection.ShouldShowHostLabels() && task.target != "" {
			header = m.targetBadge(task.target) + " " + header
		}
		b.WriteString(header)
		b.WriteByte('\n')
		line := padLine("  "+spin+" "+S.TaskName.Render(task.name), timer, m.width)
		b.WriteString(tsRow(line))
	} else {
		left := spin + " " + S.TaskName.Render(task.name)
		if m.projection.ShouldShowHostLabels() && task.target != "" {
			left = m.targetBadge(task.target) + " " + left
		}
		b.WriteString(tsRow(padLine(left, timer, m.width)))
	}
	if dense || len(task.recentLines) == 0 {
		return b.String()
	}

	maxWidth := max(m.width-12, 10)
	for _, line := range task.recentLines {
		b.WriteByte('\n')
		b.WriteString(tsOutputLine(4, tsTruncate(line, maxWidth)))
	}
	return b.String()
}

func (m tuiModel) renderFinalSummary() string {
	if len(m.projection.FailedTasks()) == 0 && m.projection.Total() == 0 {
		return ""
	}

	totalElapsed := m.projection.Elapsed()

	var b strings.Builder
	b.WriteString(m.renderDivider())
	b.WriteByte('\n')
	b.WriteString("Recap\n")

	overallIcon := S.OK.Render("✓")
	statusWord := "complete"
	if m.projection.FailedCount > 0 {
		overallIcon = S.Failed.Render("x")
		statusWord = "failed"
	}
	b.WriteString(tsRow(overallIcon, S.Bold.Render(titleRunMode(m.projection.Mode)+" "+statusWord), S.Elapsed.Render(formatElapsed(totalElapsed))))
	b.WriteByte('\n')

	// Task outcome tallies live in the recap.
	totals := recapTotals([]struct{ ok, changed, failed, skipped int }{
		{ok: m.projection.OkCount, changed: m.projection.ChangedCount, failed: m.projection.FailedCount, skipped: m.projection.SkippedCount},
	})
	b.WriteString("  tasks: " + renderTaskTotals(totals, m.projection.IsCheckMode(), m.projection.WarningCount) + "\n")

	if m.projection.FailedCount > 0 {
		b.WriteByte('\n')
		b.WriteString("Needs attention\n")
		for _, failed := range m.projection.FailedTasks() {
			path := renderTaskFailurePath(failed.actionPath, failed.name)
			prefix := "  "
			if m.projection.ShouldShowHostLabels() && failed.target != "" {
				prefix = "  " + m.targetBadge(failed.target) + " "
			}
			b.WriteString(prefix + path + "\n")
		}
	}
	if m.projection.RunDir != "" {
		b.WriteString("  Run directory: " + m.projection.RunDir + "\n")
	}
	return b.String()
}

func (m tuiModel) renderDivider() string {
	return S.Divider.Render(strings.Repeat("─", m.widthWithFallback()))
}

func (m tuiModel) renderFooter(runningCount int) string {
	if runningCount == 0 && m.projection.Total() == 0 {
		return ""
	}

	// Multi-target: one line showing target progress only.
	if len(m.projection.Targets) > 1 {
		done, failed := m.projection.TargetCounts()
		waiting := max(len(m.projection.Targets)-done-failed-runningCount, 0)
		return fmt.Sprintf("targets: %d done · %d run · %d wait · %d fail", done, runningCount, waiting, failed)
	}

	// Single-target: near-empty footer (mode, elapsed, warnings).
	line := fmt.Sprintf("%s %s", titleRunMode(m.projection.Mode), formatElapsed(m.projection.Elapsed()))
	if m.projection.WarningCount > 0 {
		line += fmt.Sprintf("   Warnings %d", m.projection.WarningCount)
	}
	return line
}

func (m tuiModel) widthWithFallback() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// statusStyle returns the styled glyph for a task outcome based on status.
func statusStyle(status string) lipgloss.Style {
	switch status {
	case "ok":
		return S.OK
	case "changed":
		return S.Changed
	case "failed":
		return S.Failed
	case "skipped":
		return S.Skipped
	default:
		return lipgloss.NewStyle()
	}
}
