package output

import (
	"io"
	"strconv"
	"strings"

	"github.com/muesli/termenv"
)

// detectColorProfile reports the color capability the terminal advertises:
// Ascii, ANSI (16-color), ANSI256 (8-bit), or TrueColor. This is a separate
// question from whether color is emitted at all — DetectColor is the sole
// authority on that, weighing TTY state, NO_COLOR, --color, and CI. This
// function only narrows how much color is used once DetectColor has already
// said "yes".
//
// WithTTY(true) makes termenv skip its own isatty check and read
// TERM/COLORTERM directly, so a forced --color=always on a pipe (e.g. in
// tests, or piped to a colorizing pager) still gets a real tier instead of
// being flattened to Ascii by a TTY check DetectColor already performed
// under its own precedence rules.
func detectColorProfile(w io.Writer) termenv.Profile {
	return termenv.NewOutput(w, termenv.WithTTY(true)).ColorProfile()
}

// sgr256Foreground extracts the color index from an 8-bit SGR foreground
// escape ("\033[38;5;Nm"). ok is false for any other escape: base-16 codes,
// attribute-only codes such as bold, or the empty string all pass through
// untouched by design — they're already within what every profile supports.
func sgr256Foreground(code string) (n int, ok bool) {
	const prefix = "\033[38;5;"
	if !strings.HasPrefix(code, prefix) || !strings.HasSuffix(code, "m") {
		return 0, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(code, prefix), "m")
	v, err := strconv.Atoi(body)
	if err != nil {
		return 0, false
	}
	return v, true
}

// downgradeSGR narrows an 8-bit SGR foreground escape to the nearest color
// the given profile supports, using termenv's own conversion table
// (Profile.Convert). Escapes that aren't 8-bit foreground codes pass through
// unchanged — after the neutral roles moved to base-16, host colors are the
// only ones that still need this.
//
// A profile of Ascii is treated as ANSI: by the time this runs, DetectColor
// has already decided color is wanted, so Ascii here just means the
// environment didn't advertise anything better. Falling back to base-16 is
// the closer match to that intent than dropping color entirely.
func downgradeSGR(profile termenv.Profile, code string) string {
	n, ok := sgr256Foreground(code)
	if !ok {
		return code
	}
	if profile == termenv.Ascii {
		profile = termenv.ANSI
	}
	seq := profile.Convert(termenv.ANSI256Color(n)).Sequence(false)
	if seq == "" {
		return code
	}
	return "\033[" + seq + "m"
}
