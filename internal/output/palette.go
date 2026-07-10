package output

import "github.com/charmbracelet/lipgloss"

// ColorRole defines the color values for a single semantic concept in the
// output palette. Each role specifies colors for both TUI (lipgloss) and
// text (ANSI) rendering, as well as non-color text attributes such as bold
// and italic.
type ColorRole struct {
	// Light is the 256-color palette index for light terminal backgrounds.
	Light string
	// Dark is the 256-color palette index for dark terminal backgrounds.
	// When Light and Dark are identical, the role is fixed (non-adaptive).
	Dark string

	// ANSI is the ANSI escape code for text-mode rendering.
	// An empty string means no color (terminal default foreground).
	ANSI string

	// Text attributes.
	Bold   bool
	Italic bool
}

// LipglossStyle returns a lipgloss.Style for this color role with foreground
// color and text attributes applied.
func (r ColorRole) LipglossStyle() lipgloss.Style {
	s := lipgloss.NewStyle()
	if r.Light != "" {
		if r.Dark != "" && r.Light != r.Dark {
			s = s.Foreground(lipgloss.AdaptiveColor{Light: r.Light, Dark: r.Dark})
		} else {
			s = s.Foreground(lipgloss.Color(r.Light))
		}
	}
	if r.Bold {
		s = s.Bold(true)
	}
	if r.Italic {
		s = s.Italic(true)
	}
	return s
}

// LipglossStyleNoColor returns a lipgloss.Style for this color role with only
// non-color text attributes applied (bold, italic). The foreground color is
// not set, so the terminal default is used.
func (r ColorRole) LipglossStyleNoColor() lipgloss.Style {
	s := lipgloss.NewStyle()
	if r.Bold {
		s = s.Bold(true)
	}
	if r.Italic {
		s = s.Italic(true)
	}
	return s
}

// HostColor resolves a host color slot index to a concrete ColorRole,
// wrapping modulo the rotation length. The bool result is false when the
// palette has no host colors or the index is negative (unknown target), so
// callers can fall back to uncolored output. Overflow (more hosts than
// colors) is handled by the wrap, not by returning false.
func (p SemanticPalette) HostColor(idx int) (ColorRole, bool) {
	if idx < 0 || len(p.HostColors) == 0 {
		return ColorRole{}, false
	}
	return p.HostColors[idx%len(p.HostColors)], true
}

// SemanticPalette defines all color roles used in the output package. It is
// the single source of truth for every color decision: status glyphs,
// transport badges, task names, elapsed times, headers, separators, detail
// text, and card elements. Each role is adaptive for light and dark terminal
// backgrounds and fully strippable when color is disabled.
type SemanticPalette struct {
	// Status outcome colors.
	OK      ColorRole
	Changed ColorRole
	Failed  ColorRole
	Skipped ColorRole

	// UI element colors.
	TaskName ColorRole
	Muted    ColorRole
	Bold     ColorRole
	Spin     ColorRole
	Divider  ColorRole
	Output   ColorRole
	Elapsed  ColorRole

	// Card element colors.
	CardTitle ColorRole
	Label     ColorRole
	Key       ColorRole
	Value     ColorRole
	TableHead ColorRole

	// HostColors is the rotation palette used to distinguish hosts by
	// color in multi-target runs. Renderers assign each target a slot by
	// roster position and resolve it here, wrapping modulo the length.
	HostColors []ColorRole
}

// DefaultPalette returns the standard semantic palette. Every role uses
// distinct light and dark values so the palette is adaptive for both light
// and dark terminal backgrounds. Roles are strippable entirely when color
// is disabled.
func DefaultPalette() SemanticPalette {
	return SemanticPalette{
		// Status outcomes — adaptive bright variants for dark terminals.
		OK:      ColorRole{Light: "2", Dark: "10", ANSI: "\033[32m"},    // green
		Changed: ColorRole{Light: "3", Dark: "11", ANSI: "\033[33m"},    // yellow
		Failed:  ColorRole{Light: "1", Dark: "9", ANSI: "\033[31m"},     // red
		Skipped: ColorRole{Light: "240", Dark: "247", ANSI: "\033[90m"}, // grey

		// UI elements.
		TaskName: ColorRole{},                                            // terminal default foreground
		Muted:    ColorRole{Light: "240", Dark: "247", ANSI: "\033[90m"}, // grey
		Bold:     ColorRole{ANSI: "\033[1m", Bold: true},                 // no color, bold
		Spin:     ColorRole{Light: "4", Dark: "12", ANSI: "\033[34m"},    // blue
		Divider:  ColorRole{Light: "237", Dark: "240", ANSI: "\033[90m"}, // dim grey
		Output:   ColorRole{Light: "241", Dark: "250", Italic: true},     // grey italic
		Elapsed:  ColorRole{Light: "240", Dark: "246", ANSI: "\033[90m"}, // grey

		// Card elements.
		CardTitle: ColorRole{Light: "4", Dark: "12", Bold: true},    // bold blue
		Label:     ColorRole{Light: "245", Dark: "250", Bold: true}, // bold grey
		Key:       ColorRole{Light: "246", Dark: "250"},             // grey
		Value:     ColorRole{Light: "252", Dark: "254"},             // light grey
		TableHead: ColorRole{Light: "252", Dark: "254", Bold: true}, // bold light grey

		// Host color rotation — eight visually distinct hues chosen to
		// avoid the status outcome colors (green/yellow/red/grey) so a host
		// badge is never confused with a status glyph. Color-only: in
		// no-color mode each role degrades to plain text.
		HostColors: []ColorRole{
			{Light: "33", Dark: "81", ANSI: "\033[38;5;81m"},    // blue
			{Light: "125", Dark: "213", ANSI: "\033[38;5;213m"}, // magenta
			{Light: "30", Dark: "80", ANSI: "\033[38;5;80m"},    // teal
			{Light: "130", Dark: "208", ANSI: "\033[38;5;208m"}, // orange
			{Light: "91", Dark: "141", ANSI: "\033[38;5;141m"},  // purple
			{Light: "162", Dark: "204", ANSI: "\033[38;5;204m"}, // rose
			{Light: "37", Dark: "79", ANSI: "\033[38;5;79m"},    // aqua
			{Light: "57", Dark: "99", ANSI: "\033[38;5;99m"},    // indigo
		},
	}
}
