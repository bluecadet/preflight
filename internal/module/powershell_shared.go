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

var powershellCombinedOutputWithEnv = func(ctx context.Context, name string, args []string, env map[string]string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = mergeEnv(env)
	return cmd.CombinedOutput()
}

type PowershellCheckParams struct {
	CheckScript string            `param:"check_script"`
	Creates     string            `param:"creates"`
	Env         map[string]string `param:"env"`
}

type PowershellApplyParams struct {
	Script string            `param:"script"`
	File   string            `param:"file"`
	Args   []string          `param:"args"`
	Env    map[string]string `param:"env"`
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
			out, err = runPowerShellInline(ctx, script, p.Env)
		} else {
			var lines []string
			lines, err = runPowerShellInlineWithOutput(ctx, script, p.Env, func(line string) {
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
		_, err = runPowerShellApplyInline(ctx, p.Script, p.Env)
	} else {
		_, err = runPowerShellFile(ctx, p.File, p.Args, p.Env)
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
		_, err := runPowerShellApplyInlineWithOutput(ctx, p.Script, p.Env, onOutput)
		return err
	}
	return runPowerShellFileWithOutput(ctx, p.File, p.Args, p.Env, onOutput)
}

func runPowerShellInline(ctx context.Context, script string, env map[string]string) ([]byte, error) {
	path, cleanup, err := writePowerShellInlineScript(script, false)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return runPowerShellFile(ctx, path, nil, env)
}

func runPowerShellApplyInline(ctx context.Context, script string, env map[string]string) ([]byte, error) {
	path, cleanup, err := writePowerShellInlineScript(script, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return runPowerShellFile(ctx, path, nil, env)
}

func runPowerShellFile(ctx context.Context, file string, args []string, env map[string]string) ([]byte, error) {
	commandArgs := append(platformPowerShellArgs(), "-File", file)
	commandArgs = append(commandArgs, args...)
	return runPowerShellCommand(ctx, env, commandArgs...)
}

func runPowerShellCommand(ctx context.Context, env map[string]string, args ...string) ([]byte, error) {
	out, err := powershellCommandOutput(ctx, platformPowerShellBinary(), args, env)
	if err != nil {
		return out, fmt.Errorf("powershell: command failed: %w\noutput: %s", err, string(out))
	}
	return out, nil
}

func runPowerShellApplyInlineWithOutput(ctx context.Context, script string, env map[string]string, onOutput target.OutputFunc) ([]string, error) {
	path, cleanup, err := writePowerShellInlineScript(script, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	commandArgs := append(platformPowerShellArgs(), "-File", path)
	return runPowerShellCommandWithOutput(ctx, onOutput, env, commandArgs...)
}

func runPowerShellInlineWithOutput(ctx context.Context, script string, env map[string]string, onOutput target.OutputFunc) ([]string, error) {
	path, cleanup, err := writePowerShellInlineScript(script, false)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	commandArgs := append(platformPowerShellArgs(), "-File", path)
	return runPowerShellCommandWithOutput(ctx, onOutput, env, commandArgs...)
}

func runPowerShellFileWithOutput(ctx context.Context, file string, args []string, env map[string]string, onOutput target.OutputFunc) error {
	commandArgs := append(platformPowerShellArgs(), "-File", file)
	commandArgs = append(commandArgs, args...)
	_, err := runPowerShellCommandWithOutput(ctx, onOutput, env, commandArgs...)
	return err
}

func runPowerShellCommandWithOutput(ctx context.Context, onOutput target.OutputFunc, env map[string]string, args ...string) ([]string, error) {
	pw, done := NewOutputPipe(onOutput)
	cmd := exec.CommandContext(ctx, platformPowerShellBinary(), args...)
	cmd.Env = mergeEnv(env)
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

func powershellCommandOutput(ctx context.Context, name string, args []string, env map[string]string) ([]byte, error) {
	if len(env) == 0 {
		return powershellCombinedOutput(ctx, name, args...)
	}
	return powershellCombinedOutputWithEnv(ctx, name, args, env)
}

func writePowerShellInlineScript(script string, exitFromNativeCode bool) (string, func(), error) {
	wrapped, err := wrapPowerShellInlineScript(script, exitFromNativeCode)
	if err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp("", "preflight-powershell-*.ps1")
	if err != nil {
		return "", nil, fmt.Errorf("powershell: create temp script: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}
	if _, err := file.Write(append([]byte{0xEF, 0xBB, 0xBF}, []byte(wrapped)...)); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("powershell: write temp script: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("powershell: close temp script: %w", err)
	}
	return file.Name(), cleanup, nil
}

func wrapPowerShellInlineScript(script string, exitFromNativeCode bool) (string, error) {
	scriptVar, err := winutil.JSONVarScript("__pf_script", script)
	if err != nil {
		return "", err
	}
	wrapped := scriptVar + `
$__pf_block = [ScriptBlock]::Create($__pf_script)
$global:LASTEXITCODE = 0
& $__pf_block
`
	if !exitFromNativeCode {
		return wrapped, nil
	}
	return wrapped + `
if ($global:LASTEXITCODE -is [int]) {
  exit $global:LASTEXITCODE
}
if ($?) {
  exit 0
}
exit 1
`, nil
}
