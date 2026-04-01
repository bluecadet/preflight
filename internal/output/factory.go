package output

import (
	"io"
	"os"
)

// Options configure renderer behavior.
type Options struct {
	Verbose   bool
	Input     io.Reader
	Interrupt func()
	Command   string
}

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
	return NewWithOptions(format, w, Options{})
}

// NewWithOptions returns a Renderer for the requested format writing to w.
func NewWithOptions(format Format, w io.Writer, options Options) Renderer {
	switch format {
	case FormatJSON, FormatJSONL:
		return NewJSONRendererWithOptions(w, options)
	case FormatTUI:
		return NewTUIRendererWithOptions(w, options)
	default:
		return NewTextRendererWithOptions(w, options)
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
