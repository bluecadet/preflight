package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxLiveLines is the threshold above which output previews are hidden (dense mode).
const maxLiveLines = 8

// maxTaskPreviewLines is the number of recent output lines shown for an active task.
const maxTaskPreviewLines = 3

// ── styles ────────────────────────────────────────────────────────────────────

var (
	tsOK      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "10"})
	tsChanged = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "3", Dark: "11"})
	tsFailed  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "1", Dark: "9"})
	tsSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tsMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tsBold    = lipgloss.NewStyle().Bold(true)
	tsAction  = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	tsSpin    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "12"})
	tsDivider = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	tsOutput  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	tsElapsed = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// tsHostPalette is the ordered set of colors cycled through for host labels.
var tsHostPalette = []lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("109")).Bold(true), // steel blue
	lipgloss.NewStyle().Foreground(lipgloss.Color("150")).Bold(true), // sage green
	lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Bold(true), // gold
	lipgloss.NewStyle().Foreground(lipgloss.Color("183")).Bold(true), // lavender
	lipgloss.NewStyle().Foreground(lipgloss.Color("73")).Bold(true),  // teal
	lipgloss.NewStyle().Foreground(lipgloss.Color("174")).Bold(true), // salmon
	lipgloss.NewStyle().Foreground(lipgloss.Color("110")).Bold(true), // cornflower
	lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Bold(true), // wheat
}

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
	id          string
	name        string
	actionPath  string // parent action prefix from TaskID, e.g. "configure-kiosk"
	target      string
	startAt     time.Time
	recentLines []string
}

// hostRecap stores the final counts emitted by EventPlayEnd for one host.
type hostRecap struct {
	target                       string
	ok, changed, failed, skipped int
}

// failedTask captures a failed task's identity for the final summary.
type failedTask struct {
	target     string
	actionPath string
	name       string
	message    string
	output     []string
}

// tuiModel is the Bubbletea model for the TUI renderer.
type tuiModel struct {
	spinner     spinner.Model
	width       int
	events      chan Event
	verbose     bool
	playName    string
	startedAt   time.Time
	playStarted bool

	// running tasks, per host
	hosts      map[string]map[string]*activeTask // host → taskID → task
	hostOrder  []string                          // hosts seen, in order
	taskOrder  map[string][]string               // host → ordered task IDs
	hostColors map[string]lipgloss.Style         // host → assigned palette color

	// committed task counts
	okCount      int
	changedCount int
	failedCount  int
	skippedCount int

	// per-host final recap (from EventPlayEnd)
	recaps       []hostRecap
	failedTasks  []failedTask // accumulated for final summary
	staticBlocks []string
	done         bool
}

type tuiEventMsg struct{ event Event }
type tuiDoneMsg struct{}

// ── constructor ───────────────────────────────────────────────────────────────

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
	switch e := e.(type) {

	case PlayStartEvent:
		if !m.playStarted {
			m.playStarted = true
			m.playName = e.PlayName
			m.startedAt = time.Now()
			line := m.renderDivider() + "\n  " + tsSpin.Render("▶") + "  " + tsBold.Render(e.PlayName) + "\n"
			return m, tea.Println(line)
		}

	case TaskStartEvent:
		if e.Target == "" {
			break
		}
		if m.hosts[e.Target] == nil {
			m.hosts[e.Target] = make(map[string]*activeTask)
			m.hostOrder = append(m.hostOrder, e.Target)
			m.hostColors[e.Target] = tsHostPalette[(len(m.hostOrder)-1)%len(tsHostPalette)]
		}
		at := &activeTask{
			id:         e.TaskID,
			name:       e.TaskName,
			actionPath: e.ActionPath,
			target:     e.Target,
			startAt:    time.Now(),
		}
		m.hosts[e.Target][e.TaskID] = at
		m.taskOrder[e.Target] = append(m.taskOrder[e.Target], e.TaskID)

	case TaskOutputEvent:
		if e.Target == "" || e.TaskID == "" {
			break
		}
		if host := m.hosts[e.Target]; host != nil {
			if at := host[e.TaskID]; at != nil {
				at.recentLines = append(at.recentLines, e.Lines...)
				if !m.verbose && len(at.recentLines) > maxTaskPreviewLines {
					at.recentLines = at.recentLines[len(at.recentLines)-maxTaskPreviewLines:]
				}
			}
		}
		return m, nil

	case TaskResultEvent:
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

		// Commit the completed task line permanently to scroll history.
		cmds = append(cmds, tea.Println(m.renderCommitted(e, elapsed)))

		// In verbose mode, print message and logs inline for every completed task.
		if m.verbose {
			if e.Message != "" {
				cmds = append(cmds, tea.Println(tsRenderOutputBlock(e.Message)))
			}
			if len(e.Output) > 0 {
				cmds = append(cmds, tea.Println(tsRenderOutputBlock(strings.Join(e.Output, "\n"))))
			}
		}

		return m, tea.Sequence(cmds...)

	case PlayEndEvent:
		m.recaps = append(m.recaps, hostRecap{
			target:  e.Target,
			ok:      e.OKCount,
			changed: e.ChangedCount,
			failed:  e.FailedCount,
			skipped: e.SkippedCount,
		})

	case WarningEvent:
		line := "  " + tsChanged.Render("⚠") + "  " + tsMuted.Render(e.Message)
		return m, tea.Println(line)

	case ErrorEvent:
		line := "  " + tsFailed.Render("✗") + "  " + tsFailed.Render(e.Message)
		return m, tea.Println(line)

	case FactsEvent:
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		var b strings.Builder
		b.WriteString(tsBold.Render("Facts for "+target+":") + "\n")
		keys := make([]string, 0, len(e.Facts))
		for k := range e.Facts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %v\n", k, e.Facts[k])
		}
		m.staticBlocks = append(m.staticBlocks, strings.TrimRight(b.String(), "\n"))
		return m, nil

	case PlanEvent:
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Target: %s\n", target)
		fmt.Fprintf(&b, "Playbook: %s\n", e.PlaybookName)
		fmt.Fprintf(&b, "Tasks (%d):\n", len(e.Tasks))
		for _, t := range e.Tasks {
			fmt.Fprintf(&b, "  %d. [%s] %s", t.Number, t.Module, t.Name)
			if t.When != "" {
				fmt.Fprintf(&b, " (when: %s)", t.When)
			}
			if len(t.Tags) > 0 {
				fmt.Fprintf(&b, " [tags: %v]", t.Tags)
			}
			b.WriteString("\n")
		}
		m.staticBlocks = append(m.staticBlocks, strings.TrimRight(b.String(), "\n"))
		return m, nil

	case StateEvent:
		var b strings.Builder
		if e.PlaybookName != "" {
			fmt.Fprintf(&b, "State diff for playbook: %s\n", e.PlaybookName)
		}
		if e.Target != "" {
			fmt.Fprintf(&b, "Target: %s\n", e.Target)
		}
		fmt.Fprintf(&b, "State file: %s\n", e.StatePath)
		fmt.Fprintf(&b, "Last applied: %s\n\n", e.LastApplied)
		if len(e.Comparisons) > 0 {
			fmt.Fprintf(&b, "%-12s %-28s %-16s %s\n", "STATUS", "TASK", "MODULE", "RECORDED STATUS")
			fmt.Fprintf(&b, "%-12s %-28s %-16s %s\n", "------------", "----------------------------", "----------------", "---------------")
			for _, c := range e.Comparisons {
				fmt.Fprintf(&b, "%-12s %-28s %-16s %s\n", c.Status, c.TaskName, c.Module, c.RecordedStatus)
			}
		}
		m.staticBlocks = append(m.staticBlocks, strings.TrimRight(b.String(), "\n"))
		return m, nil
	}

	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

// View renders only the live zone (running tasks + footer).
// Completed tasks are committed to scroll history via tea.Println in Update.
// When done, renders the final summary in place of the live zone.
func (m tuiModel) View() string {
	if m.done {
		if len(m.recaps) == 0 && len(m.staticBlocks) > 0 {
			return strings.Join(m.staticBlocks, "\n\n") + "\n"
		}
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
		if len(m.staticBlocks) > 0 {
			return strings.Join(m.staticBlocks, "\n\n") + "\n"
		}
		return ""
	}

	var b strings.Builder

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
	spin := tsSpin.Render(strings.TrimRight(m.spinner.View(), " "))
	host := m.hostStyle(at.target).Render(at.target)
	timer := tsElapsed.Render("[" + tsFmtElapsed(elapsed) + "]")
	pathMax := m.width - lipgloss.Width(spin) - lipgloss.Width(host) - lipgloss.Width(timer) - 10
	path := tsRenderPath(at.actionPath, at.name, pathMax)

	line := tsRow(spin, host, path, timer)

	if dense || len(at.recentLines) == 0 {
		return line
	}

	maxW := max(m.width-12, 10)
	var sb strings.Builder
	sb.WriteString(line)
	for _, l := range at.recentLines {
		sb.WriteString("\n" + tsOutputLine(5, tsTruncate(l, maxW)))
	}
	return sb.String()
}

// renderCommitted formats a completed task line for permanent scroll history.
func (m tuiModel) renderCommitted(e TaskResultEvent, elapsed time.Duration) string {
	icon := tsIcon(e.Status)
	host := m.hostStyle(e.Target).Render(e.Target)

	var right string
	switch {
	case e.Status == "skipped" && e.Message != "":
		right = tsMuted.Render("(" + e.Message + ")")
	case elapsed > 0 && e.Status != "skipped":
		right = tsElapsed.Render(tsFmtElapsed(elapsed))
	}

	pathMax := m.width - lipgloss.Width(icon) - lipgloss.Width(host) - lipgloss.Width(right) - 10
	path := tsRenderPath(e.ActionPath, e.TaskName, pathMax)
	return tsRow(icon, host, path, right)
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

	// Group failed tasks by host for display under each host recap line.
	failedByHost := make(map[string][]failedTask)
	for _, ft := range m.failedTasks {
		failedByHost[ft.target] = append(failedByHost[ft.target], ft)
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(m.renderDivider())
	b.WriteString("\n")

	overallIcon := tsOK.Render("✓")
	if !allOK {
		overallIcon = tsFailed.Render("✗")
	}
	hostLabel := fmt.Sprintf("%d %s", len(m.recaps), tsPluralize(len(m.recaps), "host", "hosts"))
	b.WriteString(tsRow(overallIcon, tsBold.Render(m.playName), tsMuted.Render("·  "+hostLabel), tsElapsed.Render(tsFmtElapsed(totalElapsed))) + "\n")
	b.WriteString("\n")

	for _, r := range m.recaps {
		hostIcon := tsOK.Render("✓")
		if r.failed > 0 {
			hostIcon = tsFailed.Render("✗")
		} else if r.changed > 0 {
			hostIcon = tsChanged.Render("◆")
		}
		stats := strings.Join([]string{
			tsStat(tsOK, "✓", r.ok),
			tsStat(tsChanged, "◆", r.changed),
			tsStat(tsFailed, "✗", r.failed),
			tsStat(tsSkipped, "–", r.skipped),
		}, "  ")
		b.WriteString(tsRow(hostIcon, m.hostStyle(r.target).Render(r.target), stats) + "\n")

		// Indent failed task names under the host line.
		for _, ft := range failedByHost[r.target] {
			path := tsRenderPath(ft.actionPath, ft.name, 0)
			b.WriteString("      " + tsFailed.Render("✗") + "  " + path + "\n")
			if ft.message != "" {
				for line := range strings.SplitSeq(strings.TrimSpace(ft.message), "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						b.WriteString(tsOutputLine(9, line) + "\n")
					}
				}
			}
			for _, line := range ft.output {
				line = strings.TrimSpace(line)
				if line != "" {
					b.WriteString(tsOutputLine(9, line) + "\n")
				}
			}
		}
	}

	b.WriteString("\n")
	return b.String()
}

// renderDivider renders a full-width horizontal rule for the live zone.
func (m tuiModel) renderDivider() string {
	return tsDivider.Render(strings.Repeat("─", m.divWidth()))
}

// renderFooter renders the status summary line below the live zone divider.
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

// divWidth returns a capped terminal width for divider lines.
func (m tuiModel) divWidth() int {
	if m.width <= 0 {
		return 50
	}
	return min(m.width, 50)
}

// total returns the count of all committed (non-running) tasks.
func (m tuiModel) total() int {
	return m.okCount + m.changedCount + m.failedCount + m.skippedCount
}

// ── helpers ───────────────────────────────────────────────────────────────────

// hostStyle returns the palette color assigned to target, falling back to the first palette entry.
func (m tuiModel) hostStyle(target string) lipgloss.Style {
	if s, ok := m.hostColors[target]; ok {
		return s
	}
	return tsHostPalette[0]
}

// tsRenderPath formats a display path for a task, collapsing middle segments if
// the rendered width exceeds maxWidth. Pass maxWidth <= 0 for no limit.
//
// Tiers (widest to narrowest):
//
//	Full:     A › B › C › task name
//	Collapsed: A › … › task name
//	Leaf only: task name
func tsRenderPath(actionPath, taskName string, maxWidth int) string {
	if actionPath == "" {
		return taskName
	}
	sep := tsMuted.Render(" › ")
	ellipsis := tsMuted.Render("…")

	render := func(segs []string, useDots bool) string {
		var b strings.Builder
		for i, seg := range segs {
			if useDots && i == 1 {
				b.WriteString(ellipsis)
			} else {
				b.WriteString(tsAction.Render(seg))
			}
			b.WriteString(sep)
		}
		b.WriteString(taskName)
		return b.String()
	}

	segs := strings.Split(actionPath, "/")

	full := render(segs, false)
	if maxWidth <= 0 || lipgloss.Width(full) <= maxWidth {
		return full
	}

	// Collapse middle segments to a single ellipsis: first › … › task name
	if len(segs) > 1 {
		collapsed := tsAction.Render(segs[0]) + sep + ellipsis + sep + taskName
		if lipgloss.Width(collapsed) <= maxWidth {
			return collapsed
		}
	}

	return taskName
}

// tsRenderOutputBlock formats a (possibly multi-line) message as an indented output block.
func tsRenderOutputBlock(message string) string {
	var parts []string
	for l := range strings.SplitSeq(strings.TrimSpace(message), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts = append(parts, tsOutputLine(5, l))
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

// tsStat renders a colored glyph+count pair, dimmed when the count is zero.
func tsStat(style lipgloss.Style, glyph string, n int) string {
	if n == 0 {
		return tsMuted.Render(glyph + " 0")
	}
	return style.Render(glyph) + " " + fmt.Sprintf("%d", n)
}

// tsRow renders a left-indented row with elements separated by two spaces.
func tsRow(elems ...string) string {
	return "  " + strings.Join(elems, "  ")
}

// tsOutputLine renders a pipe-gutter content line at the given indent depth.
func tsOutputLine(depth int, content string) string {
	return strings.Repeat(" ", depth) + tsOutput.Render(content)
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
	return NewTUIRendererWithOptions(w, Options{})
}

// NewTUIRendererWithOptions creates a TUIRenderer that writes to w.
func NewTUIRendererWithOptions(w io.Writer, opts Options) *TUIRenderer {
	events := make(chan Event, 64)
	model := newTUIModelWithOptions(events, opts)
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
