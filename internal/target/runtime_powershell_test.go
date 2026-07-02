package target

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type recordingPowerShellBackend struct {
	output  string
	scripts []string
}

func (b *recordingPowerShellBackend) RunPowerShellScript(_ context.Context, script string, out OutputFunc) (string, error) {
	b.scripts = append(b.scripts, script)
	if out != nil {
		for _, line := range splitOutputLines(b.output) {
			out(line)
		}
	}
	return b.output, nil
}

func (b *recordingPowerShellBackend) CopyFile(context.Context, string, string) error {
	return nil
}

func (b *recordingPowerShellBackend) RemoteTempDir() string {
	return `C:\Windows\Temp\preflight`
}

func TestEnsurePowerShellModuleWrapsEnv(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "changed"}
	params := map[string]any{
		"env": map[string]any{
			"GITHUB_TOKEN": "ghp_example",
		},
		"check_script": "Write-Output $true",
		"script":       "Write-Output $env:GITHUB_TOKEN",
	}

	result, err := ensurePowerShellModule(context.Background(), backend, params, false, nil)
	if err != nil {
		t.Fatalf("ensurePowerShellModule returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected ensurePowerShellModule to report changed")
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}

	script := backend.scripts[0]
	for _, want := range []string{
		"$__pf_env",
		"[System.Environment]::SetEnvironmentVariable($__pf_env_name",
		"$__pf_env_previous",
		"} finally {",
		"$__pf_check_script",
		"$__pf_apply_script",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected wrapped script to contain %q, got:\n%s", want, script)
		}
	}
	if strings.Contains(script, "ghp_example") {
		t.Fatalf("expected env value to avoid plaintext script embedding, got:\n%s", script)
	}
}

func TestEnsurePowerShellModuleWrapsWorkingDir(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "changed"}
	params := map[string]any{
		"working_dir":  `C:\App`,
		"check_script": "Write-Output $true",
		"script":       "Write-Output (Get-Location)",
	}

	result, err := ensurePowerShellModule(context.Background(), backend, params, false, nil)
	if err != nil {
		t.Fatalf("ensurePowerShellModule returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected ensurePowerShellModule to report changed")
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}

	script := backend.scripts[0]
	for _, want := range []string{
		"$__pf_working_dir",
		"Push-Location -LiteralPath $__pf_working_dir",
		"Pop-Location",
		"$__pf_check_script",
		"$__pf_apply_script",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected wrapped script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestEnsurePowerShellModuleResetsLastExitCodeBeforeCheckAndApply(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "changed"}
	params := map[string]any{
		"check_script": "return $true",
		"script":       "if ($LASTEXITCODE -ne 0) { throw $LASTEXITCODE }",
	}

	_, err := ensurePowerShellModule(context.Background(), backend, params, false, nil)
	if err != nil {
		t.Fatalf("ensurePowerShellModule returned error: %v", err)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}

	script := backend.scripts[0]
	for _, want := range []string{
		"$global:LASTEXITCODE = 0\n$__pf_vals = @(& $__pf_block)",
		"$global:LASTEXITCODE = 0\n& $__pf_apply_block",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected wrapped script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestCheckPowerShellModuleWrapsWorkingDir(t *testing.T) {
	backend := &recordingPowerShellBackend{output: `__PREFLIGHT_CHECK_RESULT__:eyJuZWVkc19jaGFuZ2UiOmZhbHNlLCJtZXNzYWdlIjpudWxsfQ==`}

	_, err := checkPowerShellModule(context.Background(), backend, map[string]any{
		"working_dir":  `C:\App`,
		"check_script": "return $false",
	})
	if err != nil {
		t.Fatalf("checkPowerShellModule returned error: %v", err)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}

	script := backend.scripts[0]
	for _, want := range []string{
		"Push-Location -LiteralPath $__pf_working_dir",
		"Pop-Location",
		"$checkScript",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected wrapped script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestCheckPowerShellModuleResetsLastExitCode(t *testing.T) {
	backend := &recordingPowerShellBackend{output: `__PREFLIGHT_CHECK_RESULT__:eyJuZWVkc19jaGFuZ2UiOmZhbHNlLCJtZXNzYWdlIjpudWxsfQ==`}

	_, err := checkPowerShellModule(context.Background(), backend, map[string]any{
		"check_script": "return ($LASTEXITCODE -ne 0)",
	})
	if err != nil {
		t.Fatalf("checkPowerShellModule returned error: %v", err)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}
	if !strings.HasPrefix(backend.scripts[0], "$global:LASTEXITCODE = 0\n") {
		t.Fatalf("expected check script to reset LASTEXITCODE, got:\n%s", backend.scripts[0])
	}
}

func TestApplyPowerShellModuleWrapsInlineWorkingDir(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "done"}
	params := map[string]any{
		"working_dir": `C:\App`,
		"script":      "Write-Output (Get-Location)",
	}

	result, err := applyPowerShellModule(context.Background(), backend, params, nil)
	if err != nil {
		t.Fatalf("applyPowerShellModule returned error: %v", err)
	}
	if result.Message != "done" {
		t.Fatalf("expected output %q, got %q", "done", result.Message)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}
	script := backend.scripts[0]
	for _, want := range []string{
		"Push-Location -LiteralPath $__pf_working_dir",
		"Write-Output (Get-Location)",
		"Pop-Location",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected wrapped script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestApplyPowerShellModuleWrapsInlineEnv(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "done"}
	params := map[string]any{
		"env": map[string]string{
			"NAME": "value",
		},
		"script": "Write-Output $env:NAME",
	}

	result, err := applyPowerShellModule(context.Background(), backend, params, nil)
	if err != nil {
		t.Fatalf("applyPowerShellModule returned error: %v", err)
	}
	if result.Message != "done" {
		t.Fatalf("expected output %q, got %q", "done", result.Message)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}
	script := backend.scripts[0]
	if !strings.Contains(script, "[System.Environment]::SetEnvironmentVariable($__pf_env_name") {
		t.Fatalf("expected env wrapper, got:\n%s", script)
	}
	if !strings.Contains(script, "Write-Output $env:NAME") {
		t.Fatalf("expected wrapped script body, got:\n%s", script)
	}
}

func TestApplyWindowsShellWrapsWorkingDir(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "done"}

	result, err := applyWindowsShell(context.Background(), backend, map[string]any{
		"cmd":         "git",
		"args":        []any{"status"},
		"working_dir": `C:\Repo`,
	}, nil)
	if err != nil {
		t.Fatalf("applyWindowsShell returned error: %v", err)
	}
	if result.Message != "done" {
		t.Fatalf("expected output %q, got %q", "done", result.Message)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}
	script := backend.scripts[0]
	for _, want := range []string{
		"Push-Location -LiteralPath $__pf_working_dir",
		"& $cmd @args",
		"Pop-Location",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected wrapped script to contain %q, got:\n%s", want, script)
		}
	}
	if strings.Contains(script, "Set-Location -LiteralPath") {
		t.Fatalf("expected shell wrapper not to leak location with Set-Location, got:\n%s", script)
	}
}

func TestApplyPowerShellModuleResetsLastExitCode(t *testing.T) {
	backend := &recordingPowerShellBackend{output: "done"}

	_, err := applyPowerShellModule(context.Background(), backend, map[string]any{
		"script": "if ($LASTEXITCODE -ne 0) { throw $LASTEXITCODE }",
	}, nil)
	if err != nil {
		t.Fatalf("applyPowerShellModule returned error: %v", err)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected one script, got %d", len(backend.scripts))
	}
	if !strings.HasPrefix(backend.scripts[0], "$global:LASTEXITCODE = 0\n") {
		t.Fatalf("expected inline script to reset LASTEXITCODE, got:\n%s", backend.scripts[0])
	}
}

func TestEnsurePowerShellModuleForwardsIntermediateLinesOnChange(t *testing.T) {
	// Simulate a long-running script that emits diagnostic lines before the
	// "changed" sentinel. All lines before the marker must reach onOutput and
	// nothing from the marker itself should leak through.
	backend := &recordingPowerShellBackend{output: "progress-1\nprogress-2\nprogress-3\nchanged"}
	params := map[string]any{
		"check_script": "return $true",
		"script":       "Write-Output 'progress'",
	}

	var gotLines []string
	result, err := ensurePowerShellModule(context.Background(), backend, params, false, func(line string) {
		gotLines = append(gotLines, line)
	})
	if err != nil {
		t.Fatalf("ensurePowerShellModule returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected changed=true")
	}
	want := []string{"progress-1", "progress-2", "progress-3"}
	if !reflect.DeepEqual(gotLines, want) {
		t.Fatalf("gotLines = %v, want %v", gotLines, want)
	}
}

func TestEnsurePowerShellModuleForwardsIntermediateLinesOnOk(t *testing.T) {
	// When the script reports "ok" (no change needed), any diagnostic lines
	// emitted before the marker should still be forwarded.
	backend := &recordingPowerShellBackend{output: "verified-a\nverified-b\nok"}
	params := map[string]any{
		"check_script": "return $false",
		"script":       "Write-Output 'noop'",
	}

	var gotLines []string
	result, err := ensurePowerShellModule(context.Background(), backend, params, false, func(line string) {
		gotLines = append(gotLines, line)
	})
	if err != nil {
		t.Fatalf("ensurePowerShellModule returned error: %v", err)
	}
	if result.Changed {
		t.Fatal("expected changed=false")
	}
	want := []string{"verified-a", "verified-b"}
	if !reflect.DeepEqual(gotLines, want) {
		t.Fatalf("gotLines = %v, want %v", gotLines, want)
	}
}

func TestCheckPowerShellModuleWithOutputForwardsLines(t *testing.T) {
	// check_script may emit diagnostic lines before its result. Those lines
	// must be forwarded via onOutput and not silently discarded.
	const checkResultMarker = "__PREFLIGHT_CHECK_RESULT__:eyJuZWVkc19jaGFuZ2UiOnRydWUsIm1lc3NhZ2UiOm51bGx9"
	backend := &recordingPowerShellBackend{
		output: "diag-line-1\ndiag-line-2\n" + checkResultMarker,
	}
	params := map[string]any{
		"check_script": "Write-Output 'diag'; return $true",
	}

	var gotLines []string
	result, err := checkPowerShellModuleWithOutput(context.Background(), backend, params, func(line string) {
		gotLines = append(gotLines, line)
	})
	if err != nil {
		t.Fatalf("checkPowerShellModuleWithOutput returned error: %v", err)
	}
	if !result.NeedsChange {
		t.Fatal("expected needsChange=true")
	}
	want := []string{"diag-line-1", "diag-line-2"}
	if !reflect.DeepEqual(gotLines, want) {
		t.Fatalf("gotLines = %v, want %v", gotLines, want)
	}
}

func TestPowerShellEnvRejectsInvalidValues(t *testing.T) {
	_, err := powerShellEnv(map[string]any{
		"env": map[string]any{
			"COUNT": 1,
		},
	})
	if err == nil {
		t.Fatal("expected invalid env value to fail")
	}
	if !strings.Contains(err.Error(), `powershell env "COUNT" must be a string`) {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = powerShellEnv(map[string]any{
		"env": map[string]string{
			"BAD=NAME": "value",
		},
	})
	if err == nil {
		t.Fatal("expected invalid env name to fail")
	}
	if !strings.Contains(err.Error(), `must not contain '='`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
