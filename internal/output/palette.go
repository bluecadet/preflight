package output

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// ColorRole defines the color values for a single semantic concept in the
// output palette. Each role specifies colors for both TUI (lipgloss) and
// text (ANSI) rendering, as well as non-color text attributes such as bold
// and italic.
type ColorRole struct {
	// Light is the color palette index for light terminal backgrounds.
	// Base-16 roles use "0"-"15"; a handful of roles (HostColors) still use
	// 256-color indices. Empty means terminal default foreground.
	Light string
	// Dark is the color palette index for dark terminal backgrounds.
	// When Light and Dark are identical, the role is fixed (non-adaptive).
	Dark string

	// ANSI is the ANSI escape code for text-mode rendering.
	// An empty string means no color (terminal default foreground). For
	// roles whose Light/Dark are equal, this is mechanically derivable via
	// ansiForeground and should be set from that rather than hand-typed, so
	// it can't drift from the index.
	ANSI string

	// Text attributes.
	Bold   bool
	Italic bool
	// Dim renders the foreground in a dimmer shade (SGR 2 / lipgloss
	// Faint). Used to carve a second tier out of the terminal default
	// foreground when a role needs to read as more recessed than
	// full-brightness text but doesn't warrant its own grey index.
	Dim bool
}

// ansiForeground derives the ANSI SGR escape for a palette index string. It
// is the single source of truth for turning a Light/Dark index into the
// text-renderer escape code, so the two can never drift apart for roles
// where Light == Dark.
//
// "" (terminal default foreground) maps to "". Indices 0-7 map to the
// standard SGR 30-37 codes; 8-15 map to the bright SGR 90-97 codes. Indices
// above 15 (256-color, used only by HostColors) fall back to the 8-bit
// SGR 38;5;n form.
func ansiForeground(index string) string {
	if index == "" {
		return ""
	}
	n, err := strconv.Atoi(index)
	if err != nil {
		return ""
	}
	switch {
	case n >= 0 && n <= 7:
		return fmt.Sprintf("\033[%dm", 30+n)
	case n >= 8 && n <= 15:
		return fmt.Sprintf("\033[%dm", 90+(n-8))
	default:
		return fmt.Sprintf("\033[38;5;%dm", n)
	}
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
	if r.Dim {
		s = s.Faint(true)
	}
	return s
}

// LipglossStyleNoColor returns a lipgloss.Style for this color role with only
// non-color text attributes applied (bold, italic, dim). The foreground
// color is not set, so the terminal default is used.
func (r ColorRole) LipglossStyleNoColor() lipgloss.Style {
	s := lipgloss.NewStyle()
	if r.Bold {
		s = s.Bold(true)
	}
	if r.Italic {
		s = s.Italic(true)
	}
	if r.Dim {
		s = s.Faint(true)
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

// DefaultPalette returns the standard semantic palette. Status roles carry
// distinct light and dark values so they adapt to the terminal background;
// neutral roles resolve to a single base-16 index or the terminal default
// foreground, which the theme already picks to suit its own background.
// Roles are strippable entirely when color is disabled.
func DefaultPalette() SemanticPalette {
	// grey is the base-16 "mid-grey" slot (bright black) used for all
	// "dim chrome" roles: decorative or peripheral text that should read as
	// recessed on both light and dark backgrounds. Color 7 (plain white) is
	// too close to invisible on a light background, so 8 is the only
	// base-16 index that works adaptively here. Light == Dark, so a single
	// var covers both and ANSI is derived from it directly rather than
	// hand-typed, so the two can't drift apart.
	grey := "8"
	greyANSI := ansiForeground(grey)

	return SemanticPalette{
		// Status outcomes — adaptive bright variants for dark terminals.
		OK:      ColorRole{Light: "2", Dark: "10", ANSI: "\033[32m"}, // green
		Changed: ColorRole{Light: "3", Dark: "11", ANSI: "\033[33m"}, // yellow
		Failed:  ColorRole{Light: "1", Dark: "9", ANSI: "\033[31m"},  // red
		Skipped: ColorRole{Light: grey, Dark: grey, ANSI: greyANSI},  // grey

		// UI elements.
		TaskName: ColorRole{},                                         // terminal default foreground
		Muted:    ColorRole{Light: grey, Dark: grey, ANSI: greyANSI},  // grey
		Bold:     ColorRole{ANSI: "\033[1m", Bold: true},              // no color, bold
		Spin:     ColorRole{Light: "4", Dark: "12", ANSI: "\033[34m"}, // blue
		Divider:  ColorRole{Light: grey, Dark: grey, ANSI: greyANSI},  // grey
		// Default foreground, dim + italic. Dim (rather than a grey index)
		// keeps Output visually recessed without colliding with the
		// Divider/Elapsed "dim chrome" tier below it.
		Output:  ColorRole{Italic: true, Dim: true},
		Elapsed: ColorRole{Light: grey, Dark: grey, ANSI: greyANSI}, // grey

		// Card elements.
		CardTitle: ColorRole{Light: "4", Dark: "12", Bold: true}, // bold blue
		// Label and Key are default foreground + dim: a "secondary text"
		// tier that sits between the grey8 "dim chrome" tier (Divider,
		// Elapsed, Muted, Skipped) and the full-brightness "primary
		// content" tier (Value, TableHead). Putting them at grey8 instead
		// would make Key visually identical to Divider/Elapsed — it
		// carries no Bold to distinguish it, so the two tiers would
		// collapse into one.
		Label:     ColorRole{Bold: true, Dim: true}, // default foreground, bold + dim
		Key:       ColorRole{Dim: true},             // default foreground, dim
		Value:     ColorRole{},                      // default foreground
		TableHead: ColorRole{Bold: true},            // default foreground, bold

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
