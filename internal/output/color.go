package output

import (
	"io"
	"os"
	"strings"
)

// ColorMode controls whether terminal output includes ANSI color codes.
type ColorMode int

const (
	// ColorAuto enables color when stdout is a TTY and no color-suppressing
	// env vars (NO_COLOR, CI) are set.
	ColorAuto ColorMode = iota
	// ColorAlways forces color on regardless of TTY state.
	ColorAlways
	// ColorNever disables color regardless of TTY state.
	ColorNever
)

// DetectColor returns the effective ColorMode based on the CLI --color flag,
// --no-color flag, the NO_COLOR environment variable, and whether stdout is a
// terminal. Precedence:
//
//	NO_COLOR > --no-color > --color=auto|always|never > CI/isatty autodetect
//
// The flagValue parameter is the value of --color ("auto", "always", "never", or "").
// The noColorFlag is true when --no-color was explicitly passed.
func DetectColor(flagValue string, noColorFlag bool, stdout io.Writer) ColorMode {
	// 1. NO_COLOR env var — highest precedence.
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return ColorNever
	}
	// 2. --no-color flag.
	if noColorFlag {
		return ColorNever
	}
	// 3. --color=always|never|auto.
	switch strings.ToLower(flagValue) {
	case "never":
		return ColorNever
	case "always":
		return ColorAlways
	}
	// 4. CI env var — a strong hint that stdout may not be a real terminal.
	if _, ok := os.LookupEnv("CI"); ok {
		return ColorNever
	}
	// 5. isatty autodetect.
	if isTTY(stdout) {
		return ColorAlways
	}
	return ColorNever
}

// UseColor reports whether this ColorMode means "render with ANSI colors".
func (c ColorMode) UseColor() bool {
	return c == ColorAlways
}