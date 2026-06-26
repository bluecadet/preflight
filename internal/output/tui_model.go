package output

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// activeTask represents a task currently being executed.
type activeTask struct {
	id          string
	name        string
	actionPath  string
	target      string
	startAt     time.Time
	updatedAt   time.Time
	alert       bool
	recentLines []string
}

type activeActivity struct {
	key     string
	message string
	target  string
	startAt time.Time
}

// targetOutcome tracks a completed target's result for footer display.
type targetOutcome struct {
	target string
	failed bool
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
	spinner      spinner.Model
	width        int
	events       chan Event
	verbose      bool
	maxFailLines int
	mode         string
	playName     string
	playbook     string
	targets      []string
	startedAt    time.Time
	playStarted  bool
	runDir       string

	hosts          map[string]map[string]*activeTask
	hostOrder      []string
	taskOrder      map[string][]string
	activities     map[string]*activeActivity
	activityOrder  []string
	targetOutcomes []targetOutcome

	okCount      int
	changedCount int
	failedCount  int
	skippedCount int

	failedTasks  []failedTask
	warningCount int
	hadActivity  bool
	done         bool
}

type tuiEventMsg struct{ event Event }
type tuiDoneMsg struct{}

func newTUIModel(events chan Event) tuiModel {
	return newTUIModelWithOptions(events, Options{})
}

func newTUIModelWithOptions(events chan Event, opts Options) tuiModel {
	// When colors are disabled, strip foreground colors from all TUI styles.
	colorMode := opts.Color
	if colorMode == ColorAuto {
		colorMode = DetectColor("", false, os.Stdout)
	}
	if !colorMode.UseColor() {
		noColorifyStyles()
	}

	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(tsSpin),
	)
	maxFailLines := opts.MaxFailLines
	if maxFailLines <= 0 {
		maxFailLines = defaultFailureOutputLimit
	}
	return tuiModel{
		spinner:      s,
		events:       events,
		verbose:      opts.Verbose,
		maxFailLines: maxFailLines,
		mode:         normalizeRunMode(opts.Mode),
		runDir:       opts.RunDir,
		width:        80,
		hosts:        make(map[string]map[string]*activeTask),
		taskOrder:    make(map[string][]string),
		activities:   make(map[string]*activeActivity),
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
	switch e := event.(type) {
	case RunStartEvent:
		return m.handleRunStart(e)
	case TargetStartEvent:
		return m, nil
	case TargetCompleteEvent:
		return m.handleTargetComplete(e)
	case TaskStartedEvent:
		return m.handleTaskStartedEvent(e)
	case ActivityStartEvent:
		return m.handleActivityStart(e)
	case ActivityResultEvent:
		return m.handleActivityResult(e)
	case TaskOutputEvent:
		return m.handleTaskOutput(e)
	case TaskOKEvent:
		return m.handleTaskOK(e)
	case TaskChangedEvent:
		return m.handleTaskChanged(e)
	case TaskSkippedEvent:
		return m.handleTaskSkipped(e)
	case TaskFailedEvent:
		return m.handleTaskFailed(e)
	case RunSummaryEvent:
		return m, nil
	case WarningEvent:
		m.warningCount++
		return m, tea.Println(tsRenderNotice("!", tsChanged, "warning: "+e.Message, m.width))
	case FactsEvent:
		return m.handleFacts(e)
	case PlanEvent:
		return m.printStaticBlock(renderPlanCard(e))
	case StateEvent:
		return m.printStaticBlock(renderStateCard(e))
	case ValidationEvent:
		return m.printStaticBlock(renderValidationCard(e))
	case ActionCatalogEvent:
		return m.printStaticBlock(renderActionCatalogCard(e))
	case ActionInfoEvent:
		return m.printStaticBlock(renderActionInfoCard(e))
	case ActionFetchEvent:
		return m.printStaticBlock(renderActionFetchCard(e))
	default:
		return m, nil
	}
}

func (m tuiModel) handleRunStart(e RunStartEvent) (tuiModel, tea.Cmd) {
	if m.playStarted {
		return m, nil
	}
	m.playStarted = true
	m.mode = normalizeRunMode(e.Mode)
	m.playName = e.PlaybookName
	m.playbook = e.PlaybookPath
	m.targets = append([]string(nil), e.Targets...)
	m.startedAt = time.Now()

	var lines []string
	lines = append(lines, tsBold.Render(titleRunMode(m.mode)))
	if m.playbook != "" {
		lines = append(lines, "playbook: "+m.playbook)
	} else if m.playName != "" {
		lines = append(lines, "playbook: "+m.playName)
	}
	if m.playbook != "" && m.playName != "" {
		lines = append(lines, "name: "+m.playName)
	}
	switch len(m.targets) {
	case 1:
		lines = append(lines, "target: "+m.targets[0])
	default:
		if len(m.targets) > 1 {
			lines = append(lines, fmt.Sprintf("targets: %d", len(m.targets)))
			if len(m.targets) <= 5 {
				lines = append(lines, "  "+strings.Join(m.targets, ", "))
			}
		}
	}
	return m, tea.Println(strings.Join(lines, "\n") + "\n")
}

func (m tuiModel) handleTargetComplete(e TargetCompleteEvent) (tuiModel, tea.Cmd) {
	m.targetOutcomes = append(m.targetOutcomes, targetOutcome{
		target: e.Target,
		failed: e.Outcome == "failed",
	})
	return m, nil
}

func (m tuiModel) handleTaskStartedEvent(e TaskStartedEvent) (tuiModel, tea.Cmd) {
	if e.Target == "" {
		return m, nil
	}
	if m.hosts[e.Target] == nil {
		m.hosts[e.Target] = make(map[string]*activeTask)
		m.hostOrder = append(m.hostOrder, e.Target)
	}

	m.hosts[e.Target][e.TaskID] = &activeTask{
		id:         e.TaskID,
		name:       e.TaskName,
		actionPath: e.ActionPath,
		target:     e.Target,
		startAt:    time.Now(),
		updatedAt:  time.Now(),
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
	task.updatedAt = time.Now()
	for _, line := range e.Lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "warning") || strings.Contains(lower, "error") || strings.Contains(lower, "stderr") {
			task.alert = true
			break
		}
	}
	if !m.verbose && len(task.recentLines) > maxTaskPreviewLines {
		task.recentLines = task.recentLines[len(task.recentLines)-maxTaskPreviewLines:]
	}
	return m, nil
}

func (m tuiModel) handleTaskOK(e TaskOKEvent) (tuiModel, tea.Cmd) {
	return m.removeTaskAndCommit(e.Target, e.TaskID, e.TaskName, "ok", "", nil, e.ElapsedMs)
}

func (m tuiModel) handleTaskChanged(e TaskChangedEvent) (tuiModel, tea.Cmd) {
	return m.removeTaskAndCommit(e.Target, e.TaskID, e.TaskName, "changed", "", nil, e.ElapsedMs)
}

func (m tuiModel) handleTaskSkipped(e TaskSkippedEvent) (tuiModel, tea.Cmd) {
	return m.removeTaskAndCommit(e.Target, e.TaskID, e.TaskName, "skipped", e.Reason, nil, 0)
}

func (m tuiModel) handleTaskFailed(e TaskFailedEvent) (tuiModel, tea.Cmd) {
	m.failedTasks = append(m.failedTasks, failedTask{
		target:     e.Target,
		actionPath: "",
		name:       e.TaskName,
		message:    e.FailMessage,
		output:     e.Output,
	})
	return m.removeTaskAndCommit(e.Target, e.TaskID, e.TaskName, "failed", e.FailMessage, e.Output, e.ElapsedMs)
}

func (m tuiModel) removeTaskAndCommit(target, taskID, taskName, status, message string, output []string, elapsedMs int64) (tuiModel, tea.Cmd) {
	var elapsed time.Duration
	if elapsedMs > 0 {
		elapsed = time.Duration(elapsedMs) * time.Millisecond
	}

	if host := m.hosts[target]; host != nil {
		if task := host[taskID]; task != nil {
			if elapsed == 0 {
				elapsed = time.Since(task.startAt)
			}
			delete(host, taskID)
		}
	}
	m.taskOrder[target] = removeOrderedValue(m.taskOrder[target], taskID)

	switch status {
	case "ok":
		m.okCount++
	case "changed":
		m.changedCount++
	case "failed":
		m.failedCount++
	case "skipped":
		m.skippedCount++
	}

	left := tsIconForMode(status, m.isCheckMode()) + " " + taskName
	if m.shouldShowHostLabels() && target != "" {
		left = "[" + m.displayTarget(target) + "] " + left
	}
	right := ""
	if elapsed > 0 && status != "skipped" {
		right = formatElapsed(elapsed)
	}
	line := tsRow(padLine(left, right, m.width))

	var detailLines []string
	switch status {
	case "failed":
		msg := strings.TrimSpace(message)
		if msg == "" {
			msg = "task failed"
		}
		detailLines = append(detailLines, tsOutputLines(2, "ERROR: "+msg, m.width)...)
		if len(output) > 0 {
			detailLines = append(detailLines, tsOutputLine(2, "output:"))
			for _, l := range limitFailureOutput(m.maxFailLines, output) {
				detailLines = append(detailLines, tsOutputLines(4, l, m.width)...)
			}
			if len(output) > m.maxFailLines {
				detailLines = append(detailLines, tsOutputLines(2, fmt.Sprintf("output truncated: showing last %d of %d lines", m.maxFailLines, len(output)), m.width)...)
			}
		}
		detailLines = append(detailLines, tsOutputLines(2, "target stopped: remaining tasks were not run", m.width)...)
	case "skipped":
		if message != "" {
			detailLines = tsOutputLines(2, "reason: "+message, m.width)
		}
	case "changed":
		if detail := changedDetail(message, m.isCheckMode()); detail != "" {
			detailLines = tsOutputLines(2, detail, m.width)
		}
	case "ok":
		if detail := okDetail(message); detail != "" {
			detailLines = tsOutputLines(2, detail, m.width)
		}
	}

	var b strings.Builder
	b.WriteString(line)
	for _, dl := range detailLines {
		b.WriteByte('\n')
		b.WriteString(dl)
	}
	return m, tea.Println(b.String())
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
		b.WriteString("In Progress\n")
		for _, activity := range activities {
			b.WriteString(m.renderActivity(activity))
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n") + "\n"
	}

	runningTargets := activeTargetCount(activities, running)
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
	sort.SliceStable(running, func(i, j int) bool {
		left := running[i]
		right := running[j]
		if left.alert != right.alert {
			return left.alert
		}
		if !left.startAt.Equal(right.startAt) {
			return left.startAt.Before(right.startAt)
		}
		return left.updatedAt.After(right.updatedAt)
	})
	return running
}

func (m tuiModel) renderActivity(activity *activeActivity) string {
	spin := tsSpin.Render(strings.TrimRight(m.spinner.View(), " "))
	timer := tsElapsed.Render(formatElapsed(time.Since(activity.startAt)))
	message := strings.TrimSpace(activity.message)
	if m.shouldShowHostLabels() && activity.target != "" {
		message = "[" + m.displayTarget(activity.target) + "] " + message
	}
	return tsRow(spin, tsMuted.Render(message), timer)
}

func (m tuiModel) renderRunning(task *activeTask, dense bool) string {
	spin := tsSpin.Render(strings.TrimRight(m.spinner.View(), " "))
	timer := tsElapsed.Render(formatElapsed(time.Since(task.startAt)))

	var b strings.Builder
	if task.actionPath != "" {
		header := renderDisplayPath(task.actionPath)
		if m.shouldShowHostLabels() && task.target != "" {
			header = "[" + m.displayTarget(task.target) + "] " + header
		}
		b.WriteString(header)
		b.WriteByte('\n')
		line := padLine("  "+spin+" "+task.name, timer, m.width)
		b.WriteString(tsRow(line))
	} else {
		left := spin + " " + task.name
		if m.shouldShowHostLabels() && task.target != "" {
			left = "[" + m.displayTarget(task.target) + "] " + left
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
	if len(m.failedTasks) == 0 && m.total() == 0 {
		return ""
	}

	totalElapsed := m.elapsed()

	var b strings.Builder
	b.WriteByte('\n')
	b.WriteString("Recap\n")

	overallIcon := tsOK.Render("✓")
	statusWord := "complete"
	if m.failedCount > 0 {
		overallIcon = tsFailed.Render("x")
		statusWord = "failed"
	}
	b.WriteString(tsRow(overallIcon, tsBold.Render(titleRunMode(m.mode)+" "+statusWord), tsElapsed.Render(formatElapsed(totalElapsed))))
	b.WriteByte('\n')

	totals := recapTotals([]struct{ ok, changed, failed, skipped int }{
		{ok: m.okCount, changed: m.changedCount, failed: m.failedCount, skipped: m.skippedCount},
	})
	b.WriteString("  tasks: " + renderTaskTotals(totals, m.isCheckMode(), m.warningCount) + "\n")

	if m.failedCount > 0 {
		b.WriteByte('\n')
		b.WriteString("Needs attention\n")
		for _, failed := range m.failedTasks {
			path := renderTaskFailurePath(failed.actionPath, failed.name)
			b.WriteString("  [" + m.displayTarget(failed.target) + "] " + path + "\n")
		}
		if m.runDir != "" {
			b.WriteString("  Run directory: " + m.runDir + "\n")
		}
	}
	return b.String()
}

func (m tuiModel) renderDivider() string {
	return tsDivider.Render(strings.Repeat("─", m.divWidth()))
}

func (m tuiModel) shouldShowHostLabels() bool {
	if m.playStarted {
		return len(m.targets) != 1
	}
	return true
}

func (m tuiModel) displayTarget(target string) string {
	if target == "" {
		return "local"
	}
	if len(m.targets) == 1 && m.targets[0] == "local" && target == "localhost" {
		return "local"
	}
	return target
}

func (m tuiModel) isCheckMode() bool {
	return m.mode == "check"
}

func (m tuiModel) targetCounts() (done, failed int) {
	for _, oc := range m.targetOutcomes {
		if oc.failed {
			failed++
		} else {
			done++
		}
	}
	return done, failed
}

func activeTargetCount(activities []*activeActivity, tasks []*activeTask) int {
	targets := make(map[string]struct{})
	for _, activity := range activities {
		targets[activity.target] = struct{}{}
	}
	for _, task := range tasks {
		targets[task.target] = struct{}{}
	}
	return len(targets)
}

func visibleLiveEntries(activities []*activeActivity, tasks []*activeTask, limit int) ([]*activeActivity, []*activeTask, int) {
	if limit <= 0 {
		return activities, tasks, 0
	}
	remaining := limit
	visibleActivities := activities
	if len(visibleActivities) > remaining {
		hidden := len(visibleActivities) - remaining + len(tasks)
		return visibleActivities[:remaining], nil, hidden
	}

	remaining -= len(visibleActivities)
	visibleTasks := tasks
	if len(visibleTasks) > remaining {
		hidden := len(visibleTasks) - remaining
		return visibleActivities, visibleTasks[:remaining], hidden
	}
	return visibleActivities, visibleTasks, 0
}

func (m tuiModel) elapsed() time.Duration {
	if m.startedAt.IsZero() {
		return 0
	}
	return time.Since(m.startedAt)
}

func tsIconForMode(status string, checkMode bool) string {
	switch status {
	case "ok":
		return tsOK.Render("✓")
	case "changed":
		if checkMode {
			return tsChanged.Render("!")
		}
		return tsChanged.Render("~")
	case "failed":
		return tsFailed.Render("x")
	case "skipped":
		return tsSkipped.Render("-")
	default:
		return " "
	}
}

func (m tuiModel) renderFooter(runningCount int) string {
	if runningCount == 0 && m.total() == 0 {
		return ""
	}
	done, failed := m.targetCounts()
	waiting := 0
	if len(m.targets) > 0 {
		waiting = max(len(m.targets)-done-failed-runningCount, 0)
	}
	line1 := fmt.Sprintf(
		"%s %s   Phase %s",
		titleRunMode(m.mode),
		formatElapsed(m.elapsed()),
		titleRunMode(m.mode),
	)
	switch {
	case len(m.targets) == 1:
		line1 += "   Target " + m.targets[0]
	case len(m.targets) > 1:
		line1 += fmt.Sprintf("   Targets %d   Done %d   Running %d   Waiting %d   Failed %d", len(m.targets), done, runningCount, waiting, failed)
	default:
		line1 += fmt.Sprintf("   Running %d", runningCount)
	}

	changedLabel := "Changed"
	if m.isCheckMode() {
		changedLabel = "Would change"
	}
	line2 := fmt.Sprintf(
		"Tasks %d done   OK %d   %s %d   Skipped %d   Failed %d",
		m.total(),
		m.okCount,
		changedLabel,
		m.changedCount,
		m.skippedCount,
		m.failedCount,
	)
	if m.warningCount > 0 {
		line2 += fmt.Sprintf("   Warnings %d", m.warningCount)
	}
	return line1 + "\n" + line2
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

func removeOrderedValue(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return values
}