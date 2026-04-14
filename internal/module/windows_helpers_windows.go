//go:build windows

package module

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/bluecadet/preflight/internal/target"
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

func runWindowsPowerShellWithOutput(ctx context.Context, script string, onOutput target.OutputFunc) error {
	pr, pw := io.Pipe()
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	)
	cmd.Stdout = pw
	cmd.Stderr = pw

	var (
		lines   []string
		scanErr error
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			if onOutput != nil {
				onOutput(line)
			}
		}
		scanErr = scanner.Err()
	}()

	runErr := cmd.Run()
	closeErr := pw.Close()
	<-done

	if scanErr != nil {
		if runErr != nil {
			runErr = errors.Join(runErr, scanErr)
		} else {
			return fmt.Errorf("powershell.exe read output: %w", scanErr)
		}
	}

	if runErr != nil {
		output := strings.Join(lines, "\n")
		if output != "" {
			return fmt.Errorf("powershell.exe failed: %w\noutput: %s", runErr, output)
		}
		return fmt.Errorf("powershell.exe failed: %w", runErr)
	}
	if closeErr != nil {
		return fmt.Errorf("powershell.exe close output pipe: %w", closeErr)
	}
	return nil
}

func runWindowsPowerShellWithParams(ctx context.Context, params map[string]any, body string) ([]byte, error) {
	paramSetup, err := powershellJSONVar("params", params)
	if err != nil {
		return nil, err
	}
	return runWindowsPowerShell(ctx, paramSetup+"\n"+body)
}

func runWindowsPowerShellWithParamsWithOutput(ctx context.Context, params map[string]any, body string, onOutput target.OutputFunc) error {
	paramSetup, err := powershellJSONVar("params", params)
	if err != nil {
		return err
	}
	return runWindowsPowerShellWithOutput(ctx, paramSetup+"\n"+body, onOutput)
}

func runWindowsPowerShellBool(ctx context.Context, params map[string]any, body string) (bool, error) {
	out, err := runWindowsPowerShellWithParams(ctx, params, body)
	if err != nil {
		return false, err
	}
	return parseWindowsBool(out)
}

func runWindowsPowerShellBoolWithOutput(ctx context.Context, params map[string]any, body string, onOutput target.OutputFunc) (bool, error) {
	paramSetup, err := powershellJSONVar("params", params)
	if err != nil {
		return false, err
	}

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", paramSetup+"\n"+body,
	)
	cmd.Stdout = pw
	cmd.Stderr = pw

	var (
		lines   []string
		scanErr error
		pending string
		hasLine bool
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			if hasLine && onOutput != nil {
				onOutput(pending)
			}
			pending = line
			hasLine = true
		}
		scanErr = scanner.Err()
	}()

	runErr := cmd.Run()
	closeErr := pw.Close()
	<-done

	if scanErr != nil {
		if runErr != nil {
			runErr = errors.Join(runErr, scanErr)
		} else {
			return false, fmt.Errorf("powershell.exe read output: %w", scanErr)
		}
	}

	if runErr != nil {
		output := strings.Join(lines, "\n")
		if output != "" {
			return false, fmt.Errorf("powershell.exe failed: %w\noutput: %s", runErr, output)
		}
		return false, fmt.Errorf("powershell.exe failed: %w", runErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("powershell.exe close output pipe: %w", closeErr)
	}
	if !hasLine {
		return false, fmt.Errorf("unexpected boolean output %q", "")
	}
	return parseWindowsBool([]byte(pending))
}

func powershellJSONVar(name string, value any) (string, error) {
	return winutil.JSONVarScript(name, value)
}

func parseWindowsBool(out []byte) (bool, error) {
	value, err := winutil.ParseBool(out)
	if err != nil {
		return false, fmt.Errorf("unexpected boolean output %q", strings.TrimSpace(string(out)))
	}
	return value, nil
}

func firewallPortsArg(params map[string]any) (string, error) {
	ports, err := winutil.NormalizeFirewallPorts(params["ports"])
	if err != nil {
		return "", fmt.Errorf("firewall_rule: %w", err)
	}
	return ports, nil
}
