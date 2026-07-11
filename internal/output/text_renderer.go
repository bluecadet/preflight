package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// ANSI color codes sourced from the semantic palette.
var (
	ansiReset    = "\033[0m"
	ansiGreen    = DefaultPalette().OK.ANSI
	ansiYellow   = DefaultPalette().Changed.ANSI
	ansiRed      = DefaultPalette().Failed.ANSI
	ansiGrey     = DefaultPalette().Skipped.ANSI
	ansiBold     = DefaultPalette().Bold.ANSI
	ansiTaskName = DefaultPalette().TaskName.ANSI
)

const (
	// lineWidth is the text renderer's default column width. It serves as
	// the non-TTY fallback for detectWidth (piped output stays greppable
	// at a fixed width) and as the wrapping width for fact/failure
	// output blocks in run_format.go.
	lineWidth                 = 80
	defaultFailureOutputLimit = 80
)

// TextRenderer writes the append-only form of the apply/check transcript.
type TextRenderer struct {
	w               io.Writer
	color           bool
	verbose         bool
	maxFailLines    int
	width           int
	activeTasks     map[string]time.Time
	projection      *RunProjection
	runStarted      bool
	runStartPending bool // header buffered until target info arrives
	closed          bool
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
	width := opts.Width
	if width <= 0 {
		width = detectWidth(w)
	}
	r := &TextRenderer{
		w:            w,
		color:        colorMode.UseColor(),
		verbose:      opts.Verbose,
		maxFailLines: opts.MaxFailLines,
		width:        width,
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

	// Flush a pending run-start header before any non-target event so the
	// header (with its inline target roster) always precedes task output.
	if r.runStartPending {
		if _, isTargetStart := event.(TargetStartEvent); !isTargetStart {
			r.flushRunStartHeader()
		}
	}
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
	case SupportGateEvent:
		r.emitSupportGate(e)
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
	r.writeLine(r.targetPrefix(e.Target) + "- " + e.TaskName + " started")
}

func (r *TextRenderer) emitNewTaskOK(e TaskOKEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	elapsed := r.elapsedForTask(key)
	delete(r.activeTasks, key)

	glyph := r.colorize(ansiGreen, statusGlyph("ok", r.projection.IsCheckMode()))
	left := glyph + r.targetLabel(e.Target) + " " + r.colorize(ansiTaskName, e.TaskName)
	if elapsed > 0 {
		left += "  " + formatElapsed(elapsed)
	}
	r.writeLine(left)

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

	glyph := r.colorize(ansiYellow, statusGlyph("changed", r.projection.IsCheckMode()))
	left := glyph + r.targetLabel(e.Target) + " " + r.colorize(ansiTaskName, e.TaskName)
	if elapsed > 0 {
		left += "  " + formatElapsed(elapsed)
	}
	r.writeLine(left)

	if detail := changedDetail("", r.projection.IsCheckMode()); detail != "" {
		for _, line := range indentWrapped(2, detail) {
			r.writeLine(line)
		}
	}
}

func (r *TextRenderer) emitNewTaskSkipped(e TaskSkippedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	delete(r.activeTasks, key)

	glyph := r.colorize(ansiGrey, statusGlyph("skipped", r.projection.IsCheckMode()))
	left := glyph + r.targetLabel(e.Target) + " " + r.colorize(ansiTaskName, e.TaskName)
	r.writeLine(left)
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

	glyph := r.colorize(ansiRed, statusGlyph("failed", r.projection.IsCheckMode()))
	left := glyph + r.targetLabel(e.Target) + " " + r.colorize(ansiTaskName, e.TaskName)
	if elapsed > 0 {
		left += "  " + formatElapsed(elapsed)
	}
	r.writeLine(left)

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
	r.runStartPending = true
}

// flushRunStartHeader emits the buffered run-start header. For single-target
// runs it folds the target identity into one RUN line; for multi-target runs
// it renders the block with the target roster inlined so the roster is part
// of the header rather than a separate listing.
func (r *TextRenderer) flushRunStartHeader() {
	r.runStartPending = false

	if len(r.projection.Targets) == 1 {
		playbook := r.projection.Playbook
		if playbook == "" {
			playbook = r.projection.PlayName
		}
		target := "local"
		transport := "local"
		address := ""
		if len(r.projection.TargetInfo) > 0 {
			ti := r.projection.TargetInfo[0]
			target = ti.Name
			transport = ti.Transport
			address = ti.Address
		}
		tgt := target + " (" + transport
		if address != "" {
			tgt += " • " + address
		}
		tgt += ")"
		elapsed := formatElapsed(r.projection.Elapsed())
		r.writeLine(r.colorize(ansiBold, padLine("RUN  "+playbook+" → "+tgt, elapsed, r.width)))
		r.writeBlank()
		return
	}

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
	if len(r.projection.Targets) > 1 {
		r.writeLine(fmt.Sprintf("targets: %d", len(r.projection.Targets)))
		for _, line := range buildTargetRosterLines(r.projection.TargetInfo, r.rosterColorer()) {
			r.writeLine(line)
		}
	}
	r.writeBlank()
}

func (r *TextRenderer) emitTargetStart(e TargetStartEvent) {
	if !r.runStartPending {
		return
	}
	// Single-target: flush as soon as the first (only) target arrives.
	if len(r.projection.Targets) == 1 {
		r.flushRunStartHeader()
		return
	}
	// Multi-target: flush once all targets have reported so the roster can
	// be folded into the header in arrival order.
	if len(r.projection.TargetInfo) >= len(r.projection.Targets) {
		r.flushRunStartHeader()
	}
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

// targetLabel returns the inline [target] segment (space-prefixed, no
// trailing space) for task lines in multi-target runs, or "" when target
// labels are suppressed (single-target runs) or the target is empty.
func (r *TextRenderer) targetLabel(target string) string {
	if !r.shouldShowHostLabels() || target == "" {
		return ""
	}
	return " [" + r.coloredTarget(target) + "]"
}

// targetPrefix returns the inline [target] prefix prepended to task lines in
// multi-target runs, or "" when target labels are suppressed (single-target
// runs) or the target is empty.
func (r *TextRenderer) targetPrefix(target string) string {
	if !r.shouldShowHostLabels() || target == "" {
		return ""
	}
	return "[" + r.coloredTarget(target) + "] "
}

// coloredTarget returns the display name for a target rendered in its
// assigned host color. When color is disabled or the target has no color
// slot, the plain display name is returned.
func (r *TextRenderer) coloredTarget(target string) string {
	name := r.displayTarget(target)
	if !r.color {
		return name
	}
	code := r.hostColorANSI(target)
	if code == "" {
		return name
	}
	return code + name + ansiReset
}

// hostColorANSI returns the ANSI escape code for a target's assigned host
// color, or "" when the target has no color slot (unknown target).
func (r *TextRenderer) hostColorANSI(target string) string {
	c, ok := DefaultPalette().HostColor(r.projection.HostColorIndex(target))
	if !ok {
		return ""
	}
	return c.ANSI
}

// rosterColorer returns a function that renders a target's raw name in its
// assigned host color, for coloring the run-start target roster.
func (r *TextRenderer) rosterColorer() func(string) string {
	return func(name string) string {
		if !r.color {
			return name
		}
		code := r.hostColorANSI(name)
		if code == "" {
			return name
		}
		return code + name + ansiReset
	}
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

// emitSupportGate renders the apply-start support gate refusal: a summary
// line (reusing the event's LogMessage so wording stays consistent across
// sinks) colored red, then one line per violation naming the task and the
// uniform module-by-runtime message.
func (r *TextRenderer) emitSupportGate(e SupportGateEvent) {
	r.writeLine(r.colorize(ansiRed, e.LogMessage()))
	for _, v := range e.Violations {
		r.writeLine(fmt.Sprintf("  %s: %s", v.TaskName, v.Message))
	}
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
				r.writeLine("  [" + r.coloredTarget(failed.target) + "] " + path)
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
