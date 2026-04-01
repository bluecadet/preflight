//go:build windows

package module

func platformPowerShellBinary() string {
	return "powershell.exe"
}

func platformPowerShellArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass"}
}
