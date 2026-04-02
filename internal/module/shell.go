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
)

// ShellModule runs an arbitrary shell command.
// Params:
//   - cmd (required): command to execute
//   - args: list of arguments
//   - creates: path; if it exists, skip execution
//   - working_dir: working directory for the command
type ShellModule struct{}

func (m *ShellModule) Check(_ context.Context, params map[string]any) (bool, error) {
	creates, err := paramString(params, "creates", "")
	if err != nil {
		return false, err
	}
	if creates != "" {
		_, statErr := os.Stat(creates)
		if statErr == nil {
			return false, nil // path exists, no change needed
		}
		if !os.IsNotExist(statErr) {
			return false, fmt.Errorf("shell: stat creates path %q: %w", creates, statErr)
		}
	}
	// Shell commands are not idempotent by default — always run.
	return true, nil
}

func (m *ShellModule) ApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	cmdName, err := paramStringRequired(params, "cmd")
	if err != nil {
		return err
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return err
	}
	workingDir, err := paramString(params, "working_dir", "")
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(ctx, cmdName, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
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
			return fmt.Errorf("shell: read output from %q: %w", cmdName, scanErr)
		}
	}

	if runErr != nil {
		out := strings.Join(lines, "\n")
		if out != "" {
			return fmt.Errorf("shell: command %q failed: %w\noutput: %s", cmdName, runErr, out)
		}
		return fmt.Errorf("shell: command %q failed: %w", cmdName, runErr)
	}
	if closeErr != nil {
		return fmt.Errorf("shell: close output pipe for %q: %w", cmdName, closeErr)
	}
	return nil
}

func (m *ShellModule) Apply(ctx context.Context, params map[string]any) error {
	return m.ApplyWithOutput(ctx, params, nil)
}
