package output

import (
	"fmt"
	"io"
	"strings"
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

const lineWidth = 80

// TextRenderer writes Ansible-style human-readable output.
type TextRenderer struct {
	w                  io.Writer
	color              bool
	verbose            bool
	taskOutput         map[string][]string
	streamedTaskOutput map[string]bool
}

// NewTextRenderer creates a TextRenderer. Colors are enabled only when w is a TTY.
func NewTextRenderer(w io.Writer) *TextRenderer {
	return NewTextRendererWithOptions(w, Options{})
}

// NewTextRendererWithOptions creates a TextRenderer with the provided options.
func NewTextRendererWithOptions(w io.Writer, opts Options) *TextRenderer {
	return &TextRenderer{
		w:                  w,
		color:              isTTY(w),
		verbose:            opts.Verbose,
		taskOutput:         make(map[string][]string),
		streamedTaskOutput: make(map[string]bool),
	}
}

func (r *TextRenderer) colorize(code, text string) string {
	if !r.color {
		return text
	}
	return code + text + ansiReset
}

func (r *TextRenderer) Emit(event Event) {
	p, ok := projectEvent(event)
	if !ok {
		return
	}
	switch p.kind {
	case EventPlayStart:
		r.emitPlayStart(PlayStartEvent{PlayName: p.playName})
	case EventTaskStart:
		r.emitTaskStart(TaskStartEvent{TaskName: p.task, TaskID: p.taskID, ActionPath: p.actionPath, Target: p.target})
	case EventTaskOutput:
		r.emitTaskOutput(TaskOutputEvent{TaskName: p.task, TaskID: p.taskID, Target: p.target, Lines: p.lines})
	case EventTaskResult:
		r.emitTaskResult(TaskResultEvent{TaskName: p.task, TaskID: p.taskID, ActionPath: p.actionPath, Target: p.target, Status: p.status, Message: p.message, Output: p.output})
	case EventPlayEnd:
		r.emitPlayEnd(PlayEndEvent{Target: p.target, OKCount: p.okCount, ChangedCount: p.changedCount, FailedCount: p.failedCount, SkippedCount: p.skippedCount})
	case EventError:
		r.writeLine(r.colorize(ansiRed, "ERROR: "+p.errorMessage))
	case EventWarning:
		r.writeLine(r.colorize(ansiYellow, "WARNING: "+p.message))
	case EventActivityStart:
		r.writeLine(formatActivityLine(p.target, p.message))
	case EventActivityResult:
		if p.status == "failed" {
			r.writeLine(r.colorize(ansiRed, formatActivityLine(p.target, p.message)))
		}
	case EventFacts:
		r.writeLines(renderTextFacts(FactsEvent{Target: p.target, Facts: p.facts}))
	case EventPlan:
		r.writeLines(renderTextPlan(PlanEvent{Target: p.target, PlaybookName: p.playName, Tasks: p.tasks}))
	case EventState:
		r.writeLines(renderTextState(StateEvent{Target: p.target, PlaybookName: p.playName, StatePath: p.statePath, LastApplied: p.lastApplied, Comparisons: p.comparisons}))
	case EventValidate:
		r.writeLines(renderTextValidation(ValidationEvent{PlaybookPath: p.playbookPath, PlaybookName: p.playName, TaskCount: p.taskCount, VisitedRefCount: p.visitedRefs, ResolvedRefs: p.resolvedRefs, ErrorCount: p.errorCount}))
	case EventActionList:
		r.writeLines(renderTextActionCatalog(ActionCatalogEvent{EmbeddedNamespace: p.namespace, EmbeddedRefs: p.embeddedRefs, LocalDir: p.localDir, LocalRefs: p.localRefs}))
	case EventActionInfo:
		r.writeLines(renderTextActionInfo(ActionInfoEvent{Ref: p.ref, Name: p.name, Version: p.version, Description: p.description, Author: p.author, Inputs: p.inputs, TaskNames: p.taskNames}))
	case EventActionFetch:
		r.writeLines(renderTextActionFetch(ActionFetchEvent{Entries: p.entries}))
	case EventPluginList:
		r.writeLines(renderTextPluginList(PluginListEvent{Entries: p.plugins}))
	case EventInventoryList:
		r.writeLines(renderTextInventoryList(InventoryListEvent{Hosts: p.hosts}))
	case EventSecretList:
		r.writeLines(renderTextSecretList(SecretListEvent{Entries: p.secrets}))
	}
}

func (r *TextRenderer) emitPlayStart(e PlayStartEvent) {
	title := fmt.Sprintf("PLAY [%s]", e.PlayName)
	r.writeLine(r.colorize(ansiBold, fillLine(title, "*", lineWidth)))
	r.writeBlank()
}

func (r *TextRenderer) emitTaskStart(e TaskStartEvent) {
	r.writeLine(fmt.Sprintf("TASK [%s]", e.TaskName))
}

func (r *TextRenderer) emitTaskOutput(e TaskOutputEvent) {
	if r.verbose {
		r.markTaskOutputStreamed(e)
		r.writeOutputLines(e.Lines)
		return
	}
	if !r.bufferTaskOutput(e) {
		r.writeOutputLines(e.Lines)
	}
}

func (r *TextRenderer) emitTaskResult(e TaskResultEvent) {
	label := fmt.Sprintf("TASK [%s]", e.TaskName)
	statusLines := wrapTextLine(statusLabel(e.Status, e.Message), max(lineWidth-len(label)-4, 16))
	dotsNeeded := max(lineWidth-len(label)-len(statusLines[0])-3, 1)
	r.writeLine(fmt.Sprintf("%s %s %s", label, strings.Repeat(".", dotsNeeded), r.statusColored(e.Status, statusLines[0])))
	if len(statusLines) > 1 {
		r.writeOutputLines(statusLines[1:])
	}

	if r.wasTaskOutputStreamed(e) {
		r.clearTaskOutputState(e)
		return
	}

	lines := r.takeBufferedOutput(e)
	if len(e.Output) > 0 {
		lines = e.Output
	}
	if len(lines) > 0 && (r.verbose || e.Status == "failed") {
		r.writeOutputLines(lines)
	}
}

func (r *TextRenderer) emitPlayEnd(e PlayEndEvent) {
	r.writeLine(r.colorize(ansiBold, fillLine("PLAY RECAP", "*", lineWidth)))
	r.writeLine(fmt.Sprintf("%-14s : ok=%-4d changed=%-4d failed=%-4d skipped=%-4d",
		fallbackTarget(e.Target),
		e.OKCount,
		e.ChangedCount,
		e.FailedCount,
		e.SkippedCount,
	))
	r.writeBlank()
}

func statusLabel(status, message string) string {
	if message != "" {
		return fmt.Sprintf("%s (%s)", status, message)
	}
	return status
}

func (r *TextRenderer) statusColored(status, label string) string {
	switch status {
	case "ok":
		return r.colorize(ansiGreen, label)
	case "changed":
		return r.colorize(ansiYellow, label)
	case "failed":
		return r.colorize(ansiRed, label)
	case "skipped":
		return r.colorize(ansiGrey, label)
	default:
		return label
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

func fillLine(prefix, fill string, width int) string {
	remaining := width - len(prefix)
	if remaining <= 0 {
		return prefix
	}
	return prefix + " " + strings.Repeat(fill, remaining-1)
}

func (r *TextRenderer) writeOutputLines(lines []string) {
	for _, line := range lines {
		for _, wrapped := range wrapTextLine(line, lineWidth-4) {
			r.writeLine("  │ " + wrapped)
		}
	}
}

func wrapTextLine(line string, width int) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return []string{""}
	}
	return wrapFactValue(line, width)
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
	if r.taskOutput == nil {
		r.taskOutput = make(map[string][]string)
	}
	r.taskOutput[key] = append(r.taskOutput[key], e.Lines...)
	return true
}

func (r *TextRenderer) takeBufferedOutput(e TaskResultEvent) []string {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" || r.taskOutput == nil {
		return nil
	}
	lines := r.taskOutput[key]
	delete(r.taskOutput, key)
	return lines
}

func (r *TextRenderer) markTaskOutputStreamed(e TaskOutputEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" {
		return
	}
	if r.streamedTaskOutput == nil {
		r.streamedTaskOutput = make(map[string]bool)
	}
	r.streamedTaskOutput[key] = true
}

func (r *TextRenderer) wasTaskOutputStreamed(e TaskResultEvent) bool {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" || r.streamedTaskOutput == nil {
		return false
	}
	return r.streamedTaskOutput[key]
}

func (r *TextRenderer) clearTaskOutputState(e TaskResultEvent) {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" {
		return
	}
	if r.taskOutput != nil {
		delete(r.taskOutput, key)
	}
	if r.streamedTaskOutput != nil {
		delete(r.streamedTaskOutput, key)
	}
}

// Close is a no-op for TextRenderer.
func (r *TextRenderer) Close() {}
