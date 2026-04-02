package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// newTextRenderer creates a TextRenderer with color disabled (non-TTY writer).
func newTextRenderer(w *bytes.Buffer) *TextRenderer {
	return &TextRenderer{w: w, color: false, taskOutput: make(map[string][]string)}
}

func newVerboseTextRenderer(w *bytes.Buffer) *TextRenderer {
	return &TextRenderer{w: w, color: false, verbose: true, taskOutput: make(map[string][]string)}
}

func TestTextRenderer_PlayStart(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(PlayStartEvent{PlayName: "lobby"})

	out := buf.String()
	if !strings.Contains(out, "PLAY [lobby]") {
		t.Errorf("expected PLAY [lobby] in output, got: %q", out)
	}
	if !strings.Contains(out, "*") {
		t.Errorf("expected fill characters (*) in output, got: %q", out)
	}
}

func TestTextRenderer_TaskResultOK(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(TaskResultEvent{
		TaskName: "preflight/kiosk-mode : Disable Windows Update",
		Target:   "lobby-pc-01",
		Status:   "ok",
		Message:  "no change",
	})

	out := buf.String()
	if !strings.Contains(out, "ok") {
		t.Errorf("expected 'ok' in output, got: %q", out)
	}
	if !strings.Contains(out, "TASK [preflight/kiosk-mode : Disable Windows Update]") {
		t.Errorf("expected task name in output, got: %q", out)
	}
}

func TestTextRenderer_TaskResultChanged(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(TaskResultEvent{
		TaskName: "preflight/kiosk-mode : Set shell to app",
		Target:   "lobby-pc-01",
		Status:   "changed",
	})

	out := buf.String()
	if !strings.Contains(out, "changed") {
		t.Errorf("expected 'changed' in output, got: %q", out)
	}
}

func TestTextRenderer_PlayEnd(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(PlayEndEvent{
		Target:       "lobby-pc-01",
		OKCount:      4,
		ChangedCount: 2,
		FailedCount:  1,
		SkippedCount: 0,
	})

	out := buf.String()
	if !strings.Contains(out, "PLAY RECAP") {
		t.Errorf("expected PLAY RECAP in output, got: %q", out)
	}
	if !strings.Contains(out, "lobby-pc-01") {
		t.Errorf("expected target hostname in output, got: %q", out)
	}
	if !strings.Contains(out, "ok=4") {
		t.Errorf("expected ok=4 in output, got: %q", out)
	}
	if !strings.Contains(out, "changed=2") {
		t.Errorf("expected changed=2 in output, got: %q", out)
	}
	if !strings.Contains(out, "failed=1") {
		t.Errorf("expected failed=1 in output, got: %q", out)
	}
	if !strings.Contains(out, "skipped=0") {
		t.Errorf("expected skipped=0 in output, got: %q", out)
	}
}

func TestJSONRenderer_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.Emit(TaskResultEvent{
		TaskName: "Configure firewall",
		Target:   "lobby-pc-01",
		Status:   "ok",
		Message:  "",
	})
	r.Emit(PlayEndEvent{
		Target:       "lobby-pc-01",
		OKCount:      1,
		ChangedCount: 0,
		FailedCount:  0,
		SkippedCount: 0,
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d: %q", len(lines), buf.String())
	}

	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v — %q", i, err, line)
		}
	}

	// Check first line fields.
	var first map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	if first["type"] != string(EventTaskResult) {
		t.Errorf("expected type=%q, got %q", EventTaskResult, first["type"])
	}
	if first["task"] != "Configure firewall" {
		t.Errorf("expected task=Configure firewall, got %q", first["task"])
	}
	if first["target"] != "lobby-pc-01" {
		t.Errorf("expected target=lobby-pc-01, got %q", first["target"])
	}
	if _, ok := first["ts"]; !ok {
		t.Error("expected ts field in JSON output")
	}

	// play_end should include counts.
	var second map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if second["type"] != string(EventPlayEnd) {
		t.Errorf("expected type=%q, got %q", EventPlayEnd, second["type"])
	}
	if _, ok := second["ok_count"]; !ok {
		t.Error("expected ok_count in play_end JSON line")
	}
}

func TestTextRenderer_TaskOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(TaskOutputEvent{
		Lines: []string{"line1", "line2"},
	})

	out := buf.String()
	if !strings.Contains(out, "│") {
		t.Errorf("expected │ border character in output, got: %q", out)
	}
	if !strings.Contains(out, "line1") {
		t.Errorf("expected 'line1' in output, got: %q", out)
	}
	if !strings.Contains(out, "line2") {
		t.Errorf("expected 'line2' in output, got: %q", out)
	}
}

func TestTextRenderer_DefaultHidesSuccessfulTaskOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)

	r.Emit(TaskOutputEvent{
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Lines:    []string{"line1", "line2"},
	})
	r.Emit(TaskResultEvent{
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Status:   "changed",
		Message:  "task completed",
	})

	out := buf.String()
	if strings.Contains(out, "line1") || strings.Contains(out, "line2") {
		t.Fatalf("expected successful task output to stay hidden by default, got %q", out)
	}
}

func TestTextRenderer_VerboseShowsSuccessfulTaskOutputBelowTaskResult(t *testing.T) {
	var buf bytes.Buffer
	r := newVerboseTextRenderer(&buf)

	r.Emit(TaskOutputEvent{
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Lines:    []string{"line1", "line2"},
	})
	r.Emit(TaskResultEvent{
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Status:   "changed",
		Message:  "task completed",
	})

	out := buf.String()
	taskPos := strings.Index(out, "TASK [Run smoke test]")
	linePos := strings.Index(out, "line1")
	if taskPos < 0 || linePos < 0 {
		t.Fatalf("expected task line and buffered output, got %q", out)
	}
	if linePos < taskPos {
		t.Fatalf("expected verbose task output below the task result, got %q", out)
	}
}

func TestTextRenderer_FailedTaskIncludesOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(TaskOutputEvent{
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Lines:    []string{"Launching kiosk application..."},
	})
	r.Emit(TaskResultEvent{
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Status:   "failed",
		Message:  "process exited with code 1",
		Output:   []string{"Launching kiosk application...", "Smoke test timeout after 15s"},
	})

	out := buf.String()
	if !strings.Contains(out, "TASK [Run smoke test]") {
		t.Fatalf("expected task header in output, got: %q", out)
	}
	if !strings.Contains(out, "Launching kiosk application...") {
		t.Errorf("expected first failure log in output, got: %q", out)
	}
	if !strings.Contains(out, "Smoke test timeout after 15s") {
		t.Errorf("expected second failure log in output, got: %q", out)
	}
	if strings.Count(out, "Launching kiosk application...") != 1 {
		t.Errorf("expected failure logs not to be duplicated, got: %q", out)
	}
}

func TestTextRenderer_BuffersOutputPerTargetAndTask(t *testing.T) {
	var buf bytes.Buffer
	r := newVerboseTextRenderer(&buf)

	r.Emit(TaskOutputEvent{
		Target:   "gallery-01",
		TaskID:   "sync-assets",
		TaskName: "Sync assets on gallery-01",
		Lines:    []string{"gallery-01 line"},
	})
	r.Emit(TaskOutputEvent{
		Target:   "gallery-02",
		TaskID:   "sync-assets",
		TaskName: "Sync assets on gallery-02",
		Lines:    []string{"gallery-02 line"},
	})
	r.Emit(TaskResultEvent{
		Target:   "gallery-02",
		TaskID:   "sync-assets",
		TaskName: "Sync assets on gallery-02",
		Status:   "changed",
	})
	r.Emit(TaskResultEvent{
		Target:   "gallery-01",
		TaskID:   "sync-assets",
		TaskName: "Sync assets on gallery-01",
		Status:   "changed",
	})

	out := buf.String()
	host2TaskPos := strings.Index(out, "TASK [Sync assets on gallery-02]")
	host2LinePos := strings.Index(out, "gallery-02 line")
	host1TaskPos := strings.Index(out, "TASK [Sync assets on gallery-01]")
	host1LinePos := strings.Index(out, "gallery-01 line")
	if host2TaskPos < 0 || host2LinePos < 0 || host1TaskPos < 0 || host1LinePos < 0 {
		t.Fatalf("expected per-host task output, got %q", out)
	}
	if host2LinePos < host2TaskPos {
		t.Fatalf("expected gallery-02 output below its task result, got %q", out)
	}
	if host1LinePos < host1TaskPos {
		t.Fatalf("expected gallery-01 output below its task result, got %q", out)
	}
	if strings.Contains(out[host2TaskPos:host1TaskPos], "gallery-01 line") {
		t.Fatalf("expected gallery-01 output not to leak under gallery-02 task, got %q", out)
	}
}

func TestJSONRenderer_TaskOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.Emit(TaskOutputEvent{
		TaskID: "task-1",
		Target: "host-a",
		Lines:  []string{"hello"},
	})

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v — %q", err, buf.String())
	}
	if m["type"] != string(EventTaskOutput) {
		t.Errorf("expected type=%q, got %q", EventTaskOutput, m["type"])
	}
	if m["task_id"] != "task-1" {
		t.Errorf("expected task_id=%q, got %q", "task-1", m["task_id"])
	}
	if m["target"] != "host-a" {
		t.Errorf("expected target=%q, got %q", "host-a", m["target"])
	}
	lines, ok := m["lines"].([]any)
	if !ok {
		t.Fatalf("expected 'lines' field to be an array, got: %v", m["lines"])
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if lines[0] != "hello" {
		t.Errorf("expected lines[0]=%q, got %q", "hello", lines[0])
	}
}

func TestFactory_New(t *testing.T) {
	var buf bytes.Buffer
	if _, ok := New(FormatText, &buf).(*TextRenderer); !ok {
		t.Error("expected TextRenderer for FormatText")
	}
	if _, ok := New(FormatJSON, &buf).(*JSONRenderer); !ok {
		t.Error("expected JSONRenderer for FormatJSON")
	}
	if _, ok := New(FormatJSONL, &buf).(*JSONRenderer); !ok {
		t.Error("expected JSONRenderer for FormatJSONL")
	}
	// Unknown format falls back to text.
	if _, ok := New("unknown", &buf).(*TextRenderer); !ok {
		t.Error("expected TextRenderer for unknown format")
	}
}
