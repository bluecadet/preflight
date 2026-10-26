package target

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bluecadet/preflight/internal/winutil"
)

type powerShellScriptBackend interface {
	RunPowerShellScript(ctx context.Context, script string, out OutputFunc) (string, error)
}

// User-authored scripts may inspect $LASTEXITCODE even when the previous or
// current native command did not set it. Start each script from a known success.
const powerShellLastExitCodeReset = "$global:LASTEXITCODE = 0\n"

func checkPowerShellModule(ctx context.Context, backend powerShellScriptBackend, params map[string]any) (CheckResult, error) {
	return checkPowerShellModuleWithOutput(ctx, backend, params, nil)
}

func checkPowerShellModuleWithOutput(ctx context.Context, backend powerShellScriptBackend, params map[string]any, onOutput OutputFunc) (CheckResult, error) {
	if checkScript, _ := params["check_script"].(string); strings.TrimSpace(checkScript) != "" {
		script, err := winutil.BuildPowerShellCheckScript(checkScript)
		if err != nil {
			return CheckResult{}, err
		}
		script = powerShellLastExitCodeReset + script
		script, err = wrapPowerShellWorkingDir(params, script)
		if err != nil {
			return CheckResult{}, err
		}
		script, err = wrapPowerShellEnv(params, script)
		if err != nil {
			return CheckResult{}, err
		}
		// Wrap onOutput to suppress the check-result protocol marker line before
		// forwarding lines to the caller. Real PowerShell output is forwarded as
		// it arrives; the marker is an internal encoding that users should not see.
		var streamOut OutputFunc
		if onOutput != nil {
			streamOut = func(line string) {
				if !strings.HasPrefix(line, "__PREFLIGHT_CHECK_RESULT__:") {
					onOutput(line)
				}
			}
		}
		out, err := backend.RunPowerShellScript(ctx, script, streamOut)
		if err != nil {
			return CheckResult{}, err
		}
		result, _, err := winutil.ParsePowerShellCheckOutput([]byte(out))
		if err != nil {
			return CheckResult{}, err
		}
		return CheckResult{NeedsChange: result.NeedsChange, Message: result.Message}, nil
	}

	creates, _ := params["creates"].(string)
	if creates == "" {
		return CheckResult{NeedsChange: true}, nil
	}
	script, err := powershellJSONVar("creates", creates)
	if err != nil {
		return CheckResult{}, err
	}
	script, err = wrapPowerShellWorkingDir(params, script+`
Write-Output ([bool](Test-Path -LiteralPath $creates))
`)
	if err != nil {
		return CheckResult{}, err
	}
	script, err = wrapPowerShellEnv(params, script)
	if err != nil {
		return CheckResult{}, err
	}
	out, err := backend.RunPowerShellScript(ctx, script, nil)
	if err != nil {
		return CheckResult{}, err
	}
	ok, err := parseWindowsBool(out)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{NeedsChange: !ok}, nil
}

func applyPowerShellModule(ctx context.Context, backend powerShellScriptBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	if script, _ := params["script"].(string); script != "" {
		script = powerShellLastExitCodeReset + script
		var err error
		script, err = wrapPowerShellWorkingDir(params, script)
		if err != nil {
			return ApplyResult{}, err
		}
		wrapped, err := wrapPowerShellEnv(params, script)
		if err != nil {
			return ApplyResult{}, err
		}
		output, err := backend.RunPowerShellScript(ctx, wrapped, out)
		return applyStreamed(output, nil), err
	}

	file, _ := params["file"].(string)
	if file == "" {
		return ApplyResult{}, fmt.Errorf("powershell: one of 'script' or 'file' is required")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("powershell: read %q: %w", file, err)
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return ApplyResult{}, err
	}
	scriptVar, err := powershellJSONVar("script", string(data))
	if err != nil {
		return ApplyResult{}, err
	}
	scriptArgsVar, err := powershellJSONVar("scriptArgs", args)
	if err != nil {
		return ApplyResult{}, err
	}
	script := scriptVar + `
	` + scriptArgsVar + `
$block = [ScriptBlock]::Create($script)
$global:LASTEXITCODE = 0
& $block @scriptArgs
`
	script, err = wrapPowerShellWorkingDir(params, script)
	if err != nil {
		return ApplyResult{}, err
	}
	script, err = wrapPowerShellEnv(params, script)
	if err != nil {
		return ApplyResult{}, err
	}
	output, err := backend.RunPowerShellScript(ctx, script, out)
	return applyStreamed(output, nil), err
}

func wrapPowerShellWorkingDir(params map[string]any, script string) (string, error) {
	workingDir, _ := params["working_dir"].(string)
	if strings.TrimSpace(workingDir) == "" {
		return script, nil
	}
	workingDirVar, err := powershellJSONVar("__pf_working_dir", workingDir)
	if err != nil {
		return "", err
	}
	return workingDirVar + `
$__pf_working_dir_pushed = $false
try {
  Push-Location -LiteralPath $__pf_working_dir
  $__pf_working_dir_pushed = $true
` + script + `
} finally {
  if ($__pf_working_dir_pushed) {
    Pop-Location
  }
}
`, nil
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
