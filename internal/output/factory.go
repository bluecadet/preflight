package output

import "io"

// Format selects which renderer New returns.
type Format string

const (
	FormatText  Format = "text"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
)

// New returns a Renderer for the requested format writing to w.
// FormatJSON and FormatJSONL are equivalent (both produce newline-delimited JSON).
// Any unrecognised format falls back to TextRenderer.
func New(format Format, w io.Writer) Renderer {
	switch format {
	case FormatJSON, FormatJSONL:
		return NewJSONRenderer(w)
	default:
		return NewTextRenderer(w)
	}
}
