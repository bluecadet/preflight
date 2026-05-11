package module

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bluecadet/preflight/internal/target"
)

type ShellCheckParams struct {
	Creates string `param:"creates"`
}

type ShellApplyParams struct {
	Cmd        string            `param:"cmd,required"`
	Args       []string          `param:"args"`
	WorkingDir string            `param:"working_dir"`
	Env        map[string]string `param:"env"`
}

type ShellModule struct{}

func (m *ShellModule) Check(_ context.Context, params map[string]any) (bool, error) {
	var p ShellCheckParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}
	if p.Creates != "" {
		_, statErr := os.Stat(p.Creates)
		if statErr == nil {
			return false, nil
		}
		if !os.IsNotExist(statErr) {
			return false, fmt.Errorf("shell: stat creates path %q: %w", p.Creates, statErr)
		}
	}
	return true, nil
}

func (m *ShellModule) ApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	var p ShellApplyParams
	if err := Decode(params, &p); err != nil {
		return err
	}

	pw, done := NewOutputPipe(onOutput)
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
			return fmt.Errorf("shell: read output from %q: %w", p.Cmd, result.ScanErr)
		}
	}

	if runErr != nil {
		out := strings.Join(result.Lines, "\n")
		if out != "" {
			return fmt.Errorf("shell: command %q failed: %w\noutput: %s", p.Cmd, runErr, out)
		}
		return fmt.Errorf("shell: command %q failed: %w", p.Cmd, runErr)
	}
	if closeErr != nil {
		return fmt.Errorf("shell: close output pipe for %q: %w", p.Cmd, closeErr)
	}
	return nil
}

func (m *ShellModule) Apply(ctx context.Context, params map[string]any) error {
	return m.ApplyWithOutput(ctx, params, nil)
}
