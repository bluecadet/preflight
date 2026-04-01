package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	name         string
	playName     string
	phases       []phaseView
	tasks        map[string]*taskView
	taskOrder    []string
	totalTasks   int
	recap        recapCounts
	done         bool
	selectedTask int
}

type recapCounts struct {
	ok, changed, failed, skipped int
}

type runKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Enter    key.Binding
	Collapse key.Binding
	Failed   key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func newRunKeyMap() runKeyMap {
	return runKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "task up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "task down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←/h", "prev host"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→/l", "next host"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "details"),
		),
		Collapse: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "collapse done"),
		),
		Failed: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "failed only"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
	}
}

func (k runKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Left, k.Right, k.Enter, k.Collapse, k.Failed, k.Help, k.Quit}
}

func (k runKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Enter, k.Collapse, k.Failed},
		{k.Help, k.Quit},
	}
}

type tuiEventMsg struct {
	event Event
}

type tuiDoneMsg struct{}

type tuiModel struct {
	spinner           spinner.Model
	progress          progress.Model
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
	p := progress.New()
	h := tuiNewHelp()
	keys := newRunKeyMap()
	command := options.Command
	if command == "" {
		command = "run"
	}
	return tuiModel{
		spinner:     s,
		progress:    p,
		help:        h,
		keys:        keys,
		events:      events,
		interrupt:   options.Interrupt,
		command:     command,
		interactive: options.Input != nil,
		hosts:       make(map[string]*hostView),
		width:       defaultTUIWidth,
		height:      defaultTUIHeight,
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
	case "enter":
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
	bodyHeight := max(4, m.height-lipgloss.Height(header)-lipgloss.Height(tabs)-lipgloss.Height(footer)-2)
	m.viewport.Width = max(20, m.width)
	m.viewport.Height = bodyHeight

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
	if m.selectedHost < 0 {
		m.selectedHost = 0
	}
	if m.selectedHost >= len(m.hostOrder) {
		m.selectedHost = len(m.hostOrder) - 1
	}
	host := m.currentHost()
	if host == nil {
		return
	}
	ids := m.visibleTasks(host)
	if len(ids) == 0 {
		host.selectedTask = 0
		return
	}
	if host.selectedTask < 0 {
		host.selectedTask = 0
	}
	if host.selectedTask >= len(ids) {
		host.selectedTask = len(ids) - 1
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
	tabs := m.renderTabs()
	footer := m.renderFooter()

	bodyHeight := max(4, m.height-lipgloss.Height(header)-lipgloss.Height(tabs)-lipgloss.Height(footer)-2)
	m.viewport.Width = max(20, m.width)
	m.viewport.Height = bodyHeight
	m.syncViewport()

	parts := []string{header}
	if tabs != "" {
		parts = append(parts, tabs)
	}
	parts = append(parts, m.viewport.View(), footer)
	return strings.Join(parts, "\n")
}

func (m tuiModel) renderHeader() string {
	title := lipgloss.JoinHorizontal(lipgloss.Center,
		tuiTitleStyle.Render("Preflight"),
		"  ",
		tuiCommandStyle.Render(m.command),
	)
	status := renderStatusChip(m.overallStatus())
	lines := []string{spaceBetween(m.width, title, status)}
	if subject := m.subjectLine(); subject != "" {
		lines = append(lines, tuiSubtleStyle.Render(truncateText(subject, m.width)))
	}
	if phases := m.phaseLine(); phases != "" {
		lines = append(lines, phases)
	}
	if progressLine := m.progressLine(); progressLine != "" {
		lines = append(lines, progressLine)
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) renderTabs() string {
	if len(m.hostOrder) <= 1 {
		return ""
	}
	tabs := make([]tuiTab, 0, len(m.hostOrder))
	for _, name := range m.hostOrder {
		host := m.hosts[name]
		meta := fmt.Sprintf("%d/%d", host.completedCount(), max(host.totalTasks, len(host.taskOrder)))
		tabs = append(tabs, tuiTab{
			Label:  name,
			Status: m.hostStatus(host),
			Meta:   meta,
		})
	}
	return renderTabs(tabs, m.selectedHost, m.width)
}

func (m tuiModel) renderTaskStream() (string, []int, []int) {
	host := m.currentHost()
	if host == nil {
		waiting := tuiSubtleStyle.Render("Waiting for targets...")
		return waiting, nil, nil
	}
	ids := m.visibleTasks(host)
	if len(ids) == 0 {
		empty := tuiSubtleStyle.Render("No tasks match the current filters.")
		return empty, nil, nil
	}

	width := max(24, m.width-2)
	blocks := make([]string, 0, len(ids))
	starts := make([]int, 0, len(ids))
	ends := make([]int, 0, len(ids))
	currentLine := 0
	for idx, id := range ids {
		task := host.tasks[id]
		starts = append(starts, currentLine)
		block := m.renderTaskCard(task, idx == host.selectedTask, width)
		blocks = append(blocks, block)
		currentLine += lipgloss.Height(block)
		ends = append(ends, currentLine-1)
		if idx < len(ids)-1 {
			currentLine++
		}
	}
	if host.done {
		recap := renderStats([]ScreenStat{
			{Label: "ok", Value: fmt.Sprintf("%d", host.recap.ok), Tone: "ok"},
			{Label: "changed", Value: fmt.Sprintf("%d", host.recap.changed), Tone: "changed"},
			{Label: "failed", Value: fmt.Sprintf("%d", host.recap.failed), Tone: "failed"},
			{Label: "skipped", Value: fmt.Sprintf("%d", host.recap.skipped), Tone: "skipped"},
		}, width)
		if recap != "" {
			blocks = append(blocks, tuiSectionStyle.Render("Recap")+"\n"+recap)
		}
	}
	return strings.Join(blocks, "\n\n"), starts, ends
}

func (m tuiModel) renderTaskCard(task *taskView, selected bool, width int) string {
	summaryParts := []string{m.taskStatusGlyph(task), truncateText(task.name, max(20, width-8))}
	if task.module != "" {
		summaryParts = append(summaryParts, tuiSubtleStyle.Render("("+task.module+")"))
	}
	if task.message != "" && !task.expanded {
		summaryParts = append(summaryParts, tuiSubtleStyle.Render(truncateText(task.message, max(14, width/3))))
	}
	lines := []string{strings.Join(summaryParts, "  ")}
	meta := []string{}
	if task.running {
		meta = append(meta, "running")
	}
	if joined := joinMeta(meta...); joined != "" {
		lines = append(lines, tuiSubtleStyle.Render(truncateText(joined, width)))
	}

	showPreview := !task.expanded && (task.running || task.status == "failed")
	if showPreview && len(task.logs) > 0 {
		previewLines := make([]ScreenLine, 0, min(3, len(task.logs)))
		for _, line := range m.previewLogs(task) {
			previewLines = append(previewLines, ScreenLine{
				Prefix: logPrefix(line.stream),
				Text:   line.line,
				Tone:   lineTone(line.stream),
			})
		}
		lines = append(lines, renderScreenLines(previewLines, width-2))
	}
	if task.expanded {
		detail := m.expandedTaskLines(task)
		if len(detail) > 0 {
			lines = append(lines, tuiSectionStyle.Render(m.expandedTaskTitle(task)))
			lines = append(lines, renderScreenLines(detail, width-2))
		}
	}

	block := strings.Join(lines, "\n")
	if selected {
		return tuiSelectedCardStyle.Width(width).Render(block)
	}
	if task.status == "failed" {
		return tuiMutedCardStyle.Width(width).Render(block)
	}
	return tuiCardStyle.Width(width).Render(block)
}

func (m tuiModel) renderFooter() string {
	location := ""
	host := m.currentHost()
	if host != nil {
		location = tuiSubtleStyle.Render(host.name)
		if len(m.visibleTasks(host)) > 0 {
			location = tuiSubtleStyle.Render(
				fmt.Sprintf("%s  task %d/%d", host.name, host.selectedTask+1, len(m.visibleTasks(host))),
			)
		}
	}
	if len(m.errors) > 0 {
		location = tuiStatusFailStyle.Render(truncateText(m.errors[len(m.errors)-1], max(16, m.width/2)))
	}
	m.help.ShowAll = m.showHelp
	if m.showHelp {
		return m.help.FullHelpView(m.keys.FullHelp())
	}
	helpText := m.help.ShortHelpView(m.keys.ShortHelp())
	if !m.done {
		helpText = spaceBetween(max(10, m.width/2), tuiSubtleStyle.Render("Ctrl+C cancel"), helpText)
	}
	return spaceBetween(m.width, location, helpText)
}

func (m tuiModel) subjectLine() string {
	hostCount := len(m.hostOrder)
	hostLabel := fmt.Sprintf("%d target", hostCount)
	if hostCount != 1 {
		hostLabel += "s"
	}
	playName := ""
	if host := m.currentHost(); host != nil && host.playName != "" {
		playName = "play: " + host.playName
	}
	return joinMeta(playName, hostLabel)
}

func (m tuiModel) phaseLine() string {
	parts := make([]string, 0, len(m.globalPhases)+4)
	for _, phase := range m.globalPhases {
		parts = append(parts, m.renderPhase(phase))
	}
	if host := m.currentHost(); host != nil {
		for _, phase := range host.phases {
			parts = append(parts, m.renderPhase(phase))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  ")
}

func (m tuiModel) renderPhase(phase phaseView) string {
	label := strings.Title(phase.name)
	switch {
	case phase.running:
		return m.spinner.View() + " " + label
	case phase.status != "":
		return statusGlyph(phase.status) + " " + label
	default:
		return "• " + label
	}
}

func (m tuiModel) progressLine() string {
	completed, total := m.progressCounts()
	if total == 0 {
		return ""
	}
	barWidth := clamp(12, m.width/3, 32)
	m.progress.Width = barWidth
	percent := float64(completed) / float64(total)
	bar := m.progress.ViewAs(percent)
	return spaceBetween(m.width, bar, tuiSubtleStyle.Render(fmt.Sprintf("%d/%d", completed, total)))
}

func (m tuiModel) progressCounts() (int, int) {
	var completed int
	var total int
	for _, name := range m.hostOrder {
		host := m.hosts[name]
		total += max(host.totalTasks, len(host.taskOrder))
		completed += host.completedCount()
	}
	return completed, total
}

func (h *hostView) completedCount() int {
	return h.recap.ok + h.recap.changed + h.recap.failed + h.recap.skipped
}

func (m tuiModel) overallStatus() string {
	if !m.done {
		for _, name := range m.hostOrder {
			if host := m.hosts[name]; host != nil && host.recap.failed > 0 {
				return "warning"
			}
		}
		return "running"
	}
	for _, name := range m.hostOrder {
		if host := m.hosts[name]; host != nil && host.recap.failed > 0 {
			return "failed"
		}
	}
	return "complete"
}

func (m tuiModel) hostStatus(host *hostView) string {
	for _, phase := range host.phases {
		if phase.running {
			return "running"
		}
	}
	for _, id := range host.taskOrder {
		if host.tasks[id].running {
			return "running"
		}
	}
	if host.recap.failed > 0 {
		return "failed"
	}
	if host.done {
		return "complete"
	}
	return "pending"
}

func (m tuiModel) previewLogs(task *taskView) []taskLogLine {
	previewCount := min(3, len(task.logs))
	return task.logs[len(task.logs)-previewCount:]
}

func (m tuiModel) expandedTaskLines(task *taskView) []ScreenLine {
	lines := make([]ScreenLine, 0, min(8, len(task.logs))+1)
	if task.message != "" && !m.logContainsMessage(task) {
		lines = append(lines, ScreenLine{
			Prefix: "",
			Text:   task.message,
			Tone:   task.status,
		})
	}
	if len(task.logs) == 0 {
		return lines
	}
	start := max(0, len(task.logs)-8)
	if start > 0 {
		lines = append(lines, ScreenLine{
			Prefix: "",
			Text:   fmt.Sprintf("%d earlier log lines hidden", start),
			Tone:   "info",
		})
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

func (m tuiModel) currentHost() *hostView {
	if len(m.hostOrder) == 0 {
		return nil
	}
	index := clamp(0, m.selectedHost, len(m.hostOrder)-1)
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
	index := clamp(0, host.selectedTask, len(ids)-1)
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

func (m tuiModel) taskStatusGlyph(task *taskView) string {
	if task.running {
		return m.spinner.View()
	}
	return statusGlyph(task.status)
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
	input := options.Input
	if input == nil && w == os.Stdout && isTTY(w) {
		input = os.Stdin
	}
	options.Input = input
	model := newTUIModel(events, options)
	programOptions := []tea.ProgramOption{
		tea.WithOutput(w),
		tea.WithoutSignalHandler(),
	}
	if input != nil {
		programOptions = append(programOptions, tea.WithInput(input))
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
