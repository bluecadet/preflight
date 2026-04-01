package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// newTextRenderer creates a TextRenderer with color disabled (non-TTY writer).
func newTextRenderer(w *bytes.Buffer) *TextRenderer {
	return NewTextRendererWithOptions(w, Options{})
}

func TestTextRenderer_PlayStart(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(Event{Type: EventPlayStart, PlayName: "lobby"})

	out := buf.String()
	if !strings.Contains(out, "play: lobby") {
		t.Errorf("expected play header in output, got: %q", out)
	}
}

func TestTextRenderer_TaskResultOK(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(Event{
		Type:     EventTaskResult,
		TaskPath: "1",
		TaskName: "preflight/kiosk-mode : Disable Windows Update",
		Target:   "lobby-pc-01",
		Status:   "ok",
		Message:  "no change",
	})

	out := buf.String()
	if !strings.Contains(out, "✓") {
		t.Errorf("expected success glyph in output, got: %q", out)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "no change") {
		t.Errorf("expected task path and message in output, got: %q", out)
	}
	if !strings.Contains(out, "preflight/kiosk-mode : Disable Windows Update") {
		t.Errorf("expected task name in output, got: %q", out)
	}
}

func TestTextRenderer_TaskResultChanged(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(Event{
		Type:     EventTaskResult,
		TaskName: "preflight/kiosk-mode : Set shell to app",
		Target:   "lobby-pc-01",
		Status:   "changed",
	})

	out := buf.String()
	if !strings.Contains(out, "~") {
		t.Errorf("expected change glyph in output, got: %q", out)
	}
}

func TestTextRenderer_PlayEnd(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(Event{
		Type:         EventPlayEnd,
		Target:       "lobby-pc-01",
		OKCount:      4,
		ChangedCount: 2,
		FailedCount:  1,
		SkippedCount: 0,
	})

	out := buf.String()
	if !strings.Contains(out, "recap") {
		t.Errorf("expected recap output, got: %q", out)
	}
	if !strings.Contains(out, "ok 4") {
		t.Errorf("expected ok count in output, got: %q", out)
	}
	if !strings.Contains(out, "changed 2") {
		t.Errorf("expected changed count in output, got: %q", out)
	}
	if !strings.Contains(out, "failed 1") {
		t.Errorf("expected failed count in output, got: %q", out)
	}
	if !strings.Contains(out, "skipped 0") {
		t.Errorf("expected skipped count in output, got: %q", out)
	}
}

func TestJSONRenderer_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.Emit(Event{
		Type:     EventTaskResult,
		TaskName: "Configure firewall",
		Target:   "lobby-pc-01",
		Status:   "ok",
		Message:  "",
	})
	r.Emit(Event{
		Type:         EventPlayEnd,
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

func TestTextRenderer_VerboseTaskLog(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextRendererWithOptions(&buf, Options{Verbose: true})

	r.Emit(Event{
		Type:   EventTaskLog,
		Target: "host-a",
		Stream: "stdout",
		Line:   "hello from task",
	})

	out := buf.String()
	if !strings.Contains(out, "out> hello from task") {
		t.Fatalf("expected verbose task log output, got %q", out)
	}
}

func TestJSONRenderer_TaskLogRequiresVerbose(t *testing.T) {
	var quiet bytes.Buffer
	NewJSONRenderer(&quiet).Emit(Event{
		Type:   EventTaskLog,
		TaskID: "task-1",
		Line:   "hidden",
	})
	if strings.TrimSpace(quiet.String()) != "" {
		t.Fatalf("expected default JSON renderer to suppress task logs, got %q", quiet.String())
	}

	var verbose bytes.Buffer
	NewJSONRendererWithOptions(&verbose, Options{Verbose: true}).Emit(Event{
		Type:   EventTaskLog,
		TaskID: "task-1",
		Stream: "stdout",
		Line:   "visible",
	})
	if !strings.Contains(verbose.String(), `"line":"visible"`) {
		t.Fatalf("expected verbose JSON renderer to include task logs, got %q", verbose.String())
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
	if _, ok := New(FormatTUI, &buf).(*LiveRunRenderer); !ok {
		t.Error("expected LiveRunRenderer for FormatTUI")
	}
	// Unknown format falls back to text.
	if _, ok := New("unknown", &buf).(*TextRenderer); !ok {
		t.Error("expected TextRenderer for unknown format")
	}
}
