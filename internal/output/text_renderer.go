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
	w                  io.Writer
	color              bool
	verbose            bool
	maxFailLines       int
	mode               string
	runDir             string
	runStarted         bool
	playbookPath       string
	playbookName       string
	targets            []string
	activeTasks        map[string]time.Time
	taskOutput         map[string][]string
	streamedTaskOutput map[string]bool
	lastGroupKey       string
	recaps             []hostRecap
	failedTasks        []failedTask
	warningCount       int
	okCount            int
	changedCount       int
	skippedCount       int
	runSummary         RunSummaryEvent
	closed             bool
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
		w:                  w,
		color:              colorMode.UseColor(),
		verbose:            opts.Verbose,
		maxFailLines:       opts.MaxFailLines,
		mode:               normalizeRunMode(opts.Mode),
		runDir:             opts.RunDir,
		activeTasks:        make(map[string]time.Time),
		taskOutput:         make(map[string][]string),
		streamedTaskOutput: make(map[string]bool),
	}
	if r.maxFailLines <= 0 {
		r.maxFailLines = defaultFailureOutputLimit
	}
	return r
}

func (r *TextRenderer) ensureState() {
	if r.activeTasks == nil {
		r.activeTasks = make(map[string]time.Time)
	}
	if r.taskOutput == nil {
		r.taskOutput = make(map[string][]string)
	}
	if r.streamedTaskOutput == nil {
		r.streamedTaskOutput = make(map[string]bool)
	}
	if r.mode == "" {
		r.mode = "apply"
	}
	if r.recaps == nil {
		r.recaps = make([]hostRecap, 0)
	}
	if r.failedTasks == nil {
		r.failedTasks = make([]failedTask, 0)
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
	switch e := event.(type) {
	case VersionEvent:
		// Version event is only written to the run log; no terminal output.
	case RunStartEvent:
		r.emitRunStart(e)
	case PlayStartEvent:
		r.emitPlayStart(e)
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
	case TaskStartEvent:
		r.emitTaskStart(e)
	case TaskOutputEvent:
		r.emitTaskOutput(e)
	case TaskResultEvent:
		r.emitTaskResult(e)
	case PlayEndEvent:
		r.emitPlayEnd(e)
	case ErrorEvent:
		r.lastGroupKey = ""
		r.writeLine(r.colorize(ansiRed, "ERROR: "+e.Message))
	case WarningEvent:
		r.warningCount++
		r.lastGroupKey = ""
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
	}
}

func (r *TextRenderer) emitNewTaskStarted(e TaskStartedEvent) {
	r.lastGroupKey = ""
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key != "" {
		r.activeTasks[key] = time.Now()
	}
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

	left := statusGlyph("ok", r.isCheckMode()) + " " + e.TaskName
	right := ""
	if elapsed > 0 {
		right = formatElapsed(elapsed)
	}
	r.lastGroupKey = ""
	r.writeLine(padLine(left, right, lineWidth))
	r.okCount++
}

func (r *TextRenderer) emitNewTaskChanged(e TaskChangedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	elapsed := r.elapsedForTask(key)
	delete(r.activeTasks, key)

	left := statusGlyph("changed", r.isCheckMode()) + " " + e.TaskName
	right := ""
	if elapsed > 0 {
		right = formatElapsed(elapsed)
	}
	r.lastGroupKey = ""
	r.writeLine(padLine(left, right, lineWidth))
	r.changedCount++
}

func (r *TextRenderer) emitNewTaskSkipped(e TaskSkippedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	delete(r.activeTasks, key)

	left := statusGlyph("skipped", r.isCheckMode()) + " " + e.TaskName
	r.lastGroupKey = ""
	r.writeLine(padLine(left, "", lineWidth))
	if e.Reason != "" {
		for _, line := range indentWrapped(2, "reason: "+e.Reason) {
			r.writeLine(line)
		}
	}
	r.skippedCount++
}

func (r *TextRenderer) emitNewTaskFailed(e TaskFailedEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	elapsed := r.elapsedForTask(key)
	delete(r.activeTasks, key)

	r.lastGroupKey = ""
	left := statusGlyph("failed", r.isCheckMode()) + " " + e.TaskName
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

func (r *TextRenderer) emitRunSummary(e RunSummaryEvent) {
	// Run summary will be rendered at Close. For now, just note the stats.
	r.runSummary = e
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

func (r *TextRenderer) emitPlayStart(e PlayStartEvent) {
	if r.runStarted {
		return
	}
	r.emitRunStart(RunStartEvent{
		Mode:         r.mode,
		PlaybookName: e.PlayName,
	})
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

func (r *TextRenderer) emitTaskStart(e TaskStartEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key != "" {
		r.activeTasks[key] = time.Now()
	}
	if !r.verbose {
		return
	}

	r.lastGroupKey = ""
	for _, line := range r.renderTaskStart(e) {
		r.writeLine(line)
	}
}

func (r *TextRenderer) renderTaskStart(e TaskStartEvent) []string {
	var lines []string
	if e.ActionPath != "" {
		lines = append(lines, r.groupHeader(e.Target, e.ActionPath))
		lines = append(lines, "  - "+e.TaskName+" started")
		return lines
	}
	prefix := ""
	if r.shouldShowHostLabels() && e.Target != "" {
		prefix = "[" + r.displayTarget(e.Target) + "] "
	}
	lines = append(lines, prefix+"- "+e.TaskName+" started")
	return lines
}

func (r *TextRenderer) emitTaskOutput(e TaskOutputEvent) {
	if r.verbose {
		r.markTaskOutputStreamed(e)
		r.writeOutputLines(e.Lines, 4)
		return
	}
	if !r.bufferTaskOutput(e) {
		r.writeOutputLines(e.Lines, 4)
	}
}

func (r *TextRenderer) emitTaskResult(e TaskResultEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	var elapsed time.Duration
	if started, ok := r.activeTasks[key]; ok {
		elapsed = time.Since(started)
	}
	delete(r.activeTasks, key)

	outputLines := e.Output
	if len(outputLines) == 0 {
		outputLines = r.takeBufferedOutput(e)
	} else {
		_ = r.takeBufferedOutput(e)
	}
	outputAlreadyStreamed := r.wasTaskOutputStreamed(e)
	r.clearTaskOutputState(e)
	r.recordTaskResult(e)

	for _, line := range r.renderTaskResult(e, elapsed, outputLines, outputAlreadyStreamed) {
		r.writeLine(line)
	}
}

func (r *TextRenderer) renderTaskResult(e TaskResultEvent, elapsed time.Duration, outputLines []string, outputAlreadyStreamed bool) []string {
	var lines []string
	groupKey := ""
	if e.ActionPath != "" {
		groupKey = taskGroupKey(e.Target, e.ActionPath)
		if groupKey != r.lastGroupKey {
			lines = append(lines, r.groupHeader(e.Target, e.ActionPath))
		}
	}

	row := r.taskResultRow(e, elapsed)
	if e.ActionPath != "" {
		row = "  " + row
	}
	lines = append(lines, row)
	lines = append(lines, r.taskDetailLines(e, outputLines, outputAlreadyStreamed)...)

	if groupKey != "" {
		r.lastGroupKey = groupKey
	} else {
		r.lastGroupKey = ""
	}
	return lines
}

func (r *TextRenderer) taskResultRow(e TaskResultEvent, elapsed time.Duration) string {
	left := statusGlyph(e.Status, r.isCheckMode()) + " " + e.TaskName
	if e.ActionPath == "" && r.shouldShowHostLabels() && e.Target != "" {
		left = "[" + r.displayTarget(e.Target) + "] " + left
	}
	right := ""
	if elapsed > 0 && e.Status != "skipped" {
		right = formatElapsed(elapsed)
	}
	return padLine(left, right, lineWidth)
}

func (r *TextRenderer) taskDetailLines(e TaskResultEvent, outputLines []string, outputAlreadyStreamed bool) []string {
	indent := 2
	if e.ActionPath != "" {
		indent = 4
	}

	switch e.Status {
	case "failed":
		return r.failureDetailLines(e, outputLines, outputAlreadyStreamed, indent)
	case "changed":
		if detail := changedDetail(e.Message, r.isCheckMode()); detail != "" {
			return indentWrapped(indent, detail)
		}
	case "ok":
		if detail := okDetail(e.Message); detail != "" {
			return indentWrapped(indent, detail)
		}
	case "skipped":
		if detail := skippedDetail(e.Message); detail != "" {
			return indentWrapped(indent, detail)
		}
	}

	if r.verbose && !outputAlreadyStreamed && len(outputLines) > 0 {
		return outputBlockLines(outputLines, indent)
	}
	return nil
}

func (r *TextRenderer) failureDetailLines(e TaskResultEvent, outputLines []string, outputAlreadyStreamed bool, indent int) []string {
	var lines []string
	if strings.TrimSpace(e.Message) != "" {
		lines = append(lines, indentWrapped(indent, "ERROR: "+strings.TrimSpace(e.Message))...)
	} else {
		lines = append(lines, indentWrapped(indent, "ERROR: task failed")...)
	}
	if !outputAlreadyStreamed && len(outputLines) > 0 {
		lines = append(lines, strings.Repeat(" ", indent)+"output:")
		lines = append(lines, outputBlockLines(limitFailureOutput(r.maxFailLines, outputLines), indent+2)...)
		if len(outputLines) > r.maxFailLines {
			lines = append(lines, indentWrapped(indent, fmt.Sprintf("output truncated: showing last %d of %d lines", r.maxFailLines, len(outputLines)))...)
		}
	}
	lines = append(lines, indentWrapped(indent, "target stopped: remaining tasks were not run")...)
	return lines
}

func (r *TextRenderer) emitPlayEnd(e PlayEndEvent) {
	r.recaps = append(r.recaps, hostRecap{
		target:  e.Target,
		ok:      e.OKCount,
		changed: e.ChangedCount,
		failed:  e.FailedCount,
		skipped: e.SkippedCount,
	})
}

func (r *TextRenderer) emitTargetComplete(e TargetCompleteEvent) {
	if e.Outcome != "failed" {
		// Only render a completion line when there were failures.
		return
	}
	r.lastGroupKey = ""
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
	r.lastGroupKey = ""
	r.writeLine(formatActivityLine(r.displayTarget(e.Target), e.Message))
}

func (r *TextRenderer) emitActivityResult(e ActivityResultEvent) {
	if e.Status != "failed" {
		return
	}
	r.lastGroupKey = ""
	r.writeLine(r.colorize(ansiRed, formatActivityLine(r.displayTarget(e.Target), e.Message)))
}

func (r *TextRenderer) recordTaskResult(e TaskResultEvent) {
	if e.Status != "failed" {
		return
	}
	r.failedTasks = append(r.failedTasks, failedTask{
		target:     e.Target,
		actionPath: e.ActionPath,
		name:       e.TaskName,
		message:    e.Message,
		output:     e.Output,
	})
}

func (r *TextRenderer) groupHeader(target, actionPath string) string {
	header := renderDisplayPath(actionPath)
	if r.shouldShowHostLabels() && target != "" {
		return "[" + r.displayTarget(target) + "] " + header
	}
	return header
}

func (r *TextRenderer) shouldShowHostLabels() bool {
	if r.runStarted {
		return len(r.targets) != 1
	}
	return true
}

func (r *TextRenderer) displayTarget(target string) string {
	if target == "" {
		return "local"
	}
	if len(r.targets) == 1 && r.targets[0] == "local" && target == "localhost" {
		return "local"
	}
	return target
}

func (r *TextRenderer) isCheckMode() bool {
	return r.mode == "check"
}

func (r *TextRenderer) writeLine(line string) {
	_, _ = fmt.Fprintln(r.w, line)
}

func (r *TextRenderer) writeLines(lines []string) {
	r.lastGroupKey = ""
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

func (r *TextRenderer) bufferTaskOutput(e TaskOutputEvent) bool {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" {
		return false
	}
	r.taskOutput[key] = append(r.taskOutput[key], e.Lines...)
	return true
}

func (r *TextRenderer) takeBufferedOutput(e TaskResultEvent) []string {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" {
		return nil
	}
	lines := r.taskOutput[key]
	delete(r.taskOutput, key)
	return lines
}

func (r *TextRenderer) markTaskOutputStreamed(e TaskOutputEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key != "" {
		r.streamedTaskOutput[key] = true
	}
}

func (r *TextRenderer) wasTaskOutputStreamed(e TaskResultEvent) bool {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	return key != "" && r.streamedTaskOutput[key]
}

func (r *TextRenderer) clearTaskOutputState(e TaskResultEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" {
		return
	}
	delete(r.taskOutput, key)
	delete(r.streamedTaskOutput, key)
}

// Close writes the final recap after all host workers have emitted PlayEnd.
func (r *TextRenderer) Close() {
	if r.closed {
		return
	}
	r.closed = true
	if len(r.recaps) == 0 {
		return
	}
	if r.lastGroupKey != "" {
		r.writeBlank()
	}
	r.writeFinalRecap()
}

func (r *TextRenderer) writeFinalRecap() {
	totals := recapTotals(r.recaps)
	failedTargets := failedTargetCount(r.recaps)
	modeTitle := titleRunMode(r.mode)
	statusGlyph := "✓"
	statusWord := "complete"
	if failedTargets > 0 {
		statusGlyph = "x"
		statusWord = "failed"
	}

	r.writeLine("Recap")
	r.writeLine(fmt.Sprintf("%s %s %s", statusGlyph, modeTitle, statusWord))
	if len(r.recaps) == 1 {
		r.writeLine("  target: " + r.displayTarget(r.recaps[0].target))
	} else {
		r.writeLine(fmt.Sprintf("  targets: %d complete, %d failed", len(r.recaps)-failedTargets, failedTargets))
	}
	r.writeLine("  tasks: " + renderTaskTotals(totals, r.isCheckMode(), r.warningCount))

	if failedTargets == 0 {
		return
	}

	r.writeBlank()
	r.writeLine("Needs attention")
	for _, recap := range r.recaps {
		if recap.failed == 0 {
			continue
		}
		target := r.displayTarget(recap.target)
		for _, failed := range r.failedTasks {
			if failed.target != recap.target {
				continue
			}
			path := renderTaskFailurePath(failed.actionPath, failed.name)
			r.writeLine("  [" + target + "] " + path)
		}
	}
	if r.runDir != "" {
		r.writeLine("  Run directory: " + r.runDir)
	}
}

func wrapTextLine(line string, width int) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return []string{""}
	}
	return wrapFactValue(line, width)
}
