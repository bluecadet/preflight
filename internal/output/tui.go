package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Lipgloss styles for the TUI renderer.
var (
	tuiStyleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))   // green
	tuiStyleChanged = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))   // yellow
	tuiStyleFailed  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))   // red
	tuiStyleSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // grey
	tuiStyleHeader  = lipgloss.NewStyle().Bold(true)
	tuiStyleSpinner = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // blue
)

// taskView holds display state for a single task.
type taskView struct {
	name    string
	status  string // "", "ok", "changed", "failed", "skipped"
	running bool
}

// recapCounts holds running totals.
type recapCounts struct {
	ok, changed, failed, skipped int
}

// tuiEventMsg wraps an Event for delivery via the Bubbletea message bus.
type tuiEventMsg struct {
	event Event
}

// tuiDoneMsg signals the program to quit.
type tuiDoneMsg struct{}

// tuiModel is the Bubbletea model for the TUI renderer.
type tuiModel struct {
	spinner  spinner.Model
	tasks    []taskView
	playName string
	recap    recapCounts
	done     bool
	events   chan Event
	width    int
}

func newTUIModel(events chan Event) tuiModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(tuiStyleSpinner),
	)
	return tuiModel{
		spinner: s,
		events:  events,
		width:   80,
	}
}

// Init starts the spinner and kicks off the event-drain loop.
func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForEvent())
}

// waitForEvent returns a Cmd that blocks until an event is available.
func (m tuiModel) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		e, ok := <-m.events
		if !ok {
			return tuiDoneMsg{}
		}
		return tuiEventMsg{event: e}
	}
}

// Update handles incoming messages.
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tuiEventMsg:
		m = m.applyEvent(msg.event)
		// Keep draining events.
		return m, m.waitForEvent()

	case tuiDoneMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) applyEvent(e Event) tuiModel {
	switch e.Type {
	case EventPlayStart:
		m.playName = e.PlayName

	case EventTaskResult:
		// Find existing running task or append a new one.
		found := false
		for i, t := range m.tasks {
			if t.name == e.TaskName && t.running {
				m.tasks[i].status = e.Status
				m.tasks[i].running = false
				found = true
				break
			}
		}
		if !found {
			m.tasks = append(m.tasks, taskView{
				name:    e.TaskName,
				status:  e.Status,
				running: false,
			})
		}
		// Update recap.
		switch e.Status {
		case "ok":
			m.recap.ok++
		case "changed":
			m.recap.changed++
		case "failed":
			m.recap.failed++
		case "skipped":
			m.recap.skipped++
		}

	case EventPlayEnd:
		// Use authoritative counts from the event.
		m.recap.ok = e.OKCount
		m.recap.changed = e.ChangedCount
		m.recap.failed = e.FailedCount
		m.recap.skipped = e.SkippedCount
	}
	return m
}

// View renders the current state.
func (m tuiModel) View() string {
	var b strings.Builder

	// Play header.
	if m.playName != "" {
		title := fmt.Sprintf("PLAY [%s]", m.playName)
		line := fillLine(title, "*", m.width)
		b.WriteString(tuiStyleHeader.Render(line))
		b.WriteString("\n\n")
	}

	// Task list.
	for _, t := range m.tasks {
		b.WriteString(m.renderTask(t))
		b.WriteString("\n")
	}

	// Running tasks that haven't completed yet (spinner shown).
	// (Tasks are added to m.tasks on result; while running we show the spinner
	// for the last task if it has no status yet.)
	spinnerVisible := false
	for _, t := range m.tasks {
		if t.running {
			spinnerVisible = true
			break
		}
	}
	_ = spinnerVisible

	// Live recap footer (always shown while running, final version when done).
	b.WriteString("\n")
	b.WriteString(m.renderRecap())

	return b.String()
}

func (m tuiModel) renderTask(t taskView) string {
	var icon string
	switch t.status {
	case "ok":
		icon = tuiStyleOK.Render("✓")
	case "changed":
		icon = tuiStyleChanged.Render("~")
	case "failed":
		icon = tuiStyleFailed.Render("✗")
	case "skipped":
		icon = tuiStyleSkipped.Render("-")
	default:
		if t.running {
			icon = m.spinner.View()
		} else {
			icon = " "
		}
	}
	return fmt.Sprintf("  %s  TASK [%s]", icon, t.name)
}

func (m tuiModel) renderRecap() string {
	if m.done {
		// Final recap — same format as TextRenderer.
		title := "PLAY RECAP"
		line := fillLine(title, "*", m.width)
		recap := fmt.Sprintf("%-14s : ok=%-4d changed=%-4d failed=%-4d skipped=%-4d",
			"localhost",
			m.recap.ok,
			m.recap.changed,
			m.recap.failed,
			m.recap.skipped,
		)
		return tuiStyleHeader.Render(line) + "\n" + recap + "\n"
	}
	// Live counter.
	ok := tuiStyleOK.Render(fmt.Sprintf("ok=%d", m.recap.ok))
	changed := tuiStyleChanged.Render(fmt.Sprintf("changed=%d", m.recap.changed))
	failed := tuiStyleFailed.Render(fmt.Sprintf("failed=%d", m.recap.failed))
	skipped := tuiStyleSkipped.Render(fmt.Sprintf("skipped=%d", m.recap.skipped))
	return fmt.Sprintf("  %s  %s  %s  %s", ok, changed, failed, skipped)
}

// TUIRenderer implements Renderer using a Bubbletea program.
type TUIRenderer struct {
	program *tea.Program
	events  chan Event
	done    chan struct{}
}

// NewTUIRenderer creates a TUIRenderer that writes to w.
func NewTUIRenderer(w io.Writer) *TUIRenderer {
	events := make(chan Event, 64)
	model := newTUIModel(events)
	prog := tea.NewProgram(
		model,
		tea.WithOutput(w),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
	)
	r := &TUIRenderer{
		program: prog,
		events:  events,
		done:    make(chan struct{}),
	}
	go func() {
		defer close(r.done)
		_, _ = prog.Run()
	}()
	return r
}

// Emit sends an event to the running Bubbletea program.
func (r *TUIRenderer) Emit(event Event) {
	r.events <- event
}

// Close shuts down the Bubbletea program and waits for it to exit.
func (r *TUIRenderer) Close() {
	close(r.events)
	<-r.done
}
