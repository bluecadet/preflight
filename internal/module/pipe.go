package module

import (
	"bufio"
	"errors"
	"io"
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
