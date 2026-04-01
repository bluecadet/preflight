package output

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
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

	tuiTitleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	tuiCommandStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	tuiSubtleStyle       = lipgloss.NewStyle().Foreground(tuiColorDim)
	tuiHelpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	tuiSpinnerStyle      = lipgloss.NewStyle().Foreground(tuiColorInfo)
	tuiSectionStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("187"))
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
		return lipgloss.NewStyle().Foreground(tuiColorOK)
	case "warn", "warning":
		return lipgloss.NewStyle().Foreground(tuiColorChanged)
	default:
		return lipgloss.NewStyle().Foreground(tuiColorInfo)
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

func renderStatusChip(status string) string {
	if status == "" {
		return ""
	}
	label := strings.ToUpper(status)
	style := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("16")).
		Background(tuiColorInfo).
		Bold(true)
	switch strings.ToLower(status) {
	case "ok", "ready", "complete", "success", "unchanged":
		style = style.Background(tuiColorOK)
	case "changed", "warning", "new":
		style = style.Background(tuiColorChanged)
	case "failed", "error", "removed":
		style = style.Background(tuiColorFailed)
	case "skipped", "pending", "status-only":
		style = style.Background(tuiColorSkipped)
	}
	return style.Render(label)
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
			part = tuiSubtleStyle.Render(stat.Label+" ") + toneStyle(stat.Tone).Render(stat.Value)
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
	return strings.Join(parts, "  ")
}

func renderTabs(tabs []tuiTab, active, width int) string {
	if len(tabs) == 0 || width <= 0 {
		return ""
	}
	parts := make([]string, 0, len(tabs))
	used := 0
	for i, tab := range tabs {
		label := tab.Label
		if tab.Meta != "" {
			label += " " + tuiSubtleStyle.Render(tab.Meta)
		}
		pill := label
		if tab.Status != "" {
			pill = statusGlyph(tab.Status) + " " + pill
		}
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tuiColorBorder).
			Padding(0, 1)
		if i == active {
			style = style.BorderForeground(tuiColorSelected).Bold(true)
		}
		rendered := style.Render(fmt.Sprintf("%d %s", i+1, pill))
		partWidth := lipgloss.Width(rendered)
		if used > 0 {
			partWidth++
		}
		if used+partWidth > width && len(parts) > 0 {
			break
		}
		parts = append(parts, rendered)
		used += partWidth
	}
	return strings.Join(parts, " ")
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
		return left + "\n" + right
	}
	return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
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
		prefix := line.Prefix
		if prefix != "" {
			prefix = streamStyle(line.Prefix).Render(prefix + ">")
		}
		contentWidth := width
		if prefix != "" {
			contentWidth -= lipgloss.Width(prefix) + 1
		}
		if contentWidth < 12 {
			contentWidth = 12
		}
		wrapped := wrapText(line.Text, contentWidth)
		for i, wrappedLine := range wrapped {
			if prefix != "" && i == 0 {
				rendered = append(rendered, prefix+" "+toneStyle(line.Tone).Render(wrappedLine))
				continue
			}
			indent := ""
			if prefix != "" {
				indent = strings.Repeat(" ", lipgloss.Width(prefix)+1)
			}
			rendered = append(rendered, indent+toneStyle(line.Tone).Render(wrappedLine))
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

type helpKeyMap interface {
	ShortHelp() []key.Binding
	FullHelp() [][]key.Binding
}
