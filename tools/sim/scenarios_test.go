package main

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/bluecadet/preflight/internal/output"
)

type recordingRenderer struct {
	mu     sync.Mutex
	events []output.Event
}

func (r *recordingRenderer) Emit(event output.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingRenderer) Close() {}

func (r *recordingRenderer) snapshot() []output.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func TestRunStreamingEmitsMoreThanThreePreviewLines(t *testing.T) {
	rec := &recordingRenderer{}

	runStreaming(rec, 0)

	var lines []string
	for _, event := range rec.snapshot() {
		if event.Type != output.EventTaskOutput || event.TaskID != "download-package" {
			continue
		}
		lines = append(lines, event.Lines...)
	}
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 streamed lines for download-package, got %d: %v", len(lines), lines)
	}
}

func TestRunFailuresIncludesCapturedLogsForFailedTask(t *testing.T) {
	rec := &recordingRenderer{}

	runFailures(rec, 0)

	for _, event := range rec.snapshot() {
		if event.Type != output.EventTaskResult || event.TaskID != "run-migrations" {
			continue
		}
		if event.Status != "failed" {
			t.Fatalf("expected failed status, got %q", event.Status)
		}
		if len(event.Output) == 0 {
			t.Fatal("expected captured output on failed task result")
		}
		if !slices.Contains(event.Output, "Migration aborted: connection refused: postgres:5432") {
			t.Fatalf("expected failure diagnostics in output block, got %v", event.Output)
		}
		return
	}

	t.Fatal("failed task result not found")
}

func TestRunStreamingMultiHostStreamsAcrossHosts(t *testing.T) {
	rec := &recordingRenderer{}

	runStreamingMultiHost(rec, time.Millisecond)

	hosts := make(map[string]struct{})
	for _, event := range rec.snapshot() {
		if event.Type != output.EventTaskOutput {
			continue
		}
		hosts[event.Target] = struct{}{}
	}
	if len(hosts) < 2 {
		t.Fatalf("expected streamed output from multiple hosts, got %d hosts", len(hosts))
	}
}
