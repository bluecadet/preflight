package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// ANSI color codes sourced from the semantic palette.
var (
	ansiReset  = "\033[0m"
	ansiGreen  = DefaultPalette().OK.ANSI
	ansiYellow = DefaultPalette().Changed.ANSI
	ansiRed    = DefaultPalette().Failed.ANSI
	ansiBold   = DefaultPalette().Bold.ANSI
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
	activeTasks  map[string]time.Time
	projection   *RunProjection
	runStarted   bool
	closed       bool

	// bufferedRunStart holds RunStartEvent data for single-target runs so
	// the header can be printed together with the target info from TargetStartEvent.
	bufferedRunStart *RunStartEvent
	rosterPrinted    bool
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
		activeTasks:  make(map[string]time.Time),
		projection:   NewRunProjectionWithOptions(opts),
	}
	if r.maxFailLines <= 0 {
		r.maxFailLines = defaultFailureOutputLimit
	}
	return r
}

func (r *TextRenderer) colorize(code, text string) string {
	if !r.color {
		return text
	}
	return code + text + ansiReset
}

func (r *TextRenderer) Emit(event Event) {
	// Feed the event into the shared projection so all counters and run
	// state are folded once, regardless of how many sinks consume the stream.
	r.projection.Apply(event)
	switch e := event.(type) {
	case VersionEvent:
		// Version event is only written to the run log; no terminal output.
	case RunStartEvent:
		r.emitRunStart(e)
	case TargetStartEvent:
		r.emitTargetStart(e)
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
	if r.shouldShowHostLabels() && e.Target != "" {
		left = "[" + r.displayTarget(e.Target) + "] " + left
	}
	right := ""
	if elapsed > 0 {
		right = formatElapsed(elapsed)
	}
	r.writeLine(padLine(left, right, lineWidth))

	if detail := okDetail(""); detail != "" {
		for _, line := range indentWrapped(2, detail) {
			r.writeLine(line)
		}
	}
}

func (r *TextRenderer) emitNewTaskChanged(e TaskChangedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	elapsed := r.elapsedForTask(key)
	delete(r.activeTasks, key)

	left := statusGlyph("changed", r.projection.IsCheckMode()) + " " + e.TaskName
	if r.shouldShowHostLabels() && e.Target != "" {
		left = "[" + r.displayTarget(e.Target) + "] " + left
	}
	right := ""
	if elapsed > 0 {
		right = formatElapsed(elapsed)
	}
	r.writeLine(padLine(left, right, lineWidth))

	if detail := changedDetail("", r.projection.IsCheckMode()); detail != "" {
		for _, line := range indentWrapped(2, detail) {
			r.writeLine(line)
		}
	}
}

func (r *TextRenderer) emitNewTaskSkipped(e TaskSkippedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	delete(r.activeTasks, key)

	left := statusGlyph("skipped", r.projection.IsCheckMode()) + " " + e.TaskName
	if r.shouldShowHostLabels() && e.Target != "" {
		left = "[" + r.displayTarget(e.Target) + "] " + left
	}
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
	if r.shouldShowHostLabels() && e.Target != "" {
		left = "[" + r.displayTarget(e.Target) + "] " + left
	}
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

	// For single-target runs, buffer the run start until we have the target
	// info from TargetStartEvent so the header can include transport/address.
	if len(e.Targets) == 1 {
		r.bufferedRunStart = &RunStartEvent{
			Mode:         e.Mode,
			PlaybookPath: e.PlaybookPath,
			PlaybookName: e.PlaybookName,
			Targets:      e.Targets,
		}
		return
	}

	r.flushRunStartHeader()
}

func (r *TextRenderer) flushRunStartHeader() {
	r.writeLine(r.colorize(ansiBold, titleRunMode(r.projection.Mode)))
	switch {
	case r.projection.Playbook != "":
		r.writeLine("playbook: " + r.projection.Playbook)
	case r.projection.PlayName != "":
		r.writeLine("playbook: " + r.projection.PlayName)
	}
	if r.projection.Playbook != "" && r.projection.PlayName != "" {
		r.writeLine("name: " + r.projection.PlayName)
	}
	r.writeBlank()
}

func (r *TextRenderer) emitTargetStart(e TargetStartEvent) {
	// Single-target runs: promote target info into the header line.
	if r.bufferedRunStart != nil {
		playbook := r.projection.Playbook
		if playbook == "" {
			playbook = r.projection.PlayName
		}
		target := e.Target + " (" + e.Transport
		if e.Address != "" {
			target += " • " + e.Address
		}
		target += ")"
		elapsed := formatElapsed(r.projection.Elapsed())
		r.writeLine(r.colorize(ansiBold, padLine("RUN  "+playbook+" → "+target, elapsed, lineWidth)))
		r.writeBlank()
		r.bufferedRunStart = nil
		return
	}

	// Multi-target runs: print roster line for each target.
	if !r.rosterPrinted && len(r.projection.Targets) > 1 {
		r.rosterPrinted = true
		r.writeLine("Targets:")
	}
	if r.rosterPrinted {
		r.writeLine("  " + r.targetDisplayString(e))
	}
}

func (r *TextRenderer) targetDisplayString(e TargetStartEvent) string {
	s := e.Target + " (" + e.Transport
	if e.Address != "" {
		s += " • " + e.Address
	}
	s += ")"
	return s
}

func (r *TextRenderer) emitTaskOutput(e TaskOutputEvent) {
	r.writeOutputLines(e.Lines, 4)
}

func (r *TextRenderer) emitTargetComplete(e TargetCompleteEvent) {
	if e.Outcome == "failed" {
		target := e.Target
		if target == "" {
			target = "local"
		}
		if len(r.projection.Targets) == 1 && r.projection.Targets[0] == "local" && target == "localhost" {
			target = "local"
		}
		r.writeLine(r.colorize(ansiRed, "x "+target+" — failed (see above)"))
	}
	if r.verbose && e.WinRMRoundTrips > 0 {
		r.writeLine(fmt.Sprintf("  %s", formatActivityLine(r.displayTarget(e.Target), fmt.Sprintf("winrm: %d round trips", e.WinRMRoundTrips))))
	}
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
		showTarget := r.shouldShowHostLabels()
		for _, failed := range r.projection.FailedTasks() {
			path := renderTaskFailurePath(failed.actionPath, failed.name)
			if showTarget {
				r.writeLine("  [" + r.projection.DisplayTarget(failed.target) + "] " + path)
			} else {
				r.writeLine("  " + path)
			}
		}
	}
	if r.projection.RunDir != "" {
		r.writeLine("  Run directory: " + r.projection.RunDir)
	}
}

// Close is a no-op for TextRenderer; run summary is rendered on RunSummaryEvent.
func (r *TextRenderer) Close() {
	r.closed = true
}
