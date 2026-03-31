package module

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

func (m *ShellModule) Apply(_ context.Context, params map[string]any) error {
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

	cmd := exec.Command(cmdName, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("shell: command %q failed: %w\noutput: %s", cmdName, err, string(out))
	}
	return nil
}
