//go:build windows

package module

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

var windowsCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func runWindowsCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := windowsCombinedOutput(ctx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w\noutput: %s", name, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func runWindowsPowerShell(ctx context.Context, script string) ([]byte, error) {
	return runWindowsCommand(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	)
}

func runWindowsPowerShellWithParams(ctx context.Context, params map[string]any, body string) ([]byte, error) {
	paramSetup, err := powershellJSONVar("params", params)
	if err != nil {
		return nil, err
	}
	return runWindowsPowerShell(ctx, paramSetup+"\n"+body)
}

func runWindowsPowerShellBool(ctx context.Context, params map[string]any, body string) (bool, error) {
	out, err := runWindowsPowerShellWithParams(ctx, params, body)
	if err != nil {
		return false, err
	}
	return parseWindowsBool(out)
}

func powershellJSONVar(name string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode %s params: %w", name, err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf(
		"$%s = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('%s')) | ConvertFrom-Json",
		name,
		encoded,
	), nil
}

func parseWindowsBool(out []byte) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(string(out))) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected boolean output %q", strings.TrimSpace(string(out)))
	}
}

func firewallPortsArg(params map[string]any) (string, error) {
	value, ok := params["ports"]
	if !ok || value == nil {
		return "", nil
	}

	switch typed := value.(type) {
	case int:
		return fmt.Sprintf("%d", typed), nil
	case int64:
		return fmt.Sprintf("%d", typed), nil
	case float64:
		return fmt.Sprintf("%g", typed), nil
	case string:
		return typed, nil
	case []any:
		parts := make([]string, 0, len(typed))
		for i, item := range typed {
			switch cast := item.(type) {
			case int:
				parts = append(parts, fmt.Sprintf("%d", cast))
			case int64:
				parts = append(parts, fmt.Sprintf("%d", cast))
			case float64:
				parts = append(parts, fmt.Sprintf("%g", cast))
			case string:
				parts = append(parts, cast)
			default:
				return "", fmt.Errorf("firewall_rule: ports[%d] must be a string or number, got %T", i, item)
			}
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("firewall_rule: ports must be a string, number, or list, got %T", value)
	}
}
