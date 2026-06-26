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
		e, ok := event.(output.TaskOutputEvent)
		if !ok || e.TaskID != "download-package" {
			continue
		}
		lines = append(lines, e.Lines...)
	}
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 streamed lines for download-package, got %d: %v", len(lines), lines)
	}
}

func TestRunFailuresIncludesCapturedLogsForFailedTask(t *testing.T) {
	rec := &recordingRenderer{}

	runFailures(rec, 0)

	for _, event := range rec.snapshot() {
		e, ok := event.(output.TaskFailedEvent)
		if !ok || e.TaskID != "run-migrations" {
			continue
		}
		if e.FailMessage == "" {
			t.Fatal("expected fail message on failed task event")
		}
		if len(e.Output) == 0 {
			t.Fatal("expected captured output on failed task")
		}
		if !slices.Contains(e.Output, "Migration aborted: connection refused: postgres:5432") {
			t.Fatalf("expected failure diagnostics in output block, got %v", e.Output)
		}
		return
	}

	t.Fatal("failed task event not found")
}

func TestRunStreamingCapturesOutputForSuccessfulTaskResults(t *testing.T) {
	rec := &recordingRenderer{}

	runStreaming(rec, 0)

	for _, event := range rec.snapshot() {
		e, ok := event.(output.TaskChangedEvent)
		if !ok || e.TaskID != "download-package" {
			continue
		}
		if e.TaskName == "" {
			t.Fatalf("expected task name on changed event")
		}
		return
	}

	t.Fatal("successful streamed task changed event not found")
}

func TestRunStreamingMultiHostStreamsAcrossHosts(t *testing.T) {
	rec := &recordingRenderer{}

	runStreamingMultiHost(rec, time.Millisecond)

	var hostNames []string
	for _, event := range rec.snapshot() {
		switch e := event.(type) {
		case output.TaskStartedEvent:
			if !slices.Contains(hostNames, e.Target) {
				hostNames = append(hostNames, e.Target)
			}
		}
	}

	expectedHosts := []string{"gallery-01", "gallery-02", "gallery-03"}
	for _, h := range expectedHosts {
		if !slices.Contains(hostNames, h) {
			t.Fatalf("expected host %q to appear in task_started events, got %v", h, hostNames)
		}
	}
}