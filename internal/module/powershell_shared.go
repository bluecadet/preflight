package module

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

var powershellCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type PowershellCheckParams struct {
	CheckScript string `param:"check_script"`
	Creates     string `param:"creates"`
}

type PowershellApplyParams struct {
	Script string   `param:"script"`
	File   string   `param:"file"`
	Args   []string `param:"args"`
}

func powershellCheck(ctx context.Context, params map[string]any) (bool, error) {
	return powershellCheckWithOutput(ctx, params, nil)
}

func powershellCheckWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) (bool, error) {
	var p PowershellCheckParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}
	if p.CheckScript != "" {
		script, err := winutil.BuildPowerShellCheckScript(p.CheckScript)
		if err != nil {
			return false, err
		}
		var out []byte
		if onOutput == nil {
			out, err = runPowerShellInline(ctx, script)
		} else {
			var lines []string
			lines, err = runPowerShellInlineWithOutput(ctx, script, func(line string) {
				if !winutil.IsPowerShellCheckResultLine(line) {
					onOutput(line)
				}
			})
			if err == nil {
				out = []byte(strings.Join(lines, "\n"))
			}
		}
		if err != nil {
			return false, err
		}
		result, _, err := winutil.ParsePowerShellCheckOutput(out)
		if err != nil {
			return false, err
		}
		return result.NeedsChange, nil
	}

	if p.Creates != "" {
		_, statErr := os.Stat(p.Creates)
		if statErr == nil {
			return false, nil
		}
		if !os.IsNotExist(statErr) {
			return false, fmt.Errorf("powershell: stat creates path %q: %w", p.Creates, statErr)
		}
	}
	return true, nil
}

func powershellApply(ctx context.Context, params map[string]any) error {
	var p PowershellApplyParams
	if err := Decode(params, &p); err != nil {
		return err
	}

	if p.Script == "" && p.File == "" {
		return fmt.Errorf("powershell: one of 'script' or 'file' is required")
	}

	var err error
	if p.Script != "" {
		_, err = runPowerShellInline(ctx, p.Script)
	} else {
		_, err = runPowerShellFile(ctx, p.File, p.Args)
	}
	return err
}

func powershellApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	if onOutput == nil {
		return powershellApply(ctx, params)
	}

	var p PowershellApplyParams
	if err := Decode(params, &p); err != nil {
		return err
	}

	if p.Script == "" && p.File == "" {
		return fmt.Errorf("powershell: one of 'script' or 'file' is required")
	}

	if p.Script != "" {
		_, err := runPowerShellInlineWithOutput(ctx, p.Script, onOutput)
		return err
	}
	return runPowerShellFileWithOutput(ctx, p.File, p.Args, onOutput)
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

func runPowerShellInlineWithOutput(ctx context.Context, script string, onOutput target.OutputFunc) ([]string, error) {
	return runPowerShellCommandWithOutput(ctx, onOutput, append(platformPowerShellArgs(), "-Command", script)...)
}

func runPowerShellFileWithOutput(ctx context.Context, file string, args []string, onOutput target.OutputFunc) error {
	commandArgs := append(platformPowerShellArgs(), "-File", file)
	commandArgs = append(commandArgs, args...)
	_, err := runPowerShellCommandWithOutput(ctx, onOutput, commandArgs...)
	return err
}

func runPowerShellCommandWithOutput(ctx context.Context, onOutput target.OutputFunc, args ...string) ([]string, error) {
	pw, done := NewOutputPipe(onOutput)
	cmd := exec.CommandContext(ctx, platformPowerShellBinary(), args...)
	cmd.Stdout = pw
	cmd.Stderr = pw

	runErr := cmd.Run()
	closeErr := pw.Close()
	result := <-done

	if result.ScanErr != nil {
		if runErr != nil {
			runErr = errors.Join(runErr, result.ScanErr)
		} else {
			return nil, fmt.Errorf("powershell: read command output: %w", result.ScanErr)
		}
	}

	if runErr != nil {
		out := strings.Join(result.Lines, "\n")
		if out != "" {
			return result.Lines, fmt.Errorf("powershell: command failed: %w\noutput: %s", runErr, out)
		}
		return result.Lines, fmt.Errorf("powershell: command failed: %w", runErr)
	}
	if closeErr != nil {
		return result.Lines, fmt.Errorf("powershell: close command output pipe: %w", closeErr)
	}
	return result.Lines, nil
}
