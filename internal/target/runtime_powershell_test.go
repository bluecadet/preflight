package target

import (
	"context"
	"strings"
	"testing"
)

type recordingPowerShellBackend struct {
	output  string
	scripts []string
}

func (b *recordingPowerShellBackend) RunPowerShellScript(_ context.Context, script string) (string, error) {
	b.scripts = append(b.scripts, script)
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

	changed, _, err := ensurePowerShellModule(context.Background(), backend, params, false, nil)
	if err != nil {
		t.Fatalf("ensurePowerShellModule returned error: %v", err)
	}
	if !changed {
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

	changed, _, err := ensurePowerShellModule(context.Background(), backend, params, false, nil)
	if err != nil {
		t.Fatalf("ensurePowerShellModule returned error: %v", err)
	}
	if !changed {
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

	_, _, err := ensurePowerShellModule(context.Background(), backend, params, false, nil)
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

	_, _, err := checkPowerShellModule(context.Background(), backend, map[string]any{
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

	_, _, err := checkPowerShellModule(context.Background(), backend, map[string]any{
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

	out, err := applyPowerShellModule(context.Background(), backend, params)
	if err != nil {
		t.Fatalf("applyPowerShellModule returned error: %v", err)
	}
	if out != "done" {
		t.Fatalf("expected output %q, got %q", "done", out)
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

	out, err := applyPowerShellModule(context.Background(), backend, params)
	if err != nil {
		t.Fatalf("applyPowerShellModule returned error: %v", err)
	}
	if out != "done" {
		t.Fatalf("expected output %q, got %q", "done", out)
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

	out, err := applyWindowsShell(context.Background(), backend, map[string]any{
		"cmd":         "git",
		"args":        []any{"status"},
		"working_dir": `C:\Repo`,
	})
	if err != nil {
		t.Fatalf("applyWindowsShell returned error: %v", err)
	}
	if out != "done" {
		t.Fatalf("expected output %q, got %q", "done", out)
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
	})
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
