package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// EventType identifies the kind of output event.
type EventType string

const (
	EventPlayStart  EventType = "play_start"
	EventTaskStart  EventType = "task_start"
	EventTaskResult EventType = "task_result"
	EventPlayEnd    EventType = "play_end"
	EventWarning    EventType = "warning"
	EventError      EventType = "error"
)

// Event carries all data for a single renderer call.
type Event struct {
	Type      EventType
	PlayName  string // for play_start / play_end
	TaskName  string // for task_result
	Module    string // for task_start / task_result
	Target    string // hostname
	Status    string // "ok", "changed", "failed", "skipped"
	Message   string
	Error     error
	TaskIndex int
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
	w     io.Writer
	width int
	theme terminalTheme
}

// NewTextRenderer creates a TextRenderer using the shared lipgloss theme.
func NewTextRenderer(w io.Writer) *TextRenderer {
	return &TextRenderer{
		w:     w,
		width: detectTerminalWidth(w),
		theme: newTerminalTheme(w),
	}
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
		title := lipgloss.JoinHorizontal(
			lipgloss.Center,
			r.theme.pill.Render("PLAY"),
			" ",
			r.theme.title.Render(event.PlayName),
		)
		if event.Target != "" {
			title = lipgloss.JoinHorizontal(lipgloss.Center, title, " ", r.theme.host(event.Target))
		}
		if event.TaskTotal > 0 {
			title = lipgloss.JoinHorizontal(
				lipgloss.Center,
				title,
				" ",
				r.theme.muted.Render(fmt.Sprintf("%d tasks", event.TaskTotal)),
			)
		}
		_, _ = fmt.Fprintln(r.w, title)
		_, _ = fmt.Fprintln(r.w)

	case EventTaskStart:
		line := lipgloss.JoinHorizontal(
			lipgloss.Top,
			r.theme.statusBadge("running"),
			" ",
			r.renderTaskLabel(event, true),
		)
		_, _ = fmt.Fprintln(r.w, line)

	case EventTaskResult:
		line := lipgloss.JoinHorizontal(
			lipgloss.Top,
			r.theme.statusBadge(event.Status),
			" ",
			r.renderTaskLabel(event, false),
		)
		_, _ = fmt.Fprintln(r.w, line)

	case EventPlayEnd:
		target := event.Target
		if target == "" {
			target = "localhost"
		}
		recap := lipgloss.JoinHorizontal(
			lipgloss.Center,
			r.theme.pill.Render("RECAP"),
			" ",
			r.theme.host(target),
			"  ",
			r.theme.statusBadge("ok"),
			" ",
			r.theme.value.Render(fmt.Sprintf("%d", event.OKCount)),
			"  ",
			r.theme.statusBadge("changed"),
			" ",
			r.theme.value.Render(fmt.Sprintf("%d", event.ChangedCount)),
			"  ",
			r.theme.statusBadge("failed"),
			" ",
			r.theme.value.Render(fmt.Sprintf("%d", event.FailedCount)),
			"  ",
			r.theme.statusBadge("skipped"),
			" ",
			r.theme.value.Render(fmt.Sprintf("%d", event.SkippedCount)),
		)
		_, _ = fmt.Fprintln(r.w, recap)
		_, _ = fmt.Fprintln(r.w)

	case EventError:
		msg := event.Message
		if event.Error != nil {
			msg = event.Error.Error()
		}
		_, _ = fmt.Fprintln(r.w, r.theme.note("error", msg))

	case EventWarning:
		msg := event.Message
		if event.Error != nil {
			msg = event.Error.Error()
		}
		_, _ = fmt.Fprintln(r.w, r.theme.note("warning", msg))
	}
}

func (r *TextRenderer) renderTaskLabel(event Event, showModule bool) string {
	parts := make([]string, 0, 5)
	if event.Target != "" {
		parts = append(parts, r.theme.host(event.Target))
	}
	if showModule && event.Module != "" {
		parts = append(parts, r.theme.pill.Render(event.Module))
	}
	if event.TaskName != "" {
		parts = append(parts, r.theme.value.Render(event.TaskName))
	}
	if event.Message != "" {
		parts = append(parts, r.theme.muted.Render(event.Message))
	}
	if event.TaskTotal > 0 && event.TaskIndex > 0 {
		parts = append(parts, r.theme.muted.Render(fmt.Sprintf("%d/%d", event.TaskIndex, event.TaskTotal)))
	}
	return strings.Join(parts, "  ")
}

// Close is a no-op for TextRenderer.
func (r *TextRenderer) Close() {}
