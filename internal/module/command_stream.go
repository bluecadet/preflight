package module

import (
	"context"
	"io"
	"os/exec"
	"strings"

	"github.com/bluecadet/preflight/internal/tasklog"
)

const maxCommandBufferBytes = 32 * 1024

type streamRecorder struct {
	ctx     context.Context
	stream  string
	partial string
	buffer  strings.Builder
}

func newStreamRecorder(ctx context.Context, stream string) *streamRecorder {
	return &streamRecorder{
		ctx:    ctx,
		stream: stream,
	}
}

func (r *streamRecorder) Write(p []byte) (int, error) {
	text := string(p)
	r.appendBuffer(text)

	chunk := r.partial + text
	chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
	chunk = strings.ReplaceAll(chunk, "\r", "\n")

	parts := strings.Split(chunk, "\n")
	r.partial = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		if line == "" {
			continue
		}
		tasklog.Emit(r.ctx, r.stream, line)
	}

	return len(p), nil
}

func (r *streamRecorder) Flush() {
	if r.partial == "" {
		return
	}
	tasklog.Emit(r.ctx, r.stream, r.partial)
	r.partial = ""
}

func (r *streamRecorder) String() string {
	return strings.TrimSpace(r.buffer.String())
}

func (r *streamRecorder) appendBuffer(text string) {
	if text == "" {
		return
	}

	r.buffer.WriteString(text)
	current := r.buffer.String()
	if len(current) <= maxCommandBufferBytes {
		return
	}

	r.buffer.Reset()
	r.buffer.WriteString(current[len(current)-maxCommandBufferBytes:])
}

func runCommandStreaming(ctx context.Context, cmd *exec.Cmd) (string, string, error) {
	stdout := newStreamRecorder(ctx, "stdout")
	stderr := newStreamRecorder(ctx, "stderr")
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	stdout.Flush()
	stderr.Flush()

	return stdout.String(), stderr.String(), err
}

func joinCommandOutput(stdout, stderr string) string {
	switch {
	case stdout != "" && stderr != "":
		return stdout + "\n" + stderr
	case stdout != "":
		return stdout
	default:
		return stderr
	}
}

func copyStreamedOutput(ctx context.Context, stream string, reader io.Reader) (string, error) {
	recorder := newStreamRecorder(ctx, stream)
	if _, err := io.Copy(recorder, reader); err != nil {
		return "", err
	}
	recorder.Flush()
	return recorder.String(), nil
}
