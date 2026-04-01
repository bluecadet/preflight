//go:build devtools

package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/output"
)

var (
	uiCmd = &cobra.Command{
		Use:    "ui",
		Short:  "Development-only TUI preview tools",
		Hidden: true,
	}

	uiPreviewCmd = &cobra.Command{
		Use:   "preview [scenario]",
		Short: "Preview TUI scenarios",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				return output.RunPreviewScenario(os.Stdout, os.Stdin, args[0])
			}
			return runPreviewBrowser()
		},
	}
)

func init() {
	uiCmd.AddCommand(uiPreviewCmd)
	rootCmd.AddCommand(uiCmd)
}

type previewBrowserItem struct {
	scenario output.PreviewScenario
}

func (i previewBrowserItem) FilterValue() string {
	return i.scenario.Name + " " + i.scenario.Title + " " + i.scenario.Description
}

func (i previewBrowserItem) Title() string {
	return i.scenario.Title
}

func (i previewBrowserItem) Description() string {
	return i.scenario.Name + "  " + i.scenario.Description
}

type previewBrowserKeyMap struct {
	Open key.Binding
	Help key.Binding
	Quit key.Binding
}

func newPreviewBrowserKeyMap() previewBrowserKeyMap {
	return previewBrowserKeyMap{
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open"),
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

func (k previewBrowserKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.Help, k.Quit}
}

func (k previewBrowserKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Open, k.Help, k.Quit}}
}

type previewSelectedMsg struct {
	index int
}

type previewBrowserModel struct {
	list     list.Model
	help     help.Model
	keys     previewBrowserKeyMap
	showHelp bool
	open     bool
}

func newPreviewBrowserModel(startIndex int) previewBrowserModel {
	scenarios := output.PreviewScenarios()
	items := make([]list.Item, 0, len(scenarios))
	for _, scenario := range scenarios {
		items = append(items, previewBrowserItem{scenario: scenario})
	}
	delegate := list.NewDefaultDelegate()
	delegate.Styles = list.NewDefaultItemStyles()
	delegate.Styles.NormalTitle = lipgloss.NewStyle().PaddingLeft(1)
	delegate.Styles.NormalDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).PaddingLeft(1)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("229")).
		PaddingLeft(1)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("241"))

	l := list.New(items, delegate, 0, 0)
	l.Title = "TUI Preview"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true)
	if startIndex > 0 && startIndex < len(items) {
		l.Select(startIndex)
	}

	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	h.Styles.FullKey = h.Styles.ShortKey
	h.Styles.FullDesc = h.Styles.ShortDesc
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	h.Styles.FullSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	return previewBrowserModel{
		list: l,
		help: h,
		keys: newPreviewBrowserKeyMap(),
	}
}

func (m previewBrowserModel) Init() tea.Cmd {
	return nil
}

func (m previewBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := 2
		footerHeight := 1
		m.list.SetSize(msg.Width, max(4, msg.Height-headerHeight-footerHeight))
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "esc":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "enter":
			m.open = true
			return m, func() tea.Msg {
				return previewSelectedMsg{index: m.list.Index()}
			}
		}
	case previewSelectedMsg:
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m previewBrowserModel) View() string {
	header := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render("TUI Preview"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Select a scenario to inspect."),
	)
	footer := ""
	m.help.ShowAll = m.showHelp
	if m.showHelp {
		footer = m.help.FullHelpView(m.keys.FullHelp())
	} else {
		footer = m.help.ShortHelpView(m.keys.ShortHelp())
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, stringsTrimRight(m.list.View()), footer)
}

func runPreviewBrowser() error {
	selected := 0
	for {
		model := newPreviewBrowserModel(selected)
		result, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithoutSignalHandler()).Run()
		if err != nil {
			return err
		}
		browser := result.(previewBrowserModel)
		selected = browser.list.Index()
		if !browser.open {
			return nil
		}
		item, ok := browser.list.SelectedItem().(previewBrowserItem)
		if !ok {
			return nil
		}
		if err := output.RunPreviewScenario(os.Stdout, os.Stdin, item.scenario.Name); err != nil {
			return err
		}
	}
}

func stringsTrimRight(value string) string {
	for len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
	}
	return value
}

func init() {
	uiPreviewCmd.Example = fmt.Sprintf("  %s ui preview\n  %s ui preview run-multi-host", rootCmd.Use, rootCmd.Use)
}
