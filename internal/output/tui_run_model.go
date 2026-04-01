package output

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type tuiEventMsg struct {
	event Event
}

type tuiDoneMsg struct{}

type tuiModel struct {
	spinner           spinner.Model
	help              help.Model
	keys              runKeyMap
	events            chan Event
	interrupt         func()
	command           string
	interactive       bool
	hosts             map[string]*hostView
	hostOrder         []string
	globalPhases      []phaseView
	errors            []string
	selectedHost      int
	width             int
	height            int
	tabPager          paginator.Model
	viewport          viewport.Model
	collapseCompleted bool
	failedOnly        bool
	showHelp          bool
	done              bool
}

func newTUIModel(events chan Event, options Options) tuiModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(tuiSpinnerStyle),
	)
	command := options.Command
	if command == "" {
		command = "run"
	}
	return tuiModel{
		spinner:     s,
		help:        tuiNewHelp(),
		keys:        newRunKeyMap(),
		events:      events,
		interrupt:   options.Interrupt,
		command:     command,
		interactive: options.Input != nil,
		hosts:       make(map[string]*hostView),
		width:       defaultTUIWidth,
		height:      defaultTUIHeight,
		tabPager:    newTUITabPager(),
		viewport:    viewport.New(defaultTUIWidth, defaultTUIHeight-6),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForEvent())
}

func (m tuiModel) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.events
		if !ok {
			return tuiDoneMsg{}
		}
		return tuiEventMsg{event: event}
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(40, msg.Width)
		m.height = max(14, msg.Height)
		m.syncViewport()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tuiEventMsg:
		m = m.applyEvent(msg.event)
		return m, m.waitForEvent()
	case tuiDoneMsg:
		m.done = true
		if !m.interactive {
			return m, tea.Quit
		}
		m.syncViewport()
		return m, nil
	}

	return m, nil
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		if m.done {
			return m, tea.Quit
		}
		if m.interrupt != nil {
			m.interrupt()
		}
		return m, tea.Quit
	}

	switch msg.String() {
	case "ctrl+c":
		if m.interrupt != nil {
			m.interrupt()
		}
		return m, tea.Quit
	case "q":
		if m.done || !m.interactive {
			return m, tea.Quit
		}
		return m, nil
	case "?":
		m.showHelp = !m.showHelp
	case "c":
		m.collapseCompleted = !m.collapseCompleted
	case "f":
		m.failedOnly = !m.failedOnly
	case "enter", " ":
		if task := m.currentTask(); task != nil {
			task.expanded = !task.expanded
		}
	case "up", "k":
		if host := m.currentHost(); host != nil {
			host.selectedTask--
		}
	case "down", "j":
		if host := m.currentHost(); host != nil {
			host.selectedTask++
		}
	case "left", "h":
		m.selectedHost--
	case "right", "l":
		m.selectedHost++
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			digit := int(msg.Runes[0] - '1')
			if digit >= 0 && digit < len(m.hostOrder) {
				m.selectedHost = digit
			}
		}
	}

	m.clampSelection()
	m.syncViewport()
	return m, nil
}

func (m *tuiModel) syncViewport() {
	header := m.renderHeader()
	tabs := m.renderTabs()
	footer := m.renderFooter()
	m.viewport.Width = max(20, m.width)
	m.viewport.Height = viewportBodyHeight(m.height, header, tabs, footer)

	body, starts, ends := m.renderTaskStream()
	m.viewport.SetContent(body)

	host := m.currentHost()
	if host == nil || len(starts) == 0 {
		return
	}
	selected := clamp(0, host.selectedTask, len(starts)-1)
	if starts[selected] < m.viewport.YOffset {
		m.viewport.YOffset = starts[selected]
	}
	bottom := m.viewport.YOffset + max(1, m.viewport.Height) - 1
	if ends[selected] > bottom {
		m.viewport.YOffset = max(0, ends[selected]-m.viewport.Height+1)
	}
}

func (m *tuiModel) clampSelection() {
	if len(m.hostOrder) == 0 {
		m.selectedHost = 0
		return
	}
	m.selectedHost = clamp(0, m.selectedHost, len(m.hostOrder)-1)

	host := m.currentHost()
	if host == nil {
		return
	}
	ids := m.visibleTasks(host)
	if len(ids) == 0 {
		host.selectedTask = 0
		return
	}
	host.selectedTask = clamp(0, host.selectedTask, len(ids)-1)
}

func (m tuiModel) applyEvent(event Event) tuiModel {
	switch event.Type {
	case EventPlayStart:
		if event.Target != "" {
			host := m.ensureHost(event.Target)
			host.playName = event.PlayName
		}
	case EventPhaseStart:
		if event.Target == "" {
			m.globalPhases = upsertPhase(m.globalPhases, event.Phase, "", true)
			break
		}
		host := m.ensureHost(event.Target)
		host.phases = upsertPhase(host.phases, event.Phase, "", true)
	case EventPhaseEnd:
		if event.Target == "" {
			m.globalPhases = upsertPhase(m.globalPhases, event.Phase, event.Status, false)
			break
		}
		host := m.ensureHost(event.Target)
		host.phases = upsertPhase(host.phases, event.Phase, event.Status, false)
		if event.TaskTotal > 0 {
			host.totalTasks = event.TaskTotal
		}
	case EventTaskStart:
		host := m.ensureHost(event.Target)
		task := host.ensureTask(event.TaskID, event.TaskName, event.Module)
		task.running = true
		task.status = ""
		task.message = ""
		task.module = event.Module
		if event.TaskTotal > 0 {
			host.totalTasks = event.TaskTotal
		}
	case EventTaskLog:
		host := m.ensureHost(event.Target)
		task := host.ensureTask(event.TaskID, event.TaskName, event.Module)
		task.module = event.Module
		task.appendLog(event.Stream, event.Line)
	case EventTaskResult:
		host := m.ensureHost(event.Target)
		task := host.ensureTask(event.TaskID, event.TaskName, event.Module)
		task.running = false
		task.status = event.Status
		task.message = event.Message
		task.module = event.Module
		if event.Status == "failed" {
			task.expanded = true
			if len(task.logs) == 0 && event.Message != "" {
				task.appendLog("stderr", event.Message)
			}
		}
		host.recomputeRecap()
	case EventPlayEnd:
		host := m.ensureHost(event.Target)
		host.done = true
		host.recap = recapCounts{
			ok:      event.OKCount,
			changed: event.ChangedCount,
			failed:  event.FailedCount,
			skipped: event.SkippedCount,
		}
	case EventError:
		message := event.Message
		if event.Error != nil {
			message = event.Error.Error()
		}
		if message != "" {
			m.errors = append(m.errors, message)
		}
	}

	m.clampSelection()
	m.syncViewport()
	return m
}

func (m *tuiModel) ensureHost(name string) *hostView {
	if name == "" {
		name = "local"
	}
	if host, ok := m.hosts[name]; ok {
		return host
	}
	host := &hostView{
		name:  name,
		tasks: make(map[string]*taskView),
	}
	m.hosts[name] = host
	m.hostOrder = append(m.hostOrder, name)
	return host
}

func (h *hostView) ensureTask(taskID, taskName, module string) *taskView {
	key := taskID
	if key == "" {
		key = taskName
	}
	if key == "" {
		key = fmt.Sprintf("task-%d", len(h.taskOrder)+1)
	}
	if task, ok := h.tasks[key]; ok {
		if taskName != "" {
			task.name = taskName
		}
		if module != "" {
			task.module = module
		}
		return task
	}
	task := &taskView{id: key, name: taskName, module: module}
	h.tasks[key] = task
	h.taskOrder = append(h.taskOrder, key)
	return task
}

func (h *hostView) recomputeRecap() {
	var recap recapCounts
	for _, id := range h.taskOrder {
		switch h.tasks[id].status {
		case "ok":
			recap.ok++
		case "changed":
			recap.changed++
		case "failed":
			recap.failed++
		case "skipped":
			recap.skipped++
		}
	}
	h.recap = recap
}

func (t *taskView) appendLog(stream, line string) {
	if line == "" {
		return
	}
	t.logs = append(t.logs, taskLogLine{stream: stream, line: line})
	t.logBytes += len(line)
	for len(t.logs) > maxTaskLogLines || t.logBytes > maxTaskLogBytes {
		t.logBytes -= len(t.logs[0].line)
		t.logs = t.logs[1:]
	}
}

func upsertPhase(phases []phaseView, name, status string, running bool) []phaseView {
	for i := range phases {
		if phases[i].name != name {
			continue
		}
		phases[i].running = running
		if status != "" {
			phases[i].status = status
		} else if running {
			phases[i].status = ""
		}
		return phases
	}
	return append(phases, phaseView{name: name, status: status, running: running})
}

func (m tuiModel) currentHost() *hostView {
	if len(m.hostOrder) == 0 {
		return nil
	}
	return m.hosts[m.hostOrder[clamp(0, m.selectedHost, len(m.hostOrder)-1)]]
}

func (m tuiModel) currentTask() *taskView {
	host := m.currentHost()
	if host == nil {
		return nil
	}
	ids := m.visibleTasks(host)
	if len(ids) == 0 {
		return nil
	}
	return host.tasks[ids[clamp(0, host.selectedTask, len(ids)-1)]]
}

func (m tuiModel) visibleTasks(host *hostView) []string {
	if host == nil {
		return nil
	}
	ids := make([]string, 0, len(host.taskOrder))
	for _, id := range host.taskOrder {
		task := host.tasks[id]
		if m.failedOnly && task.status != "failed" {
			continue
		}
		if m.collapseCompleted && task.status != "" && task.status != "failed" && !task.running {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (h *hostView) completedCount() int {
	return h.recap.ok + h.recap.changed + h.recap.failed + h.recap.skipped
}

func (m tuiModel) previewLogs(task *taskView) []taskLogLine {
	previewCount := min(3, len(task.logs))
	return task.logs[len(task.logs)-previewCount:]
}

func (m tuiModel) expandedTaskLines(task *taskView) []ScreenLine {
	lines := make([]ScreenLine, 0, min(8, len(task.logs))+1)
	if task.message != "" && !m.logContainsMessage(task) {
		lines = append(lines, ScreenLine{Text: task.message, Tone: task.status})
	}
	if len(task.logs) == 0 {
		return lines
	}
	start := max(0, len(task.logs)-8)
	if start > 0 {
		lines = append(lines, ScreenLine{Text: fmt.Sprintf("%d earlier log lines hidden", start), Tone: "info"})
	}
	for _, entry := range task.logs[start:] {
		lines = append(lines, ScreenLine{
			Prefix: logPrefix(entry.stream),
			Text:   entry.line,
			Tone:   detailLineTone(entry.stream),
		})
	}
	return lines
}

func (m tuiModel) expandedTaskTitle(task *taskView) string {
	if len(task.logs) > 0 {
		return "Recent logs"
	}
	return "Details"
}

func (m tuiModel) logContainsMessage(task *taskView) bool {
	needle := strings.TrimSpace(task.message)
	if needle == "" {
		return false
	}
	for _, entry := range task.logs {
		if strings.TrimSpace(entry.line) == needle {
			return true
		}
	}
	return false
}

func lineTone(stream string) string {
	switch strings.ToLower(stream) {
	case "stderr":
		return "failed"
	case "stdout":
		return "ok"
	default:
		return "info"
	}
}

func detailLineTone(stream string) string {
	switch strings.ToLower(stream) {
	case "stderr":
		return "failed"
	default:
		return ""
	}
}

func logPrefix(stream string) string {
	switch strings.ToLower(stream) {
	case "stderr":
		return "err"
	case "stdout":
		return "out"
	default:
		return "inf"
	}
}
