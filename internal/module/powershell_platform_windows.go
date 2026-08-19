//go:build windows

package module

func platformPowerShellBinary() (string, error) {
	return "powershell.exe", nil
}

func platformPowerShellArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass"}
}
