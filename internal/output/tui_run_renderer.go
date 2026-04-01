package output

import (
	"io"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// TUIRenderer implements Renderer using a Bubble Tea program.
type TUIRenderer struct {
	mu           sync.Mutex
	program      *tea.Program
	events       chan Event
	done         chan struct{}
	writer       io.Writer
	options      Options
	transcript   []Event
	printOnClose bool
}

// NewTUIRenderer creates a TUIRenderer that writes to w.
func NewTUIRenderer(w io.Writer) *TUIRenderer {
	return NewTUIRendererWithOptions(w, Options{})
}

// NewTUIRendererWithOptions creates a TUIRenderer with explicit options.
func NewTUIRendererWithOptions(w io.Writer, options Options) *TUIRenderer {
	events := make(chan Event, 256)
	input := options.Input
	if input == nil && w == os.Stdout && isTTY(w) {
		input = os.Stdin
	}
	options.Input = input

	model := newTUIModel(events, options)
	programOptions := []tea.ProgramOption{
		tea.WithOutput(w),
		tea.WithoutSignalHandler(),
	}
	if input != nil {
		programOptions = append(programOptions, tea.WithInput(input))
		programOptions = append(programOptions, tea.WithAltScreen())
	}

	prog := tea.NewProgram(model, programOptions...)
	renderer := &TUIRenderer{
		program:      prog,
		events:       events,
		done:         make(chan struct{}),
		writer:       w,
		options:      options,
		printOnClose: input != nil,
	}

	go func() {
		defer close(renderer.done)
		_, _ = prog.Run()
	}()

	return renderer
}

// Emit sends an event to the running Bubble Tea program.
func (r *TUIRenderer) Emit(event Event) {
	r.mu.Lock()
	r.transcript = append(r.transcript, event)
	r.mu.Unlock()
	r.events <- event
}

// Close shuts down the Bubble Tea program and waits for it to exit.
func (r *TUIRenderer) Close() {
	close(r.events)
	<-r.done
	if !r.printOnClose {
		return
	}

	r.mu.Lock()
	events := append([]Event(nil), r.transcript...)
	r.mu.Unlock()

	renderer := NewTextRendererWithOptions(r.writer, Options{Verbose: r.options.Verbose})
	for _, event := range events {
		renderer.Emit(event)
	}
}
