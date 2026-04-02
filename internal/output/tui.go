package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const tuiVisibleHosts = 6

// tuiEventMsg wraps an Event for delivery via the Bubble Tea message bus.
type tuiEventMsg struct {
	event Event
}

// tuiDoneMsg signals the program to quit.
type tuiDoneMsg struct{}

// recapCounts holds running totals.
type recapCounts struct {
	ok, changed, failed, skipped int
}

type hostView struct {
	target       string
	playName     string
	totalTasks   int
	completed    int
	activeTask   string
	activeModule string
	lastStatus   string
	lastMessage  string
	running      bool
	done         bool
	recap        recapCounts
}

func (h hostView) percentDone() float64 {
	if h.totalTasks <= 0 {
		return 0
	}
	return float64(h.completed) / float64(h.totalTasks)
}

func (h hostView) status() string {
	if h.running {
		return "running"
	}
	if h.recap.failed > 0 {
		return "failed"
	}
	if h.recap.changed > 0 {
		return "changed"
	}
	if h.completed > 0 || h.done {
		return "ok"
	}
	return "skipped"
}

func (h hostView) taskLabel() string {
	parts := make([]string, 0, 2)
	if h.activeModule != "" {
		parts = append(parts, h.activeModule)
	}
	if h.activeTask != "" {
		parts = append(parts, h.activeTask)
	}
	return strings.Join(parts, "  ")
}

// tuiModel is the Bubble Tea model for the streaming TUI renderer.
type tuiModel struct {
	spinner  spinner.Model
	progress progress.Model
	theme    terminalTheme
	events   chan Event
	width    int
	hosts    map[string]hostView
	order    []string
	warnings int
	errors   int
	done     bool
	spinning bool
}

func newTUIModel(events chan Event, w io.Writer) tuiModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(newTerminalTheme(w).active),
	)
	p := progress.New(
		progress.WithSolidFill("80"),
		progress.WithoutPercentage(),
		progress.WithFillCharacters('=', '-'),
		progress.WithWidth(24),
	)
	return tuiModel{
		spinner:  s,
		progress: p,
		theme:    newTerminalTheme(w),
		events:   events,
		width:    defaultTerminalWidth,
		hosts:    make(map[string]hostView),
	}
}

// Init starts the event-drain loop.
func (m tuiModel) Init() tea.Cmd {
	return m.waitForEvent()
}

// waitForEvent returns a Cmd that blocks until an event is available.
func (m tuiModel) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		e, ok := <-m.events
		if !ok {
			return tuiDoneMsg{}
		}
		return tuiEventMsg{event: e}
	}
}

// Update handles incoming messages.
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		if !m.hasRunningHosts() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.spinning = true
		return m, cmd

	case tuiEventMsg:
		m.applyEvent(msg.event)
		cmds := []tea.Cmd{m.waitForEvent()}
		if line := m.renderEventLine(msg.event); line != "" {
			cmds = append(cmds, tea.Println(line))
		}
		if m.hasRunningHosts() && !m.spinning {
			m.spinning = true
			cmds = append(cmds, m.spinner.Tick)
		}
		if !m.hasRunningHosts() {
			m.spinning = false
		}
		return m, tea.Batch(cmds...)

	case tuiDoneMsg:
		m.done = true
		m.spinning = false
		return m, tea.Quit
	}
	return m, nil
}

func (m *tuiModel) applyEvent(e Event) {
	target := normalizeTarget(e.Target)

	switch e.Type {
	case EventPlayStart:
		host := m.getHost(target)
		host.target = target
		host.playName = e.PlayName
		if e.TaskTotal > 0 {
			host.totalTasks = e.TaskTotal
		}
		m.putHost(host)

	case EventTaskStart:
		host := m.getHost(target)
		host.target = target
		host.running = true
		host.activeTask = e.TaskName
		host.activeModule = e.Module
		if e.TaskTotal > 0 {
			host.totalTasks = e.TaskTotal
		}
		if e.TaskIndex > 0 && e.TaskIndex-1 > host.completed {
			host.completed = e.TaskIndex - 1
		}
		m.putHost(host)

	case EventTaskResult:
		host := m.getHost(target)
		host.target = target
		host.running = false
		host.activeTask = ""
		host.activeModule = ""
		host.lastStatus = e.Status
		host.lastMessage = e.Message
		if e.TaskTotal > 0 {
			host.totalTasks = e.TaskTotal
		}
		if e.TaskIndex > host.completed {
			host.completed = e.TaskIndex
		} else {
			host.completed++
		}
		switch e.Status {
		case "ok":
			host.recap.ok++
		case "changed":
			host.recap.changed++
		case "failed":
			host.recap.failed++
		case "skipped":
			host.recap.skipped++
		}
		m.putHost(host)

	case EventPlayEnd:
		host := m.getHost(target)
		host.target = target
		host.running = false
		host.done = true
		host.activeTask = ""
		host.activeModule = ""
		host.recap.ok = e.OKCount
		host.recap.changed = e.ChangedCount
		host.recap.failed = e.FailedCount
		host.recap.skipped = e.SkippedCount
		if e.TaskTotal > 0 {
			host.totalTasks = e.TaskTotal
		}
		if host.totalTasks > 0 {
			host.completed = host.totalTasks
		}
		m.putHost(host)

	case EventWarning:
		m.warnings++

	case EventError:
		m.errors++
	}
}

func (m tuiModel) getHost(target string) hostView {
	if host, ok := m.hosts[target]; ok {
		return host
	}
	return hostView{target: target}
}

func (m *tuiModel) putHost(host hostView) {
	if _, ok := m.hosts[host.target]; !ok {
		m.order = append(m.order, host.target)
	}
	m.hosts[host.target] = host
}

func (m tuiModel) hasRunningHosts() bool {
	for _, target := range m.order {
		if m.hosts[target].running {
			return true
		}
	}
	return false
}

func (m tuiModel) renderEventLine(e Event) string {
	switch e.Type {
	case EventPlayStart:
		line := lipgloss.JoinHorizontal(
			lipgloss.Center,
			m.theme.pill.Render("START"),
			" ",
			m.theme.value.Render(e.PlayName),
			" ",
			m.theme.host(e.Target),
		)
		if e.TaskTotal > 0 {
			line = lipgloss.JoinHorizontal(
				lipgloss.Center,
				line,
				"  ",
				m.theme.muted.Render(fmt.Sprintf("%d tasks", e.TaskTotal)),
			)
		}
		return line

	case EventTaskResult:
		parts := []string{
			m.theme.statusBadge(e.Status),
			m.theme.host(e.Target),
			m.theme.value.Render(e.TaskName),
		}
		if e.Module != "" {
			parts = append(parts, m.theme.pill.Render(e.Module))
		}
		if e.Message != "" {
			parts = append(parts, m.theme.muted.Render(e.Message))
		}
		if e.TaskTotal > 0 && e.TaskIndex > 0 {
			parts = append(parts, m.theme.muted.Render(fmt.Sprintf("%d/%d", e.TaskIndex, e.TaskTotal)))
		}
		return strings.Join(parts, "  ")

	case EventPlayEnd:
		return lipgloss.JoinHorizontal(
			lipgloss.Center,
			m.theme.pill.Render("DONE"),
			" ",
			m.theme.host(e.Target),
			"  ",
			m.theme.statusBadge("ok"),
			" ",
			m.theme.value.Render(fmt.Sprintf("%d", e.OKCount)),
			"  ",
			m.theme.statusBadge("changed"),
			" ",
			m.theme.value.Render(fmt.Sprintf("%d", e.ChangedCount)),
			"  ",
			m.theme.statusBadge("failed"),
			" ",
			m.theme.value.Render(fmt.Sprintf("%d", e.FailedCount)),
			"  ",
			m.theme.statusBadge("skipped"),
			" ",
			m.theme.value.Render(fmt.Sprintf("%d", e.SkippedCount)),
		)

	case EventWarning:
		message := e.Message
		if e.Error != nil {
			message = e.Error.Error()
		}
		return m.theme.note("warning", message)

	case EventError:
		message := e.Message
		if e.Error != nil {
			message = e.Error.Error()
		}
		return m.theme.note("error", message)
	}
	return ""
}

// View renders the compact live status strip.
func (m tuiModel) View() string {
	if len(m.hosts) == 0 && m.warnings == 0 && m.errors == 0 {
		return m.theme.panel.Render(m.theme.muted.Render("waiting for execution events"))
	}

	playName := "Execution"
	for _, target := range m.order {
		if host := m.hosts[target]; host.playName != "" {
			playName = host.playName
			break
		}
	}

	width := max(m.width-1, 48)
	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		m.theme.pill.Render("LIVE"),
		" ",
		m.theme.title.Render(playName),
		"  ",
		m.theme.muted.Render(fmt.Sprintf("%d host(s)", len(m.hosts))),
	)

	counts := []string{
		m.theme.statusBadge("ok") + " " + m.theme.value.Render(fmt.Sprintf("%d", m.totalCounts().ok)),
		m.theme.statusBadge("changed") + " " + m.theme.value.Render(fmt.Sprintf("%d", m.totalCounts().changed)),
		m.theme.statusBadge("failed") + " " + m.theme.value.Render(fmt.Sprintf("%d", m.totalCounts().failed)),
		m.theme.statusBadge("skipped") + " " + m.theme.value.Render(fmt.Sprintf("%d", m.totalCounts().skipped)),
	}
	if m.warnings > 0 {
		counts = append(counts, m.theme.warning.Render(fmt.Sprintf("WARN %d", m.warnings)))
	}
	if m.errors > 0 {
		counts = append(counts, m.theme.error.Render(fmt.Sprintf("ERR %d", m.errors)))
	}

	rows := []string{
		header,
		strings.Join(counts, "  "),
	}
	for _, row := range m.renderHostRows(width) {
		rows = append(rows, row)
	}

	return m.theme.panel.MaxWidth(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m tuiModel) renderHostRows(width int) []string {
	targets := m.visibleTargets()
	rows := make([]string, 0, len(targets)+1)
	for _, target := range targets {
		host := m.hosts[target]
		bar := m.progress
		bar.Width = clamp(width/4, 12, 28)

		prefix := m.theme.statusBadge(host.status())
		if host.running {
			prefix = lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", m.theme.host(host.target))
		} else {
			prefix = lipgloss.JoinHorizontal(lipgloss.Center, prefix, " ", m.theme.host(host.target))
		}

		progressView := bar.ViewAs(host.percentDone())
		progressText := m.theme.muted.Render(progressFraction(host))
		statusLine := lipgloss.JoinHorizontal(
			lipgloss.Center,
			prefix,
			"  ",
			progressView,
			" ",
			progressText,
		)

		detailParts := []string{
			m.theme.statusBadge("ok") + " " + m.theme.value.Render(fmt.Sprintf("%d", host.recap.ok)),
			m.theme.statusBadge("changed") + " " + m.theme.value.Render(fmt.Sprintf("%d", host.recap.changed)),
			m.theme.statusBadge("failed") + " " + m.theme.value.Render(fmt.Sprintf("%d", host.recap.failed)),
			m.theme.statusBadge("skipped") + " " + m.theme.value.Render(fmt.Sprintf("%d", host.recap.skipped)),
		}

		taskLabel := host.taskLabel()
		if taskLabel != "" {
			detailParts = append(detailParts, m.theme.value.Render(truncate(taskLabel, width/2)))
		} else if host.lastMessage != "" {
			detailParts = append(detailParts, m.theme.muted.Render(truncate(host.lastMessage, width/2)))
		}

		rows = append(rows, lipgloss.JoinVertical(
			lipgloss.Left,
			statusLine,
			"  "+strings.Join(detailParts, "  "),
		))
	}

	hidden := len(m.hosts) - len(targets)
	if hidden > 0 {
		rows = append(rows, m.theme.muted.Render(fmt.Sprintf("+%d more host(s) hidden", hidden)))
	}
	return rows
}

func (m tuiModel) visibleTargets() []string {
	active := make([]string, 0, len(m.order))
	remaining := make([]string, 0, len(m.order))
	for _, target := range m.order {
		host := m.hosts[target]
		if host.running {
			active = append(active, target)
			continue
		}
		remaining = append(remaining, target)
	}

	visible := append(active, remaining...)
	if len(visible) > tuiVisibleHosts {
		visible = visible[:tuiVisibleHosts]
	}
	return visible
}

func (m tuiModel) totalCounts() recapCounts {
	var total recapCounts
	for _, target := range m.order {
		host := m.hosts[target]
		total.ok += host.recap.ok
		total.changed += host.recap.changed
		total.failed += host.recap.failed
		total.skipped += host.recap.skipped
	}
	return total
}

// TUIRenderer implements Renderer using an inline Bubble Tea program.
type TUIRenderer struct {
	program *tea.Program
	events  chan Event
	done    chan struct{}
}

// NewTUIRenderer creates a streaming TUI renderer that writes to w.
func NewTUIRenderer(w io.Writer) *TUIRenderer {
	events := make(chan Event, 128)
	model := newTUIModel(events, w)
	prog := tea.NewProgram(
		model,
		tea.WithInput(nil),
		tea.WithOutput(w),
		tea.WithoutSignalHandler(),
	)
	r := &TUIRenderer{
		program: prog,
		events:  events,
		done:    make(chan struct{}),
	}
	go func() {
		defer close(r.done)
		_, _ = prog.Run()
	}()
	return r
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

func normalizeTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "localhost"
	}
	return target
}

func progressFraction(host hostView) string {
	if host.totalTasks <= 0 {
		return fmt.Sprintf("%d done", host.completed)
	}
	return fmt.Sprintf("%d/%d", host.completed, host.totalTasks)
}

func truncate(value string, width int) string {
	if width <= 3 || lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:max(width-3, 1)]) + "..."
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
