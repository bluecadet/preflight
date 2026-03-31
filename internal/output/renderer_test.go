package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// newTextRenderer creates a TextRenderer with color disabled (non-TTY writer).
func newTextRenderer(w *bytes.Buffer) *TextRenderer {
	return &TextRenderer{w: w, color: false}
}

func TestTextRenderer_PlayStart(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(Event{Type: EventPlayStart, PlayName: "lobby"})

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
	r.Emit(Event{
		Type:     EventTaskResult,
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
	r.Emit(Event{
		Type:     EventTaskResult,
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
	r.Emit(Event{
		Type:         EventPlayEnd,
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
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v — %q", i, err, line)
		}
	}

	// Check first line fields.
	var first map[string]interface{}
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
	var second map[string]interface{}
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if second["type"] != string(EventPlayEnd) {
		t.Errorf("expected type=%q, got %q", EventPlayEnd, second["type"])
	}
	if _, ok := second["ok_count"]; !ok {
		t.Error("expected ok_count in play_end JSON line")
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
