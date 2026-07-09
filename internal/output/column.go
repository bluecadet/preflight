package output

import "strings"

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

// AlignLeft pads s with spaces on the right so that the byte length
// equals width. If s is already longer, it is returned unchanged.
func AlignLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// AlignRight pads s with spaces on the left so that the byte length
// equals width. If s is already longer, it is returned unchanged.
//
// Currently unused but preserved as a shared primitive. It is the natural
// counterpart to AlignLeft and would be needed if any section right-aligns
// a column (e.g., a numeric column in a table).
func AlignRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
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
