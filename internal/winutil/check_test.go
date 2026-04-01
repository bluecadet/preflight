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
