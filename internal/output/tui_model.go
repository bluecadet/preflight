package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// activeTask represents a task currently being executed.
type activeTask struct {
	id          string
	name        string
	actionPath  string
	target      string
	startAt     time.Time
	recentLines []string
}

type activeActivity struct {
	key     string
	message string
	target  string
	startAt time.Time
}

// hostRecap stores the final counts emitted by EventPlayEnd for one host.
type hostRecap struct {
	target  string
	ok      int
	changed int
	failed  int
	skipped int
}

// failedTask captures a failed task's identity for the final summary.
type failedTask struct {
	target     string
	actionPath string
	name       string
	message    string
	output     []string
}

// tuiModel is the Bubble Tea model for the TUI renderer.
type tuiModel struct {
	spinner     spinner.Model
	width       int
	events      chan Event
	verbose     bool
	playName    string
	startedAt   time.Time
	playStarted bool

	hosts         map[string]map[string]*activeTask
	hostOrder     []string
	taskOrder     map[string][]string
	hostColors    map[string]lipgloss.Style
	activities    map[string]*activeActivity
	activityOrder []string

	okCount      int
	changedCount int
	failedCount  int
	skippedCount int

	recaps      []hostRecap
	failedTasks []failedTask
	hadActivity bool
	done        bool
}

type tuiEventMsg struct{ event Event }
type tuiDoneMsg struct{}

func newTUIModel(events chan Event) tuiModel {
	return newTUIModelWithOptions(events, Options{})
}

func newTUIModelWithOptions(events chan Event, opts Options) tuiModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(tsSpin),
	)
	return tuiModel{
		spinner:    s,
		events:     events,
		verbose:    opts.Verbose,
		width:      80,
		hosts:      make(map[string]map[string]*activeTask),
		taskOrder:  make(map[string][]string),
		hostColors: make(map[string]lipgloss.Style),
		activities: make(map[string]*activeActivity),
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
	p, ok := projectEvent(event)
	if !ok {
		return m, nil
	}
	switch p.kind {
	case EventPlayStart:
		return m.handlePlayStart(PlayStartEvent{PlayName: p.playName})
	case EventTaskStart:
		return m.handleTaskStart(TaskStartEvent{TaskName: p.task, TaskID: p.taskID, ActionPath: p.actionPath, Target: p.target})
	case EventActivityStart:
		return m.handleActivityStart(ActivityStartEvent{Target: p.target, Message: p.message})
	case EventActivityResult:
		return m.handleActivityResult(ActivityResultEvent{Target: p.target, Message: p.message, Status: p.status})
	case EventTaskOutput:
		return m.handleTaskOutput(TaskOutputEvent{TaskName: p.task, TaskID: p.taskID, Target: p.target, Lines: p.lines})
	case EventTaskResult:
		return m.handleTaskResult(TaskResultEvent{TaskName: p.task, TaskID: p.taskID, ActionPath: p.actionPath, Target: p.target, Status: p.status, Message: p.message, Output: p.output})
	case EventPlayEnd:
		return m.handlePlayEnd(PlayEndEvent{Target: p.target, OKCount: p.okCount, ChangedCount: p.changedCount, FailedCount: p.failedCount, SkippedCount: p.skippedCount})
	case EventWarning:
		return m, tea.Println("  " + tsChanged.Render("⚠") + "  " + tsMuted.Render(p.message))
	case EventError:
		return m, tea.Println("  " + tsFailed.Render("✗") + "  " + tsFailed.Render(p.errorMessage))
	case EventFacts:
		return m.handleFacts(FactsEvent{Target: p.target, Facts: p.facts})
	case EventPlan:
		return m.printStaticBlock(renderPlanCard(PlanEvent{Target: p.target, PlaybookName: p.playName, Tasks: p.tasks}))
	case EventState:
		return m.printStaticBlock(renderStateCard(StateEvent{Target: p.target, PlaybookName: p.playName, StatePath: p.statePath, LastApplied: p.lastApplied, Comparisons: p.comparisons}))
	case EventValidate:
		return m.printStaticBlock(renderValidationCard(ValidationEvent{PlaybookPath: p.playbookPath, PlaybookName: p.playName, TaskCount: p.taskCount, VisitedRefCount: p.visitedRefs, ResolvedRefs: p.resolvedRefs, ErrorCount: p.errorCount}))
	case EventActionList:
		return m.printStaticBlock(renderActionCatalogCard(ActionCatalogEvent{EmbeddedNamespace: p.namespace, EmbeddedRefs: p.embeddedRefs, LocalDir: p.localDir, LocalRefs: p.localRefs}))
	case EventActionInfo:
		return m.printStaticBlock(renderActionInfoCard(ActionInfoEvent{Ref: p.ref, Name: p.name, Version: p.version, Description: p.description, Author: p.author, Inputs: p.inputs, TaskNames: p.taskNames}))
	case EventActionFetch:
		return m.printStaticBlock(renderActionFetchCard(ActionFetchEvent{Entries: p.entries}))
	default:
		return m, nil
	}
}

func (m tuiModel) handlePlayStart(e PlayStartEvent) (tuiModel, tea.Cmd) {
	if m.playStarted {
		return m, nil
	}
	m.playStarted = true
	m.playName = e.PlayName
	m.startedAt = time.Now()
	line := m.renderDivider() + "\n  " + tsSpin.Render("▶") + "  " + tsBold.Render(e.PlayName) + "\n"
	return m, tea.Println(line)
}

func (m tuiModel) handleTaskStart(e TaskStartEvent) (tuiModel, tea.Cmd) {
	if e.Target == "" {
		return m, nil
	}
	if m.hosts[e.Target] == nil {
		m.hosts[e.Target] = make(map[string]*activeTask)
		m.hostOrder = append(m.hostOrder, e.Target)
		m.hostColors[e.Target] = tsHostPalette[(len(m.hostOrder)-1)%len(tsHostPalette)]
	}

	m.hosts[e.Target][e.TaskID] = &activeTask{
		id:         e.TaskID,
		name:       e.TaskName,
		actionPath: e.ActionPath,
		target:     e.Target,
		startAt:    time.Now(),
	}
	m.taskOrder[e.Target] = append(m.taskOrder[e.Target], e.TaskID)
	return m, nil
}

func (m tuiModel) handleActivityStart(e ActivityStartEvent) (tuiModel, tea.Cmd) {
	m.hadActivity = true
	key := activityKey(e.Target, e.Message)
	if _, ok := m.activities[key]; ok {
		return m, nil
	}

	m.activities[key] = &activeActivity{
		key:     key,
		message: e.Message,
		target:  fallbackTarget(e.Target),
		startAt: time.Now(),
	}
	m.activityOrder = append(m.activityOrder, key)
	return m, nil
}

func (m tuiModel) handleActivityResult(e ActivityResultEvent) (tuiModel, tea.Cmd) {
	key := activityKey(e.Target, e.Message)
	delete(m.activities, key)
	m.activityOrder = removeOrderedValue(m.activityOrder, key)
	return m, nil
}

func (m tuiModel) handleTaskOutput(e TaskOutputEvent) (tuiModel, tea.Cmd) {
	if e.Target == "" || e.TaskID == "" {
		return m, nil
	}

	host := m.hosts[e.Target]
	if host == nil {
		return m, nil
	}
	task := host[e.TaskID]
	if task == nil {
		return m, nil
	}

	task.recentLines = append(task.recentLines, e.Lines...)
	if !m.verbose && len(task.recentLines) > maxTaskPreviewLines {
		task.recentLines = task.recentLines[len(task.recentLines)-maxTaskPreviewLines:]
	}
	return m, nil
}

func (m tuiModel) handleTaskResult(e TaskResultEvent) (tuiModel, tea.Cmd) {
	var (
		cmds    []tea.Cmd
		elapsed time.Duration
	)

	if host := m.hosts[e.Target]; host != nil {
		if task := host[e.TaskID]; task != nil {
			elapsed = time.Since(task.startAt)
			delete(host, e.TaskID)
		}
	}
	m.taskOrder[e.Target] = removeOrderedValue(m.taskOrder[e.Target], e.TaskID)
	m.recordTaskResult(e)

	cmds = append(cmds, tea.Println(m.renderCommitted(e, elapsed)))
	if m.verbose {
		if e.Message != "" {
			cmds = append(cmds, tea.Println(tsRenderOutputBlock(e.Message)))
		}
		if len(e.Output) > 0 {
			cmds = append(cmds, tea.Println(tsRenderOutputBlock(strings.Join(e.Output, "\n"))))
		}
	}

	return m, tea.Sequence(cmds...)
}

func (m *tuiModel) recordTaskResult(e TaskResultEvent) {
	switch e.Status {
	case "ok":
		m.okCount++
	case "changed":
		m.changedCount++
	case "failed":
		m.failedCount++
		m.failedTasks = append(m.failedTasks, failedTask{
			target:     e.Target,
			actionPath: e.ActionPath,
			name:       e.TaskName,
			message:    e.Message,
			output:     e.Output,
		})
	case "skipped":
		m.skippedCount++
	}
}

func (m tuiModel) handlePlayEnd(e PlayEndEvent) (tuiModel, tea.Cmd) {
	m.recaps = append(m.recaps, hostRecap{
		target:  e.Target,
		ok:      e.OKCount,
		changed: e.ChangedCount,
		failed:  e.FailedCount,
		skipped: e.SkippedCount,
	})
	return m, nil
}

func (m tuiModel) handleFacts(e FactsEvent) (tuiModel, tea.Cmd) {
	return m.printStaticBlock(renderFactsCard(e, m.width))
}

func (m tuiModel) printStaticBlock(block string) (tuiModel, tea.Cmd) {
	return m, tea.Println("\n" + block)
}

// View renders only the live zone (running tasks + footer).
func (m tuiModel) View() string {
	if m.done {
		return m.renderFinalSummary()
	}

	activities := m.orderedActivities()
	running := m.orderedTasks()
	if len(activities) == 0 && len(running) == 0 && m.total() == 0 {
		return ""
	}

	if len(activities) > 0 && len(running) == 0 && m.total() == 0 {
		var b strings.Builder
		for _, activity := range activities {
			b.WriteString(m.renderActivity(activity))
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n") + "\n"
	}

	dense := len(running)+len(activities) > maxLiveLines
	var b strings.Builder
	for _, activity := range activities {
		b.WriteString(m.renderActivity(activity))
		b.WriteByte('\n')
	}
	for _, task := range running {
		b.WriteString(m.renderRunning(task, dense))
		b.WriteByte('\n')
	}
	b.WriteString(m.renderDivider())
	b.WriteByte('\n')
	b.WriteString(m.renderFooter(len(running) + len(activities)))
	b.WriteByte('\n')
	return b.String()
}

func (m tuiModel) orderedActivities() []*activeActivity {
	activities := make([]*activeActivity, 0, len(m.activityOrder))
	for _, key := range m.activityOrder {
		if activity := m.activities[key]; activity != nil {
			activities = append(activities, activity)
		}
	}
	return activities
}

func (m tuiModel) orderedTasks() []*activeTask {
	var running []*activeTask
	for _, host := range m.hostOrder {
		for _, id := range m.taskOrder[host] {
			if task := m.hosts[host][id]; task != nil {
				running = append(running, task)
			}
		}
	}
	return running
}

func (m tuiModel) renderActivity(activity *activeActivity) string {
	spin := tsSpin.Render(strings.TrimRight(m.spinner.View(), " "))
	host := m.hostStyle(activity.target).Render(activity.target)
	timer := tsElapsed.Render("[" + tsFmtElapsed(time.Since(activity.startAt)) + "]")
	message := tsMuted.Render(strings.TrimSpace(activity.message))
	return tsRow(spin, host, message, timer)
}

func (m tuiModel) renderRunning(task *activeTask, dense bool) string {
	spin := tsSpin.Render(strings.TrimRight(m.spinner.View(), " "))
	host := m.hostStyle(task.target).Render(task.target)
	timer := tsElapsed.Render("[" + tsFmtElapsed(time.Since(task.startAt)) + "]")
	pathMax := m.width - lipgloss.Width(spin) - lipgloss.Width(host) - lipgloss.Width(timer) - 10
	line := tsRow(spin, host, tsRenderPath(task.actionPath, task.name, pathMax), timer)
	if dense || len(task.recentLines) == 0 {
		return line
	}

	maxWidth := max(m.width-12, 10)
	var b strings.Builder
	b.WriteString(line)
	for _, line := range task.recentLines {
		b.WriteByte('\n')
		b.WriteString(tsOutputLine(5, tsTruncate(line, maxWidth)))
	}
	return b.String()
}

func (m tuiModel) renderCommitted(e TaskResultEvent, elapsed time.Duration) string {
	target := fallbackTarget(e.Target)
	icon := tsIcon(e.Status)
	host := m.hostStyle(target).Render(target)

	right := ""
	switch {
	case e.Status == "skipped" && e.Message != "":
		right = tsMuted.Render("(" + e.Message + ")")
	case elapsed > 0 && e.Status != "skipped":
		right = tsElapsed.Render(tsFmtElapsed(elapsed))
	}

	pathMax := m.width - lipgloss.Width(icon) - lipgloss.Width(host) - lipgloss.Width(right) - 10
	return tsRow(icon, host, tsRenderPath(e.ActionPath, e.TaskName, pathMax), right)
}

func (m tuiModel) renderFinalSummary() string {
	if len(m.recaps) == 0 {
		return ""
	}

	totalElapsed := time.Since(m.startedAt)
	allOK := true
	for _, recap := range m.recaps {
		if recap.failed > 0 {
			allOK = false
			break
		}
	}

	failedByHost := make(map[string][]failedTask)
	for _, task := range m.failedTasks {
		failedByHost[task.target] = append(failedByHost[task.target], task)
	}

	var b strings.Builder
	b.WriteByte('\n')
	b.WriteString(m.renderDivider())
	b.WriteByte('\n')

	overallIcon := tsOK.Render("✓")
	if !allOK {
		overallIcon = tsFailed.Render("✗")
	}
	hostLabel := fmt.Sprintf("%d %s", len(m.recaps), tsPluralize(len(m.recaps), "host", "hosts"))
	b.WriteString(tsRow(
		overallIcon,
		tsBold.Render(m.playName),
		tsMuted.Render("·  "+hostLabel),
		tsElapsed.Render(tsFmtElapsed(totalElapsed)),
	))
	b.WriteString("\n\n")

	for _, recap := range m.recaps {
		target := fallbackTarget(recap.target)
		hostIcon := tsOK.Render("✓")
		if recap.failed > 0 {
			hostIcon = tsFailed.Render("✗")
		} else if recap.changed > 0 {
			hostIcon = tsChanged.Render("◆")
		}

		stats := strings.Join([]string{
			tsStat(tsOK, "✓", recap.ok),
			tsStat(tsChanged, "◆", recap.changed),
			tsStat(tsFailed, "✗", recap.failed),
			tsStat(tsSkipped, "–", recap.skipped),
		}, "  ")
		b.WriteString(tsRow(hostIcon, m.hostStyle(target).Render(target), stats))
		b.WriteByte('\n')

		for _, failed := range failedByHost[recap.target] {
			b.WriteString("      " + tsFailed.Render("✗") + "  " + tsRenderPath(failed.actionPath, failed.name, 0) + "\n")
			if failed.message != "" {
				for line := range strings.SplitSeq(strings.TrimSpace(failed.message), "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						b.WriteString(tsOutputLine(9, line) + "\n")
					}
				}
			}
			for _, line := range failed.output {
				line = strings.TrimSpace(line)
				if line != "" {
					b.WriteString(tsOutputLine(9, line) + "\n")
				}
			}
		}
	}

	b.WriteByte('\n')
	return b.String()
}

func (m tuiModel) renderDivider() string {
	return tsDivider.Render(strings.Repeat("─", m.divWidth()))
}

func (m tuiModel) renderFooter(runningCount int) string {
	if runningCount == 0 && m.total() == 0 {
		return ""
	}
	parts := []string{
		tsStat(tsSpin, "running", runningCount),
		tsStat(tsOK, "✓", m.okCount),
		tsStat(tsChanged, "◆", m.changedCount),
		tsStat(tsFailed, "✗", m.failedCount),
		tsStat(tsSkipped, "–", m.skippedCount),
	}
	return "  " + strings.Join(parts, "  ")
}

func (m tuiModel) divWidth() int {
	if m.width <= 0 {
		return 50
	}
	return min(m.width, 50)
}

func (m tuiModel) total() int {
	return m.okCount + m.changedCount + m.failedCount + m.skippedCount
}

// hostStyle returns the palette color assigned to target, falling back to the first palette entry.
func (m tuiModel) hostStyle(target string) lipgloss.Style {
	if style, ok := m.hostColors[target]; ok {
		return style
	}
	return tsHostPalette[0]
}

func removeOrderedValue(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return values
}
