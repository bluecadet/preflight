package output

import "github.com/charmbracelet/lipgloss"

const maxLiveLines = 8

// TUIStyles bundles every TUI style as a single struct sourced from the
// semantic palette. The package-level S variable is initialized at load
// time with color enabled; it can be replaced with a no-color variant at
// model construction time.
//
// This avoids loose var declarations and eliminates the need to keep a
// separate buildStyles function in sync with the var block.
type TUIStyles struct {
	OK        lipgloss.Style
	Changed   lipgloss.Style
	Failed    lipgloss.Style
	Skipped   lipgloss.Style
	Muted     lipgloss.Style
	Bold      lipgloss.Style
	Spin      lipgloss.Style
	Divider   lipgloss.Style
	Output    lipgloss.Style
	Elapsed   lipgloss.Style
	CardTitle lipgloss.Style
	Label     lipgloss.Style
	Key       lipgloss.Style
	Value     lipgloss.Style
	TableHead lipgloss.Style
	TableRule lipgloss.Style

	TransportLocal lipgloss.Style
	TransportSSH   lipgloss.Style
	TransportWinRM lipgloss.Style

	RowInset       lipgloss.Style
	CardTitleInset lipgloss.Style
	CardBodyInset  lipgloss.Style
}

// S is the active TUI styles struct. It is initialized with full color at
// package load time. Callers that need a no-color variant (e.g. the TUI
// model constructor) should reassign S = NewTUIStyles(DefaultPalette(), false).
var S = NewTUIStyles(DefaultPalette(), true)

// NewTUIStyles builds a TUIStyles struct from the given palette. When color
// is false, foreground colors are removed but non-color attributes (bold,
// italic) are preserved.
func NewTUIStyles(p SemanticPalette, color bool) TUIStyles {
	build := func(r ColorRole) lipgloss.Style {
		if color {
			return r.LipglossStyle()
		}
		return r.LipglossStyleNoColor()
	}
	return TUIStyles{
		OK:        build(p.OK),
		Changed:   build(p.Changed),
		Failed:    build(p.Failed),
		Skipped:   build(p.Skipped),
		Muted:     build(p.Muted),
		Bold:      build(p.Bold),
		Spin:      build(p.Spin),
		Divider:   build(p.Divider),
		Output:    build(p.Output),
		Elapsed:   build(p.Elapsed),
		CardTitle: build(p.CardTitle),
		Label:     build(p.Label),
		Key:       build(p.Key),
		Value:     build(p.Value),
		TableHead: build(p.TableHead),
		TableRule: build(p.TableRule),

		TransportLocal: build(p.TransportLocal),
		TransportSSH:   build(p.TransportSSH),
		TransportWinRM: build(p.TransportWinRM),

		RowInset:       lipgloss.NewStyle().PaddingLeft(2),
		CardTitleInset: lipgloss.NewStyle().PaddingLeft(2),
		CardBodyInset:  lipgloss.NewStyle().PaddingLeft(2),
	}
}
