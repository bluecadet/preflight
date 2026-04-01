package module

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/bluecadet/preflight/internal/winutil"
)

var powershellCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, stderr, err := runCommandStreaming(ctx, cmd)
	if err != nil {
		return []byte(joinCommandOutput(stdout, stderr)), err
	}
	return []byte(joinCommandOutput(stdout, stderr)), nil
}

func powershellCheck(ctx context.Context, params map[string]any) (bool, error) {
	checkScript, err := paramString(params, "check_script", "")
	if err != nil {
		return false, err
	}
	if checkScript != "" {
		script, err := winutil.BuildPowerShellCheckScript(checkScript)
		if err != nil {
			return false, err
		}
		out, err := runPowerShellInline(ctx, script)
		if err != nil {
			return false, err
		}
		result, err := winutil.ParsePowerShellCheckResult(out)
		if err != nil {
			return false, err
		}
		return result.NeedsChange, nil
	}

	creates, err := paramString(params, "creates", "")
	if err != nil {
		return false, err
	}
	if creates != "" {
		_, statErr := os.Stat(creates)
		if statErr == nil {
			return false, nil
		}
		if !os.IsNotExist(statErr) {
			return false, fmt.Errorf("powershell: stat creates path %q: %w", creates, statErr)
		}
	}
	return true, nil
}

func powershellApply(ctx context.Context, params map[string]any) error {
	script, err := paramString(params, "script", "")
	if err != nil {
		return err
	}
	file, err := paramString(params, "file", "")
	if err != nil {
		return err
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return err
	}

	if script == "" && file == "" {
		return fmt.Errorf("powershell: one of 'script' or 'file' is required")
	}

	var out []byte
	if script != "" {
		out, err = runPowerShellInline(ctx, script)
	} else {
		out, err = runPowerShellFile(ctx, file, args)
	}
	if err != nil {
		return err
	}
	_ = out
	return nil
}

func runPowerShellInline(ctx context.Context, script string) ([]byte, error) {
	return runPowerShellCommand(ctx, append(platformPowerShellArgs(), "-Command", script)...)
}

func runPowerShellFile(ctx context.Context, file string, args []string) ([]byte, error) {
	commandArgs := append(platformPowerShellArgs(), "-File", file)
	commandArgs = append(commandArgs, args...)
	return runPowerShellCommand(ctx, commandArgs...)
}

func runPowerShellCommand(ctx context.Context, args ...string) ([]byte, error) {
	out, err := powershellCombinedOutput(ctx, platformPowerShellBinary(), args...)
	if err != nil {
		return nil, fmt.Errorf("powershell: command failed: %w\noutput: %s", err, string(out))
	}
	return out, nil
}
