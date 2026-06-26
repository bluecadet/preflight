package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// newTextRenderer creates a TextRenderer with color disabled (non-TTY writer).
func newTextRenderer(w *bytes.Buffer) *TextRenderer {
	return &TextRenderer{
		w:            w,
		color:        false,
		maxFailLines: defaultFailureOutputLimit,
		activeTasks:  make(map[string]time.Time),
		projection:   NewRunProjection(),
	}
}

func newVerboseTextRenderer(w *bytes.Buffer) *TextRenderer {
	return &TextRenderer{
		w:            w,
		color:        false,
		verbose:      true,
		maxFailLines: defaultFailureOutputLimit,
		activeTasks:  make(map[string]time.Time),
		projection:   NewRunProjection(),
	}
}

func TestTextRenderer_RunStart(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "playbooks/lobby.yml",
		PlaybookName: "lobby",
		Targets:      []string{"lobby-pc-01"},
	})

	out := buf.String()
	if !strings.Contains(out, "Apply") {
		t.Errorf("expected Apply heading in output, got: %q", out)
	}
	if !strings.Contains(out, "playbook: playbooks/lobby.yml") {
		t.Errorf("expected playbook identity in output, got: %q", out)
	}
}

func TestTextRenderer_TaskOK(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test",
		Targets:      []string{"host-a"},
	})
	r.Emit(TaskStartedEvent{
		Target:   "host-a",
		TaskID:   "t1",
		TaskName: "preflight/kiosk-mode : Disable Windows Update",
	})
	r.Emit(TaskOKEvent{
		Target:    "host-a",
		TaskID:    "t1",
		TaskName:  "preflight/kiosk-mode : Disable Windows Update",
		ElapsedMs: 100,
	})

	out := buf.String()
	if !strings.Contains(out, "✓") {
		t.Errorf("expected ok glyph in output, got: %q", out)
	}
	if !strings.Contains(out, "preflight/kiosk-mode : Disable Windows Update") {
		t.Errorf("expected task name in output, got: %q", out)
	}
}

func TestTextRenderer_TaskChanged(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test",
		Targets:      []string{"host-a"},
	})
	r.Emit(TaskStartedEvent{
		Target:   "host-a",
		TaskID:   "t1",
		TaskName: "preflight/kiosk-mode : Set shell to app",
	})
	r.Emit(TaskChangedEvent{
		Target:    "host-a",
		TaskID:    "t1",
		TaskName:  "preflight/kiosk-mode : Set shell to app",
		ElapsedMs: 100,
	})

	out := buf.String()
	if !strings.Contains(out, "~") {
		t.Errorf("expected changed glyph in output, got: %q", out)
	}
}

func TestTextRenderer_SingleTargetOmitsRepeatedHostLabels(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextRendererWithOptions(&buf, Options{Mode: "apply"})
	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "playbooks/lobby.yml",
		PlaybookName: "lobby",
		Targets:      []string{"lobby-pc-01"},
	})
	r.Emit(TaskStartedEvent{
		Target:   "lobby-pc-01",
		TaskID:   "t1",
		TaskName: "Create content directory",
	})
	r.Emit(TaskOKEvent{
		Target:    "lobby-pc-01",
		TaskID:    "t1",
		TaskName:  "Create content directory",
		ElapsedMs: 100,
	})

	out := buf.String()
	if strings.Contains(out, "[lobby-pc-01]") {
		t.Fatalf("expected single-target rows to omit host label, got %q", out)
	}
	if !strings.Contains(out, "target: lobby-pc-01") {
		t.Fatalf("expected run intro to name the target, got %q", out)
	}
}

func TestJSONRenderer_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.Emit(TaskOKEvent{
		TaskName:  "Configure firewall",
		Target:    "lobby-pc-01",
		ElapsedMs: 100,
	})
	r.Emit(TargetCompleteEvent{
		Target:       "lobby-pc-01",
		Outcome:      "ok",
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
	if first["type"] != string(EventTaskOK) {
		t.Errorf("expected type=%q, got %q", EventTaskOK, first["type"])
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

	// target_complete should include target name.
	var second map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if second["type"] != string(EventTargetComplete) {
		t.Errorf("expected type=%q, got %q", EventTargetComplete, second["type"])
	}
	if _, ok := second["target"]; !ok {
		t.Error("expected target in target_complete JSON line")
	}
}

func TestTextRenderer_PluginInventorySecretLists(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(PluginListEvent{Entries: []PluginListEntry{{Name: "custom", Version: "1.0.0", Status: "ready", Path: "/tmp/preflight-plugin-custom"}}})
	r.Emit(InventoryListEvent{Hosts: []InventoryHostEntry{{Name: "kiosk-a", Address: "[IP_ADDRESS]", Transport: "winrm", Port: 5985, Groups: []string{"lab"}}}})
	r.Emit(SecretListEvent{Entries: []SecretListEntry{{Name: "api-token", File: "secrets/api-token.age"}}})

	out := buf.String()
	for _, needle := range []string{"NAME", "custom", "kiosk-a", "[IP_ADDRESS]", "api-token", "secrets/api-token.age"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected %q in output, got %q", needle, out)
		}
	}
}

func TestJSONRenderer_ListEvents(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.Emit(PluginListEvent{Entries: []PluginListEntry{{Name: "custom", Version: "1.0.0", Status: "ready", Path: "/tmp/preflight-plugin-custom"}}})
	r.Emit(InventoryListEvent{Hosts: []InventoryHostEntry{{Name: "kiosk-a", Address: "[IP_ADDRESS]", Transport: "winrm", Port: 5985, Groups: []string{"lab"}}}})
	r.Emit(SecretListEvent{Entries: []SecretListEntry{{Name: "api-token", File: "secrets/api-token.age"}}})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON lines, got %d: %q", len(lines), buf.String())
	}

	var plugin map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &plugin); err != nil {
		t.Fatalf("unmarshal plugin line: %v", err)
	}
	if plugin["type"] != string(EventPluginList) {
		t.Fatalf("expected type=%q, got %v", EventPluginList, plugin["type"])
	}
	plugins, ok := plugin["plugins"].([]any)
	if !ok || len(plugins) != 1 {
		t.Fatalf("expected one plugin entry, got %#v", plugin["plugins"])
	}

	var inventory map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &inventory); err != nil {
		t.Fatalf("unmarshal inventory line: %v", err)
	}
	if inventory["type"] != string(EventInventoryList) {
		t.Fatalf("expected type=%q, got %v", EventInventoryList, inventory["type"])
	}
	hosts, ok := inventory["hosts"].([]any)
	if !ok || len(hosts) != 1 {
		t.Fatalf("expected one host entry, got %#v", inventory["hosts"])
	}

	var secret map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &secret); err != nil {
		t.Fatalf("unmarshal secret line: %v", err)
	}
	if secret["type"] != string(EventSecretList) {
		t.Fatalf("expected type=%q, got %v", EventSecretList, secret["type"])
	}
	secrets, ok := secret["secrets"].([]any)
	if !ok || len(secrets) != 1 {
		t.Fatalf("expected one secret entry, got %#v", secret["secrets"])
	}
}

func TestTextRenderer_TaskOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(TaskOutputEvent{
		Lines: []string{"line1", "line2"},
	})

	out := buf.String()
	if !strings.Contains(out, "line1") {
		t.Errorf("expected 'line1' in output, got: %q", out)
	}
	if !strings.Contains(out, "line2") {
		t.Errorf("expected 'line2' in output, got: %q", out)
	}
}

func TestTextRenderer_FactsFormatsNestedValues(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(FactsEvent{
		Target: "exhibit-pc",
		Facts: map[string]any{
			"hostname": "EXHIBIT-01",
			"os": map[string]any{
				"name":    "Windows 11",
				"version": "10.0.26200",
				"build":   26200,
				"arch":    "arm64",
			},
			"disks": []any{
				map[string]any{
					"path":     "C:",
					"total_gb": 63.055660247802734,
					"free_gb":  23.208858489990234,
					"used_gb":  39.8468017578125,
				},
			},
		},
	})

	out := buf.String()
	if !strings.Contains(out, "os:\n") {
		t.Fatalf("expected nested os section, got %q", out)
	}
	if !strings.Contains(out, "  disks:\n") {
		t.Fatalf("expected disks section, got %q", out)
	}
	if !strings.Contains(out, "    - path: C:") {
		t.Fatalf("expected disk list entry, got %q", out)
	}
	if !strings.Contains(out, "      total_gb: 63.06") {
		t.Fatalf("expected rounded float formatting, got %q", out)
	}
}

func TestTextRenderer_FailedTaskIncludesOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test",
		Targets:      []string{"host-a"},
	})
	r.Emit(TaskStartedEvent{
		Target:   "host-a",
		TaskID:   "task-1",
		TaskName: "Run smoke test",
	})
	r.Emit(TaskOutputEvent{
		Target:   "host-a",
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Lines:    []string{"Launching kiosk application..."},
	})
	r.Emit(TaskFailedEvent{
		Target:      "host-a",
		TaskID:      "task-1",
		TaskName:    "Run smoke test",
		FailMessage: "process exited with code 1",
		Output:      []string{"Launching kiosk application...", "Smoke test timeout after 15s"},
	})

	out := buf.String()
	if !strings.Contains(out, "x Run smoke test") {
		t.Fatalf("expected task header in output, got: %q", out)
	}
	if !strings.Contains(out, "Launching kiosk application...") {
		t.Errorf("expected first failure log in output, got: %q", out)
	}
	if !strings.Contains(out, "Smoke test timeout after 15s") {
		t.Errorf("expected second failure log in output, got: %q", out)
	}
}

func TestTextRenderer_FailedTaskWrapsLongMessageAndOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	longMessage := strings.Repeat("failure-message ", 8)
	longOutput := strings.Repeat("verbose-output ", 8)

	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test",
		Targets:      []string{"host-a"},
	})
	r.Emit(TaskStartedEvent{
		Target:   "host-a",
		TaskID:   "task-1",
		TaskName: "Run smoke test",
	})
	r.Emit(TaskFailedEvent{
		Target:      "host-a",
		TaskID:      "task-1",
		TaskName:    "Run smoke test",
		FailMessage: longMessage,
		Output:      []string{longOutput},
	})

	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if len(line) > lineWidth {
			t.Fatalf("expected wrapped line <= %d chars, got %d: %q\n%s", lineWidth, len(line), line, buf.String())
		}
	}
	if !strings.Contains(buf.String(), "output:") {
		t.Fatalf("expected wrapped message/output continuation lines, got %q", buf.String())
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
	// Unknown format falls back to text.
	if _, ok := New("unknown", &buf).(*TextRenderer); !ok {
		t.Error("expected TextRenderer for unknown format")
	}
}

func TestTextRenderer_VerboseStreamsTaskOutputBeforeResult(t *testing.T) {
	var buf bytes.Buffer
	r := newVerboseTextRenderer(&buf)
	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test",
		Targets:      []string{"host-a"},
	})
	r.Emit(TaskStartedEvent{
		Target:   "host-a",
		TaskID:   "task-1",
		TaskName: "Run smoke test",
	})
	r.Emit(TaskOutputEvent{
		TaskID:   "task-1",
		TaskName: "Run smoke test",
		Lines:    []string{"line1"},
	})
	r.Emit(TaskChangedEvent{
		Target:    "host-a",
		TaskID:    "task-1",
		TaskName:  "Run smoke test",
		ElapsedMs: 100,
	})

	out := buf.String()
	if !strings.Contains(out, "line1") {
		t.Fatalf("expected streaming output visible, got %q", out)
	}
	if !strings.Contains(out, "Run smoke test") {
		t.Fatalf("expected task name in output, got %q", out)
	}
}
