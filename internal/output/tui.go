package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	maxTaskLogLines = 200
	maxTaskLogBytes = 32 * 1024
)

var (
	tuiColorOK       = lipgloss.Color("42")
	tuiColorChanged  = lipgloss.Color("214")
	tuiColorFailed   = lipgloss.Color("196")
	tuiColorSkipped  = lipgloss.Color("244")
	tuiColorInfo     = lipgloss.Color("81")
	tuiColorDim      = lipgloss.Color("241")
	tuiColorBorder   = lipgloss.Color("63")
	tuiColorSelected = lipgloss.Color("229")

	tuiBaseStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	tuiTitleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	tuiSubtleStyle     = lipgloss.NewStyle().Foreground(tuiColorDim)
	tuiPanelStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(tuiColorBorder).Padding(0, 1)
	tuiFocusedPanel    = tuiPanelStyle.Copy().BorderForeground(tuiColorSelected)
	tuiSelectedRow     = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("230"))
	tuiHelpStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	tuiSpinnerStyle    = lipgloss.NewStyle().Foreground(tuiColorInfo)
	tuiStatusOKStyle   = lipgloss.NewStyle().Foreground(tuiColorOK).Bold(true)
	tuiStatusChgStyle  = lipgloss.NewStyle().Foreground(tuiColorChanged).Bold(true)
	tuiStatusFailStyle = lipgloss.NewStyle().Foreground(tuiColorFailed).Bold(true)
	tuiStatusSkipStyle = lipgloss.NewStyle().Foreground(tuiColorSkipped).Bold(true)
)

type taskLogLine struct {
	stream string
	line   string
}

type taskView struct {
	id       string
	name     string
	module   string
	status   string
	message  string
	running  bool
	expanded bool
	logs     []taskLogLine
	logBytes int
}

type phaseView struct {
	name    string
	status  string
	running bool
}

type hostView struct {
	name       string
	playName   string
	phases     []phaseView
	tasks      map[string]*taskView
	taskOrder  []string
	totalTasks int
	recap      recapCounts
	done       bool
}

type recapCounts struct {
	ok, changed, failed, skipped int
}

type focusPane int

const (
	focusHosts focusPane = iota
	focusTasks
	focusLogs
)

type tuiEventMsg struct {
	event Event
}

type tuiDoneMsg struct{}

type tuiModel struct {
	spinner           spinner.Model
	events            chan Event
	interrupt         func()
	hosts             map[string]*hostView
	hostOrder         []string
	globalPhases      []phaseView
	errors            []string
	selectedHost      int
	selectedTask      int
	width             int
	height            int
	focus             focusPane
	showLogPane       bool
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
	return tuiModel{
		spinner:     s,
		events:      events,
		interrupt:   options.Interrupt,
		hosts:       make(map[string]*hostView),
		width:       120,
		height:      30,
		focus:       focusHosts,
		showLogPane: true,
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
		m.width = msg.Width
		m.height = msg.Height
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
		return m, tea.Quit
	}

	return m, nil
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.interrupt != nil {
			m.interrupt()
		}
		return m, tea.Quit
	case "q":
		if m.done {
			return m, tea.Quit
		}
		return m, nil
	case "?":
		m.showHelp = !m.showHelp
	case "tab":
		m.focus = m.nextFocus()
	case "l":
		m.showLogPane = !m.showLogPane
		if !m.showLogPane && m.focus == focusLogs {
			m.focus = focusTasks
		}
	case "c":
		m.collapseCompleted = !m.collapseCompleted
	case "f":
		m.failedOnly = !m.failedOnly
	case "enter":
		if task := m.currentTask(); task != nil {
			task.expanded = !task.expanded
		}
	case "up":
		m.moveSelection(-1)
	case "down":
		m.moveSelection(1)
	case "left":
		if m.focus > focusHosts {
			m.focus--
		}
	case "right":
		if m.focus < m.maxFocus() {
			m.focus++
		}
	}

	m.clampSelection()
	return m, nil
}

func (m tuiModel) nextFocus() focusPane {
	max := m.maxFocus()
	next := m.focus + 1
	if next > max {
		return focusHosts
	}
	return next
}

func (m tuiModel) maxFocus() focusPane {
	if m.showLogPane {
		return focusLogs
	}
	return focusTasks
}

func (m *tuiModel) moveSelection(delta int) {
	switch m.focus {
	case focusHosts:
		m.selectedHost += delta
	case focusTasks, focusLogs:
		m.selectedTask += delta
	}
}

func (m *tuiModel) clampSelection() {
	if len(m.hostOrder) == 0 {
		m.selectedHost = 0
		m.selectedTask = 0
		return
	}

	if m.selectedHost < 0 {
		m.selectedHost = 0
	}
	if m.selectedHost >= len(m.hostOrder) {
		m.selectedHost = len(m.hostOrder) - 1
	}

	tasks := m.visibleTasks(m.currentHost())
	if len(tasks) == 0 {
		m.selectedTask = 0
		return
	}
	if m.selectedTask < 0 {
		m.selectedTask = 0
	}
	if m.selectedTask >= len(tasks) {
		m.selectedTask = len(tasks) - 1
	}
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
	task := &taskView{
		id:     key,
		name:   taskName,
		module: module,
	}
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
	entry := taskLogLine{stream: stream, line: line}
	t.logs = append(t.logs, entry)
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
	return append(phases, phaseView{
		name:    name,
		status:  status,
		running: running,
	})
}

func (m tuiModel) View() string {
	header := m.renderHeader()
	body := m.renderBody()
	footer := m.renderFooter()

	parts := []string{header, body, footer}
	if m.showHelp {
		parts = append(parts, m.renderHelp())
	}

	return strings.Join(parts, "\n")
}

func (m tuiModel) renderHeader() string {
	title := tuiTitleStyle.Render("Preflight Run Dashboard")
	subtitle := tuiSubtleStyle.Render("tab switch pane | enter details | l logs | c collapse completed | f failed only | ? help")
	phaseLine := m.renderGlobalPhases()
	if phaseLine != "" {
		return lipgloss.JoinVertical(lipgloss.Left, title, subtitle, phaseLine)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle)
}

func (m tuiModel) renderGlobalPhases() string {
	if len(m.globalPhases) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.globalPhases))
	for _, phase := range m.globalPhases {
		label := phase.name
		if phase.running {
			label = fmt.Sprintf("%s %s", m.spinner.View(), label)
		} else if phase.status != "" {
			label = fmt.Sprintf("%s %s", statusGlyph(phase.status), label)
		}
		parts = append(parts, label)
	}
	return tuiSubtleStyle.Render("Global: " + strings.Join(parts, "  "))
}

func (m tuiModel) renderBody() string {
	hostWidth := clamp(26, m.width/5, 34)
	logWidth := 0
	if m.showLogPane {
		logWidth = clamp(34, m.width/3, 52)
	}
	taskWidth := m.width - hostWidth - logWidth - 4
	if taskWidth < 36 {
		taskWidth = 36
	}

	hostPanel := m.renderPanel("Hosts", m.renderHosts(), hostWidth, m.focus == focusHosts)
	taskPanel := m.renderPanel("Tasks", m.renderTasks(), taskWidth, m.focus == focusTasks)

	panels := []string{hostPanel, taskPanel}
	if m.showLogPane {
		logPanel := m.renderPanel("Logs", m.renderLogs(), logWidth, m.focus == focusLogs)
		panels = append(panels, logPanel)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panels...)
}

func (m tuiModel) renderPanel(title, body string, width int, focused bool) string {
	style := tuiPanelStyle
	if focused {
		style = tuiFocusedPanel
	}
	return style.Width(width).Render(title + "\n" + body)
}

func (m tuiModel) renderHosts() string {
	if len(m.hostOrder) == 0 {
		return tuiSubtleStyle.Render("Waiting for hosts...")
	}

	rows := make([]string, 0, len(m.hostOrder))
	for i, name := range m.hostOrder {
		host := m.hosts[name]
		line := fmt.Sprintf("%s %s", m.hostStatusGlyph(host), name)
		if host.totalTasks > 0 {
			line += tuiSubtleStyle.Render(fmt.Sprintf("  %d tasks", host.totalTasks))
		}
		recap := fmt.Sprintf("ok=%d ~=%d x=%d -=%d", host.recap.ok, host.recap.changed, host.recap.failed, host.recap.skipped)
		row := lipgloss.JoinVertical(lipgloss.Left, line, tuiSubtleStyle.Render(recap), tuiSubtleStyle.Render(m.hostStatusText(host)))
		if i == m.selectedHost {
			row = tuiSelectedRow.Width(24).Render(row)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) renderTasks() string {
	host := m.currentHost()
	if host == nil {
		return tuiSubtleStyle.Render("No host selected")
	}

	ids := m.visibleTasks(host)
	if len(ids) == 0 {
		return tuiSubtleStyle.Render("No tasks match the current filters")
	}

	rows := make([]string, 0, len(ids))
	for i, id := range ids {
		task := host.tasks[id]
		line := fmt.Sprintf("%s %s", m.taskStatusGlyph(task), task.name)
		if task.module != "" {
			line += tuiSubtleStyle.Render(fmt.Sprintf("  (%s)", task.module))
		}
		if task.running {
			line += tuiSubtleStyle.Render("  running")
		}
		row := line
		if task.expanded {
			meta := []string{}
			if task.message != "" {
				meta = append(meta, task.message)
			}
			if len(task.logs) > 0 {
				meta = append(meta, fmt.Sprintf("%d log lines buffered", len(task.logs)))
			}
			if len(meta) > 0 {
				row = lipgloss.JoinVertical(lipgloss.Left, row, tuiSubtleStyle.Render(strings.Join(meta, " | ")))
			}
		}
		if i == m.selectedTask {
			row = tuiSelectedRow.Render(row)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) renderLogs() string {
	task := m.currentTask()
	if task == nil {
		return tuiSubtleStyle.Render("No task selected")
	}
	if len(task.logs) == 0 {
		if task.message != "" {
			return task.message
		}
		return tuiSubtleStyle.Render("No task logs captured yet")
	}

	rows := make([]string, 0, len(task.logs))
	for _, line := range task.logs {
		prefix := "[" + line.stream + "] "
		rows = append(rows, streamStyle(line.stream).Render(prefix)+line.line)
	}
	if len(task.logs) == maxTaskLogLines || task.logBytes >= maxTaskLogBytes {
		rows = append(rows, tuiSubtleStyle.Render("... older log lines truncated in TUI"))
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) renderFooter() string {
	parts := []string{
		tuiHelpStyle.Render(fmt.Sprintf("Focus: %s", m.focusLabel())),
		tuiHelpStyle.Render(fmt.Sprintf("Collapse completed: %t", m.collapseCompleted)),
		tuiHelpStyle.Render(fmt.Sprintf("Failed only: %t", m.failedOnly)),
	}
	if len(m.errors) > 0 {
		parts = append(parts, tuiStatusFailStyle.Render(m.errors[len(m.errors)-1]))
	}
	if !m.done {
		parts = append(parts, tuiSubtleStyle.Render("Ctrl+C cancels run"))
	} else {
		parts = append(parts, tuiSubtleStyle.Render("q exits"))
	}
	return strings.Join(parts, "   ")
}

func (m tuiModel) renderHelp() string {
	lines := []string{
		"Keyboard",
		"tab cycle panes",
		"up/down move selection",
		"enter expand selected task details",
		"l toggle log pane",
		"c hide completed tasks",
		"f show failed tasks only",
		"? toggle help",
		"q quit after completion",
	}
	return tuiPanelStyle.Width(clamp(32, m.width/3, 44)).Render(strings.Join(lines, "\n"))
}

func (m tuiModel) focusLabel() string {
	switch m.focus {
	case focusHosts:
		return "hosts"
	case focusLogs:
		return "logs"
	default:
		return "tasks"
	}
}

func (m tuiModel) currentHost() *hostView {
	if len(m.hostOrder) == 0 {
		return nil
	}
	index := m.selectedHost
	if index < 0 {
		index = 0
	}
	if index >= len(m.hostOrder) {
		index = len(m.hostOrder) - 1
	}
	return m.hosts[m.hostOrder[index]]
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
	index := m.selectedTask
	if index < 0 {
		index = 0
	}
	if index >= len(ids) {
		index = len(ids) - 1
	}
	return host.tasks[ids[index]]
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

func (m tuiModel) hostStatusGlyph(host *hostView) string {
	for _, phase := range host.phases {
		if phase.running {
			return m.spinner.View()
		}
	}
	for _, id := range host.taskOrder {
		if host.tasks[id].running {
			return m.spinner.View()
		}
	}
	switch {
	case host.recap.failed > 0:
		return tuiStatusFailStyle.Render("x")
	case host.done:
		return tuiStatusOKStyle.Render("✓")
	default:
		return tuiSubtleStyle.Render("•")
	}
}

func (m tuiModel) hostStatusText(host *hostView) string {
	for _, phase := range host.phases {
		if phase.running {
			return "phase: " + phase.name
		}
	}
	for _, id := range host.taskOrder {
		task := host.tasks[id]
		if task.running {
			return "task: " + task.name
		}
	}
	if host.done {
		return "completed"
	}
	return "waiting"
}

func (m tuiModel) taskStatusGlyph(task *taskView) string {
	if task.running {
		return m.spinner.View()
	}
	return statusGlyph(task.status)
}

func statusGlyph(status string) string {
	switch status {
	case "ok":
		return tuiStatusOKStyle.Render("✓")
	case "changed":
		return tuiStatusChgStyle.Render("~")
	case "failed":
		return tuiStatusFailStyle.Render("x")
	case "skipped":
		return tuiStatusSkipStyle.Render("-")
	default:
		return tuiSubtleStyle.Render("•")
	}
}

func streamStyle(stream string) lipgloss.Style {
	switch stream {
	case "stderr":
		return lipgloss.NewStyle().Foreground(tuiColorFailed)
	case "stdout":
		return lipgloss.NewStyle().Foreground(tuiColorOK)
	default:
		return lipgloss.NewStyle().Foreground(tuiColorInfo)
	}
}

func clamp(minimum, value, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

// TUIRenderer implements Renderer using a Bubble Tea program.
type TUIRenderer struct {
	program *tea.Program
	events  chan Event
	done    chan struct{}
}

// NewTUIRenderer creates a TUIRenderer that writes to w.
func NewTUIRenderer(w io.Writer) *TUIRenderer {
	return NewTUIRendererWithOptions(w, Options{})
}

// NewTUIRendererWithOptions creates a TUIRenderer with explicit options.
func NewTUIRendererWithOptions(w io.Writer, options Options) *TUIRenderer {
	events := make(chan Event, 256)
	model := newTUIModel(events, options)
	input := options.Input
	programOptions := []tea.ProgramOption{
		tea.WithOutput(w),
		tea.WithoutSignalHandler(),
	}
	if input != nil {
		programOptions = append(programOptions, tea.WithInput(input))
	} else {
		programOptions = append(programOptions, tea.WithInput(nil))
	}
	prog := tea.NewProgram(model, programOptions...)
	renderer := &TUIRenderer{
		program: prog,
		events:  events,
		done:    make(chan struct{}),
	}
	go func() {
		defer close(renderer.done)
		_, _ = prog.Run()
	}()
	return renderer
}

// Emit sends an event to the running Bubble Tea program.
func (r *TUIRenderer) Emit(event Event) {
	r.events <- event
}

// Close shuts down the Bubble Tea program and waits for it to exit.
func (r *TUIRenderer) Close() {
	close(r.events)
	<-r.done
}
