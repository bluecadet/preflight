//go:build !windows

package module

import "fmt"

// checkServiceRunning is a stub on non-Windows platforms.
func checkServiceRunning(name string) (bool, error) {
	return false, fmt.Errorf("wait: service_running condition is only supported on Windows (service: %q)", name)
}

// rebootPending is a stub on non-Windows platforms.
func rebootPending() (bool, error) {
	return false, fmt.Errorf("reboot: pending check is only supported on Windows")
}

// applyReboot is a stub on non-Windows platforms.
func applyReboot(_ int) error {
	return fmt.Errorf("reboot: only supported on Windows")
}
