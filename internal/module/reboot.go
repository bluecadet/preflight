package module

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
)

// RebootModule triggers or checks for a pending system reboot.
// Params:
//   - condition: "always" (default) or "if_needed"
//   - timeout: seconds before reboot (default 60, Windows only)
type RebootModule struct{}

func (m *RebootModule) Check(_ context.Context, params map[string]any) (bool, error) {
	condition, err := paramString(params, "condition", "always")
	if err != nil {
		return false, err
	}
	if condition == "always" {
		return true, nil
	}
	// For "if_needed" on non-Windows, we have no way to detect pending reboot.
	return false, nil
}

func (m *RebootModule) Apply(_ context.Context, params map[string]any) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("reboot: only supported on Windows")
	}
	timeout, err := paramInt(params, "timeout", 60)
	if err != nil {
		return err
	}
	cmd := exec.Command("shutdown", "/r", "/t", strconv.Itoa(timeout))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reboot: shutdown failed: %w\noutput: %s", err, string(out))
	}
	return nil
}
