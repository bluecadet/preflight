package output

import (
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type staticKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Toggle key.Binding
	PageUp key.Binding
	PageDn key.Binding
	Help   key.Binding
	Quit   key.Binding
}

func newStaticKeyMap() staticKeyMap {
	return staticKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←/h", "prev tab"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→/l", "next tab"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("enter", "space"),
			key.WithHelp("enter/space", "details"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "b"),
			key.WithHelp("pgup/b", "page up"),
		),
		PageDn: key.NewBinding(
			key.WithKeys("pgdown", "f"),
			key.WithHelp("pgdn/f", "page down"),
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

func (k staticKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Left, k.Right, k.Toggle, k.Help, k.Quit}
}

func (k staticKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Toggle, k.PageUp, k.PageDn},
		{k.Help, k.Quit},
	}
}

type staticTabState struct {
	selected int
	expanded map[int]bool
}

type staticScreenModel struct {
	screen      Screen
	width       int
	height      int
	viewport    viewport.Model
	help        helpKeyMap
	helpModel   help.Model
	keys        staticKeyMap
	activeTab   int
	tabStates   map[int]*staticTabState
	showHelp    bool
	initialized bool
}

func newStaticScreenModel(screen Screen) staticScreenModel {
	keys := newStaticKeyMap()
	h := tuiNewHelp()
	vp := viewport.New(defaultTUIWidth, defaultTUIHeight)
	return staticScreenModel{
		screen:    screen,
		width:     defaultTUIWidth,
		height:    defaultTUIHeight,
		viewport:  vp,
		help:      keys,
		helpModel: h,
		keys:      keys,
		tabStates: make(map[int]*staticTabState),
	}
}

func (m staticScreenModel) Init() tea.Cmd {
	return nil
}

func (m staticScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(40, msg.Width)
		m.height = max(12, msg.Height)
		m.initialized = true
		m.syncViewport()
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			m.syncViewport()
			return m, nil
		}

		if len(m.screen.Tabs) > 0 {
			switch msg.String() {
			case "left", "h":
				m.activeTab--
				m.clampTab()
				m.syncViewport()
				return m, nil
			case "right", "l":
				m.activeTab++
				m.clampTab()
				m.syncViewport()
				return m, nil
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				digit := int(msg.Runes[0] - '1')
				if digit >= 0 && digit < len(m.screen.Tabs) {
					m.activeTab = digit
					m.syncViewport()
					return m, nil
				}
			}
		}

		switch m.currentContent().Kind {
		case ScreenKindList:
			state := m.currentTabState()
			switch msg.String() {
			case "up", "k":
				state.selected--
			case "down", "j":
				state.selected++
			case "enter", " ":
				if len(m.currentContent().Items) > 0 {
					state.expanded[state.selected] = !state.expanded[state.selected]
				}
			}
			m.clampSelection()
			m.syncViewport()
			return m, nil

		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m staticScreenModel) View() string {
	if !m.initialized {
		m.syncViewport()
	}
	header := m.renderHeader()
	tabs := m.renderTabs()
	footer := m.renderFooter()
	bodyHeight := viewportBodyHeight(m.height, header, tabs, footer)
	m.viewport.Width = max(20, m.width)
	m.viewport.Height = bodyHeight
	m.syncViewport()

	parts := []string{}
	if header != "" {
		parts = append(parts, header)
	}
	if tabs != "" {
		parts = append(parts, tabs)
	}
	parts = append(parts, m.viewport.View())
	if footer != "" {
		parts = append(parts, footer)
	}
	return strings.Join(parts, "\n")
}

func (m *staticScreenModel) clampTab() {
	if len(m.screen.Tabs) == 0 {
		m.activeTab = 0
		return
	}
	if m.activeTab < 0 {
		m.activeTab = 0
	}
	if m.activeTab >= len(m.screen.Tabs) {
		m.activeTab = len(m.screen.Tabs) - 1
	}
}

func (m *staticScreenModel) currentContent() ScreenContent {
	m.clampTab()
	if len(m.screen.Tabs) > 0 {
		return m.screen.Tabs[m.activeTab].Content
	}
	return m.screen.Content
}

func (m *staticScreenModel) currentTabState() *staticTabState {
	state, ok := m.tabStates[m.activeTab]
	if ok {
		return state
	}
	state = &staticTabState{expanded: make(map[int]bool)}
	for idx, item := range m.currentContent().Items {
		if item.AutoExpand {
			state.expanded[idx] = true
		}
	}
	m.tabStates[m.activeTab] = state
	return state
}

func (m *staticScreenModel) clampSelection() {
	content := m.currentContent()
	state := m.currentTabState()
	if len(content.Items) == 0 {
		state.selected = 0
		return
	}
	if state.selected < 0 {
		state.selected = 0
	}
	if state.selected >= len(content.Items) {
		state.selected = len(content.Items) - 1
	}
}

func (m *staticScreenModel) syncViewport() {
	content := m.currentContent()
	switch content.Kind {
	case ScreenKindList:
		body, starts, ends := m.renderList(content)
		m.viewport.SetContent(body)
		state := m.currentTabState()
		if len(starts) > 0 {
			selected := state.selected
			selected = clamp(0, selected, len(starts)-1)
			if starts[selected] < m.viewport.YOffset {
				m.viewport.YOffset = starts[selected]
			}
			bottom := m.viewport.YOffset + max(1, m.viewport.Height) - 1
			if ends[selected] > bottom {
				m.viewport.YOffset = max(0, ends[selected]-m.viewport.Height+1)
			}
		}
	default:
		doc := content.Document
		if doc == "" {
			doc = tuiSubtleStyle.Render(content.Empty)
		}
		m.viewport.SetContent(doc)
	}
}

func (m staticScreenModel) renderHeader() string {
	lines := make([]string, 0, 2)
	if m.screen.Subject != "" {
		lines = append(lines, tuiSubtleStyle.Render(truncateText(m.screen.Subject, m.width)))
	}
	stats := append([]ScreenStat{}, m.screen.Summary...)
	if len(m.screen.Tabs) > 0 {
		stats = append(stats, m.screen.Tabs[m.activeTab].Content.Summary...)
	} else {
		stats = append(stats, m.screen.Content.Summary...)
	}
	if rendered := renderStats(stats, m.width); rendered != "" {
		lines = append(lines, rendered)
	}
	return strings.Join(lines, "\n")
}

func (m staticScreenModel) renderTabs() string {
	if len(m.screen.Tabs) <= 1 {
		return ""
	}
	tabs := make([]tuiTab, 0, len(m.screen.Tabs))
	for _, tab := range m.screen.Tabs {
		tabs = append(tabs, tuiTab{
			Label:  tab.Label,
			Status: tab.Status,
			Meta:   tab.Meta,
		})
	}
	return renderTabs(tabs, m.activeTab, m.width)
}

func (m staticScreenModel) renderList(content ScreenContent) (string, []int, []int) {
	if len(content.Items) == 0 {
		empty := content.Empty
		if empty == "" {
			empty = "Nothing to show."
		}
		return tuiSubtleStyle.Render(empty), nil, nil
	}
	state := m.currentTabState()
	width := max(24, m.width-2)
	blocks := make([]string, 0, len(content.Items))
	starts := make([]int, 0, len(content.Items))
	ends := make([]int, 0, len(content.Items))
	currentLine := 0
	for idx, item := range content.Items {
		starts = append(starts, currentLine)
		block := m.renderItem(item, idx == state.selected, state.expanded[idx], width)
		blocks = append(blocks, block)
		currentLine += lipgloss.Height(block)
		ends = append(ends, currentLine-1)
		if idx < len(content.Items)-1 {
			currentLine++
		}
	}
	return strings.Join(blocks, "\n\n"), starts, ends
}

func (m staticScreenModel) renderItem(item ScreenItem, selected, expanded bool, width int) string {
	title := truncateText(item.Title, max(20, width-8))
	summaryParts := []string{statusGlyph(item.Status), title}
	if item.Subtitle != "" {
		summaryParts = append(summaryParts, tuiSubtleStyle.Render("("+item.Subtitle+")"))
	}
	if item.Summary != "" {
		summaryParts = append(summaryParts, tuiSubtleStyle.Render(truncateText(item.Summary, max(16, width/2))))
	}
	lines := []string{strings.Join(summaryParts, "  ")}
	if meta := joinMeta(item.Meta...); meta != "" {
		lines = append(lines, tuiSubtleStyle.Render(truncateText(meta, width)))
	}
	if len(item.Preview) > 0 {
		lines = append(lines, renderScreenLines(item.Preview, width-2))
	}
	if expanded {
		if item.DetailTitle != "" {
			lines = append(lines, tuiSectionStyle.Render(item.DetailTitle))
		}
		if len(item.Detail) > 0 {
			lines = append(lines, renderScreenLines(item.Detail, width-2))
		}
	}
	block := strings.Join(lines, "\n")
	if selected {
		return tuiSelectedCardStyle.Width(width).Render(block)
	}
	return tuiCardStyle.Width(width).Render(block)
}

func (m staticScreenModel) renderFooter() string {
	helpModel := m.helpModel
	helpModel.ShowAll = m.showHelp
	if m.showHelp {
		return helpModel.FullHelpView(m.help.FullHelp())
	}
	location := ""
	content := m.currentContent()
	if content.Kind == ScreenKindList && len(content.Items) > 0 {
		state := m.currentTabState()
		location = tuiSubtleStyle.Render(
			truncateText("item "+strconv.Itoa(state.selected+1)+"/"+strconv.Itoa(len(content.Items)), m.width/3),
		)
	}
	helpText := helpModel.ShortHelpView(m.help.ShortHelp())
	return responsiveFooter(m.width, location, helpText)
}

func RunScreenTUI(w io.Writer, options Options, screen Screen) error {
	model := newStaticScreenModel(screen)
	programOptions := []tea.ProgramOption{
		tea.WithOutput(w),
		tea.WithoutSignalHandler(),
	}
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
		programOptions = append(programOptions, tea.WithAltScreen())
	}
	prog := tea.NewProgram(model, programOptions...)
	_, err := prog.Run()
	if err != nil {
		return err
	}
	if options.Input != nil {
		return RenderScreenText(w, screen)
	}
	return nil
}
