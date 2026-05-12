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

// User-authored scripts may inspect $LASTEXITCODE even when the previous or
// current native command did not set it. Start each script from a known success.
const powerShellLastExitCodeReset = "$global:LASTEXITCODE = 0\n"

func checkPowerShellModule(ctx context.Context, backend powerShellScriptBackend, params map[string]any) (bool, string, error) {
	return checkPowerShellModuleWithOutput(ctx, backend, params, nil)
}

func checkPowerShellModuleWithOutput(ctx context.Context, backend powerShellScriptBackend, params map[string]any, onOutput OutputFunc) (bool, string, error) {
	if checkScript, _ := params["check_script"].(string); strings.TrimSpace(checkScript) != "" {
		script, err := winutil.BuildPowerShellCheckScript(checkScript)
		if err != nil {
			return false, "", err
		}
		script = powerShellLastExitCodeReset + script
		script, err = wrapPowerShellEnv(params, script)
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
	script, err = wrapPowerShellEnv(params, script+`
Write-Output ([bool](Test-Path -LiteralPath $creates))
`)
	if err != nil {
		return false, "", err
	}
	out, err := backend.RunPowerShellScript(ctx, script)
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
	combined := checkScriptVar + "\n" + applyScriptVar + "\n" + powerShellDryRunPreamble(dryRun) + `$ErrorActionPreference = 'Stop'
$__pf_block = [ScriptBlock]::Create($__pf_check_script)
$global:LASTEXITCODE = 0
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
$global:LASTEXITCODE = 0
& $__pf_apply_block
Write-Output 'changed'
`
	combined, err = wrapPowerShellEnv(params, combined)
	if err != nil {
		return false, "", err
	}
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
	return parseEnsureMarkerOutput("powershell", marker)
}

func applyPowerShellModule(ctx context.Context, backend powerShellScriptBackend, params map[string]any) (string, error) {
	if script, _ := params["script"].(string); script != "" {
		script = powerShellLastExitCodeReset + script
		wrapped, err := wrapPowerShellEnv(params, script)
		if err != nil {
			return "", err
		}
		return backend.RunPowerShellScript(ctx, wrapped)
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
	script, err := wrapPowerShellEnv(params, scriptVar+`
	`+scriptArgsVar+`
$block = [ScriptBlock]::Create($script)
$global:LASTEXITCODE = 0
& $block @scriptArgs
`)
	if err != nil {
		return "", err
	}
	return backend.RunPowerShellScript(ctx, script)
}

func wrapPowerShellEnv(params map[string]any, script string) (string, error) {
	env, err := powerShellEnv(params)
	if err != nil {
		return "", err
	}
	if len(env) == 0 {
		return script, nil
	}
	envVar, err := powershellJSONVar("__pf_env", env)
	if err != nil {
		return "", err
	}
	return envVar + `
$__pf_env_previous = @{}
foreach ($__pf_env_entry in $__pf_env.PSObject.Properties) {
  $__pf_env_name = [string]$__pf_env_entry.Name
  $__pf_env_previous[$__pf_env_name] = [pscustomobject]@{
    Exists = $null -ne [System.Environment]::GetEnvironmentVariable($__pf_env_name, 'Process')
    Value = [System.Environment]::GetEnvironmentVariable($__pf_env_name, 'Process')
  }
  [System.Environment]::SetEnvironmentVariable($__pf_env_name, [string]$__pf_env_entry.Value, 'Process')
}
try {
` + script + `
} finally {
  foreach ($__pf_env_entry in $__pf_env_previous.GetEnumerator()) {
    if ($__pf_env_entry.Value.Exists) {
      [System.Environment]::SetEnvironmentVariable([string]$__pf_env_entry.Key, [string]$__pf_env_entry.Value.Value, 'Process')
    } else {
      [System.Environment]::SetEnvironmentVariable([string]$__pf_env_entry.Key, $null, 'Process')
    }
  }
}
`, nil
}

func powerShellEnv(params map[string]any) (map[string]string, error) {
	value, ok := params["env"]
	if !ok || value == nil {
		return nil, nil
	}

	var env map[string]string
	switch typed := value.(type) {
	case map[string]string:
		env = typed
	case map[string]any:
		env = make(map[string]string, len(typed))
		for name, raw := range typed {
			text, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("powershell env %q must be a string, got %T", name, raw)
			}
			env[name] = text
		}
	default:
		return nil, fmt.Errorf("powershell env must be a map, got %T", value)
	}

	for name := range env {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("powershell env names must not be empty")
		}
		if strings.Contains(name, "=") {
			return nil, fmt.Errorf("powershell env name %q must not contain '='", name)
		}
	}
	return env, nil
}
