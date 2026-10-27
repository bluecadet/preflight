package output

import (
	"strings"
	"unicode/utf8"
)

// Column helpers for pad-to-width, left/right alignment, and truncation.
//
// These are the shared primitives used by both the TUI and text renderers
// to align task lines, roster rows, card pairs, and tables. They use plain
// byte-length (len) for width measurement, which matches the existing
// layout conventions in the codebase. For ANSI-aware width measurement,
// callers should apply lipgloss.Width separately.

// PadLine justifies left and right content to a total width by filling the
// gap between them with spaces. If right is empty, left is returned as-is
// (trimmed but without padding). If the gap is too narrow, at least one
// space is inserted.
func PadLine(left, right string, width int) string {
	left = strings.TrimRight(left, " \t")
	right = strings.TrimSpace(right)
	if right == "" {
		return left
	}
	spaces := max(width-len(left)-len(right), 1)
	return left + strings.Repeat(" ", spaces) + right
}

// AlignLeft pads s with spaces on the right so that its rune count equals
// width. If s is already longer, it is returned unchanged.
func AlignLeft(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// Truncate shortens s to at most max runes (not bytes), appending "…" when
// truncation occurs. If max is less than 2, a single "…" is returned.
func Truncate(s string, max int) string {
	if max <= 1 {
		return "…"
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
