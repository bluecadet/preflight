package output

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m tuiModel) View() string {
	header := m.renderHeader()
	tabs := m.renderTabs()
	footer := m.renderFooter()

	m.viewport.Width = max(20, m.width)
	m.viewport.Height = viewportBodyHeight(m.height, header, tabs, footer)
	m.syncViewport()

	parts := make([]string, 0, 4)
	if header != "" {
		parts = append(parts, header)
	}
	if tabs != "" {
		parts = append(parts, tabs)
	}
	parts = append(parts, strings.TrimRight(m.viewport.View(), "\n"))
	if footer != "" {
		parts = append(parts, footer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
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
	if host.done {
		recap := renderStats([]ScreenStat{
			{Label: "ok", Value: fmt.Sprintf("%d", host.recap.ok), Tone: "ok"},
			{Label: "changed", Value: fmt.Sprintf("%d", host.recap.changed), Tone: "changed"},
			{Label: "failed", Value: fmt.Sprintf("%d", host.recap.failed), Tone: "failed"},
			{Label: "skipped", Value: fmt.Sprintf("%d", host.recap.skipped), Tone: "skipped"},
		}, width)
		if recap != "" {
			blocks = append(blocks, tuiSectionStyle.Render("Recap")+"\n"+recap)
		}
	}
	return joinVerticalBlocks(blocks), starts, ends
}

func (m tuiModel) renderTaskCard(task *taskView, selected bool, width int) string {
	summaryParts := []string{m.taskStatusGlyph(task), truncateText(task.name, max(20, width-8))}
	if task.module != "" {
		summaryParts = append(summaryParts, tuiSubtleStyle.Render("("+task.module+")"))
	}
	if task.message != "" && !task.expanded {
		summaryParts = append(summaryParts, tuiSubtleStyle.Render(truncateText(task.message, max(14, width/3))))
	}
	lines := []string{strings.Join(summaryParts, "  ")}
	if task.running {
		lines = append(lines, tuiSubtleStyle.Render("running"))
	}

	if !task.expanded && (task.running || task.status == "failed") && len(task.logs) > 0 {
		previewLines := make([]ScreenLine, 0, min(3, len(task.logs)))
		for _, line := range m.previewLogs(task) {
			previewLines = append(previewLines, ScreenLine{
				Prefix: logPrefix(line.stream),
				Text:   line.line,
				Tone:   lineTone(line.stream),
			})
		}
		lines = append(lines, renderScreenLines(previewLines, width-2))
	}

	if task.expanded {
		detail := m.expandedTaskLines(task)
		if len(detail) > 0 {
			lines = append(lines, tuiSectionStyle.Render(m.expandedTaskTitle(task)))
			lines = append(lines, renderScreenLines(detail, width-2))
		}
	}

	block := strings.Join(lines, "\n")
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
	if host != nil {
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
	return lipgloss.JoinHorizontal(lipgloss.Left, withHorizontalSpacing(parts, "  ")...)
}

func (m tuiModel) renderPhase(phase phaseView) string {
	label := phaseLabel(phase.name)
	switch {
	case phase.running:
		return m.spinner.View() + " " + label
	case phase.status != "":
		return statusGlyph(phase.status) + " " + label
	default:
		return "• " + label
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
