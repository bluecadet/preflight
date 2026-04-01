package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	xterm "github.com/charmbracelet/x/term"
)

const (
	runMaxBufferedLogs  = 16
	runMaxBufferedBytes = 8 * 1024
)

var runSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type runLogLine struct {
	stream string
	line   string
}

type runTaskState struct {
	id        string
	path      string
	name      string
	module    string
	logs      []runLogLine
	logBytes  int
	streamed  bool
	completed bool
}

func (t *runTaskState) appendLog(stream, line string) {
	if line == "" {
		return
	}
	t.logs = append(t.logs, runLogLine{stream: stream, line: line})
	t.logBytes += len(line)
	for len(t.logs) > runMaxBufferedLogs || t.logBytes > runMaxBufferedBytes {
		if len(t.logs) == 0 {
			t.logBytes = 0
			return
		}
		t.logBytes -= len(t.logs[0].line)
		t.logs = t.logs[1:]
	}
}

func (t *runTaskState) drainLogs() []runLogLine {
	logs := append([]runLogLine(nil), t.logs...)
	t.logs = nil
	t.logBytes = 0
	return logs
}

type runRecap struct {
	ok      int
	changed int
	failed  int
	skipped int
}

type runHostState struct {
	name            string
	playName        string
	currentPhase    string
	phaseRunning    bool
	currentTaskID   string
	currentTask     string
	currentTaskPath string
	tasks           map[string]*runTaskState
	recap           runRecap
	done            bool
}

func (h *runHostState) ensureTask(event Event) *runTaskState {
	key := event.TaskID
	if key == "" {
		key = event.TaskName
	}
	if key == "" {
		key = event.TaskPath
	}
	if key == "" {
		key = "task"
	}
	if h.tasks == nil {
		h.tasks = make(map[string]*runTaskState)
	}
	task, ok := h.tasks[key]
	if ok {
		if event.TaskPath != "" {
			task.path = event.TaskPath
		}
		if event.TaskName != "" {
			task.name = event.TaskName
		}
		if event.Module != "" {
			task.module = event.Module
		}
		return task
	}
	task = &runTaskState{
		id:     key,
		path:   event.TaskPath,
		name:   event.TaskName,
		module: event.Module,
	}
	h.tasks[key] = task
	return task
}

type runTranscriptState struct {
	command         string
	verbose         bool
	color           bool
	playPrinted     bool
	playName        string
	globalPhase     string
	globalPhaseDone string
	hosts           map[string]*runHostState
	hostOrder       []string
	spinnerIndex    int
}

func newRunTranscriptState(options Options, color bool) runTranscriptState {
	return runTranscriptState{
		command: options.Command,
		verbose: options.Verbose,
		color:   color,
		hosts:   make(map[string]*runHostState),
	}
}

func (s *runTranscriptState) ensureHost(name string) *runHostState {
	key := name
	if key == "" {
		key = "local"
	}
	host, ok := s.hosts[key]
	if ok {
		return host
	}
	host = &runHostState{name: key, tasks: make(map[string]*runTaskState)}
	s.hosts[key] = host
	s.hostOrder = append(s.hostOrder, key)
	return host
}

func (s *runTranscriptState) stepSpinner() {
	if len(runSpinnerFrames) == 0 {
		return
	}
	s.spinnerIndex = (s.spinnerIndex + 1) % len(runSpinnerFrames)
}

func (s *runTranscriptState) apply(event Event) []string {
	lines := make([]string, 0, 4)
	switch event.Type {
	case EventPlayStart:
		host := s.ensureHost(event.Target)
		if event.PlayName != "" {
			host.playName = event.PlayName
			if s.playName == "" {
				s.playName = event.PlayName
			}
		}
		if !s.playPrinted && s.playName != "" {
			s.playPrinted = true
			lines = append(lines, lipgloss.NewStyle().Bold(true).Render("play: "+s.playName))
		}

	case EventPhaseStart:
		if event.Target == "" {
			s.globalPhase = event.Phase
			lines = append(lines, tuiSubtleStyle.Render("phase: "+event.Phase))
			break
		}
		host := s.ensureHost(event.Target)
		host.currentPhase = event.Phase
		host.phaseRunning = true
		host.done = false
		lines = append(lines, s.renderPhaseLine(event.Target, event.Phase, "", event.TaskTotal))

	case EventPhaseEnd:
		if event.Target == "" {
			s.globalPhase = ""
			s.globalPhaseDone = event.Phase
			lines = append(lines, s.renderPhaseLine("", event.Phase, event.Status, event.TaskTotal))
			break
		}
		host := s.ensureHost(event.Target)
		host.currentPhase = event.Phase
		host.phaseRunning = false
		lines = append(lines, s.renderPhaseLine(event.Target, event.Phase, event.Status, event.TaskTotal))

	case EventTaskStart:
		host := s.ensureHost(event.Target)
		task := host.ensureTask(event)
		host.currentTaskID = task.id
		host.currentTask = task.name
		host.currentTaskPath = task.path
		host.done = false
		if s.verbose {
			lines = append(lines, s.renderTaskStartLine(event))
		}

	case EventTaskLog:
		host := s.ensureHost(event.Target)
		task := host.ensureTask(event)
		if s.verbose {
			task.streamed = true
			lines = append(lines, s.renderTaskLogLine(event.Target, event.Stream, event.Line))
			break
		}
		task.appendLog(event.Stream, event.Line)

	case EventTaskResult:
		host := s.ensureHost(event.Target)
		task := host.ensureTask(event)
		host.currentTaskID = ""
		host.currentTask = ""
		host.currentTaskPath = ""
		task.completed = true
		lines = append(lines, s.renderTaskResultLine(event))
		if !s.verbose && event.Status == "failed" && !task.streamed {
			for _, logLine := range task.drainLogs() {
				lines = append(lines, s.renderTaskLogLine(event.Target, logLine.stream, logLine.line))
			}
		} else {
			task.drainLogs()
		}
		switch event.Status {
		case "ok":
			host.recap.ok++
		case "changed":
			host.recap.changed++
		case "failed":
			host.recap.failed++
		case "skipped":
			host.recap.skipped++
		}

	case EventPlayEnd:
		host := s.ensureHost(event.Target)
		host.done = true
		host.phaseRunning = false
		host.currentPhase = ""
		host.currentTaskID = ""
		host.currentTask = ""
		host.currentTaskPath = ""
		host.recap = runRecap{
			ok:      event.OKCount,
			changed: event.ChangedCount,
			failed:  event.FailedCount,
			skipped: event.SkippedCount,
		}
		lines = append(lines, s.renderRecapLine(event))

	case EventError:
		msg := event.Message
		if event.Error != nil {
			msg = event.Error.Error()
		}
		if msg != "" {
			lines = append(lines, tuiStatusFailStyle.Render("error: "+msg))
		}
	}
	return lines
}

func (s runTranscriptState) isMultiHost() bool {
	return len(s.hostOrder) > 1
}

func (s runTranscriptState) hostPrefix(target string) string {
	if !s.isMultiHost() || target == "" {
		return ""
	}
	maxWidth := 0
	for _, host := range s.hostOrder {
		maxWidth = max(maxWidth, lipgloss.Width(host))
	}
	label := lipgloss.NewStyle().Width(maxWidth).Render(target)
	return lipgloss.JoinHorizontal(lipgloss.Left, tuiSubtleStyle.Render(label), "  ")
}

func (s runTranscriptState) renderPhaseLine(target, phase, status string, taskTotal int) string {
	parts := []string{tuiSubtleStyle.Render("phase:")}
	if phase != "" {
		parts = append(parts, phase)
	}
	if status != "" {
		parts = append(parts, toneStyle(status).Render(status))
	}
	if taskTotal > 0 {
		parts = append(parts, tuiSubtleStyle.Render(fmt.Sprintf("(%d tasks)", taskTotal)))
	}
	return s.hostPrefix(target) + strings.Join(parts, " ")
}

func (s runTranscriptState) renderTaskStartLine(event Event) string {
	return s.hostPrefix(event.Target) + tuiSubtleStyle.Render("start ") + s.renderTaskCore(event.TaskPath, event.TaskName, event.Module, "")
}

func (s runTranscriptState) renderTaskResultLine(event Event) string {
	return s.hostPrefix(event.Target) + s.renderTaskCore(event.TaskPath, event.TaskName, event.Module, event.Status, event.Message)
}

func (s runTranscriptState) renderTaskCore(path, taskName, module string, statusAndMessage ...string) string {
	parts := make([]string, 0, 6)
	if len(statusAndMessage) > 0 && statusAndMessage[0] != "" {
		parts = append(parts, statusGlyph(statusAndMessage[0]))
	}
	if path != "" {
		parts = append(parts, tuiSubtleStyle.Render(path))
	}
	title := taskName
	if title == "" {
		title = "task"
	}
	parts = append(parts, title)
	if module != "" {
		parts = append(parts, tuiSubtleStyle.Render("("+module+")"))
	}
	if len(statusAndMessage) > 1 && statusAndMessage[1] != "" {
		parts = append(parts, tuiSubtleStyle.Render(statusAndMessage[1]))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func (s runTranscriptState) renderTaskLogLine(target, stream, line string) string {
	prefix := "log>"
	switch strings.ToLower(stream) {
	case "stdout", "out":
		prefix = "out>"
	case "stderr", "err", "error":
		prefix = "err>"
	case "info":
		prefix = "inf>"
	}
	streamText := streamStyle(stream).Render(prefix)
	return s.hostPrefix(target) + lipgloss.JoinHorizontal(lipgloss.Left, "  ", streamText, " ", tuiSubtleStyle.Render(line))
}

func (s runTranscriptState) renderRecapLine(event Event) string {
	counts := renderStats([]ScreenStat{
		{Label: "ok", Value: fmt.Sprintf("%d", event.OKCount), Tone: "ok"},
		{Label: "changed", Value: fmt.Sprintf("%d", event.ChangedCount), Tone: "changed"},
		{Label: "failed", Value: fmt.Sprintf("%d", event.FailedCount), Tone: "failed"},
		{Label: "skipped", Value: fmt.Sprintf("%d", event.SkippedCount), Tone: "skipped"},
	}, defaultTUIWidth)
	label := "recap"
	if counts != "" {
		label = lipgloss.JoinHorizontal(lipgloss.Left, tuiSubtleStyle.Render("recap"), "  ", counts)
	}
	return s.hostPrefix(event.Target) + label
}

func (s runTranscriptState) footerLines(width int) []string {
	if width <= 0 {
		width = defaultTUIWidth
	}
	left := lipgloss.NewStyle().Bold(true).Render(s.command)
	phase := s.footerPhase()
	if phase != "" {
		left = lipgloss.JoinHorizontal(lipgloss.Left, left, "  ", s.renderFooterPhase(phase))
	}
	counts := renderStats([]ScreenStat{
		{Label: "ok", Value: fmt.Sprintf("%d", s.totalOK()), Tone: "ok"},
		{Label: "changed", Value: fmt.Sprintf("%d", s.totalChanged()), Tone: "changed"},
		{Label: "failed", Value: fmt.Sprintf("%d", s.totalFailed()), Tone: "failed"},
		{Label: "skipped", Value: fmt.Sprintf("%d", s.totalSkipped()), Tone: "skipped"},
	}, max(12, width/2))
	line1 := s.renderFooterLine(width, left, counts)
	if line1 == "" {
		return nil
	}
	lines := []string{line1}
	if active := s.renderActiveSummary(width); active != "" {
		lines = append(lines, active)
	}
	return lines
}

func (s runTranscriptState) renderFooterLine(width int, left, right string) string {
	if width <= 0 {
		return ""
	}
	left = truncateText(left, width)
	if right == "" {
		return lipgloss.NewStyle().Width(width).Render(left)
	}
	remaining := width - lipgloss.Width(left) - 2
	if remaining <= 0 {
		return lipgloss.NewStyle().Width(width).Render(left)
	}
	right = truncateText(right, remaining)
	return lipgloss.NewStyle().Width(width).Render(spaceBetween(width, left, right))
}

func (s runTranscriptState) renderFooterPhase(phase string) string {
	if phase == "complete" {
		tone := "ok"
		if s.totalFailed() > 0 {
			tone = "failed"
		}
		return lipgloss.JoinHorizontal(lipgloss.Left, toneStyle(tone).Render(statusGlyph(tone)), " ", tuiSubtleStyle.Render("phase:"), " ", phase)
	}
	spinner := tuiSpinnerStyle.Render(runSpinnerFrames[s.spinnerIndex%len(runSpinnerFrames)])
	return lipgloss.JoinHorizontal(lipgloss.Left, spinner, " ", tuiSubtleStyle.Render("phase:"), " ", phase)
}

func (s runTranscriptState) renderActiveSummary(width int) string {
	active := make([]string, 0, len(s.hostOrder))
	for _, hostName := range s.hostOrder {
		host := s.hosts[hostName]
		if host == nil {
			continue
		}
		switch {
		case host.currentTask != "":
			label := strings.TrimSpace(strings.Join([]string{host.name, host.currentTaskPath, host.currentTask}, " "))
			active = append(active, truncateText(label, max(12, width/3)))
		case host.phaseRunning && host.currentPhase != "":
			active = append(active, truncateText(host.name+" "+host.currentPhase, max(12, width/3)))
		}
	}
	if len(active) == 0 || width < 36 {
		return ""
	}
	var joined strings.Builder
	used := 0
	for idx, item := range active {
		part := item
		if idx > 0 {
			part = " | " + part
		}
		partWidth := ansi.StringWidth(part)
		if used+partWidth > width {
			remaining := len(active) - idx
			if remaining > 0 {
				more := fmt.Sprintf(" | +%d more", remaining)
				if used+ansi.StringWidth(more) <= width {
					joined.WriteString(more)
				}
			}
			break
		}
		joined.WriteString(part)
		used += partWidth
	}
	if joined.Len() == 0 {
		return ""
	}
	return lipgloss.NewStyle().Width(width).Render(tuiSubtleStyle.Render("active: ") + joined.String())
}

func (s runTranscriptState) footerPhase() string {
	if s.globalPhase != "" {
		return s.globalPhase
	}
	for _, hostName := range s.hostOrder {
		host := s.hosts[hostName]
		if host != nil && host.phaseRunning && host.currentPhase != "" {
			return host.currentPhase
		}
	}
	if len(s.hostOrder) > 0 {
		allDone := true
		for _, hostName := range s.hostOrder {
			host := s.hosts[hostName]
			if host == nil || !host.done {
				allDone = false
				break
			}
		}
		if allDone {
			return "complete"
		}
	}
	return s.globalPhaseDone
}

func (s runTranscriptState) totalOK() int {
	total := 0
	for _, hostName := range s.hostOrder {
		total += s.hosts[hostName].recap.ok
	}
	return total
}

func (s runTranscriptState) totalChanged() int {
	total := 0
	for _, hostName := range s.hostOrder {
		total += s.hosts[hostName].recap.changed
	}
	return total
}

func (s runTranscriptState) totalFailed() int {
	total := 0
	for _, hostName := range s.hostOrder {
		total += s.hosts[hostName].recap.failed
	}
	return total
}

func (s runTranscriptState) totalSkipped() int {
	total := 0
	for _, hostName := range s.hostOrder {
		total += s.hosts[hostName].recap.skipped
	}
	return total
}

type textRunRenderer struct {
	w     io.Writer
	state runTranscriptState
}

func newTextRunRenderer(w io.Writer, options Options) *textRunRenderer {
	return &textRunRenderer{
		w:     w,
		state: newRunTranscriptState(options, isTTY(w)),
	}
}

func (r *textRunRenderer) Emit(event Event) {
	for _, line := range r.state.apply(event) {
		_, _ = fmt.Fprintln(r.w, line)
	}
}

func (r *textRunRenderer) Close() {}

// TextRenderer writes transcript-style human-readable output.
type TextRenderer struct {
	*textRunRenderer
}

// NewTextRenderer creates a TextRenderer.
func NewTextRenderer(w io.Writer) *TextRenderer {
	return NewTextRendererWithOptions(w, Options{})
}

// NewTextRendererWithOptions creates a TextRenderer with explicit options.
func NewTextRendererWithOptions(w io.Writer, options Options) *TextRenderer {
	return &TextRenderer{textRunRenderer: newTextRunRenderer(w, options)}
}

type terminalSizer interface {
	Fd() uintptr
}

// LiveRunRenderer writes the transcript and maintains a transient footer in TTY mode.
type LiveRunRenderer struct {
	mu          sync.Mutex
	base        *textRunRenderer
	writer      io.Writer
	command     string
	interactive bool
	ticker      *time.Ticker
	stopTicker  chan struct{}
	doneTicker  chan struct{}
	footerLines int
}

// NewLiveRunRenderer creates a live renderer for run commands.
func NewLiveRunRenderer(w io.Writer) *LiveRunRenderer {
	return NewLiveRunRendererWithOptions(w, Options{})
}

// NewLiveRunRendererWithOptions creates a live renderer with explicit options.
func NewLiveRunRendererWithOptions(w io.Writer, options Options) *LiveRunRenderer {
	renderer := &LiveRunRenderer{
		base:    newTextRunRenderer(w, options),
		writer:  w,
		command: options.Command,
	}
	if options.Command == "" {
		renderer.command = "run"
	}
	if _, ok := w.(terminalSizer); ok && isTTY(w) {
		renderer.interactive = true
		renderer.startTicker()
	}
	return renderer
}

func (r *LiveRunRenderer) startTicker() {
	r.ticker = time.NewTicker(150 * time.Millisecond)
	r.stopTicker = make(chan struct{})
	r.doneTicker = make(chan struct{})
	go func() {
		defer close(r.doneTicker)
		for {
			select {
			case <-r.ticker.C:
				r.mu.Lock()
				r.base.state.stepSpinner()
				r.redrawFooterLocked()
				r.mu.Unlock()
			case <-r.stopTicker:
				return
			}
		}
	}()
}

func (r *LiveRunRenderer) Emit(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clearFooterLocked()
	for _, line := range r.base.state.apply(event) {
		_, _ = fmt.Fprintln(r.writer, line)
	}
	r.redrawFooterLocked()
}

func (r *LiveRunRenderer) Close() {
	r.mu.Lock()
	r.clearFooterLocked()
	r.mu.Unlock()
	if r.ticker != nil {
		r.ticker.Stop()
		close(r.stopTicker)
		<-r.doneTicker
	}
}

func (r *LiveRunRenderer) redrawFooterLocked() {
	if !r.interactive {
		return
	}
	width, height := r.terminalSizeLocked()
	lines := r.base.state.footerLines(width)
	if len(lines) == 0 {
		r.footerLines = 0
		return
	}
	_, _ = fmt.Fprint(r.writer, "\x1b[s")
	for idx, line := range lines {
		row := height - len(lines) + idx + 1
		_, _ = fmt.Fprintf(r.writer, "\x1b[%d;1H\x1b[2K", row)
		_, _ = fmt.Fprint(r.writer, lipgloss.NewStyle().Width(width).Render(truncateText(line, width)))
	}
	_, _ = fmt.Fprint(r.writer, "\x1b[u")
	r.footerLines = len(lines)
}

func (r *LiveRunRenderer) clearFooterLocked() {
	if !r.interactive || r.footerLines == 0 {
		return
	}
	_, height := r.terminalSizeLocked()
	_, _ = fmt.Fprint(r.writer, "\x1b[s")
	for idx := 0; idx < r.footerLines; idx++ {
		row := height - r.footerLines + idx + 1
		_, _ = fmt.Fprintf(r.writer, "\x1b[%d;1H\x1b[2K", row)
	}
	_, _ = fmt.Fprint(r.writer, "\x1b[u")
	r.footerLines = 0
}

func (r *LiveRunRenderer) terminalSizeLocked() (int, int) {
	file, ok := r.writer.(*os.File)
	if !ok {
		return defaultTUIWidth, defaultTUIHeight
	}
	width, height, err := xterm.GetSize(file.Fd())
	if err != nil {
		return defaultTUIWidth, defaultTUIHeight
	}
	return max(20, width), max(4, height)
}
