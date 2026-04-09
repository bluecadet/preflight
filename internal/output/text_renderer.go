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
	w          io.Writer
	color      bool
	verbose    bool
	taskOutput map[string][]string
}

// NewTextRenderer creates a TextRenderer. Colors are enabled only when w is a TTY.
func NewTextRenderer(w io.Writer) *TextRenderer {
	return NewTextRendererWithOptions(w, Options{})
}

// NewTextRendererWithOptions creates a TextRenderer with the provided options.
func NewTextRendererWithOptions(w io.Writer, opts Options) *TextRenderer {
	return &TextRenderer{
		w:          w,
		color:      isTTY(w),
		verbose:    opts.Verbose,
		taskOutput: make(map[string][]string),
	}
}

func (r *TextRenderer) colorize(code, text string) string {
	if !r.color {
		return text
	}
	return code + text + ansiReset
}

func (r *TextRenderer) Emit(event Event) {
	switch e := event.(type) {
	case PlayStartEvent:
		r.emitPlayStart(e)
	case TaskStartEvent:
		r.emitTaskStart(e)
	case TaskOutputEvent:
		r.emitTaskOutput(e)
	case TaskResultEvent:
		r.emitTaskResult(e)
	case PlayEndEvent:
		r.emitPlayEnd(e)
	case ErrorEvent:
		r.writeLine(r.colorize(ansiRed, "ERROR: "+e.Message))
	case WarningEvent:
		r.writeLine(r.colorize(ansiYellow, "WARNING: "+e.Message))
	case ActivityStartEvent:
		r.writeLine(formatActivityLine(e.Target, e.Message))
	case ActivityResultEvent:
		if e.Status == "failed" {
			r.writeLine(r.colorize(ansiRed, formatActivityLine(e.Target, e.Message)))
		}
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
	if !r.bufferTaskOutput(e) {
		r.writeOutputLines(e.Lines)
	}
}

func (r *TextRenderer) emitTaskResult(e TaskResultEvent) {
	label := fmt.Sprintf("TASK [%s]", e.TaskName)
	statusText := r.statusColored(e.Status, e.Message)
	dotsNeeded := max(lineWidth-len(label)-len(e.Status)-3, 1)
	r.writeLine(fmt.Sprintf("%s %s %s", label, strings.Repeat(".", dotsNeeded), statusText))

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

func (r *TextRenderer) statusColored(status, message string) string {
	label := status
	if message != "" {
		label = fmt.Sprintf("%s (%s)", status, message)
	}
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
		r.writeLine("  │ " + line)
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

// Close is a no-op for TextRenderer.
func (r *TextRenderer) Close() {}
