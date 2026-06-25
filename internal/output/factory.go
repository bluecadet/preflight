package output

import (
	"io"
	"os"
)

// Format selects which renderer New returns.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatTUI  Format = "tui"
)

// Options controls optional renderer behavior.
type Options struct {
	Verbose      bool
	Mode         string
	MaxFailLines int
	RunDir       string
}

// New returns a Renderer for the requested format writing to w.
// Any unrecognised format falls back to TextRenderer.
func New(format Format, w io.Writer) Renderer {
	return NewWithOptions(format, w, Options{})
}

// NewWithOptions returns a Renderer for the requested format writing to w.
// Any unrecognised format falls back to TextRenderer.
func NewWithOptions(format Format, w io.Writer, opts Options) Renderer {
	switch format {
	case FormatJSON:
		return NewJSONRenderer(w)
	case FormatTUI:
		return NewTUIRendererWithOptions(w, opts)
	default:
		return NewTextRendererWithOptions(w, opts)
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
