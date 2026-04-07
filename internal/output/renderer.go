package output

import (
	"fmt"
	"io"
	"os"
	"sort"
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
	EventFacts      EventType = "facts"
	EventPlan       EventType = "plan"
	EventState      EventType = "state"
)

// Event is the sealed interface implemented by all renderer event types.
type Event interface{ isEvent() }

type PlayStartEvent struct{ PlayName string }
type TaskStartEvent struct{ TaskName, TaskID, ActionPath, Target string }
type TaskOutputEvent struct {
	TaskName, TaskID, Target string
	Lines                    []string
}
type TaskResultEvent struct {
	TaskName, TaskID, ActionPath, Target, Status, Message string
	Output                                                []string
}
type PlayEndEvent struct {
	Target                                           string
	OKCount, ChangedCount, FailedCount, SkippedCount int
}
type WarningEvent struct{ Message string }
type ErrorEvent struct{ Message string }

// FactsEvent carries gathered facts for a single target.
type FactsEvent struct {
	Target string
	Facts  map[string]any
}

// PlanTaskEntry describes a single planned task for PlanEvent.
type PlanTaskEntry struct {
	Number int
	Module string
	Name   string
	When   string
	Tags   []string
}

// PlanEvent carries the resolved execution plan for a single target.
type PlanEvent struct {
	Target       string
	PlaybookName string
	Tasks        []PlanTaskEntry
}

// StateEvent carries the state comparison data for a single target.
type StateEvent struct {
	Target       string
	PlaybookName string
	StatePath    string
	LastApplied  string
	Comparisons  []StateComparison
}

// StateComparison is a single row in the state diff table.
type StateComparison struct {
	Status         string
	TaskName       string
	Module         string
	RecordedStatus string
}

func (PlayStartEvent) isEvent()  {}
func (TaskStartEvent) isEvent()  {}
func (TaskOutputEvent) isEvent() {}
func (TaskResultEvent) isEvent() {}
func (PlayEndEvent) isEvent()    {}
func (WarningEvent) isEvent()    {}
func (ErrorEvent) isEvent()      {}
func (FactsEvent) isEvent()      {}
func (PlanEvent) isEvent()       {}
func (StateEvent) isEvent()      {}

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

func taskBufferKey(taskID, taskName, target string) string {
	var base string
	if taskID != "" {
		base = taskID
	} else {
		base = taskName
	}
	if target == "" {
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

// Emit writes a formatted line (or block) for the given event.
func (r *TextRenderer) Emit(event Event) {
	switch e := event.(type) {
	case PlayStartEvent:
		title := fmt.Sprintf("PLAY [%s]", e.PlayName)
		line := fillLine(title, "*", lineWidth)
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiBold, line))
		_, _ = fmt.Fprintln(r.w)

	case TaskOutputEvent:
		if !r.bufferTaskOutput(e) {
			r.writeOutputLines(e.Lines)
		}

	case TaskResultEvent:
		label := fmt.Sprintf("TASK [%s]", e.TaskName)
		statusStr := r.statusColored(e.Status, e.Message)
		dotsNeeded := lineWidth - len(label) - len(e.Status) - 3
		dotsNeeded = max(dotsNeeded, 1)
		dots := strings.Repeat(".", dotsNeeded)
		_, _ = fmt.Fprintf(r.w, "%s %s %s\n", label, dots, statusStr)
		buffered := r.takeBufferedOutput(e)
		lines := buffered
		if len(e.Output) > 0 {
			lines = e.Output
		}
		if len(lines) > 0 && (r.verbose || e.Status == "failed") {
			r.writeOutputLines(lines)
		}

	case PlayEndEvent:
		title := "PLAY RECAP"
		line := fillLine(title, "*", lineWidth)
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiBold, line))
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		recap := fmt.Sprintf("%-14s : ok=%-4d changed=%-4d failed=%-4d skipped=%-4d",
			target,
			e.OKCount,
			e.ChangedCount,
			e.FailedCount,
			e.SkippedCount,
		)
		_, _ = fmt.Fprintln(r.w, recap)
		_, _ = fmt.Fprintln(r.w)

	case ErrorEvent:
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiRed, "ERROR: "+e.Message))

	case WarningEvent:
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiYellow, "WARNING: "+e.Message))

	case FactsEvent:
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		_, _ = fmt.Fprintf(r.w, "Facts for %s:\n", target)
		keys := make([]string, 0, len(e.Facts))
		for k := range e.Facts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = fmt.Fprintf(r.w, "  %s: %v\n", k, e.Facts[k])
		}

	case PlanEvent:
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		_, _ = fmt.Fprintf(r.w, "Target: %s\n", target)
		_, _ = fmt.Fprintf(r.w, "Playbook: %s\n", e.PlaybookName)
		_, _ = fmt.Fprintf(r.w, "Tasks (%d):\n", len(e.Tasks))
		for _, t := range e.Tasks {
			_, _ = fmt.Fprintf(r.w, "  %d. [%s] %s", t.Number, t.Module, t.Name)
			if t.When != "" {
				_, _ = fmt.Fprintf(r.w, " (when: %s)", t.When)
			}
			if len(t.Tags) > 0 {
				_, _ = fmt.Fprintf(r.w, " [tags: %v]", t.Tags)
			}
			_, _ = fmt.Fprintln(r.w)
		}

	case StateEvent:
		if e.PlaybookName != "" {
			_, _ = fmt.Fprintf(r.w, "State diff for playbook: %s\n", e.PlaybookName)
		}
		if e.Target != "" {
			_, _ = fmt.Fprintf(r.w, "Target: %s\n", e.Target)
		}
		_, _ = fmt.Fprintf(r.w, "State file: %s\n", e.StatePath)
		_, _ = fmt.Fprintf(r.w, "Last applied: %s\n\n", e.LastApplied)
		if len(e.Comparisons) > 0 {
			_, _ = fmt.Fprintf(r.w, "%-12s %-28s %-16s %s\n", "STATUS", "TASK", "MODULE", "RECORDED STATUS")
			_, _ = fmt.Fprintf(r.w, "%-12s %-28s %-16s %s\n", "------------", "----------------------------", "----------------", "---------------")
			for _, c := range e.Comparisons {
				_, _ = fmt.Fprintf(r.w, "%-12s %-28s %-16s %s\n", c.Status, c.TaskName, c.Module, c.RecordedStatus)
			}
		}
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
