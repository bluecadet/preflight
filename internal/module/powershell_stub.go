//go:build !windows

package module

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// PowershellModule runs PowerShell scripts or files.
// On non-Windows platforms, attempts to exec `powershell` (WSL/cross-platform compat).
// Params:
//   - script: inline PowerShell script string
//   - file: path to a .ps1 file
//   - args: list of additional arguments (used with file)
//   - creates: path that indicates the operation is already done
type PowershellModule struct{}

func (m *PowershellModule) Check(_ context.Context, params map[string]interface{}) (bool, error) {
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

func (m *PowershellModule) Apply(_ context.Context, params map[string]interface{}) error {
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

	var cmd *exec.Cmd
	if script != "" {
		cmd = exec.Command("powershell", "-NonInteractive", "-Command", script)
	} else {
		cmdArgs := []string{"-NonInteractive", "-File", file}
		cmdArgs = append(cmdArgs, args...)
		cmd = exec.Command("powershell", cmdArgs...)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell: command failed: %w\noutput: %s", err, string(out))
	}
	return nil
}
