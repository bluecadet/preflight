package output

import (
	"io"
	"os"
)

// Format selects which renderer New returns.
type Format string

const (
	FormatText  Format = "text"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatTUI   Format = "tui"
)

// New returns a Renderer for the requested format writing to w.
// FormatJSON and FormatJSONL are equivalent (both produce newline-delimited JSON).
// Any unrecognised format falls back to TextRenderer.
func New(format Format, w io.Writer) Renderer {
	switch format {
	case FormatJSON, FormatJSONL:
		return NewJSONRenderer(w)
	case FormatTUI:
		return NewTUIRenderer(w)
	default:
		return NewTextRenderer(w)
	}
}

// AutoDetect returns FormatTUI if w is os.Stdout and stdout is a TTY,
// otherwise FormatText.
func AutoDetect(w io.Writer) Format {
	if w == os.Stdout && isTTY(w) {
		return FormatTUI
	}
	return FormatText
}
