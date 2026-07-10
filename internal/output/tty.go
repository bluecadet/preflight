package output

import (
	"io"
	"os"

	"golang.org/x/term"
)

// isTTY returns true if w is os.Stdout or os.Stderr and the fd is a terminal.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// detectWidth returns the number of columns available on w's terminal so
// finished-task elapsed times can be right-aligned to the actual display
// width. When w is not a TTY (piped output, an in-memory buffer, etc.) it
// falls back to the fixed lineWidth default, keeping piped output
// deterministic and greppable.
func detectWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok || !isTTY(w) {
		return lineWidth
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil || cols <= 0 {
		return lineWidth
	}
	return cols
}
