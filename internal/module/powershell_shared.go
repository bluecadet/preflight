package module

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

var powershellCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
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

	if script != "" {
		_, err = runPowerShellInline(ctx, script)
	} else {
		_, err = runPowerShellFile(ctx, file, args)
	}
	return err
}

func powershellApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	if onOutput == nil {
		return powershellApply(ctx, params)
	}

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

	if script != "" {
		return runPowerShellInlineWithOutput(ctx, script, onOutput)
	}
	return runPowerShellFileWithOutput(ctx, file, args, onOutput)
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
		return out, fmt.Errorf("powershell: command failed: %w\noutput: %s", err, string(out))
	}
	return out, nil
}

func runPowerShellInlineWithOutput(ctx context.Context, script string, onOutput target.OutputFunc) error {
	return runPowerShellCommandWithOutput(ctx, onOutput, append(platformPowerShellArgs(), "-Command", script)...)
}

func runPowerShellFileWithOutput(ctx context.Context, file string, args []string, onOutput target.OutputFunc) error {
	commandArgs := append(platformPowerShellArgs(), "-File", file)
	commandArgs = append(commandArgs, args...)
	return runPowerShellCommandWithOutput(ctx, onOutput, commandArgs...)
}

func runPowerShellCommandWithOutput(ctx context.Context, onOutput target.OutputFunc, args ...string) error {
	pr, pw := io.Pipe()
	cmd := exec.CommandContext(ctx, platformPowerShellBinary(), args...)
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
			return fmt.Errorf("powershell: read command output: %w", scanErr)
		}
	}

	if runErr != nil {
		out := strings.Join(lines, "\n")
		if out != "" {
			return fmt.Errorf("powershell: command failed: %w\noutput: %s", runErr, out)
		}
		return fmt.Errorf("powershell: command failed: %w", runErr)
	}
	if closeErr != nil {
		return fmt.Errorf("powershell: close command output pipe: %w", closeErr)
	}
	return nil
}
