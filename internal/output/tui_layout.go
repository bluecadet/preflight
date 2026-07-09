package output

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type tuiCardBuilder struct {
	title    string
	sections []string
}

func newTUICard(title string) *tuiCardBuilder {
	return &tuiCardBuilder{title: title}
}

func (b *tuiCardBuilder) add(section string) {
	if strings.TrimSpace(section) == "" {
		return
	}
	b.sections = append(b.sections, section)
}

func (b *tuiCardBuilder) addLabeled(title, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	b.add(S.Label.Render(title) + "\n" + body)
}

func (b *tuiCardBuilder) render() string {
	return tsRenderSection(b.title, strings.Join(b.sections, "\n\n"))
}

func tsTruncate(s string, n int) string {
	return Truncate(s, n)
}

func tsPluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func tsRow(elems ...string) string {
	return S.RowInset.Render(tsJoinHorizontal("  ", elems...))
}

func tsJoinHorizontal(gap string, elems ...string) string {
	if len(elems) == 0 {
		return ""
	}
	parts := make([]string, 0, len(elems)*2-1)
	for i, elem := range elems {
		if i > 0 {
			parts = append(parts, gap)
		}
		parts = append(parts, elem)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func tsOutputLine(depth int, content string) string {
	return lipgloss.NewStyle().PaddingLeft(depth).Render(S.Output.Render(content))
}

func tsOutputLines(depth int, content string, width int) []string {
	contentWidth := outputContentWidth(depth, width)
	wrapped := wrapFactValue(strings.TrimSpace(content), contentWidth)
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, tsOutputLine(depth, line))
	}
	return lines
}

func outputContentWidth(depth int, width int) int {
	if width <= 0 {
		width = 80
	}
	return max(width-depth-2, 16)
}

func tsRenderNotice(glyph string, style lipgloss.Style, message string, width int) string {
	prefix := "  " + style.Render(glyph) + "  "
	contentWidth := outputContentWidth(lipgloss.Width(prefix), width)
	lines := wrapFactValue(strings.TrimSpace(message), contentWidth)
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + style.Render(line)
			continue
		}
		lines[i] = strings.Repeat(" ", lipgloss.Width(prefix)) + style.Render(line)
	}
	return strings.Join(lines, "\n")
}

func tsRenderSection(title, body string) string {
	var parts []string
	if title != "" {
		parts = append(parts, S.CardTitleInset.Render(S.CardTitle.Render(title)))
	}
	if strings.TrimSpace(body) == "" {
		return strings.Join(parts, "\n")
	}
	if title != "" {
		parts = append(parts, S.TableRule.Render(strings.Repeat("─", 42)))
	}
	parts = append(parts, S.CardBodyInset.Render(body))
	return strings.Join(parts, "\n")
}

func tsRenderPairs(rows [][2]string) string {
	maxKeyWidth := 0
	for _, row := range rows {
		maxKeyWidth = max(maxKeyWidth, lipgloss.Width(row[0]))
	}

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		key := lipgloss.NewStyle().Width(maxKeyWidth).Render(S.Key.Render(row[0]))
		lines = append(lines, tsJoinHorizontal("  ", key, S.Value.Render(row[1])))
	}
	return strings.Join(lines, "\n")
}

func tsRenderSimpleTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}

	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = lipgloss.Width(header)
	}
	for _, row := range rows {
		for i := 0; i < min(len(row), len(widths)); i++ {
			widths[i] = max(widths[i], lipgloss.Width(row[i]))
		}
	}

	renderRow := func(cells []string, style lipgloss.Style) string {
		rendered := make([]string, len(headers))
		for i := range headers {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			rendered[i] = lipgloss.NewStyle().Width(widths[i]).Render(style.Render(cell))
		}
		return tsJoinHorizontal("  ", rendered...)
	}

	lines := []string{
		renderRow(headers, S.TableHead),
		S.TableRule.Render(renderRow(tsDashCells(widths), lipgloss.NewStyle())),
	}
	for _, row := range rows {
		lines = append(lines, renderRow(row, lipgloss.NewStyle()))
	}
	return strings.Join(lines, "\n")
}

func factsContentWidth(width int) int {
	if width <= 0 {
		return 72
	}
	return max(width-8, 36)
}

func wrapFactValue(value string, width int) []string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return []string{value}
	}
	if strings.Contains(value, ";") {
		if wrapped := wrapDelimited(value, width, ";"); len(wrapped) > 1 {
			return wrapped
		}
	}
	if strings.Contains(value, `\`) {
		if wrapped := wrapDelimited(value, width, `\`); len(wrapped) > 1 {
			return wrapped
		}
	}
	return wrapWords(value, width)
}

func wrapDelimited(value string, width int, delim string) []string {
	parts := strings.Split(value, delim)
	var lines []string
	current := ""
	for i, part := range parts {
		token := part
		if i < len(parts)-1 {
			token += delim
		}
		if current == "" {
			current = token
			continue
		}
		if lipgloss.Width(current)+lipgloss.Width(token) <= width {
			current += token
			continue
		}
		lines = append(lines, current)
		current = token
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func wrapWords(value string, width int) []string {
	fields := strings.Fields(value)
	if len(fields) <= 1 {
		return wrapRunes(value, width)
	}

	var lines []string
	current := ""
	for _, field := range fields {
		candidate := field
		if current != "" {
			candidate = current + " " + field
		}
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
		}
		if lipgloss.Width(field) > width {
			lines = append(lines, wrapRunes(field, width)...)
			current = ""
			continue
		}
		current = field
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func wrapRunes(value string, width int) []string {
	if width <= 0 {
		return []string{value}
	}

	runes := []rune(value)
	var lines []string
	for len(runes) > 0 {
		chunk := runes
		if len(chunk) > width {
			chunk = runes[:width]
		}
		lines = append(lines, string(chunk))
		runes = runes[len(chunk):]
	}
	return lines
}

func tsRenderBulletList(items []string, numbered bool) string {
	if len(items) == 0 {
		return S.Muted.Render("(none)")
	}

	lines := make([]string, 0, len(items))
	for i, item := range items {
		prefix := "•"
		if numbered {
			prefix = strconv.Itoa(i+1) + "."
		}
		lines = append(lines, prefix+" "+item)
	}
	return strings.Join(lines, "\n")
}

func tsDecorateStateStatus(status string) string {
	switch strings.ToUpper(status) {
	case "UNCHANGED":
		return S.OK.Render("✓ unchanged")
	case "CHANGED":
		return S.Changed.Render("◆ changed")
	case "NEW":
		return S.Changed.Render("+ new")
	case "MISSING":
		return S.Failed.Render("– missing")
	case "RECORDED":
		return S.Muted.Render("• recorded")
	default:
		return status
	}
}

func tsDashCells(widths []int) []string {
	cells := make([]string, len(widths))
	for i, width := range widths {
		cells[i] = strings.Repeat("─", max(width, 1))
	}
	return cells
}

func tsRenderOptionalBulletList(items []string) string {
	if len(items) == 0 {
		return S.Muted.Render("(none)")
	}
	return tsRenderBulletList(items, false)
}
