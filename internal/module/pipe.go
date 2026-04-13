package module

import (
	"bufio"
	"io"

	"github.com/bluecadet/preflight/internal/target"
)

type PipeResult struct {
	Lines   []string
	ScanErr error
}

func NewOutputPipe(onOutput target.OutputFunc) (w *io.PipeWriter, done <-chan PipeResult) {
	pr, pw := io.Pipe()
	ch := make(chan PipeResult, 1)
	go func() {
		var r PipeResult
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			r.Lines = append(r.Lines, line)
			if onOutput != nil {
				onOutput(line)
			}
		}
		r.ScanErr = scanner.Err()
		ch <- r
	}()
	return pw, ch
}
