package output

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/lipgloss"
)

const (
	defaultTUIWidth  = 100
	defaultTUIHeight = 28

	maxTaskLogLines = 200
	maxTaskLogBytes = 32 * 1024
)

var (
	tuiColorOK       = lipgloss.Color("42")
	tuiColorChanged  = lipgloss.Color("214")
	tuiColorFailed   = lipgloss.Color("196")
	tuiColorSkipped  = lipgloss.Color("244")
	tuiColorInfo     = lipgloss.Color("81")
	tuiColorDim      = lipgloss.Color("241")
	tuiColorBorder   = lipgloss.Color("63")
	tuiColorSelected = lipgloss.Color("229")

	tuiSubtleStyle       = lipgloss.NewStyle().Foreground(tuiColorDim)
	tuiHelpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	tuiSpinnerStyle      = lipgloss.NewStyle().Foreground(tuiColorInfo)
	tuiSectionStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("187"))
	tuiChromeStyle       = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	tuiFooterStyle       = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	tuiStatusOKStyle     = lipgloss.NewStyle().Foreground(tuiColorOK).Bold(true)
	tuiStatusChgStyle    = lipgloss.NewStyle().Foreground(tuiColorChanged).Bold(true)
	tuiStatusFailStyle   = lipgloss.NewStyle().Foreground(tuiColorFailed).Bold(true)
	tuiStatusSkipStyle   = lipgloss.NewStyle().Foreground(tuiColorSkipped).Bold(true)
	tuiCardStyle         = lipgloss.NewStyle().PaddingLeft(1)
	tuiSelectedCardStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(tuiColorSelected).
				PaddingLeft(1)
	tuiMutedCardStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(tuiColorFailed).
				Foreground(lipgloss.Color("250")).
				PaddingLeft(1)
	tuiTabStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tuiColorBorder).
			Padding(0, 1)
	tuiActiveTabStyle = tuiTabStyle.
				BorderForeground(tuiColorSelected).
				Bold(true)
	tuiPagerActiveStyle   = lipgloss.NewStyle().Foreground(tuiColorSelected)
	tuiPagerInactiveStyle = tuiSubtleStyle
)

type tuiTab struct {
	Label  string
	Status string
	Meta   string
}

type ScreenStat struct {
	Label string
	Value string
	Tone  string
}

type ScreenLine struct {
	Prefix string
	Text   string
	Tone   string
}

type ScreenItem struct {
	Title       string
	Status      string
	Subtitle    string
	Summary     string
	Meta        []string
	Preview     []ScreenLine
	DetailTitle string
	Detail      []ScreenLine
	AutoExpand  bool
}

type ScreenKind string

const (
	ScreenKindList     ScreenKind = "list"
	ScreenKindDocument ScreenKind = "document"
)

type ScreenContent struct {
	Kind     ScreenKind
	Summary  []ScreenStat
	Items    []ScreenItem
	Document string
	Empty    string
}

type ScreenTab struct {
	Label   string
	Status  string
	Meta    string
	Content ScreenContent
}

type Screen struct {
	Command string
	Subject string
	Status  string
	Summary []ScreenStat
	Tabs    []ScreenTab
	Content ScreenContent
}

func tuiNewHelp() help.Model {
	h := help.New()
	h.Styles.ShortKey = tuiHelpStyle
	h.Styles.ShortDesc = tuiSubtleStyle
	h.Styles.FullKey = tuiHelpStyle
	h.Styles.FullDesc = tuiSubtleStyle
	h.Styles.FullSeparator = tuiSubtleStyle
	h.Styles.ShortSeparator = tuiSubtleStyle
	return h
}

func statusGlyph(status string) string {
	switch strings.ToLower(status) {
	case "ok", "ready", "complete", "success", "unchanged":
		return tuiStatusOKStyle.Render("✓")
	case "changed", "warning", "new", "changed/new":
		return tuiStatusChgStyle.Render("~")
	case "failed", "error", "removed":
		return tuiStatusFailStyle.Render("✗")
	case "skipped", "pending", "status-only":
		return tuiStatusSkipStyle.Render("-")
	default:
		return tuiSubtleStyle.Render("•")
	}
}

func streamStyle(stream string) lipgloss.Style {
	switch strings.ToLower(stream) {
	case "stderr", "err", "error":
		return lipgloss.NewStyle().Foreground(tuiColorFailed)
	case "stdout", "out":
		return lipgloss.NewStyle().Foreground(tuiColorDim)
	case "warn", "warning":
		return lipgloss.NewStyle().Foreground(tuiColorChanged)
	default:
		return lipgloss.NewStyle().Foreground(tuiColorDim)
	}
}

func toneStyle(tone string) lipgloss.Style {
	switch strings.ToLower(tone) {
	case "ok", "success":
		return tuiStatusOKStyle
	case "changed", "warning", "new":
		return tuiStatusChgStyle
	case "failed", "error", "removed":
		return tuiStatusFailStyle
	case "skipped", "pending":
		return tuiStatusSkipStyle
	case "info":
		return lipgloss.NewStyle().Foreground(tuiColorInfo)
	default:
		return tuiSubtleStyle
	}
}

func renderStats(stats []ScreenStat, width int) string {
	if len(stats) == 0 || width <= 0 {
		return ""
	}
	parts := make([]string, 0, len(stats))
	used := 0
	for _, stat := range stats {
		if stat.Label == "" && stat.Value == "" {
			continue
		}
		value := stat.Value
		if value == "" {
			value = stat.Label
		}
		part := toneStyle(stat.Tone).Render(value)
		if stat.Label != "" && stat.Value != "" {
			part = lipgloss.JoinHorizontal(
				lipgloss.Left,
				tuiSubtleStyle.Render(stat.Label),
				" ",
				toneStyle(stat.Tone).Render(stat.Value),
			)
		}
		partWidth := lipgloss.Width(part)
		if used > 0 {
			partWidth += 2
		}
		if used+partWidth > width && len(parts) > 0 {
			break
		}
		parts = append(parts, part)
		used += partWidth
	}
	if len(parts) == 0 {
		return ""
	}
	segments := make([]string, 0, len(parts)*2)
	for i, part := range parts {
		if i > 0 {
			segments = append(segments, "  ")
		}
		segments = append(segments, part)
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, segments...)
}

func newTUITabPager() paginator.Model {
	pager := paginator.New()
	pager.Type = paginator.Dots
	pager.ActiveDot = tuiPagerActiveStyle.Render("•")
	pager.InactiveDot = tuiPagerInactiveStyle.Render("•")
	pager.PerPage = 1
	return pager
}

func renderTabs(tabs []tuiTab, active, width int, pager *paginator.Model) string {
	if len(tabs) == 0 || width <= 0 {
		return ""
	}

	pages := make([][]string, 0, len(tabs))
	page := make([]string, 0, len(tabs))
	used := 0
	for i, tab := range tabs {
		labelParts := []string{fmt.Sprintf("%d", i+1), tab.Label}
		if tab.Meta != "" {
			labelParts = append(labelParts, tuiSubtleStyle.Render(tab.Meta))
		}
		labelSegments := make([]string, 0, len(labelParts)*2)
		for idx, part := range labelParts {
			if idx > 0 {
				labelSegments = append(labelSegments, " ")
			}
			labelSegments = append(labelSegments, part)
		}
		label := lipgloss.JoinHorizontal(lipgloss.Left, labelSegments...)
		pill := label
		if tab.Status != "" {
			pill = lipgloss.JoinHorizontal(lipgloss.Left, statusGlyph(tab.Status), " ", pill)
		}
		style := tuiTabStyle
		if i == active {
			style = tuiActiveTabStyle
		}
		rendered := style.Render(pill)
		partWidth := lipgloss.Width(rendered)
		if used > 0 {
			partWidth++
		}
		if used+partWidth > width && len(page) > 0 {
			pages = append(pages, page)
			page = nil
			used = 0
			partWidth = lipgloss.Width(rendered)
		}
		page = append(page, rendered)
		used += partWidth
	}
	if len(page) > 0 {
		pages = append(pages, page)
	}
	if len(pages) == 0 {
		return ""
	}

	activePage := 0
	tabIndex := 0
	for pageIndex, items := range pages {
		if active >= tabIndex && active < tabIndex+len(items) {
			activePage = pageIndex
			break
		}
		tabIndex += len(items)
	}

	bodySegments := make([]string, 0, len(pages[activePage])*2)
	for idx, item := range pages[activePage] {
		if idx > 0 {
			bodySegments = append(bodySegments, " ")
		}
		bodySegments = append(bodySegments, item)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Left, bodySegments...)
	if pager == nil || len(pages) == 1 {
		return body
	}
	pager.TotalPages = len(pages)
	pager.Page = clamp(0, activePage, len(pages)-1)
	pagerLine := lipgloss.PlaceHorizontal(width, lipgloss.Center, tuiSubtleStyle.Render(pager.View()))
	return lipgloss.JoinVertical(lipgloss.Left, body, pagerLine)
}

func spaceBetween(width int, left, right string) string {
	switch {
	case right == "":
		return left
	case left == "":
		return right
	}
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if leftWidth+rightWidth+1 > width {
		return lipgloss.JoinVertical(lipgloss.Left, left, right)
	}
	spacer := lipgloss.NewStyle().Width(max(0, width-leftWidth-rightWidth))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, spacer.Render(""), right)
}

func clamp(minimum, value, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func joinMeta(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "  ")
}

func truncateText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rawLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		raw = strings.TrimRight(raw, " ")
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		words := strings.Fields(raw)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := words[0]
		for _, word := range words[1:] {
			if lipgloss.Width(current)+1+lipgloss.Width(word) <= width {
				current += " " + word
				continue
			}
			lines = append(lines, current)
			if lipgloss.Width(word) <= width {
				current = word
				continue
			}
			runes := []rune(word)
			for len(runes) > width {
				lines = append(lines, string(runes[:width]))
				runes = runes[width:]
			}
			current = string(runes)
		}
		lines = append(lines, current)
	}
	return lines
}

func renderScreenLines(lines []ScreenLine, width int) string {
	if len(lines) == 0 {
		return ""
	}
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		prefixText := ""
		prefixWidth := 0
		if line.Prefix != "" {
			prefixText = streamStyle(line.Prefix).Render(line.Prefix + ">")
			prefixWidth = lipgloss.Width(prefixText)
		}
		contentWidth := width
		if prefixWidth > 0 {
			contentWidth -= prefixWidth + 1
		}
		if contentWidth < 12 {
			contentWidth = 12
		}
		wrapped := wrapText(line.Text, contentWidth)
		for i, wrappedLine := range wrapped {
			content := tuiSubtleStyle.Render(wrappedLine)
			if prefixWidth > 0 && i == 0 {
				rendered = append(rendered, lipgloss.JoinHorizontal(lipgloss.Left, prefixText, " ", content))
				continue
			}
			if prefixWidth > 0 {
				rendered = append(rendered, lipgloss.NewStyle().PaddingLeft(prefixWidth+1).Render(content))
				continue
			}
			rendered = append(rendered, content)
		}
	}
	return strings.Join(rendered, "\n")
}

func viewportBodyHeight(totalHeight int, chromeParts ...string) int {
	used := 0
	nonEmpty := 0
	for _, part := range chromeParts {
		if part == "" {
			continue
		}
		used += lipgloss.Height(part)
		nonEmpty++
	}
	return max(4, totalHeight-used-nonEmpty)
}

func compactHelpPrompt() string {
	return tuiHelpStyle.Render("? help")
}

func responsiveFooter(width int, left, right string) string {
	if width <= 0 {
		return compactHelpPrompt()
	}
	if right == "" {
		if left == "" {
			return ""
		}
		if lipgloss.Width(left) > width {
			return compactHelpPrompt()
		}
		return tuiFooterStyle.Width(width).Render(left)
	}
	if left != "" && lipgloss.Width(left)+1+lipgloss.Width(right) <= width {
		return tuiFooterStyle.Width(width).Render(spaceBetween(width, left, right))
	}
	if left == "" && lipgloss.Width(right) <= width {
		return tuiFooterStyle.Width(width).AlignHorizontal(lipgloss.Right).Render(right)
	}
	return compactHelpPrompt()
}

type helpKeyMap interface {
	ShortHelp() []key.Binding
	FullHelp() [][]key.Binding
}
