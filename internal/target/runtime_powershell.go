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
	if checkScript, _ := params["check_script"].(string); strings.TrimSpace(checkScript) != "" {
		script, err := winutil.BuildPowerShellCheckScript(checkScript)
		if err != nil {
			return false, "", err
		}
		out, err := backend.RunPowerShellScript(ctx, script)
		if err != nil {
			return false, "", err
		}
		result, err := winutil.ParsePowerShellCheckResult([]byte(out))
		if err != nil {
			return false, "", err
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
