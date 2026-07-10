package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunLogSink_TaskFailedReasonField locks in that task_failed carries a
// `reason` field when the event has one, and omits it when empty.
func TestRunLogSink_TaskFailedReasonField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	sink, err := NewRunLogSink("reason-run", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}
	sink.Emit(TaskFailedEvent{
		Target:      "h",
		TaskID:      "t1",
		TaskName:    "do thing",
		FailMessage: "boom",
		Reason:      "unsupported_on_runtime",
	})
	sink.Emit(TaskFailedEvent{
		Target:      "h",
		TaskID:      "t2",
		TaskName:    "do other",
		FailMessage: "plain failure",
	})
	sink.Close()

	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 events, got %d", len(lines))
	}
	if !strings.Contains(lines[0], `"reason":"unsupported_on_runtime"`) {
		t.Errorf("first event missing reason field: %s", lines[0])
	}
	if strings.Contains(lines[1], `"reason"`) {
		t.Errorf("second event should omit reason: %s", lines[1])
	}
}
