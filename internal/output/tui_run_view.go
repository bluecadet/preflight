package output

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m tuiModel) View() string {
	topChrome := m.renderTopChrome()
	footer := m.renderFooter()

	m.viewport.Width = max(20, m.width)
	m.viewport.Height = viewportBodyHeight(m.height, topChrome, footer)
	m.syncViewport()

	if m.modalOpen {
		return m.renderTaskModal()
	}

	parts := make([]string, 0, 4)
	if topChrome != "" {
		parts = append(parts, topChrome, "")
	}
	parts = append(parts, strings.TrimRight(m.viewport.View(), "\n"))
	if footer != "" {
		parts = append(parts, "", footer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m tuiModel) renderTopChrome() string {
	header := m.renderHeader()
	tabs := m.renderTabs()
	switch {
	case header == "" && tabs == "":
		return ""
	case header == "":
		return tabs
	case tabs == "":
		return header
	default:
		return lipgloss.JoinVertical(lipgloss.Left, header, tabs)
	}
}

func (m tuiModel) renderHeader() string {
	lines := make([]string, 0, 2)
	if subject := m.subjectLine(); subject != "" {
		lines = append(lines, tuiSubtleStyle.Render(truncateText(subject, m.width)))
	}
	if phases := m.phaseLine(); phases != "" {
		lines = append(lines, phases)
	}
	if len(lines) == 0 {
		return ""
	}
	return tuiChromeStyle.Width(m.width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m tuiModel) renderTabs() string {
	if len(m.hostOrder) <= 1 {
		return ""
	}
	tabs := make([]tuiTab, 0, len(m.hostOrder))
	for _, name := range m.hostOrder {
		host := m.hosts[name]
		tabs = append(tabs, tuiTab{
			Label:  name,
			Status: m.hostStatus(host),
			Meta:   fmt.Sprintf("%d/%d", host.completedCount(), max(host.totalTasks, len(host.taskOrder))),
		})
	}
	return renderTabs(tabs, m.selectedHost, m.width, &m.tabPager)
}

func (m tuiModel) renderTaskStream() (string, []int, []int) {
	host := m.currentHost()
	if host == nil {
		return tuiSubtleStyle.Render("Waiting for targets..."), nil, nil
	}
	ids := m.visibleTasks(host)
	if len(ids) == 0 {
		return tuiSubtleStyle.Render("No tasks match the current filters."), nil, nil
	}

	width := max(24, m.width-2)
	blocks := make([]string, 0, len(ids))
	starts := make([]int, 0, len(ids))
	ends := make([]int, 0, len(ids))
	currentLine := 0
	for idx, id := range ids {
		starts = append(starts, currentLine)
		block := m.renderTaskCard(host.tasks[id], idx == host.selectedTask, width)
		blocks = append(blocks, block)
		currentLine += lipgloss.Height(block)
		ends = append(ends, currentLine-1)
		if idx < len(ids)-1 {
			currentLine++
		}
	}
	if len(blocks) == 0 {
		return "", starts, ends
	}
	streamParts := make([]string, 0, len(blocks)*2)
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		if len(streamParts) > 0 {
			streamParts = append(streamParts, "")
		}
		streamParts = append(streamParts, block)
	}
	return lipgloss.JoinVertical(lipgloss.Left, streamParts...), starts, ends
}

func (m tuiModel) renderTaskCard(task *taskView, selected bool, width int) string {
	glyph := m.taskStatusGlyph(task)
	glyphWidth := max(1, lipgloss.Width(glyph))
	gapWidth := 2
	contentWidth := max(16, width-glyphWidth-gapWidth)

	summaryParts := []string{truncateText(task.name, max(20, contentWidth-8))}
	if task.module != "" {
		summaryParts = append(summaryParts, tuiSubtleStyle.Render("("+task.module+")"))
	}
	if task.message != "" {
		// Keep the summary line concise; the full detail lives in the modal.
		summaryParts = append(summaryParts, tuiSubtleStyle.Render(truncateText(task.message, max(14, contentWidth/3))))
	}
	summarySegments := make([]string, 0, len(summaryParts)*2)
	for _, part := range summaryParts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if len(summarySegments) > 0 {
			summarySegments = append(summarySegments, "  ")
		}
		summarySegments = append(summarySegments, part)
	}

	lines := []string{lipgloss.JoinHorizontal(lipgloss.Left, summarySegments...)}

	if (task.running || task.status == "failed") && len(task.logs) > 0 {
		previewLines := make([]ScreenLine, 0, min(3, len(task.logs)))
		for _, line := range m.previewLogs(task) {
			previewLines = append(previewLines, ScreenLine{
				Prefix: logPrefix(line.stream),
				Text:   line.line,
				Tone:   lineTone(line.stream),
			})
		}
		lines = append(lines, renderScreenLines(previewLines, contentWidth))
	}

	contentLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		contentLines = append(contentLines, line)
	}
	content := lipgloss.NewStyle().Width(contentWidth).Render(lipgloss.JoinVertical(lipgloss.Left, contentLines...))
	glyphColumn := lipgloss.NewStyle().Width(glyphWidth).Align(lipgloss.Left, lipgloss.Top).Render(glyph)
	gap := lipgloss.NewStyle().Width(gapWidth).Render("")
	block := lipgloss.JoinHorizontal(lipgloss.Top, glyphColumn, gap, content)
	if selected {
		return tuiSelectedCardStyle.Width(width).Render(block)
	}
	if task.status == "failed" {
		return tuiMutedCardStyle.Width(width).Render(block)
	}
	return tuiCardStyle.Width(width).Render(block)
}

func (m tuiModel) renderFooter() string {
	location := ""
	host := m.currentHost()
	if host != nil && host.done {
		recap := renderStats([]ScreenStat{
			{Label: "ok", Value: fmt.Sprintf("%d", host.recap.ok), Tone: "ok"},
			{Label: "changed", Value: fmt.Sprintf("%d", host.recap.changed), Tone: "changed"},
			{Label: "failed", Value: fmt.Sprintf("%d", host.recap.failed), Tone: "failed"},
			{Label: "skipped", Value: fmt.Sprintf("%d", host.recap.skipped), Tone: "skipped"},
		}, max(20, m.width/2))
		location = recap
		if len(m.hostOrder) > 1 && recap != "" {
			location = lipgloss.JoinHorizontal(lipgloss.Left, tuiSubtleStyle.Render(host.name), "  ", recap)
		}
	} else if host != nil {
		location = tuiSubtleStyle.Render(host.name)
		if len(m.visibleTasks(host)) > 0 {
			location = tuiSubtleStyle.Render(fmt.Sprintf("%s  task %d/%d", host.name, host.selectedTask+1, len(m.visibleTasks(host))))
		}
	}
	if len(m.errors) > 0 {
		location = tuiStatusFailStyle.Render(truncateText(m.errors[len(m.errors)-1], max(16, m.width/2)))
	}
	m.help.ShowAll = m.showHelp
	if m.showHelp {
		return m.help.FullHelpView(m.keys.FullHelp())
	}
	helpText := m.help.ShortHelpView(m.keys.ShortHelp())
	if !m.done {
		helpText = spaceBetween(max(10, m.width/2), tuiSubtleStyle.Render("Ctrl+C cancel"), helpText)
	}
	return responsiveFooter(m.width, location, helpText)
}

func (m tuiModel) renderTaskModal() string {
	task := m.currentTask()
	if task == nil {
		return ""
	}

	modalWidth := max(40, min(m.width-6, 100))
	modalHeight := max(10, min(m.height-4, 28))
	titleParts := []string{m.taskStatusGlyph(task), truncateText(task.name, max(20, modalWidth-12))}
	if task.module != "" {
		titleParts = append(titleParts, tuiSubtleStyle.Render("("+task.module+")"))
	}
	titleSegments := make([]string, 0, len(titleParts)*2)
	for _, part := range titleParts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if len(titleSegments) > 0 {
			titleSegments = append(titleSegments, "  ")
		}
		titleSegments = append(titleSegments, part)
	}
	title := lipgloss.JoinHorizontal(lipgloss.Left, titleSegments...)
	meta := ""
	if host := m.currentHost(); host != nil {
		meta = tuiSubtleStyle.Render(host.name)
	}
	footer := tuiSubtleStyle.Render("esc close  ↑/↓ scroll  pgup/pgdn page")

	parts := []string{title}
	if meta != "" {
		parts = append(parts, meta)
	}
	if section := m.taskDetailTitle(task); section != "" {
		parts = append(parts, tuiSectionStyle.Render(section))
	}
	parts = append(parts, strings.TrimRight(m.detailViewport.View(), "\n"), footer)
	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	modal := lipgloss.NewStyle().
		Width(modalWidth).
		Height(modalHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuiColorSelected).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m tuiModel) subjectLine() string {
	hostCount := len(m.hostOrder)
	hostLabel := fmt.Sprintf("%d target", hostCount)
	if hostCount != 1 {
		hostLabel += "s"
	}
	playName := ""
	if host := m.currentHost(); host != nil && host.playName != "" {
		playName = "play: " + host.playName
	}
	return joinMeta(playName, hostLabel)
}

func (m tuiModel) phaseLine() string {
	parts := make([]string, 0, len(m.globalPhases)+4)
	for _, phase := range m.globalPhases {
		parts = append(parts, m.renderPhase(phase))
	}
	if host := m.currentHost(); host != nil {
		for _, phase := range host.phases {
			parts = append(parts, m.renderPhase(phase))
		}
	}
	segments := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if len(segments) > 0 {
			segments = append(segments, "  ")
		}
		segments = append(segments, part)
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, segments...)
}

func (m tuiModel) renderPhase(phase phaseView) string {
	label := phaseLabel(phase.name)
	switch {
	case phase.running:
		return lipgloss.JoinHorizontal(lipgloss.Left, m.spinner.View(), " ", label)
	case phase.status != "":
		return lipgloss.JoinHorizontal(lipgloss.Left, statusGlyph(phase.status), " ", label)
	default:
		return lipgloss.JoinHorizontal(lipgloss.Left, "•", " ", label)
	}
}

func (m tuiModel) hostStatus(host *hostView) string {
	for _, phase := range host.phases {
		if phase.running {
			return "running"
		}
	}
	for _, id := range host.taskOrder {
		if host.tasks[id].running {
			return "running"
		}
	}
	if host.recap.failed > 0 {
		return "failed"
	}
	if host.done {
		return "complete"
	}
	return "pending"
}

func (m tuiModel) taskStatusGlyph(task *taskView) string {
	if task.running {
		return m.spinner.View()
	}
	return statusGlyph(task.status)
}

func phaseLabel(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}
