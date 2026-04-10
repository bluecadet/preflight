package winutil

import (
	"strings"
	"testing"
)

func TestBuildPowerShellCheckScriptWrapsResult(t *testing.T) {
	script, err := BuildPowerShellCheckScript("return $true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, fragment := range []string{
		"ConvertFrom-Json",
		"[ScriptBlock]::Create($checkScript)",
		"ConvertTo-Json -Compress",
		"needs_change",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected wrapper to contain %q, got:\n%s", fragment, script)
		}
	}
}

func TestParsePowerShellCheckResult_BoolAndMessage(t *testing.T) {
	result, err := ParsePowerShellCheckResult([]byte(`{"needs_change":"true","message":"rename pending"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NeedsChange {
		t.Fatal("expected needs_change=true")
	}
	if result.Message != "rename pending" {
		t.Fatalf("unexpected message %q", result.Message)
	}
}

func TestParsePowerShellCheckResult_InvalidPayload(t *testing.T) {
	_, err := ParsePowerShellCheckResult([]byte(`{"message":"missing field"}`))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "needs_change") {
		t.Fatalf("expected needs_change error, got %v", err)
	}
}

func TestParsePowerShellCheckOutput_StripsLogsAndMarker(t *testing.T) {
	result, lines, err := ParsePowerShellCheckOutput([]byte("checking registry\n__PREFLIGHT_CHECK_RESULT__:eyJuZWVkc19jaGFuZ2UiOnRydWUsIm1lc3NhZ2UiOiJyZW5hbWUgcGVuZGluZyJ9\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NeedsChange {
		t.Fatal("expected needs_change=true")
	}
	if result.Message != "rename pending" {
		t.Fatalf("unexpected message %q", result.Message)
	}
	if len(lines) != 1 || lines[0] != "checking registry" {
		t.Fatalf("unexpected output lines %v", lines)
	}
}
