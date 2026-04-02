package output

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// EventType identifies the kind of output event.
type EventType string

const (
	EventPlayStart  EventType = "play_start"
	EventTaskStart  EventType = "task_start"
	EventTaskOutput EventType = "task_output"
	EventTaskResult EventType = "task_result"
	EventPlayEnd    EventType = "play_end"
	EventWarning    EventType = "warning"
	EventError      EventType = "error"
)

// Event carries all data for a single renderer call.
type Event struct {
	Type     EventType
	PlayName string // for play_start / play_end
	TaskName string // for task_start / task_output / task_result
	TaskID   string // for task_start / task_output / task_result; slash-separated nesting path e.g. "action/subtask"
	Target   string // hostname
	Status   string // "ok", "changed", "failed", "skipped"
	Message  string
	Error    error
	Lines    []string // for task_output: one or more streamed output lines
	Output   []string // for task_result: full captured output for failed tasks
	// For play_end recap:
	OKCount      int
	ChangedCount int
	FailedCount  int
	SkippedCount int
}

// Renderer is the interface that all output renderers implement.
type Renderer interface {
	Emit(event Event)
	Close()
}

// ANSI color codes.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiGrey   = "\033[90m"
	ansiBold   = "\033[1m"
	ansiCyan   = "\033[36m"
)

const lineWidth = 80

// isTTY returns true if w is os.Stdout or os.Stderr and the fd is a terminal.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	// ModeCharDevice is set for terminal devices.
	return (fi.Mode() & os.ModeCharDevice) != 0
}

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

func fillLine(prefix, fill string, width int) string {
	remaining := width - len(prefix)
	if remaining <= 0 {
		return prefix
	}
	return prefix + " " + strings.Repeat(fill, remaining-1)
}

func (r *TextRenderer) writeOutputLines(lines []string) {
	for _, line := range lines {
		_, _ = fmt.Fprintf(r.w, "  │ %s\n", line)
	}
}

func (r *TextRenderer) taskKey(event Event) string {
	var base string
	if event.TaskID != "" {
		base = event.TaskID
	} else {
		base = event.TaskName
	}
	if event.Target == "" {
		return base
	}
	return event.Target + "\x00" + base
}

func (r *TextRenderer) bufferTaskOutput(event Event) bool {
	key := r.taskKey(event)
	if key == "" {
		return false
	}
	if r.taskOutput == nil {
		r.taskOutput = make(map[string][]string)
	}
	r.taskOutput[key] = append(r.taskOutput[key], event.Lines...)
	return true
}

func (r *TextRenderer) takeBufferedOutput(event Event) []string {
	key := r.taskKey(event)
	if key == "" || r.taskOutput == nil {
		return nil
	}
	lines := r.taskOutput[key]
	delete(r.taskOutput, key)
	return lines
}

// Emit writes a formatted line (or block) for the given event.
func (r *TextRenderer) Emit(event Event) {
	switch event.Type {
	case EventPlayStart:
		title := fmt.Sprintf("PLAY [%s]", event.PlayName)
		line := fillLine(title, "*", lineWidth)
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiBold, line))
		_, _ = fmt.Fprintln(r.w)

	case EventTaskOutput:
		if !r.bufferTaskOutput(event) {
			r.writeOutputLines(event.Lines)
		}

	case EventTaskResult:
		label := fmt.Sprintf("TASK [%s]", event.TaskName)
		// Build dots then status
		statusStr := r.statusColored(event.Status, event.Message)
		dotsNeeded := lineWidth - len(label) - len(event.Status) - 3
		dotsNeeded = max(dotsNeeded, 1)
		dots := strings.Repeat(".", dotsNeeded)
		_, _ = fmt.Fprintf(r.w, "%s %s %s\n", label, dots, statusStr)
		buffered := r.takeBufferedOutput(event)
		lines := buffered
		if len(event.Output) > 0 {
			lines = event.Output
		}
		if len(lines) > 0 && (r.verbose || event.Status == "failed") {
			r.writeOutputLines(lines)
		}

	case EventPlayEnd:
		title := "PLAY RECAP"
		line := fillLine(title, "*", lineWidth)
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiBold, line))
		target := event.Target
		if target == "" {
			target = "localhost"
		}
		recap := fmt.Sprintf("%-14s : ok=%-4d changed=%-4d failed=%-4d skipped=%-4d",
			target,
			event.OKCount,
			event.ChangedCount,
			event.FailedCount,
			event.SkippedCount,
		)
		_, _ = fmt.Fprintln(r.w, recap)
		_, _ = fmt.Fprintln(r.w)

	case EventError:
		msg := event.Message
		if event.Error != nil {
			msg = event.Error.Error()
		}
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiRed, "ERROR: "+msg))

	case EventWarning:
		msg := event.Message
		if event.Error != nil {
			msg = event.Error.Error()
		}
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiYellow, "WARNING: "+msg))
	}
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

// Close is a no-op for TextRenderer.
func (r *TextRenderer) Close() {}
