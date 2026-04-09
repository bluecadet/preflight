package output

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// TUIRenderer implements Renderer using a Bubble Tea program.
type TUIRenderer struct {
	program *tea.Program
	events  chan Event
	done    chan struct{}
}

// NewTUIRenderer creates a TUIRenderer that writes to w.
func NewTUIRenderer(w io.Writer) *TUIRenderer {
	return NewTUIRendererWithOptions(w, Options{})
}

// NewTUIRendererWithOptions creates a TUIRenderer that writes to w.
func NewTUIRendererWithOptions(w io.Writer, opts Options) *TUIRenderer {
	events := make(chan Event, 64)
	model := newTUIModelWithOptions(events, opts)
	program := tea.NewProgram(
		model,
		tea.WithOutput(w),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
	)

	r := &TUIRenderer{
		program: program,
		events:  events,
		done:    make(chan struct{}),
	}
	go func() {
		defer close(r.done)
		_, _ = program.Run()
	}()
	return r
}

// Emit sends an event to the running Bubble Tea program.
func (r *TUIRenderer) Emit(event Event) {
	r.events <- event
}

// Close shuts down the Bubble Tea program and waits for it to exit.
func (r *TUIRenderer) Close() {
	close(r.events)
	<-r.done
}
