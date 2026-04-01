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
	EventPhaseStart EventType = "phase_start"
	EventPhaseEnd   EventType = "phase_end"
	EventTaskStart  EventType = "task_start"
	EventTaskLog    EventType = "task_log"
	EventTaskResult EventType = "task_result"
	EventPlayEnd    EventType = "play_end"
	EventError      EventType = "error"
)

// Event carries all data for a single renderer call.
type Event struct {
	Type      EventType
	PlayName  string // for play_start / play_end
	Phase     string // for phase_start / phase_end
	TaskID    string // for task_start / task_log / task_result
	TaskName  string // for task_result
	Target    string // hostname
	Module    string // task module
	Stream    string // stdout / stderr / info
	Line      string // task log line
	Status    string // "ok", "changed", "failed", "skipped"
	Message   string
	Error     error
	TaskTotal int
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
	w       io.Writer
	color   bool
	verbose bool
}

// NewTextRenderer creates a TextRenderer. Colors are enabled only when w is a TTY.
func NewTextRenderer(w io.Writer) *TextRenderer {
	return NewTextRendererWithOptions(w, Options{})
}

// NewTextRendererWithOptions creates a TextRenderer with explicit options.
func NewTextRendererWithOptions(w io.Writer, options Options) *TextRenderer {
	return &TextRenderer{
		w:       w,
		color:   isTTY(w),
		verbose: options.Verbose,
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

// Emit writes a formatted line (or block) for the given event.
func (r *TextRenderer) Emit(event Event) {
	switch event.Type {
	case EventPlayStart:
		title := fmt.Sprintf("PLAY [%s]", event.PlayName)
		line := fillLine(title, "*", lineWidth)
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiBold, line))
		_, _ = fmt.Fprintln(r.w)

	case EventPhaseStart:
		if !r.verbose {
			return
		}
		target := r.targetPrefix(event.Target)
		_, _ = fmt.Fprintf(r.w, "%sPHASE [%s] starting\n", target, event.Phase)

	case EventPhaseEnd:
		if !r.verbose {
			return
		}
		target := r.targetPrefix(event.Target)
		status := event.Status
		if status == "" {
			status = "ok"
		}
		if event.TaskTotal > 0 {
			_, _ = fmt.Fprintf(r.w, "%sPHASE [%s] %s (%d tasks)\n", target, event.Phase, status, event.TaskTotal)
			return
		}
		_, _ = fmt.Fprintf(r.w, "%sPHASE [%s] %s\n", target, event.Phase, status)

	case EventTaskStart:
		if !r.verbose {
			return
		}
		target := r.targetPrefix(event.Target)
		if event.Module != "" {
			_, _ = fmt.Fprintf(r.w, "%sSTART [%s] (%s)\n", target, event.TaskName, event.Module)
			return
		}
		_, _ = fmt.Fprintf(r.w, "%sSTART [%s]\n", target, event.TaskName)

	case EventTaskLog:
		if !r.verbose {
			return
		}
		target := r.targetPrefix(event.Target)
		stream := event.Stream
		if stream == "" {
			stream = "log"
		}
		_, _ = fmt.Fprintf(r.w, "%s  [%s] %s\n", target, stream, event.Line)

	case EventTaskResult:
		label := fmt.Sprintf("TASK [%s]", event.TaskName)
		// Build dots then status
		statusStr := r.statusColored(event.Status, event.Message)
		dotsNeeded := lineWidth - len(label) - len(event.Status) - 3
		dotsNeeded = max(dotsNeeded, 1)
		dots := strings.Repeat(".", dotsNeeded)
		_, _ = fmt.Fprintf(r.w, "%s %s %s\n", label, dots, statusStr)

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
	}
}

func (r *TextRenderer) targetPrefix(target string) string {
	if target == "" {
		return ""
	}
	return fmt.Sprintf("%s | ", target)
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
