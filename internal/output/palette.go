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
	Muted   ColorRole
	Bold    ColorRole
	Spin    ColorRole
	Divider ColorRole
	Output  ColorRole
	Elapsed ColorRole

	// Card element colors.
	CardTitle ColorRole
	Label     ColorRole
	Key       ColorRole
	Value     ColorRole
	TableHead ColorRole

	// Transport badge colors (reserved for future use).
	TransportLocal ColorRole
	TransportSSH   ColorRole
	TransportWinRM ColorRole
}

// DefaultPalette returns the standard semantic palette with the same color
// values as the pre-existing ad-hoc styles. This ensures no behavior change
// during the prefactor — all rendered output and golden snapshots remain
// identical.
func DefaultPalette() SemanticPalette {
	return SemanticPalette{
		// Status outcomes — adaptive bright variants for dark terminals.
		OK:      ColorRole{Light: "2", Dark: "10", ANSI: "\033[32m"},    // green
		Changed: ColorRole{Light: "3", Dark: "11", ANSI: "\033[33m"},    // yellow
		Failed:  ColorRole{Light: "1", Dark: "9", ANSI: "\033[31m"},     // red
		Skipped: ColorRole{Light: "240", Dark: "240", ANSI: "\033[90m"}, // grey

		// UI elements.
		Muted:   ColorRole{Light: "240", Dark: "240", ANSI: "\033[90m"}, // grey
		Bold:    ColorRole{ANSI: "\033[1m", Bold: true},                 // no color, bold
		Spin:    ColorRole{Light: "4", Dark: "12", ANSI: "\033[34m"},    // blue
		Divider: ColorRole{Light: "237", Dark: "237", ANSI: "\033[90m"}, // dim grey
		Output:  ColorRole{Light: "241", Dark: "241", Italic: true},     // grey italic
		Elapsed: ColorRole{Light: "240", Dark: "240", ANSI: "\033[90m"}, // grey

		// Card elements.
		CardTitle: ColorRole{Light: "4", Dark: "12", Bold: true},    // bold blue
		Label:     ColorRole{Light: "245", Dark: "245", Bold: true}, // bold grey
		Key:       ColorRole{Light: "246", Dark: "246"},             // grey
		Value:     ColorRole{Light: "252", Dark: "252"},             // light grey
		TableHead: ColorRole{Light: "252", Dark: "252", Bold: true}, // bold light grey

		// Transport badges (reserved — not yet used by any renderer).
		TransportLocal: ColorRole{Light: "8", Dark: "8"},                    // grey
		TransportSSH:   ColorRole{Light: "4", Dark: "12", ANSI: "\033[34m"}, // blue
		TransportWinRM: ColorRole{Light: "6", Dark: "14", ANSI: "\033[36m"}, // cyan
	}
}
