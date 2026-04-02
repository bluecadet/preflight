package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxLiveLines is the threshold above which output previews are hidden (dense mode).
const maxLiveLines = 8

// ── styles ────────────────────────────────────────────────────────────────────

var (
	tsOK      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "10"})
	tsChanged = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "3", Dark: "11"})
	tsFailed  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "1", Dark: "9"})
	tsSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tsMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tsBold    = lipgloss.NewStyle().Bold(true)
	tsHost    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "6", Dark: "14"}).Bold(true)
	tsAction  = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	tsSpin    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "12"})
	tsDivider = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	tsOutput  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	tsElapsed = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// tsIcon returns a colored glyph representing a completed task status.
func tsIcon(status string) string {
	switch status {
	case "ok":
		return tsOK.Render("✓")
	case "changed":
		return tsChanged.Render("◆")
	case "failed":
		return tsFailed.Render("✗")
	case "skipped":
		return tsSkipped.Render("–")
	default:
		return " "
	}
}

// ── types ─────────────────────────────────────────────────────────────────────

// activeTask represents a task currently being executed.
type activeTask struct {
	id         string
	name       string
	actionPath string // parent action prefix from TaskID, e.g. "configure-kiosk"
	target     string
	startAt    time.Time
	lastOutput string
}

// hostRecap stores the final counts emitted by EventPlayEnd for one host.
type hostRecap struct {
	target                       string
	ok, changed, failed, skipped int
}

// tuiModel is the Bubbletea model for the TUI renderer.
type tuiModel struct {
	spinner     spinner.Model
	width       int
	events      chan Event
	playName    string
	startedAt   time.Time
	playStarted bool

	// running tasks, per host
	hosts     map[string]map[string]*activeTask // host → taskID → task
	hostOrder []string                          // hosts seen, in order
	taskOrder map[string][]string               // host → ordered task IDs

	// committed task counts
	okCount      int
	changedCount int
	failedCount  int
	skippedCount int

	// per-host final recap (from EventPlayEnd)
	recaps []hostRecap
	done   bool
}

type tuiEventMsg struct{ event Event }
type tuiDoneMsg struct{}

// ── constructor ───────────────────────────────────────────────────────────────

func newTUIModel(events chan Event) tuiModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(tsSpin),
	)
	return tuiModel{
		spinner:   s,
		events:    events,
		width:     80,
		hosts:     make(map[string]map[string]*activeTask),
		taskOrder: make(map[string][]string),
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

// ── Update ────────────────────────────────────────────────────────────────────

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
		newM, cmd := m.applyEvent(msg.event)
		return newM, tea.Batch(cmd, newM.waitForEvent())

	case tuiDoneMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) applyEvent(e Event) (tuiModel, tea.Cmd) {
	switch e.Type {

	case EventPlayStart:
		if !m.playStarted {
			m.playStarted = true
			m.playName = e.PlayName
			m.startedAt = time.Now()
			line := "\n  " + tsBold.Render(e.PlayName)
			return m, tea.Println(line)
		}

	case EventTaskStart:
		if e.Target == "" {
			break
		}
		if m.hosts[e.Target] == nil {
			m.hosts[e.Target] = make(map[string]*activeTask)
			m.hostOrder = append(m.hostOrder, e.Target)
		}
		at := &activeTask{
			id:         e.TaskID,
			name:       e.TaskName,
			actionPath: tsParseActionPath(e.TaskID),
			target:     e.Target,
			startAt:    time.Now(),
		}
		m.hosts[e.Target][e.TaskID] = at
		m.taskOrder[e.Target] = append(m.taskOrder[e.Target], e.TaskID)

	case EventTaskResult:
		var cmds []tea.Cmd

		// Determine elapsed time from when the task started.
		var elapsed time.Duration
		if host := m.hosts[e.Target]; host != nil {
			if at := host[e.TaskID]; at != nil {
				elapsed = time.Since(at.startAt)
				delete(host, e.TaskID)
			}
		}
		// Remove from task order.
		order := m.taskOrder[e.Target]
		for i, id := range order {
			if id == e.TaskID {
				m.taskOrder[e.Target] = append(order[:i], order[i+1:]...)
				break
			}
		}

		// Update counters.
		switch e.Status {
		case "ok":
			m.okCount++
		case "changed":
			m.changedCount++
		case "failed":
			m.failedCount++
		case "skipped":
			m.skippedCount++
		}

		// Commit the completed task line permanently to scroll history.
		cmds = append(cmds, tea.Println(m.renderCommitted(e, elapsed)))

		// On failure, commit the error message block too.
		if e.Status == "failed" && e.Message != "" {
			cmds = append(cmds, tea.Println(tsRenderOutputBlock(e.Message)))
		}

		return m, tea.Batch(cmds...)

	case EventPlayEnd:
		m.recaps = append(m.recaps, hostRecap{
			target:  e.Target,
			ok:      e.OKCount,
			changed: e.ChangedCount,
			failed:  e.FailedCount,
			skipped: e.SkippedCount,
		})

	case EventWarning:
		msg := e.Message
		if e.Error != nil {
			msg = e.Error.Error()
		}
		line := "  " + tsChanged.Render("⚠") + "  " + tsMuted.Render(msg)
		return m, tea.Println(line)

	case EventError:
		msg := e.Message
		if e.Error != nil {
			msg = e.Error.Error()
		}
		line := "  " + tsFailed.Render("✗") + "  " + tsFailed.Render(msg)
		return m, tea.Println(line)
	}

	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

// View renders only the live zone (running tasks + footer).
// Completed tasks are committed to scroll history via tea.Println in Update.
// When done, renders the final summary in place of the live zone.
func (m tuiModel) View() string {
	if m.done {
		return m.renderFinalSummary()
	}

	// Collect running tasks in deterministic (insertion) order.
	var running []*activeTask
	for _, host := range m.hostOrder {
		for _, id := range m.taskOrder[host] {
			if at := m.hosts[host][id]; at != nil {
				running = append(running, at)
			}
		}
	}

	// Nothing to show yet.
	if len(running) == 0 && m.total() == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")

	dense := len(running) > maxLiveLines

	for _, at := range running {
		b.WriteString(m.renderRunning(at, dense))
		b.WriteString("\n")
	}

	b.WriteString(m.renderDivider())
	b.WriteString("\n")
	b.WriteString(m.renderFooter(len(running)))
	b.WriteString("\n")

	return b.String()
}

// renderRunning formats a single running task line for the live zone.
func (m tuiModel) renderRunning(at *activeTask, dense bool) string {
	elapsed := time.Since(at.startAt)
	spin := tsSpin.Render(m.spinner.View())
	host := tsHost.Render(fmt.Sprintf("%-20s", tsTruncate(at.target, 20)))
	path := tsRenderPath(at.actionPath, at.name)
	timer := "  " + tsElapsed.Render("["+tsFmtElapsed(elapsed)+"]")

	line := fmt.Sprintf("  %s  %s  %s%s", spin, host, path, timer)

	if dense || at.lastOutput == "" {
		return line
	}

	preview := fmt.Sprintf("     %s  %s",
		tsMuted.Render("└"),
		tsOutput.Render(tsTruncate(at.lastOutput, m.width-12)),
	)
	return line + "\n" + preview
}

// renderCommitted formats a completed task line for permanent scroll history.
func (m tuiModel) renderCommitted(e Event, elapsed time.Duration) string {
	icon := tsIcon(e.Status)
	host := tsHost.Render(fmt.Sprintf("%-20s", tsTruncate(e.Target, 20)))
	actionPath := tsParseActionPath(e.TaskID)
	path := tsRenderPath(actionPath, e.TaskName)

	var trailer string
	switch {
	case e.Status == "skipped" && e.Message != "":
		trailer = "  " + tsMuted.Render("("+e.Message+")")
	case elapsed > 0 && e.Status != "skipped":
		trailer = "  " + tsElapsed.Render(tsFmtElapsed(elapsed))
	}

	return fmt.Sprintf("  %s  %s  %s%s", icon, host, path, trailer)
}

// renderFinalSummary renders the closing block shown after execution completes.
func (m tuiModel) renderFinalSummary() string {
	if len(m.recaps) == 0 {
		return "\n"
	}

	totalElapsed := time.Since(m.startedAt)
	allOK := true
	for _, r := range m.recaps {
		if r.failed > 0 {
			allOK = false
			break
		}
	}

	w := m.divWidth()
	div := tsDivider.Render(strings.Repeat("─", w))

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(div)
	b.WriteString("\n")

	overallIcon := tsOK.Render("✓")
	if !allOK {
		overallIcon = tsFailed.Render("✗")
	}
	hostLabel := fmt.Sprintf("%d %s", len(m.recaps), tsPluralize(len(m.recaps), "host", "hosts"))
	fmt.Fprintf(&b, "  %s  %s  %s  %s\n",
		overallIcon,
		tsBold.Render(m.playName),
		tsMuted.Render("·  "+hostLabel),
		tsElapsed.Render(tsFmtElapsed(totalElapsed)),
	)

	for _, r := range m.recaps {
		hostIcon := tsOK.Render("✓")
		if r.failed > 0 {
			hostIcon = tsFailed.Render("✗")
		} else if r.changed > 0 {
			hostIcon = tsChanged.Render("◆")
		}

		var parts []string
		if r.ok > 0 {
			parts = append(parts, tsOK.Render(fmt.Sprintf("%d ok", r.ok)))
		}
		if r.changed > 0 {
			parts = append(parts, tsChanged.Render(fmt.Sprintf("%d changed", r.changed)))
		}
		if r.failed > 0 {
			parts = append(parts, tsFailed.Render(fmt.Sprintf("%d failed", r.failed)))
		}
		if r.skipped > 0 {
			parts = append(parts, tsSkipped.Render(fmt.Sprintf("%d skipped", r.skipped)))
		}

		fmt.Fprintf(&b, "     %s  %-22s  %s\n",
			hostIcon,
			tsHost.Render(r.target),
			strings.Join(parts, tsMuted.Render("  ")),
		)
	}

	b.WriteString(div)
	b.WriteString("\n")
	return b.String()
}

// renderDivider renders a full-width horizontal rule for the live zone.
func (m tuiModel) renderDivider() string {
	return tsDivider.Render(strings.Repeat("─", m.divWidth()))
}

// renderFooter renders the status summary line below the live zone divider.
func (m tuiModel) renderFooter(runningCount int) string {
	var parts []string

	if runningCount > 0 {
		parts = append(parts, tsSpin.Render(fmt.Sprintf("%d running", runningCount)))
	}

	done := m.okCount + m.changedCount
	if done > 0 {
		parts = append(parts, tsOK.Render(fmt.Sprintf("%d done", done)))
	}
	if m.skippedCount > 0 {
		parts = append(parts, tsSkipped.Render(fmt.Sprintf("%d skipped", m.skippedCount)))
	}
	if m.failedCount > 0 {
		parts = append(parts, tsFailed.Render(fmt.Sprintf("%d failed", m.failedCount)))
	}

	if len(parts) == 0 {
		return ""
	}

	return "  " + strings.Join(parts, tsMuted.Render("  ·  "))
}

// divWidth returns a capped terminal width for divider lines.
func (m tuiModel) divWidth() int {
	if m.width <= 0 {
		return 60
	}
	return min(m.width, 80)
}

// total returns the count of all committed (non-running) tasks.
func (m tuiModel) total() int {
	return m.okCount + m.changedCount + m.failedCount + m.skippedCount
}

// ── helpers ───────────────────────────────────────────────────────────────────

// tsParseActionPath extracts the parent action prefix from a TaskID.
// "configure-kiosk/set-wallpaper" → "configure-kiosk"
// "a/b/c"                         → "a/b"
// "top-level-task"                → ""
func tsParseActionPath(taskID string) string {
	idx := strings.LastIndex(taskID, "/")
	if idx < 0 {
		return ""
	}
	return taskID[:idx]
}

// tsRenderPath formats a display path for a task.
// With parent action: "configure-kiosk  ›  set wallpaper"
// Top-level:          "install-chrome"
func tsRenderPath(actionPath, taskName string) string {
	if actionPath == "" {
		return taskName
	}
	return tsAction.Render(actionPath) + tsMuted.Render(" › ") + taskName
}

// tsRenderOutputBlock formats a (possibly multi-line) message as an indented output block.
func tsRenderOutputBlock(message string) string {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	var parts []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("     %s  %s",
			tsMuted.Render("│"),
			tsOutput.Render(l),
		))
	}
	return strings.Join(parts, "\n")
}

// tsFmtElapsed formats a duration concisely: "0.3s", "12s", "1m02s".
func tsFmtElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", mins, secs)
}

// tsTruncate shortens s to at most n bytes, appending "…" if needed.
func tsTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}

// tsPluralize returns singular or plural based on count.
func tsPluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// ── TUIRenderer ───────────────────────────────────────────────────────────────

// TUIRenderer implements Renderer using a Bubbletea program.
type TUIRenderer struct {
	program *tea.Program
	events  chan Event
	done    chan struct{}
}

// NewTUIRenderer creates a TUIRenderer that writes to w.
func NewTUIRenderer(w io.Writer) *TUIRenderer {
	events := make(chan Event, 64)
	model := newTUIModel(events)
	prog := tea.NewProgram(
		model,
		tea.WithOutput(w),
		tea.WithInput(nil),
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

// Emit sends an event to the running Bubbletea program.
func (r *TUIRenderer) Emit(event Event) {
	r.events <- event
}

// Close shuts down the Bubbletea program and waits for it to exit.
func (r *TUIRenderer) Close() {
	close(r.events)
	<-r.done
}
