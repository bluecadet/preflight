package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLogSink_SupportGateEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	sink, err := NewRunLogSink("gate-run", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}
	sink.Emit(SupportGateEvent{
		Target:  "posix-host",
		Runtime: "posix-shell",
		Reason:  "unsupported_on_runtime",
		Violations: []SupportGateViolation{
			{TaskName: "install tools", Module: "system_package", Reason: "unsupported_on_runtime", Message: "module \"system_package\" is not supported on posix-shell (supported: windows-powershell)"},
			{TaskName: "manage service", Module: "service", Reason: "unsupported_on_runtime", Message: "module \"service\" is not supported on posix-shell (supported: windows-powershell)"},
		},
	})
	sink.Close()

	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 event, got %d", len(lines))
	}
	line := lines[0]

	// Type, level, reason, runtime present.
	if !strings.Contains(line, `"type":"support_gate"`) {
		t.Errorf("missing type support_gate: %s", line)
	}
	if !strings.Contains(line, `"level":"error"`) {
		t.Errorf("missing level error: %s", line)
	}
	if !strings.Contains(line, `"reason":"unsupported_on_runtime"`) {
		t.Errorf("missing reason: %s", line)
	}
	if !strings.Contains(line, `"runtime":"posix-shell"`) {
		t.Errorf("missing runtime: %s", line)
	}
	// Target correlation.
	if !strings.Contains(line, `"target":"posix-host"`) {
		t.Errorf("missing target: %s", line)
	}
	// Two violations, each carrying the task/module/message.
	if !strings.Contains(line, `"task":"install tools"`) {
		t.Errorf("missing first violation task: %s", line)
	}
	if !strings.Contains(line, `"module":"system_package"`) {
		t.Errorf("missing first violation module: %s", line)
	}
	if !strings.Contains(line, `"task":"manage service"`) {
		t.Errorf("missing second violation task: %s", line)
	}
	// msg summary names the runtime.
	if !strings.Contains(line, `"msg":"gate: 2 task(s) cannot run on this target (posix-shell)"`) {
		t.Errorf("missing/incorrect summary msg: %s", line)
	}

	// Schema-valid.
	var doc any
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestJSONRenderer_SupportGateEvent(t *testing.T) {
	var buf strings.Builder
	r := NewJSONRenderer(&buf)
	r.Emit(SupportGateEvent{
		Target:  "h",
		Runtime: "posix-shell",
		Reason:  "unsupported_on_runtime",
		Violations: []SupportGateViolation{
			{TaskName: "t", Module: "m", Reason: "unsupported_on_runtime", Message: "msg"},
		},
	})
	out := buf.String()
	if !strings.Contains(out, `"type":"support_gate"`) {
		t.Errorf("json missing type: %s", out)
	}
	if !strings.Contains(out, `"runtime":"posix-shell"`) {
		t.Errorf("json missing runtime: %s", out)
	}
	if !strings.Contains(out, `"reason":"unsupported_on_runtime"`) {
		t.Errorf("json missing reason: %s", out)
	}
}

func TestSupportGateEvent_RedactsSecrets(t *testing.T) {
	orig := SupportGateEvent{
		Target:  "h",
		Runtime: "posix-shell",
		Reason:  "unsupported_on_runtime",
		Violations: []SupportGateViolation{
			{TaskName: "t", Module: "m", Reason: "unsupported_on_runtime", Message: "contains supersecret value"},
		},
	}
	got := orig.Redact([]string{"supersecret"}).(SupportGateEvent)
	if strings.Contains(got.Violations[0].Message, "supersecret") {
		t.Errorf("secret not redacted from violation message: %q", got.Violations[0].Message)
	}
}
