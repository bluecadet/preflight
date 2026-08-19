package output

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunLogSink_CloseReportsRunJSONWriteFailure forces writeRunJSON's
// final os.Rename to fail (by pre-occupying run.json's path with a
// non-empty directory, which os.Rename refuses to replace with a file on
// every platform) and asserts Close surfaces that error rather than
// silently producing a run directory with a JSONL log but no run.json —
// the exact failure mode the Close() error-return refactor exists to
// prevent.
func TestRunLogSink_CloseReportsRunJSONWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	sink, err := NewRunLogSink("close-fail-run", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}

	sink.Emit(RunSummaryEvent{Status: "success", OKCount: 1, ElapsedMs: 100})

	// Occupy run.json's destination path with a non-empty directory so the
	// final os.Rename(tmpPath, "run.json") inside writeRunJSON cannot
	// succeed, regardless of platform.
	blockedPath := filepath.Join(dir, "run.json")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", blockedPath, err)
	}
	if err := os.WriteFile(filepath.Join(blockedPath, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(occupied): %v", err)
	}

	err = sink.Close()
	if err == nil {
		t.Fatal("expected Close to report the run.json write failure, got nil")
	}
	t.Logf("Close correctly reported: %v", err)

	// The JSONL log itself should still have been flushed and closed
	// successfully — only the run.json summary write failed.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected run.jsonl to exist despite the run.json failure: %v", statErr)
	}

	// run.json's path must remain the untouched blocking directory: the
	// failed write must not leave a corrupt or partial file behind.
	if info, statErr := os.Stat(blockedPath); statErr != nil || !info.IsDir() {
		t.Fatalf("expected run.json's path to remain the untouched blocking directory, info=%v err=%v", info, statErr)
	}
}

// TestRunLogSink_CloseReturnsNilOnSuccess is the control case: a normal
// Close with a writable run directory returns no error.
func TestRunLogSink_CloseReturnsNilOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	sink, err := NewRunLogSink("close-ok-run", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}
	sink.Emit(RunSummaryEvent{Status: "success", OKCount: 1, ElapsedMs: 100})

	if err := sink.Close(); err != nil {
		t.Fatalf("Close returned an unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "run.json")); statErr != nil {
		t.Fatalf("expected run.json to be written on success: %v", statErr)
	}
}

// TestBus_ClosePropagatesRunLogSinkError confirms the fan-out Bus does not
// swallow a wrapped sink's Close error, since cmd/apply.go's runPlaybook
// relies on exactly this to reach the process exit status.
func TestBus_ClosePropagatesRunLogSinkError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	sink, err := NewRunLogSink("bus-close-fail-run", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}
	sink.Emit(RunSummaryEvent{Status: "success", OKCount: 1, ElapsedMs: 100})

	blockedPath := filepath.Join(dir, "run.json")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", blockedPath, err)
	}
	if err := os.WriteFile(filepath.Join(blockedPath, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(occupied): %v", err)
	}

	bus := NewBus(Synchronized(sink))
	if err := bus.Close(); err == nil {
		t.Fatal("expected Bus.Close to propagate the wrapped RunLogSink's Close error")
	}
}
