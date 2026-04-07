//go:build windows

package module

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/bluecadet/preflight/internal/winutil"
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
	return winutil.JSONVarScript(name, value)
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
	ports, err := winutil.NormalizeFirewallPorts(params["ports"])
	if err != nil {
		return "", fmt.Errorf("firewall_rule: %w", err)
	}
	return ports, nil
}
