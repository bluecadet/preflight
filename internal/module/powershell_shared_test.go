package module

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/winutil"
)

func TestPowershellCheck_UsesBooleanCheckScriptResult(t *testing.T) {
	orig := powershellCombinedOutput
	t.Cleanup(func() { powershellCombinedOutput = orig })

	called := false
	powershellCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		called = true
		if !containsArg(args, "-Command") {
			t.Fatalf("expected inline PowerShell command, got args %v", args)
		}
		return []byte(`{"needs_change":true}`), nil
	}

	needed, err := powershellCheck(context.Background(), map[string]any{
		"check_script": "return $true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected PowerShell to be invoked")
	}
	if !needed {
		t.Fatal("expected check to report change needed")
	}
}

func TestPowershellCheck_UsesObjectCheckScriptResult(t *testing.T) {
	orig := powershellCombinedOutput
	t.Cleanup(func() { powershellCombinedOutput = orig })

	powershellCombinedOutput = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"needs_change":false,"message":"already good"}`), nil
	}

	needed, err := powershellCheck(context.Background(), map[string]any{
		"check_script": "return @{ needs_change = $false; message = 'already good' }",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needed {
		t.Fatal("expected check to report already compliant")
	}
}

func TestPowershellCheck_InvalidCheckScriptOutput(t *testing.T) {
	orig := powershellCombinedOutput
	t.Cleanup(func() { powershellCombinedOutput = orig })

	powershellCombinedOutput = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"message":"missing field"}`), nil
	}

	_, err := powershellCheck(context.Background(), map[string]any{
		"check_script": "return @{ message = 'missing field' }",
	})
	if err == nil {
		t.Fatal("expected invalid output error")
	}
	if !strings.Contains(err.Error(), "needs_change") {
		t.Fatalf("expected needs_change parse error, got %v", err)
	}
}

func TestPowershellCheck_CheckScriptTakesPrecedenceOverCreates(t *testing.T) {
	orig := powershellCombinedOutput
	t.Cleanup(func() { powershellCombinedOutput = orig })

	called := false
	powershellCombinedOutput = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		called = true
		return []byte(`{"needs_change":false}`), nil
	}

	path := filepath.Join(t.TempDir(), "already-there.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}

	needed, err := powershellCheck(context.Background(), map[string]any{
		"check_script": "return $false",
		"creates":      path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected check_script to run before creates logic")
	}
	if needed {
		t.Fatal("expected check_script result to win")
	}
}

func TestPowershellCheckResultParsing_IgnoresMarkerLine(t *testing.T) {
	result, lines, err := winutil.ParsePowerShellCheckOutput([]byte("checking text scale\n__PREFLIGHT_CHECK_RESULT__:eyJuZWVkc19jaGFuZ2UiOnRydWV9"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NeedsChange {
		t.Fatal("expected check to report change needed")
	}
	if len(lines) != 1 || lines[0] != "checking text scale" {
		t.Fatalf("unexpected output lines %v", lines)
	}
}

func TestPowershellApply_InlineScript(t *testing.T) {
	orig := powershellCombinedOutput
	t.Cleanup(func() { powershellCombinedOutput = orig })

	var captured []string
	powershellCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		captured = append([]string{}, args...)
		return []byte("ok"), nil
	}

	if err := powershellApply(context.Background(), map[string]any{
		"script": "Write-Output 'hello'",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !containsArg(captured, "-Command") {
		t.Fatalf("expected -Command invocation, got %v", captured)
	}
}

func TestPowershellApply_PassesEnvironment(t *testing.T) {
	orig := powershellCombinedOutputWithEnv
	t.Cleanup(func() { powershellCombinedOutputWithEnv = orig })

	var captured map[string]string
	powershellCombinedOutputWithEnv = func(_ context.Context, _ string, _ []string, env map[string]string) ([]byte, error) {
		captured = env
		return []byte("ok"), nil
	}

	if err := powershellApply(context.Background(), map[string]any{
		"script": "Write-Output $env:PREFLIGHT_TEST",
		"env": map[string]any{
			"PREFLIGHT_TEST": "expected",
		},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured["PREFLIGHT_TEST"] != "expected" {
		t.Fatalf("expected env to be passed, got %#v", captured)
	}
}

func TestPowershellCheck_PassesEnvironment(t *testing.T) {
	orig := powershellCombinedOutputWithEnv
	t.Cleanup(func() { powershellCombinedOutputWithEnv = orig })

	var captured map[string]string
	powershellCombinedOutputWithEnv = func(_ context.Context, _ string, _ []string, env map[string]string) ([]byte, error) {
		captured = env
		return []byte(`{"needs_change":false}`), nil
	}

	needed, err := powershellCheck(context.Background(), map[string]any{
		"check_script": "return $false",
		"env": map[string]any{
			"PREFLIGHT_TEST": "expected",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needed {
		t.Fatal("expected check to report no change")
	}
	if captured["PREFLIGHT_TEST"] != "expected" {
		t.Fatalf("expected env to be passed, got %#v", captured)
	}
}

func TestPowershellApply_FileScript(t *testing.T) {
	orig := powershellCombinedOutput
	t.Cleanup(func() { powershellCombinedOutput = orig })

	var captured []string
	powershellCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		captured = append([]string{}, args...)
		return []byte("ok"), nil
	}

	if err := powershellApply(context.Background(), map[string]any{
		"file": filepath.Join("scripts", "apply.ps1"),
		"args": []any{"-Verbose"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !containsArg(captured, "-File") {
		t.Fatalf("expected -File invocation, got %v", captured)
	}
	if !containsArg(captured, filepath.Join("scripts", "apply.ps1")) {
		t.Fatalf("expected script path in args, got %v", captured)
	}
}

func TestPowershellApply_ErrorIncludesOutput(t *testing.T) {
	orig := powershellCombinedOutput
	t.Cleanup(func() { powershellCombinedOutput = orig })

	powershellCombinedOutput = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("script execution failed: access denied"), fmt.Errorf("exit status 1")
	}

	err := powershellApply(context.Background(), map[string]any{
		"script": "Invoke-Something",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "script execution failed: access denied") {
		t.Fatalf("expected error to contain output, got: %v", err)
	}
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}
