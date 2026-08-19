package module

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

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
		// onOutput is caller-supplied (ultimately wired up through several
		// layers to output rendering) and runs once per line of command
		// output; a panic in it must fail only this command's output
		// collection, not the whole process. ch is buffered, and this is
		// the only send in this goroutine's lifetime, so it can never
		// block even if the caller below has stopped reading it.
		defer func() {
			if rec := recover(); rec != nil {
				r.ScanErr = fmt.Errorf("read output: panic: %v\n%s", rec, debug.Stack())
				ch <- r
			}
		}()
		reader := bufio.NewReader(pr)
		for {
			line, err := readOutputPipeLine(reader)
			if err != nil && line == "" {
				if !errors.Is(err, io.EOF) {
					r.ScanErr = err
				}
				break
			}
			r.Lines = append(r.Lines, line)
			if onOutput != nil {
				onOutput(line)
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					r.ScanErr = err
				}
				break
			}
		}
		ch <- r
	}()
	return pw, ch
}

func readOutputPipeLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, err
}
