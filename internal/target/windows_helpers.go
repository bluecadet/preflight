package target

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bluecadet/preflight/internal/winutil"
)

const windowsTargetInfoScript = `
$os = Get-CimInstance Win32_OperatingSystem
$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
[pscustomobject]@{
  hostname = $env:COMPUTERNAME
  version  = [string]$os.Version
  build    = [string]$os.BuildNumber
  arch     = $arch
} | ConvertTo-Json -Compress
`

type windowsPowerShellRunner func(context.Context, string) (string, error)

func powershellJSONVar(name string, value any) (string, error) {
	return winutil.JSONVarScript(name, value)
}

func parseWindowsBool(out string) (bool, error) {
	value, _, err := parseWindowsBoolOutput(out)
	return value, err
}

func parseWindowsBoolOutput(out string) (bool, []string, error) {
	lines := splitOutputLines(out)
	if len(lines) == 0 {
		return false, nil, fmt.Errorf("unexpected boolean output %q", strings.TrimSpace(out))
	}
	value, err := winutil.ParseBool(lines[len(lines)-1])
	if err != nil {
		return false, nil, fmt.Errorf("unexpected boolean output %q", strings.TrimSpace(out))
	}
	return value, lines[:len(lines)-1], nil
}

func parseEnsureMarkerResult(name, out string) (EnsureResult, error) {
	switch strings.TrimSpace(out) {
	case "ok":
		return EnsureResult{Changed: false, Message: "already in desired state"}, nil
	case "would-change":
		return EnsureResult{Changed: true, Message: "would apply change (dry-run)"}, nil
	case "changed":
		return EnsureResult{Changed: true, Message: "change applied"}, nil
	default:
		return EnsureResult{}, fmt.Errorf("%s ensure: unexpected output %q", name, strings.TrimSpace(out))
	}
}

func readRemoteWindowsFile(ctx context.Context, transport Transport, run windowsPowerShellRunner, path string) ([]byte, error) {
	script, err := powershellJSONVar("path", path)
	if err != nil {
		return nil, err
	}
	stdout, err := run(ctx, script+`
if (-not (Test-Path -LiteralPath $path)) {
  throw "file not found: $path"
}
[Convert]::ToBase64String([IO.File]::ReadAllBytes($path))
`)
	if err != nil {
		return nil, wrapTargetError(transport, fmt.Sprintf("read %q", path), err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
	if err != nil {
		return nil, wrapTargetError(transport, fmt.Sprintf("read %q", path), fmt.Errorf("decode remote file: %w", err))
	}
	return decoded, nil
}

func remoteWindowsTargetInfo(ctx context.Context, transport Transport, run windowsPowerShellRunner) (TargetInfo, error) {
	stdout, err := run(ctx, windowsTargetInfoScript)
	if err != nil {
		return TargetInfo{}, wrapTargetError(transport, "info", err)
	}

	var payload struct {
		Hostname string `json:"hostname"`
		Version  string `json:"version"`
		Build    string `json:"build"`
		Arch     string `json:"arch"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		return TargetInfo{}, wrapTargetError(transport, "info", fmt.Errorf("parse target info: %w", err))
	}

	return TargetInfo{
		Hostname:    payload.Hostname,
		OSVersion:   payload.Version,
		OSBuild:     payload.Build,
		Arch:        NormalizeArchitecture(payload.Arch),
		OSFamily:    OSFamilyWindows,
		RuntimeKind: RuntimeKindWindowsPowerShell,
		Transport:   transport,
	}, nil
}

func paramStringSlice(params map[string]any, key string) ([]string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		result := make([]string, 0, len(typed))
		for i, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] must be a string, got %T", key, i, item)
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s must be a string list, got %T", key, value)
	}
}

func paramString(params map[string]any, key, defaultVal string) (string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return defaultVal, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	if text == "" {
		return defaultVal, nil
	}
	return text, nil
}

func paramStringRequired(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return "", fmt.Errorf("required param %q is missing", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	if text == "" {
		return "", fmt.Errorf("required param %q must not be empty", key)
	}
	return text, nil
}

// requireStringParam reads a required string param and returns a module-prefixed
// missing-param error when it is absent or empty. The error wording matches the
// convention used across the POSIX modules ("<module>: required param <key> is
// missing") so callers stay uniform.
func requireStringParam(params map[string]any, key, module string) (string, error) {
	v, ok := params[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("%s: required param %q is missing", module, key)
	}
	return v, nil
}

// paramInt reads an optional integer param that may arrive as int, int64, or
// float64 (YAML/JSON numeric decoders disagree on the concrete type). A
// positive value overrides def; otherwise def is returned. Collapses the
// repeated 3-arm type switch used by modules that accept a numeric timeout.
func paramInt(params map[string]any, key string, def int) int {
	switch raw := params[key].(type) {
	case int:
		if raw > 0 {
			return raw
		}
	case int64:
		if raw > 0 {
			return int(raw)
		}
	case float64:
		if raw > 0 {
			return int(raw)
		}
	}
	return def
}
