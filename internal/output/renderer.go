package output

import (
	"io"
	"os"
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
	TaskPath  string // display path like 2.1 for task_start / task_log / task_result
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
