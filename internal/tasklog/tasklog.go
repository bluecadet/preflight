package tasklog

import (
	"context"
	"fmt"
	"strings"
)

// Entry is a single task-scoped log line emitted during execution.
type Entry struct {
	Target   string
	TaskID   string
	TaskPath string
	TaskName string
	Module   string
	Stream   string
	Line     string
}

// Sink receives task-scoped log entries.
type Sink interface {
	EmitTaskLog(Entry)
}

type contextKey struct{}

type contextValue struct {
	sink  Sink
	entry Entry
}

// WithTask attaches a task-scoped logging sink to ctx.
func WithTask(ctx context.Context, sink Sink, entry Entry) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, contextValue{
		sink:  sink,
		entry: entry,
	})
}

// Emit writes a single line to the task log sink attached to ctx.
func Emit(ctx context.Context, stream, line string) {
	value, ok := ctx.Value(contextKey{}).(contextValue)
	if !ok || value.sink == nil {
		return
	}
	trimmed := strings.TrimRight(line, "\r\n")
	value.entry.Stream = stream
	value.entry.Line = trimmed
	value.sink.EmitTaskLog(value.entry)
}

// EmitLines splits text into individual lines and emits each one separately.
func EmitLines(ctx context.Context, stream, text string) {
	if text == "" {
		return
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for line := range strings.SplitSeq(normalized, "\n") {
		if line == "" {
			continue
		}
		Emit(ctx, stream, line)
	}
}

// Infof emits an informational line for the current task.
func Infof(ctx context.Context, format string, args ...any) {
	Emit(ctx, "info", fmt.Sprintf(format, args...))
}

// Errorf emits an error/debugging line for the current task.
func Errorf(ctx context.Context, format string, args ...any) {
	Emit(ctx, "stderr", fmt.Sprintf(format, args...))
}
