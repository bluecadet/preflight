package output

import "github.com/charmbracelet/bubbles/key"

type runKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Toggle   key.Binding
	Collapse key.Binding
	Failed   key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func newRunKeyMap() runKeyMap {
	return runKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "task up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "task down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←/h", "prev host"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→/l", "next host"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("enter", "space"),
			key.WithHelp("enter/space", "details"),
		),
		Collapse: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "collapse done"),
		),
		Failed: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "failed only"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc"),
			key.WithHelp("q/esc", "quit"),
		),
	}
}

func (k runKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Left, k.Right, k.Toggle, k.Collapse, k.Failed, k.Help, k.Quit}
}

func (k runKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Toggle, k.Collapse, k.Failed},
		{k.Help, k.Quit},
	}
}
