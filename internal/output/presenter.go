package output

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const defaultTerminalWidth = 96

// KeyValue is a label/value row for presenter metadata blocks.
type KeyValue struct {
	Label string
	Value string
}

type terminalTheme struct {
	eyebrow    lipgloss.Style
	title      lipgloss.Style
	subtitle   lipgloss.Style
	section    lipgloss.Style
	label      lipgloss.Style
	value      lipgloss.Style
	muted      lipgloss.Style
	headerCell lipgloss.Style
	cell       lipgloss.Style
	panel      lipgloss.Style
	pill       lipgloss.Style
	ok         lipgloss.Style
	changed    lipgloss.Style
	failed     lipgloss.Style
	skipped    lipgloss.Style
	active     lipgloss.Style
	info       lipgloss.Style
	warning    lipgloss.Style
	error      lipgloss.Style
	success    lipgloss.Style
}

func newTerminalTheme(w io.Writer) terminalTheme {
	renderer := lipgloss.NewRenderer(w)

	return terminalTheme{
		eyebrow: renderer.NewStyle().
			Foreground(lipgloss.Color("109")).
			Bold(true),
		title: renderer.NewStyle().
			Foreground(lipgloss.Color("230")).
			Bold(true),
		subtitle: renderer.NewStyle().
			Foreground(lipgloss.Color("246")),
		section: renderer.NewStyle().
			Foreground(lipgloss.Color("109")).
			Bold(true),
		label: renderer.NewStyle().
			Foreground(lipgloss.Color("109")).
			Bold(true),
		value: renderer.NewStyle().
			Foreground(lipgloss.Color("252")),
		muted: renderer.NewStyle().
			Foreground(lipgloss.Color("244")),
		headerCell: renderer.NewStyle().
			Foreground(lipgloss.Color("230")).
			Bold(true),
		cell: renderer.NewStyle().
			Foreground(lipgloss.Color("252")),
		panel: renderer.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1),
		pill: renderer.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("238")).
			Padding(0, 1),
		ok: renderer.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("29")).
			Bold(true).
			Padding(0, 1),
		changed: renderer.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("172")).
			Bold(true).
			Padding(0, 1),
		failed: renderer.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("160")).
			Bold(true).
			Padding(0, 1),
		skipped: renderer.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("240")).
			Bold(true).
			Padding(0, 1),
		active: renderer.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("31")).
			Bold(true).
			Padding(0, 1),
		info: renderer.NewStyle().
			Foreground(lipgloss.Color("117")),
		warning: renderer.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true),
		error: renderer.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true),
		success: renderer.NewStyle().
			Foreground(lipgloss.Color("114")).
			Bold(true),
	}
}

func (t terminalTheme) statusBadge(status string) string {
	label := strings.ToUpper(strings.TrimSpace(status))
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "unchanged", "ready", "success":
		return t.ok.Render(label)
	case "changed", "new", "status-only":
		return t.changed.Render(label)
	case "failed", "error", "removed":
		return t.failed.Render(label)
	case "skipped":
		return t.skipped.Render(label)
	case "active", "running":
		return t.active.Render("RUNNING")
	default:
		if label == "" {
			label = "INFO"
		}
		return t.pill.Render(label)
	}
}

func (t terminalTheme) note(kind, message string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "success":
		return t.success.Render("OK  " + message)
	case "warning":
		return t.warning.Render("WARN  " + message)
	case "error":
		return t.error.Render("ERR  " + message)
	default:
		return t.info.Render("INFO  " + message)
	}
}

func (t terminalTheme) host(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "localhost"
	}
	return t.pill.Render(target)
}

// Presenter renders shared lipgloss layouts for human-readable CLI output.
type Presenter struct {
	width int
	theme terminalTheme
}

// NewPresenter creates a presenter tuned to the destination writer.
func NewPresenter(w io.Writer) *Presenter {
	return &Presenter{
		width: detectTerminalWidth(w),
		theme: newTerminalTheme(w),
	}
}

// Width returns the detected terminal width or a sensible default.
func (p *Presenter) Width() int {
	return p.width
}

// Title renders a top-level title block with an optional subtitle.
func (p *Presenter) Title(title, subtitle string) string {
	lines := []string{
		p.theme.eyebrow.Render("PREFLIGHT"),
		p.theme.title.Render(title),
	}
	if subtitle != "" {
		lines = append(lines, p.theme.subtitle.Render(subtitle))
	}
	return p.theme.panel.MaxWidth(p.width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// Section renders a named content block.
func (p *Presenter) Section(title, body string) string {
	if body == "" {
		return p.theme.section.Render(strings.ToUpper(title))
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		p.theme.section.Render(strings.ToUpper(title)),
		body,
	)
}

// KeyValues renders aligned label/value rows.
func (p *Presenter) KeyValues(items []KeyValue) string {
	maxLabelWidth := 0
	for _, item := range items {
		if w := lipgloss.Width(item.Label); w > maxLabelWidth {
			maxLabelWidth = w
		}
	}

	lines := make([]string, 0, len(items))
	for _, item := range items {
		label := p.theme.label.Width(maxLabelWidth).Render(item.Label)
		value := p.theme.value.Render(item.Value)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, label, "  ", value))
	}
	return strings.Join(lines, "\n")
}

// Bullets renders a simple bullet list.
func (p *Presenter) Bullets(items []string) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, p.theme.value.Render("- "+item))
	}
	return strings.Join(lines, "\n")
}

// Table renders a fixed-width table with aligned columns.
func (p *Presenter) Table(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}

	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = lipgloss.Width(header)
	}
	for _, row := range rows {
		for i := range headers {
			var cell string
			if i < len(row) {
				cell = row[i]
			}
			if w := lipgloss.Width(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	renderRow := func(cells []string, style lipgloss.Style) string {
		parts := make([]string, 0, len(headers))
		for i := range headers {
			value := ""
			if i < len(cells) {
				value = cells[i]
			}
			cell := style.Width(widths[i]).Render(value)
			parts = append(parts, cell)
		}
		return strings.Join(parts, "  ")
	}

	lines := []string{renderRow(headers, p.theme.headerCell)}
	for _, row := range rows {
		lines = append(lines, renderRow(row, p.theme.cell))
	}
	return strings.Join(lines, "\n")
}

// Notice renders a one-line status callout.
func (p *Presenter) Notice(kind, message string) string {
	return p.theme.note(kind, message)
}

// Host renders a target pill.
func (p *Presenter) Host(target string) string {
	return p.theme.host(target)
}

// StatusBadge renders a colored status badge.
func (p *Presenter) StatusBadge(status string) string {
	return p.theme.statusBadge(status)
}

// Muted renders secondary text.
func (p *Presenter) Muted(value string) string {
	return p.theme.muted.Render(value)
}

// JoinBlocks joins non-empty blocks with a blank line.
func (p *Presenter) JoinBlocks(blocks ...string) string {
	nonEmpty := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, block)
	}
	return strings.Join(nonEmpty, "\n\n")
}

func detectTerminalWidth(w io.Writer) int {
	file, ok := w.(*os.File)
	if !ok || !isTTY(w) {
		return defaultTerminalWidth
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width <= 0 {
		return defaultTerminalWidth
	}
	return width
}
