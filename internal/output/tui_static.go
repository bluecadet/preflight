package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
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

type staticListItem struct {
	index int
	item  ScreenItem
}

func (i staticListItem) FilterValue() string {
	parts := []string{i.item.Title, i.item.Subtitle, i.item.Summary}
	parts = append(parts, i.item.Meta...)
	for _, line := range i.item.Preview {
		parts = append(parts, line.Text)
	}
	for _, line := range i.item.Detail {
		parts = append(parts, line.Text)
	}
	return strings.Join(parts, " ")
}

type staticListDelegate struct{}

func newStaticListDelegate() staticListDelegate {
	return staticListDelegate{}
}

func (d staticListDelegate) Height() int  { return 2 }
func (d staticListDelegate) Spacing() int { return 1 }
func (d staticListDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

func (d staticListDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	screenItem, ok := item.(staticListItem)
	if !ok {
		return
	}

	width := max(20, m.Width()-2)
	selected := index == m.Index()
	title := truncateText(screenItem.item.Title, max(16, width-8))

	summary := []string{statusGlyph(screenItem.item.Status), title}
	if screenItem.item.Subtitle != "" {
		summary = append(summary, tuiSubtleStyle.Render("("+screenItem.item.Subtitle+")"))
	}

	secondLine := ""
	switch {
	case screenItem.item.Summary != "":
		secondLine = truncateText(screenItem.item.Summary, width)
	case len(screenItem.item.Meta) > 0:
		secondLine = truncateText(joinMeta(screenItem.item.Meta...), width)
	case len(screenItem.item.Preview) > 0:
		secondLine = truncateText(screenItem.item.Preview[0].Text, width)
	}

	lines := []string{strings.Join(summary, "  ")}
	if secondLine == "" {
		lines = append(lines, "")
	} else {
		lines = append(lines, tuiSubtleStyle.Render(secondLine))
	}

	block := strings.Join(lines, "\n")
	style := tuiCardStyle.Width(width)
	if selected {
		style = tuiSelectedCardStyle.Width(width)
	} else if screenItem.item.Status == "failed" {
		style = tuiMutedCardStyle.Width(width)
	}

	_, _ = io.WriteString(w, style.Render(block))
}

type staticTabState struct {
	list      list.Model
	detail    viewport.Model
	document  viewport.Model
	expanded  bool
	hasList   bool
	hasDoc    bool
	ready     bool
	lastCount int
}

type staticScreenModel struct {
	screen      Screen
	width       int
	height      int
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
	return staticScreenModel{
		screen:    screen,
		width:     defaultTUIWidth,
		height:    defaultTUIHeight,
		help:      keys,
		helpModel: tuiNewHelp(),
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
		m.syncActiveState()
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
			m.syncActiveState()
			return m, nil
		}

		if len(m.screen.Tabs) > 0 {
			switch msg.String() {
			case "left", "h":
				m.activeTab--
				m.clampTab()
				m.syncActiveState()
				return m, nil
			case "right", "l":
				m.activeTab++
				m.clampTab()
				m.syncActiveState()
				return m, nil
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				digit := int(msg.Runes[0] - '1')
				if digit >= 0 && digit < len(m.screen.Tabs) {
					m.activeTab = digit
					m.syncActiveState()
					return m, nil
				}
			}
		}

		state := m.currentTabState()
		content := m.currentContent()
		switch content.Kind {
		case ScreenKindList:
			if msg.String() == "enter" || msg.String() == " " {
				state.expanded = !state.expanded
				m.syncActiveState()
				return m, nil
			}
			var cmd tea.Cmd
			state.list, cmd = state.list.Update(msg)
			m.syncActiveState()
			return m, cmd
		default:
			var cmd tea.Cmd
			state.document, cmd = state.document.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m staticScreenModel) View() string {
	if !m.initialized {
		m.syncActiveState()
	}

	header := m.renderHeader()
	tabs := m.renderTabs()
	body := m.renderBody()
	footer := m.renderFooter()

	parts := make([]string, 0, 4)
	if header != "" {
		parts = append(parts, header)
	}
	if tabs != "" {
		parts = append(parts, tabs)
	}
	parts = append(parts, strings.TrimRight(body, "\n"))
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
	m.activeTab = clamp(0, m.activeTab, len(m.screen.Tabs)-1)
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

	content := m.currentContent()
	state = &staticTabState{}
	switch content.Kind {
	case ScreenKindList:
		state.list = newStaticListModel(content, max(20, m.width), max(4, m.height))
		state.detail = viewport.New(max(20, m.width), max(4, m.height/3))
		state.hasList = true
		for idx, item := range content.Items {
			if !item.AutoExpand {
				continue
			}
			state.list.Select(idx)
			state.expanded = true
			break
		}
	default:
		state.document = viewport.New(max(20, m.width), max(4, m.height))
		state.hasDoc = true
	}
	state.ready = true
	m.tabStates[m.activeTab] = state
	return state
}

func newStaticListModel(content ScreenContent, width, height int) list.Model {
	items := make([]list.Item, 0, len(content.Items))
	for idx, item := range content.Items {
		items = append(items, staticListItem{index: idx, item: item})
	}

	delegate := newStaticListDelegate()
	model := list.New(items, delegate, width, height)
	model.DisableQuitKeybindings()
	model.SetFilteringEnabled(false)
	model.SetShowTitle(false)
	model.SetShowFilter(false)
	model.SetShowStatusBar(false)
	model.SetShowPagination(false)
	model.SetShowHelp(false)
	model.Styles.NoItems = tuiSubtleStyle
	model.Styles.PaginationStyle = tuiSubtleStyle
	return model
}

func (m *staticScreenModel) syncActiveState() {
	state := m.currentTabState()
	content := m.currentContent()
	header := m.renderHeader()
	tabs := m.renderTabs()
	footer := m.renderFooter()
	bodyHeight := viewportBodyHeight(m.height, header, tabs, footer)
	bodyWidth := max(20, m.width)

	switch content.Kind {
	case ScreenKindList:
		if state.lastCount != len(content.Items) {
			items := make([]list.Item, 0, len(content.Items))
			for idx, item := range content.Items {
				items = append(items, staticListItem{index: idx, item: item})
			}
			_ = state.list.SetItems(items)
			state.lastCount = len(content.Items)
		}

		detail := m.selectedDetail(content, state)
		detailHeight := 0
		if state.expanded && detail != "" && bodyHeight >= 9 {
			detailLines := lipgloss.Height(detail)
			detailHeight = min(max(4, min(detailLines, bodyHeight/2)), bodyHeight-5)
		}

		listHeight := bodyHeight
		if detailHeight > 0 {
			listHeight = max(4, bodyHeight-detailHeight-1)
		}
		state.list.SetSize(bodyWidth, listHeight)
		state.detail.Width = bodyWidth
		state.detail.Height = detailHeight
		state.detail.SetContent(detail)

	default:
		doc := content.Document
		if doc == "" {
			doc = tuiSubtleStyle.Render(content.Empty)
		}
		state.document.Width = bodyWidth
		state.document.Height = bodyHeight
		state.document.SetContent(doc)
	}
}

func (m staticScreenModel) renderBody() string {
	state := m.currentTabState()
	content := m.currentContent()
	bodyHeight := viewportBodyHeight(m.height, m.renderHeader(), m.renderTabs(), m.renderFooter())
	switch content.Kind {
	case ScreenKindList:
		body := clipRenderedHeight(strings.TrimRight(state.list.View(), "\n"), state.list.Height())
		if !state.expanded {
			return clipRenderedHeight(body, bodyHeight)
		}
		detail := clipRenderedHeight(strings.TrimRight(state.detail.View(), "\n"), state.detail.Height)
		if detail == "" {
			return clipRenderedHeight(body, bodyHeight)
		}
		return clipRenderedHeight(body+"\n"+detail, bodyHeight)
	default:
		return clipRenderedHeight(strings.TrimRight(state.document.View(), "\n"), bodyHeight)
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
			truncateText(fmt.Sprintf("item %d/%d", state.list.Index()+1, len(content.Items)), m.width/3),
		)
	}

	return responsiveFooter(m.width, location, helpModel.ShortHelpView(m.help.ShortHelp()))
}

func (m staticScreenModel) selectedDetail(content ScreenContent, state *staticTabState) string {
	if !state.expanded || len(content.Items) == 0 {
		return ""
	}
	selected, ok := state.list.SelectedItem().(staticListItem)
	if !ok {
		return ""
	}
	item := selected.item
	width := max(20, m.width-2)

	title := []string{statusGlyph(item.Status), truncateText(item.Title, max(16, width-8))}
	if item.Subtitle != "" {
		title = append(title, tuiSubtleStyle.Render("("+item.Subtitle+")"))
	}

	lines := []string{strings.Join(title, "  ")}
	if item.Summary != "" {
		lines = append(lines, tuiSubtleStyle.Render(truncateText(item.Summary, width)))
	}
	if meta := joinMeta(item.Meta...); meta != "" {
		lines = append(lines, tuiSubtleStyle.Render(truncateText(meta, width)))
	}
	if len(item.Preview) > 0 {
		lines = append(lines, renderScreenLines(item.Preview, width-2))
	}
	if len(item.Detail) > 0 {
		if item.DetailTitle != "" {
			lines = append(lines, tuiSectionStyle.Render(item.DetailTitle))
		}
		lines = append(lines, renderScreenLines(item.Detail, width-2))
	}

	style := tuiSelectedCardStyle.Width(max(20, m.width-2))
	return style.Render(strings.Join(lines, "\n"))
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

func clipRenderedHeight(text string, height int) string {
	if height <= 0 || text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= height {
		return text
	}
	return strings.Join(lines[:height], "\n")
}
