package module

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bluecadet/preflight/internal/target"
)

type ShellCheckParams struct {
	Creates    string `param:"creates"`
	WorkingDir string `param:"working_dir"`
}

type ShellApplyParams struct {
	Cmd        string            `param:"cmd,required"`
	Args       []string          `param:"args"`
	WorkingDir string            `param:"working_dir"`
	Env        map[string]string `param:"env"`
}

type ShellModule struct{}

func (m *ShellModule) Check(_ context.Context, params map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	var p ShellCheckParams
	if err := Decode(params, &p); err != nil {
		return target.CheckResult{}, err
	}
	if p.Creates != "" {
		_, statErr := os.Stat(pathInWorkingDir(p.Creates, p.WorkingDir))
		if statErr == nil {
			return target.CheckResult{NeedsChange: false}, nil
		}
		if !os.IsNotExist(statErr) {
			return target.CheckResult{}, fmt.Errorf("shell: stat creates path %q: %w", p.Creates, statErr)
		}
	}
	return target.CheckResult{NeedsChange: true}, nil
}

func pathInWorkingDir(path, workingDir string) string {
	if path == "" || workingDir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workingDir, path)
}

func (m *ShellModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	var p ShellApplyParams
	if err := Decode(params, &p); err != nil {
		return target.ApplyResult{}, err
	}

	pw, done := NewOutputPipe(out)
	cmd := exec.CommandContext(ctx, p.Cmd, p.Args...)
	if p.WorkingDir != "" {
		cmd.Dir = p.WorkingDir
	}
	cmd.Env = mergeEnv(p.Env)
	cmd.Stdout = pw
	cmd.Stderr = pw

	runErr := cmd.Run()
	closeErr := pw.Close()
	result := <-done

	if result.ScanErr != nil {
		if runErr != nil {
			runErr = errors.Join(runErr, result.ScanErr)
		} else {
			return target.ApplyResult{}, fmt.Errorf("shell: read output from %q: %w", p.Cmd, result.ScanErr)
		}
	}

	if runErr != nil {
		outStr := strings.Join(result.Lines, "\n")
		if outStr != "" {
			return target.ApplyResult{}, fmt.Errorf("shell: command %q failed: %w\noutput: %s", p.Cmd, runErr, outStr)
		}
		return target.ApplyResult{}, fmt.Errorf("shell: command %q failed: %w", p.Cmd, runErr)
	}
	if closeErr != nil {
		return target.ApplyResult{}, fmt.Errorf("shell: close output pipe for %q: %w", p.Cmd, closeErr)
	}
	return target.ApplyResult{}, nil
}
