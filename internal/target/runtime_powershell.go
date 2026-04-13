package target

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bluecadet/preflight/internal/winutil"
)

type powerShellScriptBackend interface {
	RunPowerShellScript(ctx context.Context, script string) (string, error)
}

func checkPowerShellModule(ctx context.Context, backend powerShellScriptBackend, params map[string]any) (bool, string, error) {
	return checkPowerShellModuleWithOutput(ctx, backend, params, nil)
}

func checkPowerShellModuleWithOutput(ctx context.Context, backend powerShellScriptBackend, params map[string]any, onOutput OutputFunc) (bool, string, error) {
	if checkScript, _ := params["check_script"].(string); strings.TrimSpace(checkScript) != "" {
		script, err := winutil.BuildPowerShellCheckScript(checkScript)
		if err != nil {
			return false, "", err
		}
		out, err := backend.RunPowerShellScript(ctx, script)
		if err != nil {
			return false, "", err
		}
		result, outputLines, err := winutil.ParsePowerShellCheckOutput([]byte(out))
		if err != nil {
			return false, "", err
		}
		if onOutput != nil {
			for _, line := range outputLines {
				onOutput(line)
			}
		}
		return result.NeedsChange, result.Message, nil
	}

	creates, _ := params["creates"].(string)
	if creates == "" {
		return true, "", nil
	}
	script, err := powershellJSONVar("creates", creates)
	if err != nil {
		return false, "", err
	}
	out, err := backend.RunPowerShellScript(ctx, script+`
Write-Output ([bool](Test-Path -LiteralPath $creates))
`)
	if err != nil {
		return false, "", err
	}
	ok, err := parseWindowsBool(out)
	if err != nil {
		return false, "", err
	}
	return !ok, "", nil
}

// ensurePowerShellModule combines check and apply into a single round trip when
// both check_script and script are inline strings (the common case). Returns
// errEnsureNotHandled for other configurations to fall back to check+apply.
func ensurePowerShellModule(ctx context.Context, backend powerShellScriptBackend, params map[string]any, dryRun bool, onOutput OutputFunc) (bool, string, error) {
	checkScript, _ := params["check_script"].(string)
	applyScript, _ := params["script"].(string)
	if strings.TrimSpace(checkScript) == "" || strings.TrimSpace(applyScript) == "" {
		return false, "", errEnsureNotHandled
	}

	checkScriptVar, err := winutil.JSONVarScript("__pf_check_script", checkScript)
	if err != nil {
		return false, "", err
	}
	applyScriptVar, err := winutil.JSONVarScript("__pf_apply_script", applyScript)
	if err != nil {
		return false, "", err
	}
	dryRunVal := "$false"
	if dryRun {
		dryRunVal = "$true"
	}
	combined := checkScriptVar + "\n" + applyScriptVar + "\n" + `$__pf_dry_run = ` + dryRunVal + `
$ErrorActionPreference = 'Stop'
$__pf_block = [ScriptBlock]::Create($__pf_check_script)
$__pf_vals = @(& $__pf_block)
if ($__pf_vals.Count -eq 0) { throw "powershell check_script must return a bool or object" }
$__pf_result = $__pf_vals[$__pf_vals.Count - 1]
if ($__pf_vals.Count -gt 1) {
  foreach ($__pf_entry in @($__pf_vals[0..($__pf_vals.Count - 2)])) { Write-Output $__pf_entry }
}
$__pf_needs = if ($__pf_result -is [bool]) { [bool]$__pf_result } else { [bool]$__pf_result.needs_change }
if (-not $__pf_needs) { Write-Output 'ok'; exit 0 }
if ($__pf_dry_run) { Write-Output 'would-change'; exit 0 }
$ErrorActionPreference = 'Stop'
$__pf_apply_block = [ScriptBlock]::Create($__pf_apply_script)
& $__pf_apply_block
Write-Output 'changed'
`
	out, err := backend.RunPowerShellScript(ctx, combined)
	if err != nil {
		return false, "", err
	}
	lines := splitOutputLines(out)
	if len(lines) == 0 {
		return false, "", fmt.Errorf("powershell ensure: empty output")
	}
	marker := strings.TrimSpace(lines[len(lines)-1])
	outputLines := lines[:len(lines)-1]
	if onOutput != nil {
		for _, line := range outputLines {
			onOutput(line)
		}
	}
	switch marker {
	case "ok":
		return false, "already in desired state", nil
	case "would-change":
		return true, "would apply change (dry-run)", nil
	case "changed":
		return true, "change applied", nil
	default:
		return false, "", fmt.Errorf("powershell ensure: unexpected output %q", marker)
	}
}

func applyPowerShellModule(ctx context.Context, backend powerShellScriptBackend, params map[string]any) (string, error) {
	if script, _ := params["script"].(string); script != "" {
		return backend.RunPowerShellScript(ctx, script)
	}

	file, _ := params["file"].(string)
	if file == "" {
		return "", fmt.Errorf("powershell: one of 'script' or 'file' is required")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("powershell: read %q: %w", file, err)
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return "", err
	}
	scriptVar, err := powershellJSONVar("script", string(data))
	if err != nil {
		return "", err
	}
	scriptArgsVar, err := powershellJSONVar("scriptArgs", args)
	if err != nil {
		return "", err
	}
	return backend.RunPowerShellScript(ctx, scriptVar+`
`+scriptArgsVar+`
$block = [ScriptBlock]::Create($script)
& $block @scriptArgs
`)
}
