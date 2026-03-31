//go:build windows

package module

import (
	"fmt"
	"os/exec"
	"strings"
)

// checkServiceRunning queries whether a Windows service is running.
func checkServiceRunning(name string) (bool, error) {
	out, err := exec.Command("sc", "query", name).CombinedOutput()
	if err != nil {
		// sc returns non-zero when service doesn't exist
		return false, nil //nolint:nilerr
	}
	return strings.Contains(string(out), "RUNNING"), nil
}

// rebootPending checks the Windows PendingFileRenameOperations registry key.
func rebootPending() (bool, error) {
	out, err := exec.Command("reg", "query",
		`HKLM\SYSTEM\CurrentControlSet\Control\Session Manager`,
		"/v", "PendingFileRenameOperations").CombinedOutput()
	if err != nil {
		// Key absent → no reboot pending
		return false, nil //nolint:nilerr
	}
	_ = out
	return true, nil
}

// applyReboot executes a Windows shutdown /r command.
func applyReboot(timeoutSecs int) error {
	out, err := exec.Command("shutdown", "/r", "/t", fmt.Sprintf("%d", timeoutSecs)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reboot: shutdown failed: %w\noutput: %s", err, string(out))
	}
	return nil
}
