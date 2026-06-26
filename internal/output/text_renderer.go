package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// ANSI color codes.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiGrey   = "\033[90m"
	ansiBold   = "\033[1m"
)

const (
	lineWidth                 = 80
	defaultFailureOutputLimit = 80
)

// TextRenderer writes the append-only form of the apply/check transcript.
type TextRenderer struct {
	w            io.Writer
	color        bool
	verbose      bool
	maxFailLines int
	mode         string
	runDir       string
	runStarted   bool
	playbookPath string
	playbookName string
	targets      []string
	activeTasks  map[string]time.Time
	projection   *RunProjection
	closed       bool
}

// NewTextRenderer creates a TextRenderer. Colors are enabled only when w is a TTY.
func NewTextRenderer(w io.Writer) *TextRenderer {
	return NewTextRendererWithOptions(w, Options{})
}

// NewTextRendererWithOptions creates a TextRenderer with the provided options.
func NewTextRendererWithOptions(w io.Writer, opts Options) *TextRenderer {
	colorMode := opts.Color
	if colorMode == ColorAuto {
		colorMode = DetectColor("", false, w)
	}
	r := &TextRenderer{
		w:            w,
		color:        colorMode.UseColor(),
		verbose:      opts.Verbose,
		maxFailLines: opts.MaxFailLines,
		mode:         normalizeRunMode(opts.Mode),
		runDir:       opts.RunDir,
		activeTasks:  make(map[string]time.Time),
		projection:   NewRunProjectionWithOptions(opts),
	}
	if r.maxFailLines <= 0 {
		r.maxFailLines = defaultFailureOutputLimit
	}
	r.activeTasks = make(map[string]time.Time)
	return r
}

func (r *TextRenderer) ensureState() {
	if r.activeTasks == nil {
		r.activeTasks = make(map[string]time.Time)
	}
	if r.mode == "" {
		r.mode = "apply"
	}
}

func (r *TextRenderer) colorize(code, text string) string {
	if !r.color {
		return text
	}
	return code + text + ansiReset
}

func (r *TextRenderer) Emit(event Event) {
	r.ensureState()
	// Feed the event into the shared projection so all counters and run
	// state are folded once, regardless of how many sinks consume the stream.
	r.projection.Apply(event)
	switch e := event.(type) {
	case VersionEvent:
		// Version event is only written to the run log; no terminal output.
	case RunStartEvent:
		r.emitRunStart(e)
	case TargetStartEvent:
		// Target-level events are handled by the runner activity emit; no terminal output needed.
	case TargetCompleteEvent:
		r.emitTargetComplete(e)
	case TaskStartedEvent:
		r.emitNewTaskStarted(e)
	case TaskOKEvent:
		r.emitNewTaskOK(e)
	case TaskChangedEvent:
		r.emitNewTaskChanged(e)
	case TaskSkippedEvent:
		r.emitNewTaskSkipped(e)
	case TaskFailedEvent:
		r.emitNewTaskFailed(e)
	case DiagnosticEvent:
		// Diagnostic detail is already carried in TaskFailedEvent; no render-time output.
	case RunSummaryEvent:
		r.emitRunSummary(e)
	case TaskOutputEvent:
		r.emitTaskOutput(e)
	case WarningEvent:
		r.writeLine(r.colorize(ansiYellow, "WARNING: "+e.Message))
	case ActivityStartEvent:
		r.emitActivityStart(e)
	case ActivityResultEvent:
		r.emitActivityResult(e)
	case FactsEvent:
		r.writeLines(renderTextFacts(e))
	case PlanEvent:
		r.writeLines(renderTextPlan(e))
	case StateEvent:
		r.writeLines(renderTextState(e))
	case ValidationEvent:
		r.writeLines(renderTextValidation(e))
	case ActionCatalogEvent:
		r.writeLines(renderTextActionCatalog(e))
	case ActionInfoEvent:
		r.writeLines(renderTextActionInfo(e))
	case ActionFetchEvent:
		r.writeLines(renderTextActionFetch(e))
	case PluginListEvent:
		r.writeLines(renderTextPluginList(e))
	case InventoryListEvent:
		r.writeLines(renderTextInventoryList(e))
	case SecretListEvent:
		r.writeLines(renderTextSecretList(e))
	default: // New event types with no terminal output (TargetStartEvent etc.)
	}
}

func (r *TextRenderer) emitNewTaskStarted(e TaskStartedEvent) {
	r.activeTasks[taskBufferKey(e.TaskID, e.TaskName, e.Target)] = time.Now()
	if !r.verbose {
		return
	}
	prefix := ""
	if r.shouldShowHostLabels() && e.Target != "" {
		prefix = "[" + r.displayTarget(e.Target) + "] "
	}
	r.writeLine(prefix + "- " + e.TaskName + " started")
}

func (r *TextRenderer) emitNewTaskOK(e TaskOKEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	elapsed := r.elapsedForTask(key)
	delete(r.activeTasks, key)

	left := statusGlyph("ok", r.projection.IsCheckMode()) + " " + e.TaskName
	right := ""
	if elapsed > 0 {
		right = formatElapsed(elapsed)
	}
	r.writeLine(padLine(left, right, lineWidth))
}

func (r *TextRenderer) emitNewTaskChanged(e TaskChangedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	elapsed := r.elapsedForTask(key)
	delete(r.activeTasks, key)

	left := statusGlyph("changed", r.projection.IsCheckMode()) + " " + e.TaskName
	right := ""
	if elapsed > 0 {
		right = formatElapsed(elapsed)
	}
	r.writeLine(padLine(left, right, lineWidth))
}

func (r *TextRenderer) emitNewTaskSkipped(e TaskSkippedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	delete(r.activeTasks, key)

	left := statusGlyph("skipped", r.projection.IsCheckMode()) + " " + e.TaskName
	r.writeLine(padLine(left, "", lineWidth))
	if e.Reason != "" {
		for _, line := range indentWrapped(2, "reason: "+e.Reason) {
			r.writeLine(line)
		}
	}
}

func (r *TextRenderer) emitNewTaskFailed(e TaskFailedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	elapsed := r.elapsedForTask(key)
	delete(r.activeTasks, key)

	left := statusGlyph("failed", r.projection.IsCheckMode()) + " " + e.TaskName
	right := ""
	if elapsed > 0 {
		right = formatElapsed(elapsed)
	}
	r.writeLine(padLine(left, right, lineWidth))

	indent := 2
	if e.FailMessage != "" {
		for _, line := range indentWrapped(indent, "ERROR: "+e.FailMessage) {
			r.writeLine(line)
		}
	}
	if len(e.Output) > 0 {
		r.writeLine(strings.Repeat(" ", indent) + "output:")
		for _, line := range outputBlockLines(limitFailureOutput(r.maxFailLines, e.Output), indent+2) {
			r.writeLine(line)
		}
	}
}

func (r *TextRenderer) elapsedForTask(key string) time.Duration {
	if key == "" {
		return 0
	}
	started, ok := r.activeTasks[key]
	if !ok {
		return 0
	}
	return time.Since(started)
}

func (r *TextRenderer) emitRunStart(e RunStartEvent) {
	if r.runStarted {
		return
	}
	r.runStarted = true
	r.mode = normalizeRunMode(e.Mode)
	r.playbookPath = strings.TrimSpace(e.PlaybookPath)
	r.playbookName = strings.TrimSpace(e.PlaybookName)
	r.targets = append([]string(nil), e.Targets...)

	r.writeLine(r.colorize(ansiBold, titleRunMode(r.mode)))
	if r.playbookPath != "" {
		r.writeLine("playbook: " + r.playbookPath)
	} else if r.playbookName != "" {
		r.writeLine("playbook: " + r.playbookName)
	}
	if r.playbookPath != "" && r.playbookName != "" {
		r.writeLine("name: " + r.playbookName)
	}
	r.writeTargetIntro()
	r.writeBlank()
}

func (r *TextRenderer) writeTargetIntro() {
	switch len(r.targets) {
	case 0:
		return
	case 1:
		r.writeLine("target: " + r.targets[0])
	default:
		r.writeLine(fmt.Sprintf("targets: %d", len(r.targets)))
		if len(r.targets) <= 5 {
			r.writeLine("  " + strings.Join(r.targets, ", "))
		}
	}
}

func (r *TextRenderer) emitTaskOutput(e TaskOutputEvent) {
	r.writeOutputLines(e.Lines, 4)
}

func (r *TextRenderer) emitTargetComplete(e TargetCompleteEvent) {
	if e.Outcome != "failed" {
		return
	}
	target := e.Target
	if target == "" {
		target = "local"
	}
	if len(r.targets) == 1 && r.targets[0] == "local" && target == "localhost" {
		target = "local"
	}
	r.writeLine(r.colorize(ansiRed, "x "+target+" — failed (see above)"))
}

func (r *TextRenderer) emitActivityStart(e ActivityStartEvent) {
	if !r.verbose {
		return
	}
	r.writeLine(formatActivityLine(r.displayTarget(e.Target), e.Message))
}

func (r *TextRenderer) emitActivityResult(e ActivityResultEvent) {
	if e.Status != "failed" {
		return
	}
	r.writeLine(r.colorize(ansiRed, formatActivityLine(r.displayTarget(e.Target), e.Message)))
}

func (r *TextRenderer) shouldShowHostLabels() bool {
	return r.projection.ShouldShowHostLabels()
}

func (r *TextRenderer) displayTarget(target string) string {
	return r.projection.DisplayTarget(target)
}

func (r *TextRenderer) writeLine(line string) {
	_, _ = fmt.Fprintln(r.w, line)
}

func (r *TextRenderer) writeLines(lines []string) {
	for _, line := range lines {
		r.writeLine(line)
	}
}

func (r *TextRenderer) writeBlank() {
	r.writeLine("")
}

func (r *TextRenderer) writeOutputLines(lines []string, indent int) {
	for _, line := range outputBlockLines(lines, indent) {
		r.writeLine(line)
	}
}

func taskBufferKey(taskID, taskName, target string) string {
	base := taskID
	if base == "" {
		base = taskName
	}
	if base == "" || target == "" {
		return base
	}
	return target + "\x00" + base
}

func (r *TextRenderer) emitRunSummary(e RunSummaryEvent) {
	if r.closed {
		return
	}

	modeTitle := titleRunMode(r.projection.Mode)
	statusGlyph := r.colorize(ansiGreen, "✓")
	statusWord := "complete"
	if r.projection.FailedCount > 0 {
		statusGlyph = r.colorize(ansiRed, "x")
		statusWord = "failed"
	}

	r.writeLine("")
	r.writeLine("Recap")
	r.writeLine(fmt.Sprintf("%s %s %s", statusGlyph, modeTitle, statusWord))

	totals := recapTotals([]struct{ ok, changed, failed, skipped int }{
		{ok: r.projection.OkCount, changed: r.projection.ChangedCount, failed: r.projection.FailedCount, skipped: r.projection.SkippedCount},
	})
	r.writeLine("  tasks: " + renderTaskTotals(totals, r.projection.IsCheckMode(), r.projection.WarningCount))

	if r.projection.FailedCount > 0 {
		r.writeLine("")
		r.writeLine("Needs attention")
		for _, failed := range r.projection.FailedTasks() {
			path := renderTaskFailurePath(failed.actionPath, failed.name)
			r.writeLine("  [" + r.projection.DisplayTarget(failed.target) + "] " + path)
		}
		if r.runDir != "" {
			r.writeLine("  Run directory: " + r.runDir)
		}
	}
}

// Close is a no-op for TextRenderer; run summary is rendered on RunSummaryEvent.
func (r *TextRenderer) Close() {
	r.closed = true
}
