package logging

import (
	"io"
	"log/slog"
	"os"
)

// Init configures the default slog logger. When verbose is true, the level is
// set to Debug; otherwise it is Warn (suppressing Info-level noise during
// normal operation).
func Init(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

// Discard configures a silent logger (useful in tests).
func Discard() {
	handler := slog.NewTextHandler(io.Discard, nil)
	slog.SetDefault(slog.New(handler))
}
